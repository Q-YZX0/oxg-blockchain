package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/Q-YZX0/oxy-blockchain/internal/consensus"
	"github.com/Q-YZX0/oxy-blockchain/internal/execution"
	"github.com/Q-YZX0/oxy-blockchain/internal/health"
	"github.com/Q-YZX0/oxy-blockchain/internal/metrics"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// RestServer maneja el servidor HTTP REST local
type RestServer struct {
	host          string
	port          string
	storage       *storage.BlockchainDB
	consensus     *consensus.CometBFT
	healthChecker *health.HealthChecker
	metrics       *metrics.Metrics
	executor      *execution.EVMExecutor
	server        *http.Server
}

// NewRestServer crea un nuevo servidor REST
func NewRestServer(
	host string,
	port string,
	storage *storage.BlockchainDB,
	consensus *consensus.CometBFT,
	healthChecker *health.HealthChecker,
	metrics *metrics.Metrics,
	executor *execution.EVMExecutor,
) *RestServer {
	return &RestServer{
		host:          host,
		port:          port,
		storage:       storage,
		consensus:     consensus,
		healthChecker: healthChecker,
		metrics:       metrics,
		executor:      executor,
	}
}

// Start inicia el servidor REST
func (s *RestServer) Start() error {
	mux := http.NewServeMux()

	// Endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/api/v1/blocks/", s.handleBlocks)
	mux.HandleFunc("/api/v1/transactions/", s.handleTransactions)
	mux.HandleFunc("/api/v1/accounts/", s.handleAccounts)
	mux.HandleFunc("/api/v1/submit-tx", s.handleSubmitTx)
	mux.HandleFunc("/api/v1/validators", s.handleValidators) // Nuevo endpoint

	// Middleware CORS básico
	handler := s.corsMiddleware(mux)

	addr := s.host + ":" + s.port
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Log antes de iniciar (usar fmt.Printf y os.Stdout para asegurar que se vea)
	fmt.Fprintf(os.Stdout, "[REST Server] Intentando iniciar en %s\n", addr)
	os.Stdout.Sync() // Forzar escritura inmediata
	
	// ListenAndServe es bloqueante - si funciona, nunca retorna hasta que se cierre el servidor
	// Si hay error, retorna inmediatamente
	fmt.Fprintf(os.Stdout, "[REST Server] Llamando a ListenAndServe()...\n")
	os.Stdout.Sync()
	
	// Iniciar goroutine para confirmar que el servidor está escuchando después de un breve delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		// Intentar hacer una conexión local para verificar que el servidor está escuchando
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[REST Server] ADVERTENCIA: Servidor no responde en /health después de 500ms: %v\n", err)
			os.Stderr.Sync()
		} else {
			resp.Body.Close()
			fmt.Fprintf(os.Stdout, "[REST Server] ✅ Servidor confirmado escuchando en %s (health check OK)\n", addr)
			os.Stdout.Sync()
		}
	}()
	
	err := s.server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "[REST Server] ERROR al iniciar: %v (tipo: %T)\n", err, err)
		os.Stderr.Sync()
		return err
	}
	
	// Si llegamos aquí, el servidor se cerró correctamente
	fmt.Fprintf(os.Stdout, "[REST Server] Servidor cerrado\n")
	os.Stdout.Sync()
	return nil
}

// Stop detiene el servidor REST
func (s *RestServer) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

// corsMiddleware añade headers CORS
func (s *RestServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleHealth maneja el endpoint /health
func (s *RestServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.healthChecker.CheckHealth()
	
	w.Header().Set("Content-Type", "application/json")
	
	// Retornar código HTTP apropiado
	if status.Status == "unhealthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else if status.Status == "degraded" {
		w.WriteHeader(http.StatusOK) // 200 pero con advertencia
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(status)
}

// handleMetrics maneja el endpoint /metrics
func (s *RestServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verificar que metrics no sea nil
	if s.metrics == nil {
		http.Error(w, "Metrics not available", http.StatusServiceUnavailable)
		return
	}

	// Actualizar métricas dinámicas desde consenso y executor si están disponibles
	if s.consensus != nil {
		// Actualizar altura de bloque actual
		latestBlock, err := s.consensus.GetLatestBlock()
		if err == nil && latestBlock != nil {
			s.metrics.SetBlockHeight(latestBlock.Header.Height)
		}
		
		// Actualizar tamaño del mempool
		mempool := s.consensus.GetMempool()
		if mempool != nil {
			s.metrics.SetMempoolSize(len(mempool))
		}
	}
	
	// Obtener métricas actualizadas
	metricsData := s.metrics.GetMetrics()
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(metricsData)
}

// handleBlocks maneja /api/v1/blocks/{height} o /api/v1/blocks/latest
func (s *RestServer) handleBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extraer height del path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/blocks/")
	
	var block *consensus.Block
	var err error

	if path == "latest" || path == "" {
		// Obtener último bloque
		block, err = s.consensus.GetLatestBlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Parsear altura
		height, parseErr := strconv.ParseUint(path, 10, 64)
		if parseErr != nil {
			http.Error(w, "Invalid block height", http.StatusBadRequest)
			return
		}

		// Obtener bloque por altura
		blockData, dbErr := s.storage.GetBlock(height)
		if dbErr != nil {
			http.Error(w, "Block not found", http.StatusNotFound)
			return
		}

		if err := json.Unmarshal(blockData, &block); err != nil {
			http.Error(w, "Error decoding block", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(block)
}

// handleTransactions maneja /api/v1/transactions/{hash}
func (s *RestServer) handleTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extraer hash del path
	txHash := r.URL.Path[len("/api/v1/transactions/"):]

	// Obtener transacción desde storage
	txData, err := s.storage.GetTransaction(txHash)
	if err != nil {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(txData)
}

// handleAccounts maneja /api/v1/accounts/{address} y /api/v1/accounts/{address}/fund
func (s *RestServer) handleAccounts(w http.ResponseWriter, r *http.Request) {
	// Extraer dirección del path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")
	
	// Log para debug
	log.Printf("🔍 handleAccounts: path=%s, method=%s", path, r.Method)
	
	// Verificar si es el endpoint de fondear
	// El path puede ser "0x.../fund" o solo "/fund" si la dirección viene en el path completo
	if strings.HasSuffix(path, "/fund") {
		// Remover "/fund" del path
		address := strings.TrimSuffix(path, "/fund")
		log.Printf("💰 handleFundAccount: address=%s", address)
		s.handleFundAccount(w, r, address)
		return
	}
	
	// Endpoint GET /api/v1/accounts/{address}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	address := path
	
	// Validar dirección
	if !common.IsHexAddress(address) {
		http.Error(w, "Invalid Ethereum address", http.StatusBadRequest)
		return
	}

	// Obtener estado de cuenta desde el executor EVM
	if s.executor == nil {
		http.Error(w, "EVM executor not available", http.StatusServiceUnavailable)
		return
	}

	accountState, err := s.executor.GetState(address)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting account state: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(accountState)
}

// handleFundAccount maneja POST /api/v1/accounts/{address}/fund
func (s *RestServer) handleFundAccount(w http.ResponseWriter, r *http.Request, address string) {
	log.Printf("💰 handleFundAccount llamado: address=%s, method=%s", address, r.Method)
	
	if r.Method != http.MethodPost {
		log.Printf("❌ handleFundAccount: método incorrecto: %s (esperado POST)", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validar dirección (puede estar vacía si viene del path completo)
	if address == "" {
		// Intentar extraer del path completo
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")
		path = strings.TrimSuffix(path, "/fund")
		address = path
		log.Printf("🔍 handleFundAccount: dirección extraída del path: %s", address)
	}
	
	// Validar dirección
	if !common.IsHexAddress(address) {
		log.Printf("❌ handleFundAccount: dirección inválida: %s", address)
		http.Error(w, "Invalid Ethereum address", http.StatusBadRequest)
		return
	}
	
	log.Printf("✅ handleFundAccount: dirección válida: %s", address)

	// Parsear body
	var req struct {
		Amount string `json:"amount"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Error parsing request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Amount == "" {
		http.Error(w, "Amount is required", http.StatusBadRequest)
		return
	}

	// Obtener executor EVM
	if s.executor == nil {
		http.Error(w, "EVM executor not available", http.StatusServiceUnavailable)
		return
	}

	// Fondear cuenta
	if err := s.executor.FundAccount(address, req.Amount); err != nil {
		http.Error(w, fmt.Sprintf("Error funding account: %v", err), http.StatusInternalServerError)
		return
	}

	// Obtener estado actualizado de la cuenta
	accountState, err := s.executor.GetState(address)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting account state: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Account %s funded with %s tokens", address, req.Amount),
		"account": accountState,
	})
}

// handleSubmitTx maneja /api/v1/submit-tx
func (s *RestServer) handleSubmitTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decodificar transacción del body
	var tx consensus.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, fmt.Sprintf("Invalid transaction format: %v", err), http.StatusBadRequest)
		return
	}

	// Validar transacción básica
	if tx.Hash == "" {
		http.Error(w, "Transaction hash required", http.StatusBadRequest)
		return
	}

	if tx.From == "" {
		http.Error(w, "Transaction from address required", http.StatusBadRequest)
		return
	}

	// Enviar transacción al consensus
	if err := s.consensus.SubmitTransaction(&tx); err != nil {
		http.Error(w, fmt.Sprintf("Error submitting transaction: %v", err), http.StatusBadRequest)
		return
	}

	// Retornar confirmación
	response := map[string]interface{}{
		"success": true,
		"hash":    tx.Hash,
		"message": "Transaction submitted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleValidators maneja /api/v1/validators
func (s *RestServer) handleValidators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.consensus == nil {
		http.Error(w, "Consensus not available", http.StatusServiceUnavailable)
		return
	}

	// Obtener validadores activos
	validators := s.consensus.GetValidators()
	
	// Convertir a formato JSON para la API
	type ValidatorInfo struct {
		Address      string   `json:"address"`
		PubKey       string   `json:"pubKey"`
		Stake        string   `json:"stake"`
		Power        int64    `json:"power"`
		Jailed       bool     `json:"jailed"`
		CreatedAt    string   `json:"createdAt"`
		LastActiveAt string   `json:"lastActiveAt"`
	}

	validatorInfos := make([]ValidatorInfo, 0, len(validators))
	for _, v := range validators {
		validatorInfos = append(validatorInfos, ValidatorInfo{
			Address:      v.Address,
			PubKey:       fmt.Sprintf("0x%x", v.PubKey),
			Stake:        v.Stake.String(),
			Power:        v.Power,
			Jailed:       v.Jailed,
			CreatedAt:    v.CreatedAt.Format(time.RFC3339),
			LastActiveAt: v.LastActiveAt.Format(time.RFC3339),
		})
	}

	response := map[string]interface{}{
		"validators": validatorInfos,
		"count":      len(validatorInfos),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

