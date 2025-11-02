package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/Q-YZX0/oxy-blockchain/internal/consensus"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// MeshBridge conecta CometBFT con oxygen-sdk mesh network
type MeshBridge struct {
	ctx           context.Context
	consensus     *consensus.CometBFT
	meshEndpoint  string
	conn          *websocket.Conn
	connMutex     sync.RWMutex
	running       bool
	stopChan      chan struct{}
	topics        map[string]bool // Topics suscritos
	topicsMutex   sync.RWMutex
	queryHandler  *QueryHandler // Handler de queries
}

// MeshMessage representa un mensaje del mesh network
type MeshMessage struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
}

// MeshTopics
const (
	TopicTransactions = "transactions"
	TopicBlocks       = "blocks"
	TopicValidators   = "validators"
)

// MessageTypes
const (
	MessageTypeSubscribe   = "subscribe"
	MessageTypeUnsubscribe = "unsubscribe"
	MessageTypePublish    = "publish"
	MessageTypePong       = "pong"
	MessageTypePing       = "ping"
)

// NewMeshBridge crea un nuevo puente con mesh network
func NewMeshBridge(
	ctx context.Context,
	consensus *consensus.CometBFT,
	meshEndpoint string,
	storage *storage.BlockchainDB,
) *MeshBridge {
	mb := &MeshBridge{
		ctx:          ctx,
		consensus:    consensus,
		meshEndpoint: meshEndpoint,
		running:      false,
		stopChan:     make(chan struct{}),
		topics:       make(map[string]bool),
	}

	// Crear query handler si tenemos storage
	if storage != nil {
		mb.queryHandler = NewQueryHandler(ctx, storage, consensus, mb)
	}

	return mb
}

// Start inicia el puente con mesh
func (mb *MeshBridge) Start() error {
	if mb.running {
		return fmt.Errorf("mesh bridge ya está corriendo")
	}

	// Conectar a oxygen-sdk mesh endpoint
	if err := mb.connect(); err != nil {
		return fmt.Errorf("error conectando a mesh: %w", err)
	}

	// Suscribirse a topics necesarios
	topics := []string{
		TopicTransactions,
		TopicBlocks,
		TopicValidators,
		"oxy-blockchain:query",
		"oxy-blockchain:response",
	}
	
	for _, topic := range topics {
		if err := mb.subscribe(topic); err != nil {
			log.Printf("Advertencia: error suscribiéndose a topic %s: %v", topic, err)
		}
	}

	// Iniciar goroutine para leer mensajes
	go mb.readMessages()

	// Iniciar heartbeat (ping/pong)
	go mb.heartbeat()

	log.Println("✅ Mesh bridge iniciado y conectado")
	mb.running = true
	return nil
}

// connect establece conexión WebSocket con mesh endpoint
func (mb *MeshBridge) connect() error {
	mb.connMutex.Lock()
	defer mb.connMutex.Unlock()

	// Parsear URL
	u, err := url.Parse(mb.meshEndpoint)
	if err != nil {
		return fmt.Errorf("error parseando mesh endpoint: %w", err)
	}

	// Asegurar que es WebSocket
	if u.Scheme != "ws" && u.Scheme != "wss" {
		u.Scheme = "ws"
	}

	// Conectar WebSocket con timeout
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("error estableciendo conexión WebSocket: %w", err)
	}

	mb.conn = conn
	log.Printf("Conectado a mesh endpoint: %s", u.String())
	
	return nil
}

// subscribe se suscribe a un topic
func (mb *MeshBridge) subscribe(topic string) error {
	mb.connMutex.RLock()
	conn := mb.conn
	mb.connMutex.RUnlock()

	if conn == nil {
		return fmt.Errorf("no hay conexión establecida")
	}

	// Enviar mensaje de suscripción
	msg := MeshMessage{
		Type:  MessageTypeSubscribe,
		Topic: topic,
	}

	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("error enviando suscripción: %w", err)
	}

	// Marcar topic como suscrito
	mb.topicsMutex.Lock()
	mb.topics[topic] = true
	mb.topicsMutex.Unlock()

	log.Printf("Suscrito a topic: %s", topic)
	return nil
}

// readMessages lee mensajes del WebSocket
func (mb *MeshBridge) readMessages() {
	defer func() {
		// Recover de panics de WebSocket para evitar que terminen el proceso
		if r := recover(); r != nil {
			log.Printf("⚠️ Panic en readMessages recuperado: %v", r)
			// Cerrar conexión y reconectar
			mb.closeConnection()
			time.Sleep(2 * time.Second)
			mb.reconnect()
		}
	}()

	for {
		select {
		case <-mb.stopChan:
			return
		case <-mb.ctx.Done():
			return
		default:
			mb.connMutex.RLock()
			conn := mb.conn
			mb.connMutex.RUnlock()

			if conn == nil {
				time.Sleep(2 * time.Second)
				// Intentar reconectar si no hay conexión
				if mb.running {
					mb.reconnect()
				}
				continue
			}

			// Leer mensaje con timeout más largo (120 segundos para evitar desconexiones frecuentes)
			// El servidor mesh puede tardar en responder, especialmente si no hay tráfico
			readDeadline := time.Now().Add(120 * time.Second)
			if err := conn.SetReadDeadline(readDeadline); err != nil {
				log.Printf("⚠️ Error configurando read deadline: %v", err)
				mb.closeConnection()
				time.Sleep(2 * time.Second)
				mb.reconnect()
				continue
			}
			
			var msg MeshMessage
			if err := conn.ReadJSON(&msg); err != nil {
				// Verificar si es un error de cierre esperado
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("⚠️ Error leyendo mensaje WebSocket: %v", err)
				} else if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("ℹ️ Conexión WebSocket cerrada normalmente")
				} else {
					log.Printf("⚠️ Error WebSocket: %v", err)
				}
				
				// Cerrar conexión actual y reconectar
				mb.closeConnection()
				time.Sleep(2 * time.Second)
				
				// Intentar reconectar solo si aún está corriendo
				if mb.running {
					mb.reconnect()
				}
				continue
			}

			// Procesar mensaje
			if err := mb.handleMessage(&msg); err != nil {
				log.Printf("⚠️ Error procesando mensaje: %v", err)
			}
		}
	}
}

// handleMessage procesa un mensaje recibido
func (mb *MeshBridge) handleMessage(msg *MeshMessage) error {
	switch msg.Type {
	case MessageTypePing:
		// Responder pong
		return mb.sendMessage(&MeshMessage{
			Type: MessageTypePong,
		})
	
	case MessageTypePublish:
		// Procesar mensaje publicado en un topic
		return mb.ReceiveMessage(msg.Topic, msg.Data)
	
	case "query":
		// Procesar query recibida (formato directo)
		if mb.queryHandler != nil {
			var queryReq QueryRequest
			if err := json.Unmarshal(msg.Data, &queryReq); err == nil {
				if err := mb.queryHandler.HandleQuery(queryReq); err != nil {
					log.Printf("Error manejando query: %v", err)
				}
			}
		}
		return nil
	
	case "response":
		// Procesar respuesta recibida (formato directo)
		if mb.queryHandler != nil {
			var queryResp QueryResponse
			if err := json.Unmarshal(msg.Data, &queryResp); err == nil {
				mb.queryHandler.HandleResponse(queryResp)
			}
		}
		return nil
	
	case "error":
		// Mensajes de error del servidor mesh (pueden ser errores de conexión, autenticación, etc.)
		// Ignorar silenciosamente - ya se manejan los errores en readMessages()
		return nil
	
	default:
		// Solo loguear tipos desconocidos que no sean "error" (para evitar spam)
		if msg.Type != "error" {
			log.Printf("Tipo de mensaje desconocido: %s", msg.Type)
		}
	}
	
	return nil
}

// closeConnection cierra la conexión WebSocket de forma segura
func (mb *MeshBridge) closeConnection() {
	mb.connMutex.Lock()
	defer mb.connMutex.Unlock()

	if mb.conn != nil {
		// Cerrar de forma no bloqueante para evitar deadlocks
		conn := mb.conn
		mb.conn = nil
		
		// Cerrar en goroutine separada para evitar bloquear
		go func() {
			defer func() {
				if r := recover(); r != nil {
					// Ignorar panics al cerrar (puede estar ya cerrado)
				}
			}()
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
			conn.Close()
		}()
	}
}

// reconnect intenta reconectar al mesh
func (mb *MeshBridge) reconnect() {
	log.Println("Intentando reconectar a mesh...")
	
	for i := 0; i < 5; i++ {
		if err := mb.connect(); err == nil {
			// Re-suscribirse a todos los topics
			mb.topicsMutex.RLock()
			topics := make([]string, 0, len(mb.topics))
			for topic := range mb.topics {
				topics = append(topics, topic)
			}
			mb.topicsMutex.RUnlock()
			
			for _, topic := range topics {
				mb.subscribe(topic)
			}
			
			log.Println("✅ Reconectado a mesh exitosamente")
			return
		}
		
		time.Sleep(time.Duration(i+1) * 2 * time.Second)
	}
	
	log.Printf("⚠️ No se pudo reconectar a mesh después de 5 intentos")
}

// heartbeat envía ping periódico para mantener la conexión viva
func (mb *MeshBridge) heartbeat() {
	// Ping cada 45 segundos (menos frecuente que el timeout de 120s para evitar timeout)
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mb.stopChan:
			return
		case <-mb.ctx.Done():
			return
		case <-ticker.C:
			if err := mb.sendMessage(&MeshMessage{
				Type: MessageTypePing,
			}); err != nil {
				log.Printf("Error enviando ping: %v", err)
			}
		}
	}
}

// sendMessage envía un mensaje
func (mb *MeshBridge) sendMessage(msg *MeshMessage) error {
	mb.connMutex.RLock()
	conn := mb.conn
	mb.connMutex.RUnlock()

	if conn == nil {
		return fmt.Errorf("no hay conexión establecida")
	}

	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(msg)
}

// Stop detiene el puente
func (mb *MeshBridge) Stop() error {
	if !mb.running {
		return nil
	}

	// Cerrar stop channel
	close(mb.stopChan)

	// Desconectar WebSocket
	mb.connMutex.Lock()
	if mb.conn != nil {
		mb.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		mb.conn.Close()
		mb.conn = nil
	}
	mb.connMutex.Unlock()

	mb.running = false
	log.Println("⏹️ Mesh bridge detenido")
	return nil
}

// BroadcastTransaction transmite una transacción por la mesh
func (mb *MeshBridge) BroadcastTransaction(tx *consensus.Transaction) error {
	if !mb.running {
		return fmt.Errorf("mesh bridge no está corriendo")
	}

	// Codificar transacción
	txData, err := json.Marshal(tx)
	if err != nil {
		return fmt.Errorf("error codificando transacción: %w", err)
	}

	// Publicar en topic de transacciones
	msg := &MeshMessage{
		Type:  MessageTypePublish,
		Topic: TopicTransactions,
		Data:  txData,
	}

	if err := mb.sendMessage(msg); err != nil {
		return fmt.Errorf("error transmitiendo transacción: %w", err)
	}

	log.Printf("📤 Transacción transmitida por mesh: %s", tx.Hash)
	return nil
}

// BroadcastBlock transmite un bloque por la mesh
func (mb *MeshBridge) BroadcastBlock(block *consensus.Block) error {
	if !mb.running {
		return fmt.Errorf("mesh bridge no está corriendo")
	}

	// Codificar bloque
	blockData, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("error codificando bloque: %w", err)
	}

	// Publicar en topic de bloques
	msg := &MeshMessage{
		Type:  MessageTypePublish,
		Topic: TopicBlocks,
		Data:  blockData,
	}

	if err := mb.sendMessage(msg); err != nil {
		return fmt.Errorf("error transmitiendo bloque: %w", err)
	}

	log.Printf("📤 Bloque transmitido por mesh: altura %d", block.Header.Height)
	return nil
}

// ReceiveMessage maneja mensajes recibidos de la mesh
func (mb *MeshBridge) ReceiveMessage(topic string, data []byte) error {
	switch topic {
	case TopicTransactions:
		var tx consensus.Transaction
		if err := json.Unmarshal(data, &tx); err != nil {
			return fmt.Errorf("error decodificando transacción: %w", err)
		}
		
		// Enviar transacción a CometBFT para validación
		if err := mb.consensus.SubmitTransaction(&tx); err != nil {
			log.Printf("Error enviando transacción a consenso: %v", err)
			return err
		}
		
		log.Printf("📥 Transacción recibida de mesh: %s", tx.Hash)
		
	case TopicBlocks:
		var block consensus.Block
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("error decodificando bloque: %w", err)
		}
		
		// Procesar bloque recibido
		// Validar que el bloque sea más reciente que el actual
		currentBlock, err := mb.consensus.GetLatestBlock()
		if err == nil && currentBlock != nil {
			if block.Header.Height > currentBlock.Header.Height {
				log.Printf("📥 Bloque recibido de mesh: altura %d (actual: %d)", block.Header.Height, currentBlock.Header.Height)
				// Nota: CometBFT maneja la sincronización de bloques automáticamente
				// Este mensaje es solo para logging. CometBFT se encargará de aplicar el bloque
				// si es válido según su consenso
			} else {
				log.Printf("📥 Bloque recibido de mesh: altura %d (ignorado, altura actual: %d)", block.Header.Height, currentBlock.Header.Height)
			}
		} else {
			log.Printf("📥 Bloque recibido de mesh: altura %d", block.Header.Height)
		}
		
	case TopicValidators:
		// Procesar actualizaciones de validadores
		// Por ahora, solo loguear. La gestión de validadores se hace internamente
		log.Printf("📥 Actualización de validadores recibida de mesh")
		// Nota: Las actualizaciones de validadores se manejan a través del ValidatorSet
		// y se propagan automáticamente por CometBFT durante la rotación de validadores
	
	case "oxy-blockchain:query":
		// Procesar query recibida
		if mb.queryHandler != nil {
			var queryReq QueryRequest
			if err := json.Unmarshal(data, &queryReq); err == nil {
				if err := mb.queryHandler.HandleQuery(queryReq); err != nil {
					log.Printf("Error manejando query: %v", err)
				}
			}
		}
		return nil
	
	case "oxy-blockchain:response":
		// Procesar respuesta recibida
		if mb.queryHandler != nil {
			var queryResp QueryResponse
			if err := json.Unmarshal(data, &queryResp); err == nil {
				mb.queryHandler.HandleResponse(queryResp)
			}
		}
		return nil
		
	default:
		log.Printf("⚠️ Topic desconocido recibido: %s", topic)
	}
	
	return nil
}

// sendQueryMessage envía un mensaje de query por mesh
func (mb *MeshBridge) sendQueryMessage(data []byte) error {
	msg := &MeshMessage{
		Type:  MessageTypePublish,
		Topic: "oxy-blockchain:query",
		Data:  data,
	}
	return mb.sendMessage(msg)
}

// sendResponseMessage envía un mensaje de respuesta por mesh
func (mb *MeshBridge) sendResponseMessage(data []byte) error {
	msg := &MeshMessage{
		Type:  MessageTypePublish,
		Topic: "oxy-blockchain:response",
		Data:  data,
	}
	return mb.sendMessage(msg)
}

