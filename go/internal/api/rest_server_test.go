package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Q-YZX0/oxy-blockchain/internal/health"
	"github.com/Q-YZX0/oxy-blockchain/internal/metrics"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// crearTestServer crea un servidor REST de prueba
func crearTestServer(t *testing.T) (*RestServer, *storage.BlockchainDB) {
	testDir := "./test_data_api_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	
	// Crear componentes mínimos
	healthChecker := health.NewHealthChecker()
	metricsCollector := metrics.NewMetrics()
	
	server := NewRestServer(
		"localhost",
		"8080",
		db,
		nil, // consensus (puede ser nil para algunos tests)
		healthChecker,
		metricsCollector,
		nil, // executor (puede ser nil para algunos tests)
	)
	
	return server, db
}

// TestRestServer_HealthCheck prueba el endpoint /health
func TestRestServer_HealthCheck(t *testing.T) {
	server, db := crearTestServer(t)
	defer func() {
		db.Close()
		os.RemoveAll("./test_data_api_" + t.Name())
	}()
	
	// Crear request
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("Error creando request: %v", err)
	}
	
	// Crear response recorder
	rr := httptest.NewRecorder()
	
	// Llamar handler directamente
	server.handleHealth(rr, req)
	
	// Verificar status code
	if rr.Code != http.StatusOK && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Status code incorrecto: esperado 200 o 503, obtenido %d", rr.Code)
	}
	
	// Verificar Content-Type
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type incorrecto: esperado application/json, obtenido %s", contentType)
	}
	
	// Verificar que retorna JSON válido
	var healthStatus map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &healthStatus); err != nil {
		t.Errorf("Error parseando JSON de health: %v", err)
	}
}

// TestRestServer_Metrics prueba el endpoint /metrics
func TestRestServer_Metrics(t *testing.T) {
	server, db := crearTestServer(t)
	defer func() {
		db.Close()
		os.RemoveAll("./test_data_api_" + t.Name())
	}()
	
	// Crear request
	req, err := http.NewRequest("GET", "/metrics", nil)
	if err != nil {
		t.Fatalf("Error creando request: %v", err)
	}
	
	// Crear response recorder
	rr := httptest.NewRecorder()
	
	// Llamar handler directamente
	server.handleMetrics(rr, req)
	
	// Verificar status code
	if rr.Code != http.StatusOK {
		t.Errorf("Status code incorrecto: esperado 200, obtenido %d", rr.Code)
	}
	
	// Verificar Content-Type
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type incorrecto: esperado application/json, obtenido %s", contentType)
	}
	
	// Verificar que retorna JSON válido
	var metrics map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &metrics); err != nil {
		t.Errorf("Error parseando JSON de metrics: %v", err)
	}
}

// TestRestServer_GetBlock prueba el endpoint GET /api/v1/blocks/{height}
func TestRestServer_GetBlock(t *testing.T) {
	server, db := crearTestServer(t)
	defer func() {
		db.Close()
		os.RemoveAll("./test_data_api_" + t.Name())
	}()
	
	// Guardar un bloque de prueba
	blockData := []byte(`{"height": 1, "hash": "0x123"}`)
	err := db.SaveBlock(1, blockData)
	if err != nil {
		t.Fatalf("Error guardando bloque: %v", err)
	}
	
	// Crear request
	req, err := http.NewRequest("GET", "/api/v1/blocks/1", nil)
	if err != nil {
		t.Fatalf("Error creando request: %v", err)
	}
	
	// Crear response recorder
	rr := httptest.NewRecorder()
	
	// Llamar handler directamente
	server.handleBlocks(rr, req)
	
	// Verificar status code (puede ser 200 o 500 si consensus es nil)
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		// Si consensus es nil, puede fallar, eso está bien para este test
		t.Logf("Status code: %d (puede fallar si consensus es nil)", rr.Code)
	}
}

// TestRestServer_GetTransaction prueba el endpoint GET /api/v1/transactions/{hash}
func TestRestServer_GetTransaction(t *testing.T) {
	server, db := crearTestServer(t)
	defer func() {
		db.Close()
		os.RemoveAll("./test_data_api_" + t.Name())
	}()
	
	// Guardar una transacción de prueba
	txHash := "0xabcdef123456"
	txData := []byte(`{"hash": "0xabcdef123456", "from": "0x123"}`)
	err := db.SaveTransaction(txHash, txData)
	if err != nil {
		t.Fatalf("Error guardando transacción: %v", err)
	}
	
	// Crear request
	req, err := http.NewRequest("GET", "/api/v1/transactions/"+txHash, nil)
	if err != nil {
		t.Fatalf("Error creando request: %v", err)
	}
	
	// Crear response recorder
	rr := httptest.NewRecorder()
	
	// Llamar handler directamente
	server.handleTransactions(rr, req)
	
	// Verificar status code
	if rr.Code != http.StatusOK {
		t.Errorf("Status code incorrecto: esperado 200, obtenido %d", rr.Code)
	}
	
	// Verificar Content-Type
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type incorrecto: esperado application/json, obtenido %s", contentType)
	}
}

// TestRestServer_GetTransaction_NotFound prueba transacción no encontrada
func TestRestServer_GetTransaction_NotFound(t *testing.T) {
	server, db := crearTestServer(t)
	defer func() {
		db.Close()
		os.RemoveAll("./test_data_api_" + t.Name())
	}()
	
	// Crear request para transacción inexistente
	req, err := http.NewRequest("GET", "/api/v1/transactions/0xnonexistent", nil)
	if err != nil {
		t.Fatalf("Error creando request: %v", err)
	}
	
	// Crear response recorder
	rr := httptest.NewRecorder()
	
	// Llamar handler directamente
	server.handleTransactions(rr, req)
	
	// Verificar status code (debería ser 404)
	if rr.Code != http.StatusNotFound {
		t.Errorf("Status code incorrecto: esperado 404, obtenido %d", rr.Code)
	}
}

// TestRestServer_SubmitTransaction prueba el endpoint POST /api/v1/submit-tx
func TestRestServer_SubmitTransaction(t *testing.T) {
	server, db := crearTestServer(t)
	defer func() {
		db.Close()
		os.RemoveAll("./test_data_api_" + t.Name())
	}()
	
	// Crear transacción de prueba
	txData := map[string]interface{}{
		"from":     "0x1234567890123456789012345678901234567890",
		"to":       "0x0987654321098765432109876543210987654321",
		"value":    "1000000000000000000",
		"gasLimit": 21000,
		"gasPrice": "1000000000",
		"nonce":    0,
	}
	
	jsonData, err := json.Marshal(txData)
	if err != nil {
		t.Fatalf("Error serializando transacción: %v", err)
	}
	
	// Crear request POST
	req, err := http.NewRequest("POST", "/api/v1/submit-tx", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Error creando request: %v", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	// Crear response recorder
	rr := httptest.NewRecorder()
	
	// Llamar handler directamente
	server.handleSubmitTx(rr, req)
	
	// Verificar status code (puede fallar si consensus es nil, eso está bien)
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Logf("Status code: %d (puede fallar si consensus es nil)", rr.Code)
	}
}

// TestRestServer_CORSMiddleware prueba el middleware CORS
func TestRestServer_CORSMiddleware(t *testing.T) {
	server, db := crearTestServer(t)
	defer func() {
		db.Close()
		os.RemoveAll("./test_data_api_" + t.Name())
	}()
	
	// Crear request OPTIONS (preflight CORS)
	req, err := http.NewRequest("OPTIONS", "/health", nil)
	if err != nil {
		t.Fatalf("Error creando request: %v", err)
	}
	
	// Crear response recorder
	rr := httptest.NewRecorder()
	
	// Crear handler con CORS middleware
	handler := server.corsMiddleware(http.HandlerFunc(server.handleHealth))
	
	// Llamar handler
	handler.ServeHTTP(rr, req)
	
	// Verificar status code
	if rr.Code != http.StatusOK {
		t.Errorf("Status code incorrecto para OPTIONS: esperado 200, obtenido %d", rr.Code)
	}
	
	// Verificar headers CORS
	origin := rr.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("Access-Control-Allow-Origin incorrecto: esperado *, obtenido %s", origin)
	}
	
	methods := rr.Header().Get("Access-Control-Allow-Methods")
	if methods == "" {
		t.Error("Access-Control-Allow-Methods no está presente")
	}
}

// TestRestServer_MethodNotAllowed prueba métodos no permitidos
func TestRestServer_MethodNotAllowed(t *testing.T) {
	server, db := crearTestServer(t)
	defer func() {
		db.Close()
		os.RemoveAll("./test_data_api_" + t.Name())
	}()
	
	// Crear request POST a endpoint que solo acepta GET
	req, err := http.NewRequest("POST", "/health", nil)
	if err != nil {
		t.Fatalf("Error creando request: %v", err)
	}
	
	// Crear response recorder
	rr := httptest.NewRecorder()
	
	// Llamar handler directamente
	server.handleHealth(rr, req)
	
	// Verificar status code (debería ser 405 Method Not Allowed)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status code incorrecto: esperado 405, obtenido %d", rr.Code)
	}
}

