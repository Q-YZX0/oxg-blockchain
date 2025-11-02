package network

import (
	"context"
	"fmt"
	"log"

	"github.com/Q-YZX0/oxy-blockchain/internal/consensus"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// P2PNetwork maneja la comunicación P2P usando oxygen-sdk mesh
type P2PNetwork struct {
	ctx          context.Context
	config       *Config
	consensus    *consensus.CometBFT
	meshBridge   *MeshBridge
	meshEndpoint string
	running      bool
}

// Config contiene la configuración de la red P2P
type Config struct {
	MeshEndpoint string
	PeerID       string
}

// NewP2PNetwork crea una nueva instancia de la red P2P
func NewP2PNetwork(
	ctx context.Context,
	config *Config,
	consensus *consensus.CometBFT,
	storage *storage.BlockchainDB,
) (*P2PNetwork, error) {
	
	// Crear mesh bridge con storage para query handler
	meshBridge := NewMeshBridge(ctx, consensus, config.MeshEndpoint, storage)
	
	n := &P2PNetwork{
		ctx:          ctx,
		config:       config,
		consensus:    consensus,
		meshBridge:   meshBridge,
		meshEndpoint: config.MeshEndpoint,
		running:      false,
	}

	log.Println("Red P2P inicializada")
	return n, nil
}

// Start inicia la red P2P
func (n *P2PNetwork) Start() error {
	if n.running {
		return fmt.Errorf("red P2P ya está corriendo")
	}

	// Iniciar mesh bridge
	if err := n.meshBridge.Start(); err != nil {
		return fmt.Errorf("error iniciando mesh bridge: %w", err)
	}

	n.running = true
	log.Println("✅ Red P2P iniciada")
	return nil
}

// Stop detiene la red P2P
func (n *P2PNetwork) Stop() error {
	if !n.running {
		return nil
	}

	// Detener mesh bridge
	if err := n.meshBridge.Stop(); err != nil {
		return fmt.Errorf("error deteniendo mesh bridge: %w", err)
	}

	n.running = false
	log.Println("⏹️  Red P2P detenida")
	return nil
}

// BroadcastTransaction transmite una transacción por la mesh
func (n *P2PNetwork) BroadcastTransaction(tx *consensus.Transaction) error {
	if !n.running {
		return fmt.Errorf("red P2P no está corriendo")
	}

	return n.meshBridge.BroadcastTransaction(tx)
}

// BroadcastBlock transmite un bloque por la mesh
func (n *P2PNetwork) BroadcastBlock(block *consensus.Block) error {
	if !n.running {
		return fmt.Errorf("red P2P no está corriendo")
	}

	return n.meshBridge.BroadcastBlock(block)
}

