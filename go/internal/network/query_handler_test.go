package network

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// TestQueryHandler_HandleQuery prueba el manejo de queries
func TestQueryHandler_HandleQuery(t *testing.T) {
	ctx := context.Background()
	
	// Crear storage temporal
	testDir := "./test_data_query_handler_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()
	
	// Crear query handler (sin consensus ni mesh bridge real para simplificar)
	handler := NewQueryHandler(ctx, db, nil, nil)
	
	// Query de altura
	request := QueryRequest{
		Type:      "query",
		Path:      "height",
		RequestID: "test-request-1",
	}
	
	err = handler.HandleQuery(request)
	// HandleQuery debería manejar la query (aunque falle al enviar respuesta sin mesh bridge)
	// Verificamos que no crashee
	if err != nil {
		t.Logf("Error esperado (sin mesh bridge): %v", err)
	}
}

// TestQueryHandler_HandleQuery_Block prueba query de bloque
func TestQueryHandler_HandleQuery_Block(t *testing.T) {
	ctx := context.Background()
	
	testDir := "./test_data_query_block_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()
	
	// Guardar un bloque de prueba
	blockData := []byte(`{"height": 1, "hash": "0x123"}`)
	err = db.SaveBlock(1, blockData)
	if err != nil {
		t.Fatalf("Error guardando bloque: %v", err)
	}
	
	handler := NewQueryHandler(ctx, db, nil, nil)
	
	// Query de bloque
	request := QueryRequest{
		Type:      "query",
		Path:      "block/1",
		RequestID: "test-request-2",
	}
	
	err = handler.HandleQuery(request)
	if err != nil {
		t.Logf("Error esperado (sin mesh bridge): %v", err)
	}
}

// TestQueryHandler_HandleQuery_Transaction prueba query de transacción
func TestQueryHandler_HandleQuery_Transaction(t *testing.T) {
	ctx := context.Background()
	
	testDir := "./test_data_query_tx_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()
	
	// Guardar una transacción de prueba
	txHash := "0xabcdef123456"
	txData := []byte(`{"hash": "0xabcdef123456", "from": "0x123"}`)
	err = db.SaveTransaction(txHash, txData)
	if err != nil {
		t.Fatalf("Error guardando transacción: %v", err)
	}
	
	handler := NewQueryHandler(ctx, db, nil, nil)
	
	// Query de transacción
	request := QueryRequest{
		Type:      "query",
		Path:      "tx/" + txHash,
		RequestID: "test-request-3",
	}
	
	err = handler.HandleQuery(request)
	if err != nil {
		t.Logf("Error esperado (sin mesh bridge): %v", err)
	}
}

// TestQueryHandler_HandleQuery_Account prueba query de cuenta
func TestQueryHandler_HandleQuery_Account(t *testing.T) {
	ctx := context.Background()
	
	testDir := "./test_data_query_account_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()
	
	handler := NewQueryHandler(ctx, db, nil, nil)
	
	// Query de cuenta (sin executor EVM, usará fallback)
	request := QueryRequest{
		Type:      "query",
		Path:      "account/0x1234567890123456789012345678901234567890",
		RequestID: "test-request-4",
	}
	
	err = handler.HandleQuery(request)
	if err != nil {
		t.Logf("Error esperado (sin mesh bridge): %v", err)
	}
}

// TestQueryHandler_HandleQuery_InvalidPath prueba query con path inválido
func TestQueryHandler_HandleQuery_InvalidPath(t *testing.T) {
	ctx := context.Background()
	
	testDir := "./test_data_query_invalid_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()
	
	handler := NewQueryHandler(ctx, db, nil, nil)
	
	// Query con path inválido
	request := QueryRequest{
		Type:      "query",
		Path:      "invalid/path",
		RequestID: "test-request-5",
	}
	
	err = handler.HandleQuery(request)
	if err != nil {
		t.Logf("Error esperado (sin mesh bridge): %v", err)
	}
}

// TestQueryHandler_HandleResponse prueba el manejo de respuestas
func TestQueryHandler_HandleResponse(t *testing.T) {
	ctx := context.Background()
	
	testDir := "./test_data_response_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()
	
	handler := NewQueryHandler(ctx, db, nil, nil)
	
	// Crear canal de respuesta pendiente
	responseChan := make(chan QueryResponse, 1)
	requestID := "test-request-6"
	
	handler.mu.Lock()
	handler.pendingQueries[requestID] = responseChan
	handler.mu.Unlock()
	
	// Crear respuesta
	response := QueryResponse{
		Type:      "response",
		RequestID: requestID,
		Path:      "height",
		Data:      json.RawMessage(`{"height": 10}`),
	}
	
	// Manejar respuesta
	handler.HandleResponse(response)
	
	// Verificar que la respuesta llegó
	select {
	case received := <-responseChan:
		if received.RequestID != requestID {
			t.Errorf("RequestID incorrecto: esperado %s, obtenido %s", requestID, received.RequestID)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout esperando respuesta")
	}
}

// TestQueryHandler_HandleResponse_NoPending prueba respuesta sin query pendiente
func TestQueryHandler_HandleResponse_NoPending(t *testing.T) {
	ctx := context.Background()
	
	testDir := "./test_data_response_none_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()
	
	handler := NewQueryHandler(ctx, db, nil, nil)
	
	// Respuesta sin query pendiente (no debería crashear)
	response := QueryResponse{
		Type:      "response",
		RequestID: "non-existent-request",
		Path:      "height",
		Data:      json.RawMessage(`{"height": 10}`),
	}
	
	// Esto no debería crashear
	handler.HandleResponse(response)
}

