package main

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Q-YZX0/oxy-blockchain/internal/api"
	"github.com/Q-YZX0/oxy-blockchain/internal/config"
	"github.com/Q-YZX0/oxy-blockchain/internal/consensus"
	"github.com/Q-YZX0/oxy-blockchain/internal/execution"
	"github.com/Q-YZX0/oxy-blockchain/internal/health"
	"github.com/Q-YZX0/oxy-blockchain/internal/logger"
	"github.com/Q-YZX0/oxy-blockchain/internal/metrics"
	"github.com/Q-YZX0/oxy-blockchain/internal/network"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

func main() {
	// Log inmediato para verificar que el proceso inicia
	fmt.Fprintf(os.Stdout, "[MAIN] Proceso testnet iniciado\n")
	os.Stdout.Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Configuración
	fmt.Fprintf(os.Stdout, "[MAIN] Cargando configuración...\n")
	os.Stdout.Sync()
	cfg := config.LoadConfig()

	fmt.Fprintf(os.Stdout, "[MAIN] Configuración cargada: APIEnabled=%v, APIPort=%s\n", cfg.APIEnabled, cfg.APIPort)
	os.Stdout.Sync()

	// Inicializar logger estructurado
	useJSON := os.Getenv("OXY_LOG_JSON") == "true"
	fmt.Fprintf(os.Stdout, "[MAIN] Inicializando logger (Level=%s, JSON=%v)...\n", cfg.LogLevel, useJSON)
	os.Stdout.Sync()
	logger.Init(cfg.LogLevel, useJSON)

	// Inicializar health checker y métricas
	fmt.Fprintf(os.Stdout, "[MAIN] Inicializando health checker y métricas...\n")
	os.Stdout.Sync()
	healthChecker := health.NewHealthChecker()
	metricsInstance := metrics.NewMetrics()

	// Inicializar storage
	fmt.Fprintf(os.Stdout, "[MAIN] Inicializando storage (DataDir=%s)...\n", cfg.DataDir)
	os.Stdout.Sync()
	fmt.Fprintf(os.Stdout, "[MAIN] Llamando a storage.NewBlockchainDB()...\n")
	os.Stdout.Sync()
	db, err := storage.NewBlockchainDB(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[MAIN] ERROR inicializando storage: %v\n", err)
		os.Stderr.Sync()
		logger.Fatalf("Error inicializando storage: %v", err)
	}
	fmt.Fprintf(os.Stdout, "[MAIN] storage.NewBlockchainDB() completado exitosamente\n")
	os.Stdout.Sync()
	defer db.Close()

	fmt.Fprintf(os.Stdout, "[MAIN] Después de defer db.Close()\n")
	os.Stdout.Sync()

	// Reportar estado del storage al health checker
	fmt.Fprintf(os.Stdout, "[MAIN] Llamando a healthChecker.SetStorageHealth(true)...\n")
	os.Stdout.Sync()
	healthChecker.SetStorageHealth(true)
	fmt.Fprintf(os.Stdout, "[MAIN] healthChecker.SetStorageHealth() completado\n")
	os.Stdout.Sync()

	// Inicializar motor de ejecución (EVM)
	fmt.Fprintf(os.Stdout, "[MAIN] Inicializando ejecutor EVM...\n")
	os.Stdout.Sync()
	fmt.Fprintf(os.Stdout, "[MAIN] Llamando a execution.NewEVMExecutor(db)...\n")
	os.Stdout.Sync()
	evm := execution.NewEVMExecutor(db)
	fmt.Fprintf(os.Stdout, "[MAIN] execution.NewEVMExecutor() completado\n")
	os.Stdout.Sync()

	// Iniciar ejecutor EVM
	fmt.Fprintf(os.Stdout, "[MAIN] Llamando a evm.Start()...\n")
	os.Stdout.Sync()
	if err := evm.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[MAIN] ERROR iniciando ejecutor EVM: %v\n", err)
		os.Stderr.Sync()
		logger.Fatalf("Error iniciando ejecutor EVM: %v", err)
	}
	fmt.Fprintf(os.Stdout, "[MAIN] Ejecutor EVM iniciado exitosamente\n")
	os.Stdout.Sync()
	defer evm.Stop()

	// Reportar estado del EVM al health checker
	healthChecker.SetEVMHealth(true)

	// Inicializar conjunto de validadores
	// 1000 OXG mínimo (con 18 decimales) = 1000 * 10^18
	// Para testnet, usar minStake más bajo (10 OXG en lugar de 1000 OXG)
	// Esto permite que validadores con power=10 (10 OXG) sean válidos
	minStakeValue := os.Getenv("OXY_MIN_STAKE")
	if minStakeValue == "" {
		// Default para testnet: 10 OXG (1000 OXG para producción)
		minStakeValue = "10"
	}
	minStakeInt, _ := new(big.Int).SetString(minStakeValue, 10)
	minStake := new(big.Int).Mul(minStakeInt, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	fmt.Fprintf(os.Stdout, "[MAIN] minStake configurado: %s OXG\n", minStakeValue)
	os.Stdout.Sync()
	maxValidators := 100
	validators := consensus.NewValidatorSet(db, evm, minStake, maxValidators)

	// Cargar validadores guardados
	if err := validators.LoadValidators(); err != nil {
		logger.Warnf("Error cargando validadores: %v", err)
	}

	// Inicializar consenso (CometBFT)
	fmt.Fprintf(os.Stdout, "[MAIN] Inicializando CometBFT (DataDir=%s, ChainID=%s)...\n", cfg.DataDir, cfg.ChainID)
	os.Stdout.Sync()
	consensusConfig := &consensus.Config{
		DataDir:       cfg.DataDir,
		ChainID:       cfg.ChainID,
		ValidatorAddr: cfg.ValidatorAddr,
		ValidatorKey:  cfg.ValidatorKey,
	}

	fmt.Fprintf(os.Stdout, "[MAIN] Llamando a consensus.NewCometBFT()...\n")
	os.Stdout.Sync()
	consensusEngine, err := consensus.NewCometBFT(ctx, consensusConfig, db, evm, validators)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[MAIN] ERROR inicializando consenso: %v\n", err)
		os.Stderr.Sync()
		logger.Fatalf("Error inicializando consenso: %v", err)
	}
	fmt.Fprintf(os.Stdout, "[MAIN] CometBFT inicializado exitosamente\n")
	os.Stdout.Sync()
	
	// Conectar métricas al consenso para actualizarlas automáticamente
	consensusEngine.SetMetrics(metricsInstance)
	fmt.Fprintf(os.Stdout, "[MAIN] Métricas conectadas al consenso\n")
	os.Stdout.Sync()

	// Reportar estado del consenso al health checker
	healthChecker.SetConsensusHealth(true)

	// Inicializar red P2P (integración con oxygen-sdk mesh)
	fmt.Fprintf(os.Stdout, "[MAIN] Inicializando red P2P (MeshEndpoint=%s)...\n", cfg.MeshEndpoint)
	os.Stdout.Sync()
	networkConfig := &network.Config{
		MeshEndpoint: cfg.MeshEndpoint,
		PeerID:       cfg.ValidatorAddr,
	}

	fmt.Fprintf(os.Stdout, "[MAIN] Llamando a network.NewP2PNetwork()...\n")
	os.Stdout.Sync()
	p2pNetwork, err := network.NewP2PNetwork(ctx, networkConfig, consensusEngine, db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[MAIN] ERROR inicializando red P2P: %v\n", err)
		os.Stderr.Sync()
		logger.Fatalf("Error inicializando red P2P: %v", err)
	}
	fmt.Fprintf(os.Stdout, "[MAIN] Red P2P inicializada exitosamente\n")
	os.Stdout.Sync()

	// Iniciar componentes
	fmt.Fprintf(os.Stdout, "[MAIN] Iniciando consensusEngine.Start()...\n")
	os.Stdout.Sync()
	if err := consensusEngine.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[MAIN] ERROR iniciando consenso: %v\n", err)
		os.Stderr.Sync()
		logger.Fatalf("Error iniciando consenso: %v", err)
	}
	fmt.Fprintf(os.Stdout, "[MAIN] consensusEngine.Start() completado\n")
	os.Stdout.Sync()
	defer consensusEngine.Stop()

	fmt.Fprintf(os.Stdout, "[MAIN] Iniciando p2pNetwork.Start()...\n")
	os.Stdout.Sync()
	if err := p2pNetwork.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[MAIN] ERROR iniciando red P2P: %v\n", err)
		os.Stderr.Sync()
		logger.Fatalf("Error iniciando red P2P: %v", err)
	}
	fmt.Fprintf(os.Stdout, "[MAIN] p2pNetwork.Start() completado\n")
	os.Stdout.Sync()
	defer p2pNetwork.Stop()

	// Reportar estado de la mesh network al health checker
	healthChecker.SetMeshHealth(true)

	// Iniciar servidor REST si está habilitado
	logger.Infof("Configuración API REST: APIEnabled=%v, APIPort=%s, APIHost=%s", cfg.APIEnabled, cfg.APIPort, cfg.APIHost)
	var restServer *api.RestServer
	if cfg.APIEnabled {
		restServer = api.NewRestServer(
			cfg.APIHost,
			cfg.APIPort,
			db,
			consensusEngine,
			healthChecker,
			metricsInstance,
			evm,
		)

		// Iniciar servidor REST en goroutine
		go func() {
			fmt.Fprintf(os.Stdout, "[MAIN] Goroutine API REST iniciada\n")
			os.Stdout.Sync()
			logger.Infof("Iniciando servidor REST local en %s:%s", cfg.APIHost, cfg.APIPort)
			// Dar un pequeño delay para asegurar que todo esté inicializado
			time.Sleep(500 * time.Millisecond)
			fmt.Fprintf(os.Stdout, "[MAIN] Llamando a restServer.Start()\n")
			os.Stdout.Sync()
			if err := restServer.Start(); err != nil && err != http.ErrServerClosed {
				fmt.Fprintf(os.Stderr, "[MAIN] ERROR: restServer.Start() retornó error: %v\n", err)
				os.Stderr.Sync()
				logger.Errorf("Error iniciando servidor REST: %v", err)
				logger.Errorf("Detalles del error: tipo=%T, error=%v", err, err)
			} else if err == http.ErrServerClosed {
				fmt.Fprintf(os.Stdout, "[MAIN] Servidor cerrado correctamente\n")
				os.Stdout.Sync()
			} else {
				// ListenAndServe nunca debería retornar nil a menos que se cierre el servidor
				fmt.Fprintf(os.Stdout, "[MAIN] restServer.Start() retornó sin error (puede estar bloqueado)\n")
				os.Stdout.Sync()
			}
		}()

		defer func() {
			if restServer != nil {
				if err := restServer.Stop(); err != nil {
					logger.Errorf("Error deteniendo servidor REST: %v", err)
				}
			}
		}()
	}

	logger.Info("Oxy•gen Blockchain iniciada correctamente")

	// Manejar señales de terminación
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("Deteniendo Oxy•gen Blockchain...")
}
