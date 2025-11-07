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
	mux.HandleFunc("/health/liveness", s.handleLiveness)
	mux.HandleFunc("/health/readiness", s.handleReadiness)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/metrics/prometheus", s.handlePrometheusMetrics)
	mux.HandleFunc("/api/v1/blocks/", s.handleBlocks)
	mux.HandleFunc("/api/v1/transactions/", s.handleTransactions)
	mux.HandleFunc("/api/v1/accounts/", s.handleAccounts)
	mux.HandleFunc("/api/v1/submit-tx", s.handleSubmitTx)
	mux.HandleFunc("/api/v1/validators", s.handleValidators) // Nuevo endpoint

    // Middlewares: CORS, RateLimit, MaxBody
    handler := s.maxBodyMiddleware(
        s.rateLimitMiddleware(
            s.corsMiddleware(mux),
        ),
    )

	addr := s.host + ":" + s.port
    // Timeouts configurables por env
    readTimeout := getEnvDurationMs("OXY_REST_READ_TIMEOUT_MS", 15000)
    writeTimeout := getEnvDurationMs("OXY_REST_WRITE_TIMEOUT_MS", 15000)

    s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
        ReadTimeout:  readTimeout,
        WriteTimeout: writeTimeout,
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
        allowed := os.Getenv("OXY_REST_CORS_ORIGINS")
        origin := r.Header.Get("Origin")
        if allowed == "*" || allowed == "" {
            w.Header().Set("Access-Control-Allow-Origin", "*")
        } else if origin != "" && isOriginAllowed(origin, allowed) {
            w.Header().Set("Access-Control-Allow-Origin", origin)
            w.Header().Set("Vary", "Origin")
        }
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware aplica un rate limit simple por IP (token bucket en memoria)
func (s *RestServer) rateLimitMiddleware(next http.Handler) http.Handler {
    type bucket struct {
        tokens     float64
        lastRefill time.Time
    }
    var (
        rps   = getEnvFloat("OXY_REST_RATE_LIMIT_RPS", 50)
        burst = getEnvFloat("OXY_REST_BURST", 100)
        store = make(map[string]*bucket)
    )
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ip := r.RemoteAddr
        b, ok := store[ip]
        now := time.Now()
        if !ok {
            b = &bucket{tokens: burst, lastRefill: now}
            store[ip] = b
        }
        // Refill tokens
        elapsed := now.Sub(b.lastRefill).Seconds()
        b.tokens = minFloat(burst, b.tokens+elapsed*rps)
        b.lastRefill = now
        if b.tokens < 1 {
            w.Header().Set("Retry-After", "1")
            http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
            return
        }
        b.tokens -= 1
        next.ServeHTTP(w, r)
    })
}

// maxBodyMiddleware limita el tamaño del body según env
func (s *RestServer) maxBodyMiddleware(next http.Handler) http.Handler {
    maxBytes := getEnvInt("OXY_REST_MAX_BODY_BYTES", 1048576)
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))
        next.ServeHTTP(w, r)
    })
}

func isOriginAllowed(origin string, allowedList string) bool {
    for _, a := range strings.Split(allowedList, ",") {
        if strings.TrimSpace(a) == origin {
            return true
        }
    }
    return false
}

func getEnvDurationMs(key string, defMs int) time.Duration {
    if v := os.Getenv(key); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            return time.Duration(n) * time.Millisecond
        }
    }
    return time.Duration(defMs) * time.Millisecond
}

func getEnvFloat(key string, def float64) float64 {
    if v := os.Getenv(key); v != "" {
        if f, err := strconv.ParseFloat(v, 64); err == nil {
            return f
        }
    }
    return def
}

func getEnvInt(key string, def int) int {
    if v := os.Getenv(key); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            return n
        }
    }
    return def
}

func minFloat(a, b float64) float64 {
    if a < b {
        return a
    }
    return b
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

// handleLiveness maneja el endpoint /health/liveness
func (s *RestServer) handleLiveness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	isLive := s.healthChecker.IsLive()
	w.Header().Set("Content-Type", "application/json")
	
	if isLive {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "alive",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "dead",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

// handleReadiness maneja el endpoint /health/readiness
func (s *RestServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	isReady := s.healthChecker.IsReady()
	w.Header().Set("Content-Type", "application/json")
	
	if isReady {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ready",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not_ready",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
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

// handlePrometheusMetrics maneja el endpoint /metrics/prometheus
func (s *RestServer) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.metrics == nil {
		http.Error(w, "Metrics not available", http.StatusServiceUnavailable)
		return
	}

	// Actualizar métricas dinámicas
	if s.consensus != nil {
		latestBlock, err := s.consensus.GetLatestBlock()
		if err == nil && latestBlock != nil {
			s.metrics.SetBlockHeight(latestBlock.Header.Height)
		}
		mempool := s.consensus.GetMempool()
		if mempool != nil {
			s.metrics.SetMempoolSize(len(mempool))
		}
	}

	metricsData := s.metrics.GetMetrics()
	uptimeSeconds := metricsData.Uptime.Seconds()

	// Formato Prometheus
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)

	// Escribir métricas en formato Prometheus
	fmt.Fprintf(w, "# HELP oxy_blocks_processed_total Total number of blocks processed\n")
	fmt.Fprintf(w, "# TYPE oxy_blocks_processed_total counter\n")
	fmt.Fprintf(w, "oxy_blocks_processed_total %d\n", metricsData.BlocksProcessed)

	fmt.Fprintf(w, "# HELP oxy_block_processing_time_seconds Average block processing time\n")
	fmt.Fprintf(w, "# TYPE oxy_block_processing_time_seconds gauge\n")
	fmt.Fprintf(w, "oxy_block_processing_time_seconds %.6f\n", metricsData.BlockProcessingTime.Seconds())

	fmt.Fprintf(w, "# HELP oxy_transactions_processed_total Total number of transactions processed\n")
	fmt.Fprintf(w, "# TYPE oxy_transactions_processed_total counter\n")
	fmt.Fprintf(w, "oxy_transactions_processed_total %d\n", metricsData.TransactionsProcessed)

	fmt.Fprintf(w, "# HELP oxy_transactions_rejected_total Total number of transactions rejected\n")
	fmt.Fprintf(w, "# TYPE oxy_transactions_rejected_total counter\n")
	fmt.Fprintf(w, "oxy_transactions_rejected_total %d\n", metricsData.TransactionsRejected)

	fmt.Fprintf(w, "# HELP oxy_transactions_per_second Current transactions per second\n")
	fmt.Fprintf(w, "# TYPE oxy_transactions_per_second gauge\n")
	fmt.Fprintf(w, "oxy_transactions_per_second %.2f\n", metricsData.TransactionsPerSecond)

	fmt.Fprintf(w, "# HELP oxy_peers_connected Number of connected peers\n")
	fmt.Fprintf(w, "# TYPE oxy_peers_connected gauge\n")
	fmt.Fprintf(w, "oxy_peers_connected %d\n", metricsData.PeersConnected)

	fmt.Fprintf(w, "# HELP oxy_messages_received_total Total messages received\n")
	fmt.Fprintf(w, "# TYPE oxy_messages_received_total counter\n")
	fmt.Fprintf(w, "oxy_messages_received_total %d\n", metricsData.MessagesReceived)

	fmt.Fprintf(w, "# HELP oxy_messages_sent_total Total messages sent\n")
	fmt.Fprintf(w, "# TYPE oxy_messages_sent_total counter\n")
	fmt.Fprintf(w, "oxy_messages_sent_total %d\n", metricsData.MessagesSent)

	fmt.Fprintf(w, "# HELP oxy_block_height Current block height\n")
	fmt.Fprintf(w, "# TYPE oxy_block_height gauge\n")
	fmt.Fprintf(w, "oxy_block_height %d\n", metricsData.CurrentBlockHeight)

	fmt.Fprintf(w, "# HELP oxy_state_db_size_bytes State database size in bytes\n")
	fmt.Fprintf(w, "# TYPE oxy_state_db_size_bytes gauge\n")
	fmt.Fprintf(w, "oxy_state_db_size_bytes %d\n", metricsData.StateDBSize)

	fmt.Fprintf(w, "# HELP oxy_mempool_size Current mempool size\n")
	fmt.Fprintf(w, "# TYPE oxy_mempool_size gauge\n")
	fmt.Fprintf(w, "oxy_mempool_size %d\n", metricsData.MempoolSize)

	fmt.Fprintf(w, "# HELP oxy_gas_used_total Total gas used\n")
	fmt.Fprintf(w, "# TYPE oxy_gas_used_total counter\n")
	fmt.Fprintf(w, "oxy_gas_used_total %d\n", metricsData.TotalGasUsed)

	fmt.Fprintf(w, "# HELP oxy_gas_used_average Average gas used per transaction\n")
	fmt.Fprintf(w, "# TYPE oxy_gas_used_average gauge\n")
	fmt.Fprintf(w, "oxy_gas_used_average %d\n", metricsData.AverageGasUsed)

	fmt.Fprintf(w, "# HELP oxy_uptime_seconds Node uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE oxy_uptime_seconds gauge\n")
	fmt.Fprintf(w, "oxy_uptime_seconds %.2f\n", uptimeSeconds)

	if !metricsData.LastBlockTime.IsZero() {
		fmt.Fprintf(w, "# HELP oxy_last_block_time_seconds Timestamp of last block\n")
		fmt.Fprintf(w, "# TYPE oxy_last_block_time_seconds gauge\n")
		fmt.Fprintf(w, "oxy_last_block_time_seconds %.0f\n", float64(metricsData.LastBlockTime.Unix()))
	}
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

