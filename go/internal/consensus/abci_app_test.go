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
	// Limpiar directorio de Pebble DB si existe
	pebbleDir := filepath.Join(dir, "evm_state")
	if err := os.RemoveAll(pebbleDir); err != nil {
		return fmt.Errorf("error limpiando directorio Pebble DB: %w", err)
	}
	
	// Limpiar todo el directorio
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("error limpiando directorio de test: %w", err)
	}
	
	return nil
}

// createTestDir crea un directorio único de test con timestamp en un directorio temporal del sistema
func createTestDir(testName string) string {
	timestamp := time.Now().UnixNano()
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, "oxy_blockchain_test", fmt.Sprintf("test_data_%s_%d", testName, timestamp))
}

// TestABCIApp_BasicFlow prueba el flujo básico de ABCI
func TestABCIApp_BasicFlow(t *testing.T) {
	ctx := context.Background()
	
	// Limpiar directorio de test si existe de una ejecución anterior
	testDir := createTestDir("basic")
	defer func() {
		if err := cleanupTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio de test: %v", err)
		}
	}()
	
	// Asegurar que el directorio esté limpio antes de empezar
	if err := cleanupTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando directorio antes del test: %v", err)
	}
	
	// Crear storage temporal con directorio único
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()

	// Crear ejecutor EVM (nueva API: solo requiere storage)
	evm := execution.NewEVMExecutor(db)
	if err := evm.Start(); err != nil {
		// Si falla por problemas de Pebble DB, limpiar y reintentar
		if err := cleanupTestDir(testDir); err == nil {
			db, err = storage.NewBlockchainDB(testDir)
			if err == nil {
				evm = execution.NewEVMExecutor(db)
				if err := evm.Start(); err != nil {
					t.Fatalf("Error iniciando EVM después de limpieza: %v", err)
				}
			}
		} else {
			t.Fatalf("Error iniciando EVM: %v", err)
		}
	}
	defer func() {
		if err := evm.Stop(); err != nil {
			t.Logf("Advertencia: error deteniendo EVM: %v", err)
		}
	}()

	// Crear ABCI app
	app := NewABCIApp(db, evm, nil, "test-chain")

	// Probar InitChain (nueva API v1.0.1)
	initChainReq := &abcitypes.InitChainRequest{
		ChainId: "test-chain",
		Validators: []abcitypes.ValidatorUpdate{},
		Time: time.Now(),
	}
	resp, err := app.InitChain(ctx, initChainReq)
	if err != nil {
		t.Fatalf("Error en InitChain: %v", err)
	}
	if resp.Validators == nil {
		t.Error("InitChain debe retornar validadores")
	}

	// Probar Info (nueva API v1.0.1)
	infoReq := &abcitypes.InfoRequest{}
	infoResp, err := app.Info(ctx, infoReq)
	if err != nil {
		t.Fatalf("Error en Info: %v", err)
	}
	if infoResp.Data == "" {
		t.Error("Info debe retornar datos")
	}

	// Probar FinalizeBlock (reemplaza BeginBlock + DeliverTx + EndBlock en nueva API v1.0.1)
	finalizeBlockReq := &abcitypes.FinalizeBlockRequest{
		Height: 1,
		Time: time.Now(),
		Txs: [][]byte{},
	}
	finalizeBlockResp, err := app.FinalizeBlock(ctx, finalizeBlockReq)
	if err != nil {
		t.Fatalf("Error en FinalizeBlock: %v", err)
	}
	if finalizeBlockResp.TxResults == nil {
		t.Error("FinalizeBlock debe retornar resultados de transacciones")
	}

	// Probar Commit (nueva API v1.0.1)
	commitReq := &abcitypes.CommitRequest{}
	commitResp, err := app.Commit(ctx, commitReq)
	if err != nil {
		t.Fatalf("Error en Commit: %v", err)
	}
	if commitResp == nil {
		t.Error("Commit debe retornar respuesta")
	}
}

// TestABCIApp_CheckTx prueba validación de transacciones
func TestABCIApp_CheckTx(t *testing.T) {
	ctx := context.Background()
	
	// Limpiar directorio de test si existe de una ejecución anterior
	testDir := createTestDir("checktx")
	defer func() {
		if err := cleanupTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio de test: %v", err)
		}
	}()
	
	// Asegurar que el directorio esté limpio antes de empezar
	if err := cleanupTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando directorio antes del test: %v", err)
	}
	
	// Crear storage temporal con directorio único para evitar conflictos
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()

	// Crear ejecutor EVM (nueva API: solo requiere storage)
	evm := execution.NewEVMExecutor(db)
	if err := evm.Start(); err != nil {
		// Si falla por problemas de Pebble DB, limpiar y reintentar
		if err := cleanupTestDir(testDir); err == nil {
			db, err = storage.NewBlockchainDB(testDir)
			if err == nil {
				evm = execution.NewEVMExecutor(db)
				if err := evm.Start(); err != nil {
					t.Fatalf("Error iniciando EVM después de limpieza: %v", err)
				}
			}
		} else {
			t.Fatalf("Error iniciando EVM: %v", err)
		}
	}
	defer func() {
		if err := evm.Stop(); err != nil {
			t.Logf("Advertencia: error deteniendo EVM: %v", err)
		}
	}()

	// Crear ABCI app
	app := NewABCIApp(db, evm, nil, "test-chain")

	// Generar clave privada para testing
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}

	fromAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Crear transacción de prueba
	tx := Transaction{
		From:     fromAddr.Hex(),
		To:       common.HexToAddress("0x0000000000000000000000000000000000000000").Hex(),
		Value:    "1000000000000000000", // 1 OXG
		GasLimit: 21000,
		GasPrice: "1000000000", // 1 gwei
		Nonce:    0,
		Hash:     "0x1234567890abcdef",
		Signature: []byte{},
	}

	// Serializar transacción
	txData, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Error serializando transacción: %v", err)
	}

	// Probar CheckTx (nueva API v1.0.1)
	checkTxReq := &abcitypes.CheckTxRequest{
		Tx: txData,
	}

	checkTxResp, err := app.CheckTx(ctx, checkTxReq)
	if err != nil {
		t.Fatalf("Error en CheckTx: %v", err)
	}

	// La transacción debería ser rechazada por falta de firma válida
	if checkTxResp.Code == 0 {
		t.Error("CheckTx debería rechazar transacción sin firma válida")
	}
}

// TestABCIApp_Query prueba el sistema de queries
func TestABCIApp_Query(t *testing.T) {
	ctx := context.Background()
	
	// Limpiar directorio de test si existe de una ejecución anterior
	testDir := createTestDir("query")
	defer func() {
		if err := cleanupTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio de test: %v", err)
		}
	}()
	
	// Asegurar que el directorio esté limpio antes de empezar
	if err := cleanupTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando directorio antes del test: %v", err)
	}
	
	// Crear storage temporal con directorio único
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()

	// Crear ejecutor EVM (nueva API: solo requiere storage)
	evm := execution.NewEVMExecutor(db)
	if err := evm.Start(); err != nil {
		// Si falla por problemas de Pebble DB, limpiar y reintentar
		if err := cleanupTestDir(testDir); err == nil {
			db, err = storage.NewBlockchainDB(testDir)
			if err == nil {
				evm = execution.NewEVMExecutor(db)
				if err := evm.Start(); err != nil {
					t.Fatalf("Error iniciando EVM después de limpieza: %v", err)
				}
			}
		} else {
			t.Fatalf("Error iniciando EVM: %v", err)
		}
	}
	defer func() {
		if err := evm.Stop(); err != nil {
			t.Logf("Advertencia: error deteniendo EVM: %v", err)
		}
	}()

	// Crear ABCI app
	app := NewABCIApp(db, evm, nil, "test-chain")

	// Probar query de altura (nueva API v1.0.1)
	queryReq := &abcitypes.QueryRequest{
		Path: "height",
	}

	queryResp, err := app.Query(ctx, queryReq)
	if err != nil {
		t.Fatalf("Error en Query: %v", err)
	}
	if queryResp.Code != 0 {
		t.Errorf("Query de altura falló: %s", queryResp.Log)
	}

	// Probar query de balance (nueva API v1.0.1)
	queryReq.Path = "balance/0x0000000000000000000000000000000000000000"
	queryResp, err = app.Query(ctx, queryReq)
	if err != nil {
		t.Fatalf("Error en Query: %v", err)
	}
	if queryResp.Code != 0 {
		t.Errorf("Query de balance falló: %s", queryResp.Log)
	}
}

