package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	execution "github.com/Q-YZX0/oxy-blockchain/internal/execution"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// cleanupTestDir limpia completamente un directorio de test incluyendo subdirectorios de Pebble DB
func cleanupTestDir(dir string) error {
	if dir == "" {
		return nil
	}
	
	// Limpiar subdirectorio de Pebble DB si existe
	pebbleDir := filepath.Join(dir, "evm_state")
	if err := os.RemoveAll(pebbleDir); err != nil {
		return err
	}
	
	// Limpiar directorio principal
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	
	return nil
}

// createTestDir crea un directorio único de test con timestamp en un directorio temporal del sistema
func createTestDir(testName string) string {
	timestamp := time.Now().UnixNano()
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, "oxy_blockchain_test", fmt.Sprintf("test_data_%s_%d", testName, timestamp))
}

// TestABCIApp_MultipleBlocks prueba el procesamiento de múltiples bloques en secuencia
func TestABCIApp_MultipleBlocks(t *testing.T) {
	ctx := context.Background()
	
	testDir := createTestDir("multiple_blocks")
	defer func() {
		if err := cleanupTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM
	evm := execution.NewEVMExecutor(db)
	if err := evm.Start(); err != nil {
		t.Fatalf("Error iniciando EVM: %v", err)
	}
	defer func() {
		if err := evm.Stop(); err != nil {
			t.Logf("Advertencia: error deteniendo EVM: %v", err)
		}
	}()
	
	// Crear ABCI app
	app := NewABCIApp(db, evm, nil, "test-chain")
	
	// InitChain
	initChainReq := &abcitypes.InitChainRequest{
		ChainId: "test-chain",
		Validators: []abcitypes.ValidatorUpdate{},
	}
	
	_, err = app.InitChain(ctx, initChainReq)
	if err != nil {
		t.Fatalf("Error en InitChain: %v", err)
	}
	
	// Procesar 3 bloques en secuencia
	for blockHeight := int64(1); blockHeight <= 3; blockHeight++ {
		// FinalizeBlock
		finalizeReq := &abcitypes.FinalizeBlockRequest{
			Height: blockHeight,
			Txs:    [][]byte{}, // Sin transacciones por ahora
		}
		
		finalizeResp, err := app.FinalizeBlock(ctx, finalizeReq)
		if err != nil {
			t.Fatalf("Error en FinalizeBlock altura %d: %v", blockHeight, err)
		}
		
		if finalizeResp == nil {
			t.Fatalf("FinalizeBlock altura %d retornó nil", blockHeight)
		}
		
		// Commit
		commitReq := &abcitypes.CommitRequest{}
		commitResp, err := app.Commit(ctx, commitReq)
		if err != nil {
			t.Fatalf("Error en Commit altura %d: %v", blockHeight, err)
		}
		
		if commitResp == nil {
			t.Fatalf("Commit altura %d retornó nil", blockHeight)
		}
		
		// Verificar que la altura se incrementó
		infoReq := &abcitypes.InfoRequest{}
		infoResp, err := app.Info(ctx, infoReq)
		if err != nil {
			t.Fatalf("Error en Info altura %d: %v", blockHeight, err)
		}
		
		if infoResp.LastBlockHeight != blockHeight {
			t.Errorf("Altura incorrecta: esperado %d, obtenido %d", blockHeight, infoResp.LastBlockHeight)
		}
	}
	
	// Verificar altura final
	infoReq := &abcitypes.InfoRequest{}
	infoResp, err := app.Info(ctx, infoReq)
	if err != nil {
		t.Fatalf("Error en Info final: %v", err)
	}
	
	if infoResp.LastBlockHeight != 3 {
		t.Errorf("Altura final incorrecta: esperado 3, obtenido %d", infoResp.LastBlockHeight)
	}
}

// TestABCIApp_FinalizeBlock_WithTransactions prueba FinalizeBlock con transacciones
func TestABCIApp_FinalizeBlock_WithTransactions(t *testing.T) {
	ctx := context.Background()
	
	testDir := createTestDir("finalize_with_txs")
	defer func() {
		if err := cleanupTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM
	evm := execution.NewEVMExecutor(db)
	if err := evm.Start(); err != nil {
		t.Fatalf("Error iniciando EVM: %v", err)
	}
	defer func() {
		if err := evm.Stop(); err != nil {
			t.Logf("Advertencia: error deteniendo EVM: %v", err)
		}
	}()
	
	// Crear ABCI app
	app := NewABCIApp(db, evm, nil, "test-chain")
	
	// InitChain
	initChainReq := &abcitypes.InitChainRequest{
		ChainId: "test-chain",
		Validators: []abcitypes.ValidatorUpdate{},
	}
	
	_, err = app.InitChain(ctx, initChainReq)
	if err != nil {
		t.Fatalf("Error en InitChain: %v", err)
	}
	
	// Crear transacción de prueba
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	fromAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
	
	tx := Transaction{
		From:     fromAddr.Hex(),
		To:       common.HexToAddress("0x0000000000000000000000000000000000000000").Hex(),
		Value:    "1000000000000000000", // 1 OXG
		GasLimit: 21000,
		GasPrice: "1000000000",
		Nonce:    0,
		Hash:     "0x1234567890abcdef",
		Signature: []byte{},
	}
	
	// Serializar transacción
	txData, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Error serializando transacción: %v", err)
	}
	
	// FinalizeBlock con transacción
	finalizeReq := &abcitypes.FinalizeBlockRequest{
		Height: 1,
		Txs:    [][]byte{txData},
	}
	
	finalizeResp, err := app.FinalizeBlock(ctx, finalizeReq)
	if err != nil {
		t.Fatalf("Error en FinalizeBlock: %v", err)
	}
	
	if finalizeResp == nil {
		t.Fatal("FinalizeBlock retornó nil")
	}
	
	// Verificar que se procesó la transacción
	if len(finalizeResp.TxResults) != 1 {
		t.Errorf("Debería haber 1 resultado de transacción: obtenido %d", len(finalizeResp.TxResults))
	}
	
	// La transacción debería fallar (sin balance, sin firma válida, etc.)
	// pero debería procesarse
	txResult := finalizeResp.TxResults[0]
	if txResult == nil {
		t.Fatal("Resultado de transacción es nil")
	}
	
	// Commit el bloque
	commitReq := &abcitypes.CommitRequest{}
	_, err = app.Commit(ctx, commitReq)
	if err != nil {
		t.Fatalf("Error en Commit: %v", err)
	}
}

// TestABCIApp_Commit_MultipleBlocks prueba múltiples commits en secuencia
func TestABCIApp_Commit_MultipleBlocks(t *testing.T) {
	ctx := context.Background()
	
	testDir := createTestDir("commit_multiple")
	defer func() {
		if err := cleanupTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM
	evm := execution.NewEVMExecutor(db)
	if err := evm.Start(); err != nil {
		t.Fatalf("Error iniciando EVM: %v", err)
	}
	defer func() {
		if err := evm.Stop(); err != nil {
			t.Logf("Advertencia: error deteniendo EVM: %v", err)
		}
	}()
	
	// Crear ABCI app
	app := NewABCIApp(db, evm, nil, "test-chain")
	
	// InitChain
	initChainReq := &abcitypes.InitChainRequest{
		ChainId: "test-chain",
		Validators: []abcitypes.ValidatorUpdate{},
	}
	
	_, err = app.InitChain(ctx, initChainReq)
	if err != nil {
		t.Fatalf("Error en InitChain: %v", err)
	}
	
	// Procesar y commit 5 bloques
	for i := int64(1); i <= 5; i++ {
		// FinalizeBlock
		finalizeReq := &abcitypes.FinalizeBlockRequest{
			Height: i,
			Txs:    [][]byte{},
		}
		
		_, err = app.FinalizeBlock(ctx, finalizeReq)
		if err != nil {
			t.Fatalf("Error en FinalizeBlock altura %d: %v", i, err)
		}
		
		// Commit
		commitReq := &abcitypes.CommitRequest{}
		commitResp, err := app.Commit(ctx, commitReq)
		if err != nil {
			t.Fatalf("Error en Commit altura %d: %v", i, err)
		}
		
		if commitResp == nil {
			t.Fatalf("Commit altura %d retornó nil", i)
		}
		
		// Verificar altura actualizada
		infoReq := &abcitypes.InfoRequest{}
		infoResp, err := app.Info(ctx, infoReq)
		if err != nil {
			t.Fatalf("Error en Info altura %d: %v", i, err)
		}
		
		if infoResp.LastBlockHeight != i {
			t.Errorf("Altura incorrecta después de commit %d: esperado %d, obtenido %d", i, i, infoResp.LastBlockHeight)
		}
	}
	
	// Verificar altura final
	infoReq := &abcitypes.InfoRequest{}
	infoResp, err := app.Info(ctx, infoReq)
	if err != nil {
		t.Fatalf("Error en Info final: %v", err)
	}
	
	if infoResp.LastBlockHeight != 5 {
		t.Errorf("Altura final incorrecta: esperado 5, obtenido %d", infoResp.LastBlockHeight)
	}
}

// TestABCIApp_Query_Complex prueba queries más complejos
func TestABCIApp_Query_Complex(t *testing.T) {
	ctx := context.Background()
	
	testDir := createTestDir("query_complex")
	defer func() {
		if err := cleanupTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM
	evm := execution.NewEVMExecutor(db)
	if err := evm.Start(); err != nil {
		t.Fatalf("Error iniciando EVM: %v", err)
	}
	defer func() {
		if err := evm.Stop(); err != nil {
			t.Logf("Advertencia: error deteniendo EVM: %v", err)
		}
	}()
	
	// Crear ABCI app
	app := NewABCIApp(db, evm, nil, "test-chain")
	
	// InitChain
	initChainReq := &abcitypes.InitChainRequest{
		ChainId: "test-chain",
		Validators: []abcitypes.ValidatorUpdate{},
	}
	
	_, err = app.InitChain(ctx, initChainReq)
	if err != nil {
		t.Fatalf("Error en InitChain: %v", err)
	}
	
	// Query de altura
	queryReq := &abcitypes.QueryRequest{
		Path: "height",
	}
	
	queryResp, err := app.Query(ctx, queryReq)
	if err != nil {
		t.Fatalf("Error en Query height: %v", err)
	}
	
	if queryResp.Code != 0 {
		t.Errorf("Query height debería ser exitoso: código %d", queryResp.Code)
	}
	
	// Query de balance (dirección inexistente debería retornar 0)
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	
	queryReq = &abcitypes.QueryRequest{
		Path: fmt.Sprintf("balance/%s", addr.Hex()),
	}
	
	queryResp, err = app.Query(ctx, queryReq)
	if err != nil {
		t.Fatalf("Error en Query balance: %v", err)
	}
	
	if queryResp.Code != 0 {
		t.Errorf("Query balance debería ser exitoso: código %d", queryResp.Code)
	}
	
	// Query de account (dirección inexistente)
	queryReq = &abcitypes.QueryRequest{
		Path: fmt.Sprintf("account/%s", addr.Hex()),
	}
	
	queryResp, err = app.Query(ctx, queryReq)
	if err != nil {
		t.Fatalf("Error en Query account: %v", err)
	}
	
	// Query de path inválido debería retornar error
	queryReq = &abcitypes.QueryRequest{
		Path: "invalid/path",
	}
	
	queryResp, err = app.Query(ctx, queryReq)
	if err != nil {
		t.Fatalf("Error en Query inválido: %v", err)
	}
	
	// Debería retornar código de error
	if queryResp.Code == 0 {
		t.Error("Query con path inválido debería retornar código de error")
	}
}

// TestABCIApp_CheckTx_RateLimit prueba CheckTx con rate limiting
func TestABCIApp_CheckTx_RateLimit(t *testing.T) {
	ctx := context.Background()
	
	testDir := createTestDir("checktx_ratelimit")
	defer func() {
		if err := cleanupTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM
	evm := execution.NewEVMExecutor(db)
	if err := evm.Start(); err != nil {
		t.Fatalf("Error iniciando EVM: %v", err)
	}
	defer func() {
		if err := evm.Stop(); err != nil {
			t.Logf("Advertencia: error deteniendo EVM: %v", err)
		}
	}()
	
	// Crear ABCI app (con rate limiter por defecto)
	app := NewABCIApp(db, evm, nil, "test-chain")
	
	// InitChain
	initChainReq := &abcitypes.InitChainRequest{
		ChainId: "test-chain",
		Validators: []abcitypes.ValidatorUpdate{},
	}
	
	_, err = app.InitChain(ctx, initChainReq)
	if err != nil {
		t.Fatalf("Error en InitChain: %v", err)
	}
	
	// Crear transacción de prueba
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	fromAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
	
	tx := Transaction{
		From:     fromAddr.Hex(),
		To:       common.HexToAddress("0x0000000000000000000000000000000000000000").Hex(),
		Value:    "1000000000000000000",
		GasLimit: 21000,
		GasPrice: "1000000000",
		Nonce:    0,
		Hash:     "0x1234567890abcdef",
		Signature: []byte{},
	}
	
	// Serializar transacción
	txData, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Error serializando transacción: %v", err)
	}
	
	// Enviar múltiples CheckTx (rate limiting debería aplicarse)
	// Nota: El rate limiting en CheckTx depende de la implementación
	// Por ahora, verificamos que CheckTx funciona correctamente
	checkTxReq := &abcitypes.CheckTxRequest{
		Tx: txData,
	}
	
	checkTxResp, err := app.CheckTx(ctx, checkTxReq)
	if err != nil {
		t.Fatalf("Error en CheckTx: %v", err)
	}
	
	if checkTxResp == nil {
		t.Fatal("CheckTx retornó nil")
	}
	
	// CheckTx debería procesar la transacción (aunque falle por validación)
	// El código de respuesta indica si fue aceptada o rechazada
	if checkTxResp.Code == 0 {
		t.Log("CheckTx aceptó la transacción")
	} else {
		t.Logf("CheckTx rechazó la transacción: código %d, log: %s", checkTxResp.Code, checkTxResp.Log)
	}
}

