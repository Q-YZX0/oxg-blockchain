package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
	execution "github.com/Q-YZX0/oxy-blockchain/internal/execution"
	"github.com/Q-YZX0/oxy-blockchain/internal/metrics"
)

// CometBFT es el wrapper para CometBFT que maneja el consenso
type CometBFT struct {
	ctx         context.Context
	config      *Config
	storage     *storage.BlockchainDB
	executor    *execution.EVMExecutor
	node        *CometBFTNode
	mempool     []*Transaction
	mempoolMutex sync.RWMutex
	rateLimiter *RateLimiter
	running     bool
}

// Config contiene la configuración del consenso
type Config struct {
	DataDir       string
	ChainID       string
	ValidatorAddr string
	ValidatorKey  string
}

// NewCometBFT crea una nueva instancia del motor de consenso
func NewCometBFT(
	ctx context.Context,
	config *Config,
	storage *storage.BlockchainDB,
	executor *execution.EVMExecutor,
	validators *ValidatorSet,
) (*CometBFT, error) {
	
	// Crear nodo CometBFT con validators
	cometNode, err := NewCometBFTNode(ctx, config, storage, executor, validators)
	if err != nil {
		return nil, fmt.Errorf("error creando nodo CometBFT: %w", err)
	}
	
	// Crear rate limiter: 10 transacciones por segundo por dirección, ventana de 1 segundo, límite de mempool 10000
	rateLimiter := NewRateLimiter(10, time.Second, 10000)
	rateLimiter.StartCleanup(30 * time.Second)

	c := &CometBFT{
		ctx:         ctx,
		config:      config,
		storage:     storage,
		executor:    executor,
		node:        cometNode,
		mempool:     make([]*Transaction, 0),
		rateLimiter: rateLimiter,
		running:     false,
	}
	
	// Conectar el mempool local con ABCIApp para que PrepareProposal pueda usarlo
	if cometNode.abciApp != nil {
		cometNode.abciApp.SetGetMempool(c.GetMempool)
		cometNode.abciApp.SetClearMempoolTx(c.RemoveTransactionFromMempool)
	}

	log.Println("Consenso CometBFT inicializado")
	return c, nil
}

// SetMetrics establece las métricas en el ABCIApp
func (c *CometBFT) SetMetrics(m *metrics.Metrics) {
	if c.node != nil && c.node.abciApp != nil {
		c.node.abciApp.SetMetrics(m)
	}
}

// Start inicia el motor de consenso
func (c *CometBFT) Start() error {
	if c.running {
		return fmt.Errorf("consenso ya está corriendo")
	}

	// Iniciar nodo CometBFT
	if err := c.node.node.Start(); err != nil {
		return fmt.Errorf("error iniciando nodo CometBFT: %w", err)
	}

	// Conectar a otros validadores vía mesh network
	// CometBFT maneja la conexión a peers automáticamente a través de:
	// 1. Configuración de persistent_peers en config.toml
	// 2. Discovery automático mediante P2P gossiping
	// 3. Mesh network integration (oxygen-sdk) para validadores físicos
	// 
	// Nota: Los validadores pueden conectarse directamente si conocen sus direcciones
	// Para discovery automático, el mesh_bridge publica información de validadores
	// en el topic "validators" y otros nodos pueden descubrirlos

	c.running = true
	log.Println("✅ Consenso CometBFT iniciado")
	return nil
}

// Stop detiene el motor de consenso
func (c *CometBFT) Stop() error {
	if !c.running {
		return nil
	}

	// Detener nodo CometBFT
	if err := c.node.node.Stop(); err != nil {
		return fmt.Errorf("error deteniendo nodo CometBFT: %w", err)
	}

	c.running = false
	log.Println("⏹️  Consenso CometBFT detenido")
	return nil
}

// IsValidator retorna si este nodo es validador
func (c *CometBFT) IsValidator() bool {
	return c.config.ValidatorAddr != ""
}

// GetLatestBlock retorna el último bloque validado
func (c *CometBFT) GetLatestBlock() (*Block, error) {
	if !c.running {
		return nil, fmt.Errorf("consenso no está corriendo")
	}

	// Obtener altura del último bloque
	height, err := c.storage.GetLatestHeight()
	if err != nil {
		// Si no hay altura guardada, retornar bloque genesis (altura 0)
		height = 0
	}

	// Obtener bloque desde storage
	blockData, err := c.storage.GetBlock(height)
	if err != nil {
		// Si no existe el bloque, crear bloque vacío
		return &Block{
			Header: BlockHeader{
				Height:     height,
				Hash:       "",
				ParentHash: "",
				Timestamp:  time.Now(),
				ChainID:    c.config.ChainID,
			},
			Transactions: []*Transaction{},
			Receipts:     []*TransactionReceipt{},
		}, nil
	}

	// Decodificar bloque
	var block Block
	if err := json.Unmarshal(blockData, &block); err != nil {
		return nil, fmt.Errorf("error decodificando bloque: %w", err)
	}

	return &block, nil
}

// SubmitTransaction envía una transacción para ser validada
func (c *CometBFT) SubmitTransaction(tx *Transaction) error {
	if !c.running {
		return fmt.Errorf("consenso no está corriendo")
	}

	// Validar que la transacción tenga hash
	if tx.Hash == "" {
		return fmt.Errorf("transacción sin hash")
	}

	// Rate limiting: verificar límite de mempool
	c.mempoolMutex.RLock()
	mempoolSize := len(c.mempool)
	c.mempoolMutex.RUnlock()

	if !c.rateLimiter.CheckMempoolSize(mempoolSize) {
		return fmt.Errorf("mempool lleno: límite alcanzado")
	}

	// Rate limiting: verificar límite por dirección
	if !c.rateLimiter.Allow(tx.From) {
		return fmt.Errorf("rate limit excedido para dirección %s", tx.From)
	}

	// Validar que no esté ya en el mempool
	c.mempoolMutex.Lock()
	for _, existingTx := range c.mempool {
		if existingTx.Hash == tx.Hash {
			c.mempoolMutex.Unlock()
			return fmt.Errorf("transacción ya está en el mempool")
		}
	}

	// Agregar al mempool local
	c.mempool = append(c.mempool, tx)
	c.mempoolMutex.Unlock()

	log.Printf("📥 Transacción agregada al mempool: %s", tx.Hash)

	// La transacción será procesada por CometBFT cuando PrepareProposal use el mempool local
	// PrepareProposal incluirá las transacciones del mempool local en los bloques

	return nil
}

// GetMempool retorna las transacciones en el mempool
func (c *CometBFT) GetMempool() []*Transaction {
	c.mempoolMutex.RLock()
	defer c.mempoolMutex.RUnlock()

	result := make([]*Transaction, len(c.mempool))
	copy(result, c.mempool)
	return result
}

// GetExecutor retorna el executor EVM (para uso interno de otros componentes)
func (c *CometBFT) GetExecutor() *execution.EVMExecutor {
	return c.executor
}

// GetStorage retorna el storage (para uso interno de otros componentes)
func (c *CometBFT) GetStorage() *storage.BlockchainDB {
	return c.storage
}

// ClearMempool limpia el mempool (llamado después de producir un bloque)
func (c *CometBFT) ClearMempool() {
	c.mempoolMutex.Lock()
	defer c.mempoolMutex.Unlock()

	c.mempool = make([]*Transaction, 0)
}

// RemoveTransactionFromMempool remueve una transacción específica del mempool
func (c *CometBFT) RemoveTransactionFromMempool(txHash string) {
	c.mempoolMutex.Lock()
	defer c.mempoolMutex.Unlock()

	// Buscar y remover la transacción
	for i, tx := range c.mempool {
		if tx.Hash == txHash {
			// Remover sin preservar orden (más rápido)
			c.mempool[i] = c.mempool[len(c.mempool)-1]
			c.mempool = c.mempool[:len(c.mempool)-1]
			log.Printf("🗑️ Transacción removida del mempool: %s", txHash)
			return
		}
	}
}

// GetValidators retorna la lista de validadores activos
func (c *CometBFT) GetValidators() []*Validator {
	if c.node == nil || c.node.abciApp == nil || c.node.abciApp.validators == nil {
		return []*Validator{}
	}
	
	return c.node.abciApp.validators.GetActiveValidators()
}

// GetValidatorSet retorna el ValidatorSet completo (para uso interno)
func (c *CometBFT) GetValidatorSet() *ValidatorSet {
	if c.node == nil || c.node.abciApp == nil {
		return nil
	}
	return c.node.abciApp.validators
}

