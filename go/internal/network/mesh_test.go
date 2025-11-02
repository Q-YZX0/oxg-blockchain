package network

import (
	"context"
	"os"
	"testing"

	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// TestMeshBridgeBasic verifica la creación básica del bridge
func TestMeshBridgeBasic(t *testing.T) {
	ctx := context.Background()

	// Crear storage temporal para el test
	testDir := "./test_data_mesh_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()

	// Crear mesh bridge con firma actualizada (requiere consensus y storage)
	meshBridge := NewMeshBridge(ctx, nil, "ws://localhost:3001", db)

	if meshBridge == nil {
		t.Fatal("NewMeshBridge no debería retornar nil")
	}

	if meshBridge.running {
		t.Error("MeshBridge no debería estar corriendo al inicio")
	}
}

// TestMeshBridgeStartStop verifica el inicio y detención del bridge
func TestMeshBridgeStartStop(t *testing.T) {
	ctx := context.Background()

	// Crear storage temporal
	testDir := "./test_data_mesh_stop_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()

	meshBridge := NewMeshBridge(ctx, nil, "ws://localhost:3001", db)

	// Nota: Este test fallará si no hay servidor WebSocket en localhost:3001
	// Por ahora, solo verificamos que el método existe y puede ser llamado
	err = meshBridge.Start()
	if err != nil {
		// Esperado si no hay servidor WebSocket
		t.Logf("Error esperado al iniciar (no hay servidor): %v", err)
	}

	err = meshBridge.Stop()
	if err != nil {
		t.Errorf("Error al detener bridge: %v", err)
	}
}

// TestMeshBridgeReconnect verifica la lógica de reconexión
func TestMeshBridgeReconnect(t *testing.T) {
	ctx := context.Background()

	// Crear storage temporal
	testDir := "./test_data_mesh_reconnect_" + t.Name()
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		db.Close()
		os.RemoveAll(testDir)
	}()

	meshBridge := NewMeshBridge(ctx, nil, "ws://localhost:3001", db)

	// Simular reconexión (sin servidor real, solo verificar lógica)
	meshBridge.running = true
	meshBridge.topics["transactions"] = true

	// La función reconnect intentará reconectar
	// Como no hay servidor, fallará pero no debería crashear
	meshBridge.reconnect()

	// Si llegamos aquí, la función no crasheó
}
