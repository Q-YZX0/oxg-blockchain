package consensus

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Q-YZX0/oxy-blockchain/internal/execution"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
	cometcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cometlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/types"
)

// CometBFTNode maneja el nodo CometBFT
type CometBFTNode struct {
	node    *node.Node
	abciApp *ABCIApp
	config  *Config
	running bool
}

// NewCometBFTNode crea una nueva instancia del nodo CometBFT
func NewCometBFTNode(
	ctx context.Context,
	cfg *Config,
	storage *storage.BlockchainDB,
	executor *execution.EVMExecutor,
	validators *ValidatorSet,
) (*CometBFTNode, error) {

	// Crear aplicación ABCI con validators
	// Nota: El mempool se establecerá después de crear CometBFT completo
	abciApp := NewABCIApp(storage, executor, validators, cfg.ChainID)

	// Crear configuración de CometBFT
	cometConfig := cometcfg.DefaultConfig()
	cometConfig.SetRoot(filepath.Join(cfg.DataDir, "cometbft"))

	// Configurar para crear bloques vacíos automáticamente (importante para testnet)
	cometConfig.Consensus.CreateEmptyBlocks = true
	cometConfig.Consensus.CreateEmptyBlocksInterval = 1 * time.Second // Crear bloques vacíos cada segundo
	
	// Configurar peers persistentes si se proporcionan
	if persistentPeers := os.Getenv("OXY_PERSISTENT_PEERS"); persistentPeers != "" {
		cometConfig.P2P.PersistentPeers = persistentPeers
		fmt.Fprintf(os.Stdout, "[CometBFT] PersistentPeers configurados: %s\n", persistentPeers)
		os.Stdout.Sync()
	}
	
	// Configurar seeds si se proporcionan
	if seeds := os.Getenv("OXY_SEEDS"); seeds != "" {
		cometConfig.P2P.Seeds = seeds
		fmt.Fprintf(os.Stdout, "[CometBFT] Seeds configurados: %s\n", seeds)
		os.Stdout.Sync()
	}

	// Asegurar que el directorio existe
	if err := os.MkdirAll(cometConfig.RootDir, 0755); err != nil {
		return nil, fmt.Errorf("error creando directorio CometBFT: %w", err)
	}

	// Inicializar CometBFT si no existe
	fmt.Fprintf(os.Stdout, "[CometBFT] Verificando si está inicializado...\n")
	os.Stdout.Sync()

	genesisFile := filepath.Join(cometConfig.RootDir, "config", "genesis.json")
	keyFile := cometConfig.PrivValidatorKeyFile()
	stateFile := cometConfig.PrivValidatorStateFile()

	genesisExists := false
	keyExists := false

	if _, err := os.Stat(genesisFile); err == nil {
		genesisExists = true
		fmt.Fprintf(os.Stdout, "[CometBFT] genesis.json existe\n")
	} else {
		fmt.Fprintf(os.Stdout, "[CometBFT] genesis.json NO existe\n")
	}

	if _, err := os.Stat(keyFile); err == nil {
		keyExists = true
		fmt.Fprintf(os.Stdout, "[CometBFT] priv_validator_key.json existe\n")
	} else {
		fmt.Fprintf(os.Stdout, "[CometBFT] priv_validator_key.json NO existe: %s\n", keyFile)
	}
	os.Stdout.Sync()

	if !genesisExists || !keyExists {
		fmt.Fprintf(os.Stdout, "[CometBFT] No está completamente inicializado, inicializando...\n")
		os.Stdout.Sync()

		// Si solo falta genesis, crear configuración completa
		// Si solo falta keys, generar solo las claves
		if !keyExists {
			fmt.Fprintf(os.Stdout, "[CometBFT] Generando claves faltantes...\n")
			os.Stdout.Sync()
			if err := generateKeys(cometConfig); err != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ERROR generando claves: %v\n", err)
				os.Stderr.Sync()
				return nil, fmt.Errorf("error generando claves: %w", err)
			}
		}

		if !genesisExists {
			fmt.Fprintf(os.Stdout, "[CometBFT] Creando configuración...\n")
			os.Stdout.Sync()
			if err := initializeCometBFT(cometConfig, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ERROR inicializando: %v\n", err)
				os.Stderr.Sync()
				return nil, fmt.Errorf("error inicializando CometBFT: %w", err)
			}
		}

		fmt.Fprintf(os.Stdout, "[CometBFT] Inicialización completada\n")
		os.Stdout.Sync()
	} else {
		fmt.Fprintf(os.Stdout, "[CometBFT] Ya está completamente inicializado\n")
		os.Stdout.Sync()

		// Verificar si el genesis tiene validadores, si no, agregar el validador generado
		fmt.Fprintf(os.Stdout, "[CometBFT] Verificando si el genesis tiene validadores...\n")
		os.Stdout.Sync()
		genesis, err := types.GenesisDocFromFile(genesisFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[CometBFT] ERROR cargando genesis: %v\n", err)
			os.Stderr.Sync()
			return nil, fmt.Errorf("error cargando genesis: %w", err)
		}

		if len(genesis.Validators) == 0 {
			fmt.Fprintf(os.Stdout, "[CometBFT] Genesis no tiene validadores, agregando validador...\n")
			os.Stdout.Sync()

			// Cargar private validator para obtener su clave pública
			pv := privval.LoadFilePV(keyFile, stateFile)
			pubKey, err := pv.GetPubKey()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ERROR obteniendo clave pública: %v\n", err)
				os.Stderr.Sync()
				return nil, fmt.Errorf("error obteniendo clave pública: %w", err)
			}

			// Agregar validador al genesis
			validator := types.GenesisValidator{
				Address: pubKey.Address(),
				PubKey:  pubKey,
				Name:    "initial-validator",
				Power:   10, // Power inicial para el validador
			}

			genesis.Validators = []types.GenesisValidator{validator}

			if err := genesis.SaveAs(genesisFile); err != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ERROR guardando genesis con validador: %v\n", err)
				os.Stderr.Sync()
				return nil, fmt.Errorf("error guardando genesis con validador: %w", err)
			}
			fmt.Fprintf(os.Stdout, "[CometBFT] Genesis actualizado con validador (address=%s, power=%d)\n", validator.Address, validator.Power)
			os.Stdout.Sync()

			// Eliminar directorio completo de datos de CometBFT porque el genesis cambió
			// El hash del genesis guardado en la DB no coincidirá con el nuevo
			// Necesitamos eliminar TODO el directorio data, excepto priv_validator_state.json
			fmt.Fprintf(os.Stdout, "[CometBFT] Eliminando bases de datos antiguas (genesis cambió)...\n")
			os.Stdout.Sync()
			dataDir := filepath.Join(cometConfig.RootDir, "data")

			// Guardar priv_validator_state.json temporalmente si existe
			stateFileBackup := stateFile + ".backup"
			if _, err := os.Stat(stateFile); err == nil {
				if err := os.Rename(stateFile, stateFileBackup); err != nil {
					fmt.Fprintf(os.Stderr, "[CometBFT] ADVERTENCIA: error respaldando state file: %v\n", err)
					os.Stderr.Sync()
				} else {
					fmt.Fprintf(os.Stdout, "[CometBFT] State file respaldado: %s\n", stateFileBackup)
					os.Stdout.Sync()
				}
			}

			// Eliminar todo el directorio data
			if err := os.RemoveAll(dataDir); err != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ERROR eliminando directorio data: %v\n", err)
				os.Stderr.Sync()
			} else {
				fmt.Fprintf(os.Stdout, "[CometBFT] Directorio data eliminado: %s\n", dataDir)
				os.Stdout.Sync()
			}

			// Recrear directorio data
			if err := os.MkdirAll(dataDir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ERROR recreando directorio data: %v\n", err)
				os.Stderr.Sync()
			} else {
				fmt.Fprintf(os.Stdout, "[CometBFT] Directorio data recreado: %s\n", dataDir)
				os.Stdout.Sync()
			}

			// Restaurar priv_validator_state.json si existía
			if _, err := os.Stat(stateFileBackup); err == nil {
				if err := os.Rename(stateFileBackup, stateFile); err != nil {
					fmt.Fprintf(os.Stderr, "[CometBFT] ADVERTENCIA: error restaurando state file: %v\n", err)
					os.Stderr.Sync()
				} else {
					fmt.Fprintf(os.Stdout, "[CometBFT] State file restaurado: %s\n", stateFile)
					os.Stdout.Sync()
				}
			}

			fmt.Fprintf(os.Stdout, "[CometBFT] Bases de datos eliminadas, CometBFT iniciará desde cero\n")
			os.Stdout.Sync()

			// Crear marcador para indicar que el genesis cambió
			// Esto se verificará antes de crear el nodo
			genesisChangedMarker := filepath.Join(cometConfig.RootDir, "config", ".genesis_changed")
			if err := os.WriteFile(genesisChangedMarker, []byte("genesis updated"), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ADVERTENCIA: error creando marcador: %v\n", err)
				os.Stderr.Sync()
			}
		} else {
			fmt.Fprintf(os.Stdout, "[CometBFT] Genesis ya tiene %d validadores\n", len(genesis.Validators))
			os.Stdout.Sync()

			// Verificar si las bases de datos existen y pueden causar conflicto
			// Si el genesis tiene validadores pero las bases de datos se crearon con un genesis diferente,
			// habrá un error de hash mismatch. Mejor prevenir eliminando las bases de datos si parecen estar
			// en un estado inconsistente (por ejemplo, si no hay bloques pero hay bases de datos)
			dataDir := filepath.Join(cometConfig.RootDir, "data")
			blockstoreDB := filepath.Join(dataDir, "blockstore.db")

			if _, err := os.Stat(blockstoreDB); err == nil {
				// Las bases de datos existen, pero verificamos si pueden causar problemas
				// Por seguridad, si detectamos que el genesis fue modificado recientemente
				// o si hay un problema conocido, eliminamos las bases de datos
				fmt.Fprintf(os.Stdout, "[CometBFT] Bases de datos existentes detectadas\n")
				os.Stdout.Sync()

				// Intentar detectar si hay un desajuste potencial
				// Por ahora, si las bases de datos existen y el genesis tiene validadores,
				// asumimos que están en buen estado. Si hay un error más adelante,
				// se manejará en el catch del error de hash mismatch.
			}
		}
	}

	// Verificar hash mismatch: eliminar bases de datos si es necesario
	// Esto se hace mediante el marcador .genesis_changed que se verifica antes de crear el nodo

	// Cargar configuración
	fmt.Fprintf(os.Stdout, "[CometBFT] Validando configuración...\n")
	os.Stdout.Sync()
	if err := cometConfig.ValidateBasic(); err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR validando configuración: %v\n", err)
		os.Stderr.Sync()
		return nil, fmt.Errorf("configuración inválida: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Configuración válida\n")
	os.Stdout.Sync()

	// Verificar si hay bases de datos que vamos a eliminar
	// Si las hay, debemos eliminar el state file ANTES de cargar el private validator
	dataDirCheck := filepath.Join(cometConfig.RootDir, "data")
	dbExistsCheck := false
	if _, err := os.Stat(filepath.Join(dataDirCheck, "state.db")); err == nil {
		dbExistsCheck = true
	}

	// Cargar private validator (nueva API v1.0.1: solo retorna un valor)
	// keyFile y stateFile ya están definidos arriba
	keyFile = cometConfig.PrivValidatorKeyFile()
	stateFile = cometConfig.PrivValidatorStateFile()

	// Si hay bases de datos que vamos a eliminar, eliminar el state file ANTES de cargarlo
	if dbExistsCheck {
		fmt.Fprintf(os.Stdout, "[CometBFT] Bases de datos detectadas, eliminando state file antes de cargar private validator...\n")
		os.Stdout.Sync()
		if _, err := os.Stat(stateFile); err == nil {
			if removeErr := os.Remove(stateFile); removeErr != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ADVERTENCIA: error eliminando state file: %v\n", removeErr)
				os.Stderr.Sync()
			} else {
				fmt.Fprintf(os.Stdout, "[CometBFT] State file eliminado para evitar height regression\n")
				os.Stdout.Sync()
			}
		}
	}

	// Asegurar que el directorio del state file existe antes de cargar el private validator
	stateDir := filepath.Dir(stateFile)
	fmt.Fprintf(os.Stdout, "[CometBFT] Creando directorio para state file: %s\n", stateDir)
	os.Stdout.Sync()
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR creando directorio para state file: %v\n", err)
		os.Stderr.Sync()
		return nil, fmt.Errorf("error creando directorio para state file: %w", err)
	}
	// Verificar que el directorio existe después de crearlo
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR: Directorio no existe después de crearlo: %s\n", stateDir)
		os.Stderr.Sync()
		return nil, fmt.Errorf("directorio no existe después de crearlo: %s", stateDir)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Directorio para state file creado/verificado exitosamente\n")
	os.Stdout.Sync()

	fmt.Fprintf(os.Stdout, "[CometBFT] Cargando private validator...\n")
	fmt.Fprintf(os.Stdout, "[CometBFT] KeyFile: %s\n", keyFile)
	fmt.Fprintf(os.Stdout, "[CometBFT] StateFile: %s\n", stateFile)
	os.Stdout.Sync()

	// Verificar que los archivos existen
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR: KeyFile no existe: %s\n", keyFile)
		os.Stderr.Sync()
		return nil, fmt.Errorf("private validator key file no existe: %s", keyFile)
	}
	// Verificar una vez más que el directorio del state file existe antes de llamar a LoadFilePV
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR: Directorio del state file no existe antes de LoadFilePV: %s\n", stateDir)
		os.Stderr.Sync()
		return nil, fmt.Errorf("directorio del state file no existe: %s", stateDir)
	}

	// Si el state file no existe, crear uno vacío con height=0 para que LoadFilePV pueda leerlo
	// LoadFilePV necesita que el archivo exista, pero puede estar vacío o con estructura mínima
	// El formato correcto de priv_validator_state.json según CometBFT:
	// - height debe ser STRING ("0")
	// - round debe ser NÚMERO (0)
	// - step debe ser NÚMERO (0)
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stdout, "[CometBFT] StateFile no existe, creando archivo vacío con height=0...\n")
		os.Stdout.Sync()
		// Crear un archivo de estado mínimo con height=0 (formato esperado por CometBFT)
		// Formato: {"height":"0","round":0,"step":0}
		// height es string, round y step son números
		emptyState := []byte(`{"height":"0","round":0,"step":0}`)
		// Crear el archivo con el contenido mínimo
		if err := os.WriteFile(stateFile, emptyState, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "[CometBFT] ERROR creando state file vacío: %v\n", err)
			os.Stderr.Sync()
			// Si falla, verificar que el directorio existe
			if _, dirErr := os.Stat(stateDir); os.IsNotExist(dirErr) {
				fmt.Fprintf(os.Stderr, "[CometBFT] ERROR: El directorio del state file tampoco existe: %s\n", stateDir)
				os.Stderr.Sync()
			}
			return nil, fmt.Errorf("error creando state file vacío: %w", err)
		}
		fmt.Fprintf(os.Stdout, "[CometBFT] State file vacío creado exitosamente (height=0) en: %s\n", stateFile)
		os.Stdout.Sync()

		// Verificar que el archivo se creó correctamente
		if _, verifyErr := os.Stat(stateFile); os.IsNotExist(verifyErr) {
			fmt.Fprintf(os.Stderr, "[CometBFT] ERROR: El archivo no existe después de crearlo: %s\n", stateFile)
			os.Stderr.Sync()
			return nil, fmt.Errorf("archivo no existe después de crearlo: %s", stateFile)
		}
		fmt.Fprintf(os.Stdout, "[CometBFT] State file verificado exitosamente\n")
		os.Stdout.Sync()
	}

	fmt.Fprintf(os.Stdout, "[CometBFT] Cargando private validator con LoadFilePV...\n")
	os.Stdout.Sync()

	pv := privval.LoadFilePV(keyFile, stateFile)
	fmt.Fprintf(os.Stdout, "[CometBFT] Private validator cargado\n")
	os.Stdout.Sync()

	// Crear node key
	fmt.Fprintf(os.Stdout, "[CometBFT] Cargando node key...\n")
	os.Stdout.Sync()
	nodeKeyFile := cometConfig.NodeKeyFile()
	fmt.Fprintf(os.Stdout, "[CometBFT] NodeKeyFile: %s\n", nodeKeyFile)
	os.Stdout.Sync()
	nodeKey, err := p2p.LoadNodeKey(nodeKeyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR cargando node key: %v\n", err)
		os.Stderr.Sync()
		return nil, fmt.Errorf("error cargando node key: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Node key cargado exitosamente\n")
	os.Stdout.Sync()

	// Crear aplicación ABCI directamente (LocalClientCreator)
	// CometBFT usará LocalClientCreator para comunicarse in-process con la aplicación
	fmt.Fprintf(os.Stdout, "[CometBFT] Creando app creator (LocalClientCreator)...\n")
	os.Stdout.Sync()
	appCreator := proxy.NewLocalClientCreator(abciApp)
	fmt.Fprintf(os.Stdout, "[CometBFT] App creator creado\n")
	os.Stdout.Sync()

    // Crear logger para CometBFT (habilitar logs para diagnóstico)
    fmt.Fprintf(os.Stdout, "[CometBFT] Creando logger para CometBFT (TMLogger con stdout)...\n")
    os.Stdout.Sync()
    cometLogger := cometlog.NewTMLogger(cometlog.NewSyncWriter(os.Stdout))
    fmt.Fprintf(os.Stdout, "[CometBFT] Logger creado (TMLogger: logs de consenso habilitados)\n")
    os.Stdout.Sync()

	// Antes de crear el nodo, SIEMPRE eliminar bases de datos si hay un marcador o si el genesis fue modificado
	// Esto es crítico para evitar hash mismatch después de modificar el genesis
	dataDir := filepath.Join(cometConfig.RootDir, "data")
	genesisChangedMarker := filepath.Join(cometConfig.RootDir, "config", ".genesis_changed")
	// stateDir ya está definido arriba (línea 283), no redeclarar

	// Verificar si el marcador existe O si las bases de datos existen (lo que indica posible conflicto)
	markerExists := false
	if _, err := os.Stat(genesisChangedMarker); err == nil {
		markerExists = true
		fmt.Fprintf(os.Stdout, "[CometBFT] Marcador de genesis cambiado detectado\n")
		os.Stdout.Sync()
	}

	// Verificar si hay bases de datos existentes que puedan causar conflicto
	dbExists := false
	if _, err := os.Stat(filepath.Join(dataDir, "state.db")); err == nil {
		dbExists = true
		fmt.Fprintf(os.Stdout, "[CometBFT] Bases de datos existentes detectadas\n")
		os.Stdout.Sync()
	}

	// Si hay marcador O si hay bases de datos (para evitar hash mismatch), eliminarlas
	if markerExists || dbExists {
		fmt.Fprintf(os.Stdout, "[CometBFT] Eliminando bases de datos para evitar hash mismatch (marcador=%v, dbExists=%v)...\n", markerExists, dbExists)
		os.Stdout.Sync()

		// Eliminar priv_validator_state.json para evitar height regression
		// Si eliminamos las bases de datos, el state file debe empezar desde height=0
		if _, statErr := os.Stat(stateFile); statErr == nil {
			fmt.Fprintf(os.Stdout, "[CometBFT] Eliminando priv_validator_state.json para evitar height regression...\n")
			os.Stdout.Sync()
			if removeErr := os.Remove(stateFile); removeErr != nil {
				fmt.Fprintf(os.Stderr, "[CometBFT] ADVERTENCIA: error eliminando state file: %v\n", removeErr)
				os.Stderr.Sync()
			} else {
				fmt.Fprintf(os.Stdout, "[CometBFT] priv_validator_state.json eliminado exitosamente (se regenerará desde height=0)\n")
				os.Stdout.Sync()
			}
		}

		// Eliminar TODO el directorio data completamente
		fmt.Fprintf(os.Stdout, "[CometBFT] Eliminando directorio data: %s\n", dataDir)
		os.Stdout.Sync()
		if removeErr := os.RemoveAll(dataDir); removeErr != nil {
			fmt.Fprintf(os.Stderr, "[CometBFT] ERROR eliminando directorio data: %v\n", removeErr)
			os.Stderr.Sync()
		} else {
			fmt.Fprintf(os.Stdout, "[CometBFT] Directorio data eliminado exitosamente\n")
			os.Stdout.Sync()
		}

		// Esperar para asegurar que los archivos se liberen completamente
		fmt.Fprintf(os.Stdout, "[CometBFT] Esperando 300ms para liberar archivos...\n")
		os.Stdout.Sync()
		time.Sleep(300 * time.Millisecond)

		// Recrear directorio data vacío
		fmt.Fprintf(os.Stdout, "[CometBFT] Recreando directorio data: %s\n", stateDir)
		os.Stdout.Sync()
		if mkdirErr := os.MkdirAll(stateDir, 0755); mkdirErr != nil {
			fmt.Fprintf(os.Stderr, "[CometBFT] ERROR recreando directorio data: %v\n", mkdirErr)
			os.Stderr.Sync()
		} else {
			fmt.Fprintf(os.Stdout, "[CometBFT] Directorio data recreado exitosamente\n")
			os.Stdout.Sync()
		}

		// Eliminar marcador si existe
		if markerExists {
			os.Remove(genesisChangedMarker)
			fmt.Fprintf(os.Stdout, "[CometBFT] Marcador eliminado\n")
			os.Stdout.Sync()
		}

		fmt.Fprintf(os.Stdout, "[CometBFT] Bases de datos completamente eliminadas, listo para crear nodo\n")
		os.Stdout.Sync()
	}

	// Crear nodo CometBFT (nueva API v1.0.1: necesita context.Context y firma diferente)
	fmt.Fprintf(os.Stdout, "[CometBFT] Creando nodo CometBFT (node.NewNode)...\n")
	os.Stdout.Sync()
	cometNode, err := node.NewNode(
		ctx,
		cometConfig,
		pv,
		nodeKey,
		appCreator,
		node.DefaultGenesisDocProviderFunc(cometConfig),
		cometcfg.DefaultDBProvider,
		node.DefaultMetricsProvider(cometConfig.Instrumentation),
		cometLogger,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR creando nodo CometBFT: %v\n", err)
		os.Stderr.Sync()
		return nil, fmt.Errorf("error creando nodo CometBFT: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Nodo CometBFT creado exitosamente\n")
	os.Stdout.Sync()

	fmt.Fprintf(os.Stdout, "[CometBFT] Creando estructura CometBFTNode...\n")
	os.Stdout.Sync()
	cometNodeStruct := &CometBFTNode{
		node:    cometNode,
		abciApp: abciApp,
		config:  cfg,
		running: false,
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Estructura CometBFTNode creada\n")
	os.Stdout.Sync()

	fmt.Fprintf(os.Stdout, "[CometBFT] Retornando de NewCometBFT()...\n")
	os.Stdout.Sync()
	return cometNodeStruct, nil
}

// isCometBFTInitialized verifica si CometBFT ya está inicializado
func isCometBFTInitialized(cfg *cometcfg.Config) bool {
	genesisFile := filepath.Join(cfg.RootDir, "config", "genesis.json")
	keyFile := cfg.PrivValidatorKeyFile()

	// Verificar que tanto genesis.json como las claves existan
	genesisExists := false
	keyExists := false

	if _, err := os.Stat(genesisFile); err == nil {
		genesisExists = true
	}
	if _, err := os.Stat(keyFile); err == nil {
		keyExists = true
	}

	// Ambos deben existir para considerar CometBFT inicializado
	return genesisExists && keyExists
}

// initializeCometBFT inicializa CometBFT usando el comando cometbft
func initializeCometBFT(cfg *cometcfg.Config, appConfig *Config) error {
	// Crear comando cometbft init
	cmd := exec.Command("cometbft", "init", "--home", cfg.RootDir)

	// Ejecutar comando
	if err := cmd.Run(); err != nil {
		// Si cometbft no está instalado, crear configuración manual
		return createCometBFTConfig(cfg, appConfig)
	}

	// Modificar genesis para incluir chain ID
	genesisFile := filepath.Join(cfg.RootDir, "config", "genesis.json")
	genesis, err := types.GenesisDocFromFile(genesisFile)
	if err != nil {
		return fmt.Errorf("error cargando genesis: %w", err)
	}

	genesis.ChainID = appConfig.ChainID

	if err := genesis.SaveAs(genesisFile); err != nil {
		return fmt.Errorf("error guardando genesis: %w", err)
	}

	return nil
}

// createCometBFTConfig crea configuración de CometBFT manualmente
func createCometBFTConfig(cfg *cometcfg.Config, appConfig *Config) error {
	fmt.Fprintf(os.Stdout, "[CometBFT] Creando configuración manualmente...\n")
	os.Stdout.Sync()

	// Crear directorios necesarios
	dirs := []string{
		filepath.Join(cfg.RootDir, "config"),
		filepath.Join(cfg.RootDir, "data"),
	}

	fmt.Fprintf(os.Stdout, "[CometBFT] Creando directorios...\n")
	os.Stdout.Sync()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "[CometBFT] ERROR creando directorio %s: %v\n", dir, err)
			os.Stderr.Sync()
			return fmt.Errorf("error creando directorio %s: %w", dir, err)
		}
		fmt.Fprintf(os.Stdout, "[CometBFT] Directorio creado: %s\n", dir)
		os.Stdout.Sync()
	}

	// Crear genesis básico
	fmt.Fprintf(os.Stdout, "[CometBFT] Creando genesis...\n")
	os.Stdout.Sync()
	genesis := &types.GenesisDoc{
		ChainID:         appConfig.ChainID,
		GenesisTime:     time.Now(),
		ConsensusParams: types.DefaultConsensusParams(),
	}

	genesisFile := filepath.Join(cfg.RootDir, "config", "genesis.json")
	if err := genesis.SaveAs(genesisFile); err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR guardando genesis: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error guardando genesis: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Genesis guardado: %s\n", genesisFile)
	os.Stdout.Sync()

	// Generar claves si no existen
	fmt.Fprintf(os.Stdout, "[CometBFT] Generando claves...\n")
	os.Stdout.Sync()
	if err := generateKeys(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR generando claves: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error generando claves: %w", err)
	}

	// Verificar que las claves se crearon
	keyFile := cfg.PrivValidatorKeyFile()
	stateFile := cfg.PrivValidatorStateFile()
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR: KeyFile no existe después de generateKeys: %s\n", keyFile)
		os.Stderr.Sync()
		return fmt.Errorf("keyFile no existe después de generateKeys: %s", keyFile)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] KeyFile verificado: %s\n", keyFile)
	os.Stdout.Sync()

	// Cargar el private validator para obtener su clave pública
	fmt.Fprintf(os.Stdout, "[CometBFT] Cargando private validator para agregar al genesis...\n")
	os.Stdout.Sync()
	pv := privval.LoadFilePV(keyFile, stateFile)

	// Obtener clave pública del validador
	fmt.Fprintf(os.Stdout, "[CometBFT] Obteniendo clave pública del validador...\n")
	os.Stdout.Sync()
	pubKey, err := pv.GetPubKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR obteniendo clave pública: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error obteniendo clave pública: %w", err)
	}

	// Agregar validador al genesis
	fmt.Fprintf(os.Stdout, "[CometBFT] Agregando validador al genesis...\n")
	os.Stdout.Sync()
	validator := types.GenesisValidator{
		Address: pubKey.Address(),
		PubKey:  pubKey,
		Name:    "initial-validator",
		Power:   10, // Power inicial para el validador
	}

	// Recargar genesis y agregar validador
	genesis, err = types.GenesisDocFromFile(genesisFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR recargando genesis: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error recargando genesis: %w", err)
	}

	genesis.Validators = []types.GenesisValidator{validator}

	if err := genesis.SaveAs(genesisFile); err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR guardando genesis con validador: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error guardando genesis con validador: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Genesis actualizado con validador (address=%s, power=%d)\n", validator.Address, validator.Power)
	os.Stdout.Sync()

	return nil
}

// configureCometBFTForEmptyBlocks configura CometBFT para crear bloques vacíos automáticamente
func configureCometBFTForEmptyBlocks(cfg *cometcfg.Config) error {
	// Configurar para crear bloques vacíos automáticamente
	cfg.Consensus.CreateEmptyBlocks = true
	cfg.Consensus.CreateEmptyBlocksInterval = 1 * time.Second // Crear bloques vacíos cada segundo

    // Ajustar timeouts de consenso para entornos de un solo validador
    // Valores conservadores para que el pipeline no se quede esperando innecesariamente
    cfg.Consensus.TimeoutPropose = 1 * time.Second
    cfg.Consensus.TimeoutProposeDelta = 200 * time.Millisecond
    cfg.Consensus.TimeoutPrevote = 1 * time.Second
    cfg.Consensus.TimeoutPrevoteDelta = 200 * time.Millisecond
    cfg.Consensus.TimeoutPrecommit = 1 * time.Second
    cfg.Consensus.TimeoutPrecommitDelta = 200 * time.Millisecond
    cfg.Consensus.TimeoutCommit = 1 * time.Second

	// Configurar peers persistentes si se proporcionan
	if persistentPeers := os.Getenv("OXY_PERSISTENT_PEERS"); persistentPeers != "" {
		cfg.P2P.PersistentPeers = persistentPeers
		fmt.Fprintf(os.Stdout, "[CometBFT] PersistentPeers configurados: %s\n", persistentPeers)
		os.Stdout.Sync()
	}
	
	// Configurar seeds si se proporcionan
	if seeds := os.Getenv("OXY_SEEDS"); seeds != "" {
		cfg.P2P.Seeds = seeds
		fmt.Fprintf(os.Stdout, "[CometBFT] Seeds configurados: %s\n", seeds)
		os.Stdout.Sync()
	}

	// Guardar configuración actualizada
	configFile := filepath.Join(cfg.RootDir, "config", "config.toml")
	cometcfg.WriteConfigFile(configFile, cfg) // WriteConfigFile no retorna error

	return nil
}

// generateKeys genera claves para CometBFT si no existen
func generateKeys(cfg *cometcfg.Config) error {
	keyFile := cfg.PrivValidatorKeyFile()
	stateFile := cfg.PrivValidatorStateFile()
	nodeKeyFile := cfg.NodeKeyFile()

	fmt.Fprintf(os.Stdout, "[CometBFT] Generando claves...\n")
	fmt.Fprintf(os.Stdout, "[CometBFT] KeyFile: %s\n", keyFile)
	fmt.Fprintf(os.Stdout, "[CometBFT] StateFile: %s\n", stateFile)
	fmt.Fprintf(os.Stdout, "[CometBFT] NodeKeyFile: %s\n", nodeKeyFile)
	os.Stdout.Sync()

	// Verificar si ya existen
	if _, err := os.Stat(keyFile); err == nil {
		fmt.Fprintf(os.Stdout, "[CometBFT] KeyFile ya existe\n")
		os.Stdout.Sync()
		return nil // Ya existe
	}

	// Asegurar que el directorio existe
	keyDir := filepath.Dir(keyFile)
	fmt.Fprintf(os.Stdout, "[CometBFT] Verificando directorio para KeyFile: %s\n", keyDir)
	os.Stdout.Sync()
	if err := os.MkdirAll(keyDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR creando directorio para KeyFile: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error creando directorio para KeyFile: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Directorio creado/verificado: %s\n", keyDir)
	os.Stdout.Sync()

	// Asegurar que el directorio para stateFile también existe
	stateDir := filepath.Dir(stateFile)
	fmt.Fprintf(os.Stdout, "[CometBFT] Verificando directorio para StateFile: %s\n", stateDir)
	os.Stdout.Sync()
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR creando directorio para StateFile: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error creando directorio para StateFile: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Directorio creado/verificado: %s\n", stateDir)
	os.Stdout.Sync()

	// Generar nueva clave privada (nueva API v1.0.1: necesita función keyGen que retorna crypto.PrivKey)
	keyGen := func() (crypto.PrivKey, error) {
		return ed25519.GenPrivKey(), nil
	}

	fmt.Fprintf(os.Stdout, "[CometBFT] Llamando a privval.GenFilePV()...\n")
	os.Stdout.Sync()
	pv, err := privval.GenFilePV(keyFile, stateFile, keyGen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR generando private validator: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error generando private validator: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Private validator generado (objeto creado)\n")
	os.Stdout.Sync()

	// Guardar el private validator al archivo explícitamente
	fmt.Fprintf(os.Stdout, "[CometBFT] Guardando private validator al archivo...\n")
	os.Stdout.Sync()
	pv.Save()
	fmt.Fprintf(os.Stdout, "[CometBFT] Private validator guardado al archivo\n")
	os.Stdout.Sync()

	// Verificar que el archivo se creó
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR: KeyFile no se creó después de Save(): %s\n", keyFile)
		os.Stderr.Sync()
		return fmt.Errorf("keyFile no se creó después de Save(): %s", keyFile)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] KeyFile verificado: %s\n", keyFile)
	os.Stdout.Sync()

	// Generar node key
	fmt.Fprintf(os.Stdout, "[CometBFT] Generando node key...\n")
	os.Stdout.Sync()
	nodeKey, err := p2p.LoadOrGenNodeKey(nodeKeyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[CometBFT] ERROR generando node key: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error generando node key: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[CometBFT] Node key generado\n")
	os.Stdout.Sync()

	// Verificar que los archivos se crearon
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		return fmt.Errorf("keyFile no se creó después de GenFilePV: %s", keyFile)
	}
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		// State file puede no crearse inmediatamente, está bien
		fmt.Fprintf(os.Stdout, "[CometBFT] StateFile no existe aún, se creará al iniciar\n")
		os.Stdout.Sync()
	}

	_ = pv
	_ = nodeKey

	fmt.Fprintf(os.Stdout, "[CometBFT] Claves generadas exitosamente\n")
	os.Stdout.Sync()
	return nil
}
