package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"

	"github.com/Q-YZX0/oxy-blockchain/internal/config"
	"github.com/Q-YZX0/oxy-blockchain/internal/consensus"
	"github.com/Q-YZX0/oxy-blockchain/internal/execution"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
	"github.com/Q-YZX0/oxy-blockchain/internal/network"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Configuración
	cfg := config.LoadConfig()

	// Inicializar storage
	db, err := storage.NewBlockchainDB(cfg.DataDir)
	if err != nil {
		log.Fatalf("Error inicializando storage: %v", err)
	}
	defer db.Close()

	// Inicializar motor de ejecución (EVM)
	evm := execution.NewEVMExecutor(db)
	
	// Iniciar ejecutor EVM
	if err := evm.Start(); err != nil {
		log.Fatalf("Error iniciando ejecutor EVM: %v", err)
	}
	defer evm.Stop()

	// Inicializar conjunto de validadores
	minStake := big.NewInt(1000) // 1000 OXG mínimo
	minStake.Mul(minStake, big.NewInt(1e18)) // Multiplicar por 1e18 para decimales
	maxValidators := 100
	validators := consensus.NewValidatorSet(db, evm, minStake, maxValidators)
	
	// Cargar validadores guardados
	if err := validators.LoadValidators(); err != nil {
		log.Printf("Advertencia: error cargando validadores: %v", err)
	}

	// Inicializar consenso (CometBFT)
	consensusConfig := &consensus.Config{
		DataDir:       cfg.DataDir,
		ChainID:       cfg.ChainID,
		ValidatorAddr: cfg.ValidatorAddr,
		ValidatorKey:  cfg.ValidatorKey,
	}
	
	consensusEngine, err := consensus.NewCometBFT(ctx, consensusConfig, db, evm, validators)
	if err != nil {
		log.Fatalf("Error inicializando consenso: %v", err)
	}

	// Inicializar red P2P (integración con oxygen-sdk mesh)
	networkConfig := &network.Config{
		MeshEndpoint: cfg.MeshEndpoint,
		PeerID:       cfg.ValidatorAddr,
	}
	
	p2pNetwork, err := network.NewP2PNetwork(ctx, networkConfig, consensusEngine, db)
	if err != nil {
		log.Fatalf("Error inicializando red P2P: %v", err)
	}

	// Iniciar componentes
	if err := consensusEngine.Start(); err != nil {
		log.Fatalf("Error iniciando consenso: %v", err)
	}
	defer consensusEngine.Stop()

	if err := p2pNetwork.Start(); err != nil {
		log.Fatalf("Error iniciando red P2P: %v", err)
	}
	defer p2pNetwork.Stop()

	fmt.Println("✅ Oxy•gen Blockchain iniciada correctamente")

	// Manejar señales de terminación
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	fmt.Println("\n⏹️  Deteniendo Oxy•gen Blockchain...")
}



