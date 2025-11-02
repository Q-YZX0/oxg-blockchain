package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Q-YZX0/oxy-blockchain/internal/consensus"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// QueryHandler maneja queries P2P por mesh network
type QueryHandler struct {
	ctx           context.Context
	storage       *storage.BlockchainDB
	consensus     *consensus.CometBFT
	meshBridge    *MeshBridge
	pendingQueries map[string]chan QueryResponse
	mu            sync.RWMutex
}

// QueryRequest representa una solicitud de query
type QueryRequest struct {
	Type      string `json:"type"`      // "query"
	Path      string `json:"path"`      // "block/123", "account/0x...", etc.
	RequestID string `json:"request_id"` // UUID para identificar la respuesta
	From      string `json:"from,omitempty"` // Dirección del solicitante
}

// QueryResponse representa una respuesta a una query
type QueryResponse struct {
	Type      string          `json:"type"`       // "response"
	RequestID string          `json:"request_id"` // UUID de la request original
	Path      string          `json:"path"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     string          `json:"error,omitempty"`
	From      string          `json:"from"`       // Dirección del nodo que responde
}

const (
	QueryTopic = "oxy-blockchain:query"
	ResponseTopic = "oxy-blockchain:response"
)

// NewQueryHandler crea un nuevo manejador de queries
func NewQueryHandler(
	ctx context.Context,
	storage *storage.BlockchainDB,
	consensus *consensus.CometBFT,
	meshBridge *MeshBridge,
) *QueryHandler {
	return &QueryHandler{
		ctx:            ctx,
		storage:        storage,
		consensus:      consensus,
		meshBridge:     meshBridge,
		pendingQueries: make(map[string]chan QueryResponse),
	}
}

// Start inicia el handler de queries
func (qh *QueryHandler) Start() error {
	// El query handler se integra con mesh_bridge cuando se crea
	// mesh_bridge ya se suscribió a los topics necesarios
	// Aquí solo inicializamos el handler
	
	log.Println("Query handler iniciado e integrado con mesh bridge")
	return nil
}

// Query realiza una query a otros nodos por mesh network
func (qh *QueryHandler) Query(path string, timeout time.Duration) (*QueryResponse, error) {
	// Generar request ID único
	requestID := generateRequestID()
	
	// Crear canal para recibir respuesta
	responseChan := make(chan QueryResponse, 1)
	
	// Registrar query pendiente
	qh.mu.Lock()
	qh.pendingQueries[requestID] = responseChan
	qh.mu.Unlock()
	
	// Limpiar después de timeout
	defer func() {
		qh.mu.Lock()
		delete(qh.pendingQueries, requestID)
		qh.mu.Unlock()
	}()
	
	// Crear request
	request := QueryRequest{
		Type:      "query",
		Path:      path,
		RequestID: requestID,
	}
	
	// Enviar query por mesh
	if err := qh.sendQuery(request); err != nil {
		return nil, fmt.Errorf("error enviando query: %w", err)
	}
	
	// Esperar respuesta con timeout
	select {
	case response := <-responseChan:
		return &response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout esperando respuesta para query: %s", path)
	case <-qh.ctx.Done():
		return nil, fmt.Errorf("contexto cancelado")
	}
}

// HandleQuery maneja una query recibida de otro nodo
func (qh *QueryHandler) HandleQuery(request QueryRequest) error {
	// Procesar query localmente
	var response QueryResponse
	
	switch {
	case request.Path == "height" || request.Path == "status":
		height, err := qh.storage.GetLatestHeight()
		if err != nil {
			response = QueryResponse{
				Type:      "response",
				RequestID: request.RequestID,
				Path:      request.Path,
				Error:     fmt.Sprintf("error obteniendo altura: %v", err),
			}
		} else {
			data, _ := json.Marshal(map[string]interface{}{
				"height": height,
			})
			response = QueryResponse{
				Type:      "response",
				RequestID: request.RequestID,
				Path:      request.Path,
				Data:      data,
			}
		}
	
	case len(request.Path) > 6 && request.Path[:6] == "block/":
		// Extraer altura
		var height uint64
		fmt.Sscanf(request.Path[6:], "%d", &height)
		
		blockData, err := qh.storage.GetBlock(height)
		if err != nil {
			response = QueryResponse{
				Type:      "response",
				RequestID: request.RequestID,
				Path:      request.Path,
				Error:     fmt.Sprintf("bloque no encontrado: altura %d", height),
			}
		} else {
			response = QueryResponse{
				Type:      "response",
				RequestID: request.RequestID,
				Path:      request.Path,
				Data:      blockData,
			}
		}
	
	case len(request.Path) > 3 && request.Path[:3] == "tx/":
		// Extraer hash de transacción
		txHash := request.Path[3:]
		
		txData, err := qh.storage.GetTransaction(txHash)
		if err != nil {
			response = QueryResponse{
				Type:      "response",
				RequestID: request.RequestID,
				Path:      request.Path,
				Error:     fmt.Sprintf("transacción no encontrada: %s", txHash),
			}
		} else {
			response = QueryResponse{
				Type:      "response",
				RequestID: request.RequestID,
				Path:      request.Path,
				Data:      txData,
			}
		}
	
	case len(request.Path) > 7 && request.Path[:7] == "account/":
		// Extraer dirección
		address := request.Path[7:]
		
		// Obtener estado de cuenta desde el executor EVM
		// Necesitamos acceso al executor desde consensus
		accountState, err := qh.getAccountState(address)
		if err != nil {
			response = QueryResponse{
				Type:      "response",
				RequestID: request.RequestID,
				Path:      request.Path,
				Error:     fmt.Sprintf("error obteniendo estado de cuenta: %v", err),
			}
		} else {
			data, _ := json.Marshal(accountState)
			response = QueryResponse{
				Type:      "response",
				RequestID: request.RequestID,
				Path:      request.Path,
				Data:      data,
			}
		}
	
	default:
		response = QueryResponse{
			Type:      "response",
			RequestID: request.RequestID,
			Path:      request.Path,
			Error:     fmt.Sprintf("query path desconocido: %s", request.Path),
		}
	}
	
	// Enviar respuesta por mesh
	return qh.sendResponse(response)
}

// HandleResponse maneja una respuesta recibida de otro nodo
func (qh *QueryHandler) HandleResponse(response QueryResponse) {
	qh.mu.RLock()
	responseChan, exists := qh.pendingQueries[response.RequestID]
	qh.mu.RUnlock()
	
	if exists {
		// Enviar respuesta al canal correspondiente
		select {
		case responseChan <- response:
		default:
			// Canal cerrado o buffer lleno, ignorar
		}
	}
}

// sendQuery envía una query por mesh network
func (qh *QueryHandler) sendQuery(request QueryRequest) error {
	requestData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("error serializando query: %w", err)
	}
	
	// Usar mesh_bridge para enviar mensaje
	return qh.meshBridge.sendQueryMessage(requestData)
}

// sendResponse envía una respuesta por mesh network
func (qh *QueryHandler) sendResponse(response QueryResponse) error {
	responseData, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("error serializando respuesta: %w", err)
	}
	
	// Usar mesh_bridge para enviar mensaje
	return qh.meshBridge.sendResponseMessage(responseData)
}

var requestIDCounter int64
var requestIDMutex sync.Mutex

// generateRequestID genera un ID único para una query
func generateRequestID() string {
	requestIDMutex.Lock()
	defer requestIDMutex.Unlock()
	requestIDCounter++
	return fmt.Sprintf("query-%d-%d", time.Now().UnixNano(), requestIDCounter)
}

// getAccountState obtiene el estado de una cuenta (helper)
func (qh *QueryHandler) getAccountState(address string) (map[string]interface{}, error) {
	// Intentar obtener desde el executor EVM si está disponible
	executor := qh.consensus.GetExecutor()
	if executor != nil {
		accountState, err := executor.GetState(address)
		if err == nil && accountState != nil {
			return map[string]interface{}{
				"address":  accountState.Address,
				"balance":  accountState.Balance,
				"nonce":    accountState.Nonce,
				"codeHash": accountState.CodeHash,
				"storage":  accountState.Storage,
			}, nil
		}
	}
	
	// Fallback: intentar desde storage
	accountData, err := qh.storage.GetAccount(address)
	if err == nil && accountData != nil {
		var accountState map[string]interface{}
		if err := json.Unmarshal(accountData, &accountState); err == nil {
			return accountState, nil
		}
	}
	
	// Si no hay datos, retornar estructura básica
	return map[string]interface{}{
		"address": address,
		"balance": "0",
		"nonce":   0,
	}, nil
}

