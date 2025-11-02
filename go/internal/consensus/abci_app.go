package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	execution "github.com/Q-YZX0/oxy-blockchain/internal/execution"
	cryptosigner "github.com/Q-YZX0/oxy-blockchain/internal/crypto"
	"github.com/Q-YZX0/oxy-blockchain/internal/logger"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
	"github.com/Q-YZX0/oxy-blockchain/internal/metrics"
)

// ABCIApp implementa la interfaz ABCI de CometBFT
// Esta es la aplicación que corre sobre CometBFT
type ABCIApp struct {
	storage            *storage.BlockchainDB
	executor           *execution.EVMExecutor
	validators         *ValidatorSet
	state              *AppState
	currentBlockHeight uint64
	currentBlockTime   int64
	currentBlockTxs    []*Transaction
	currentBlockReceipts []*TransactionReceipt
	chainID            string
	getMempool         func() []*Transaction // Función para obtener el mempool local
	clearMempoolTx     func(string)          // Función para limpiar una transacción del mempool
	metrics            *metrics.Metrics      // Referencia a las métricas (opcional)
}

// AppState mantiene el estado de la aplicación
type AppState struct {
	Height      int64
	AppHash     []byte
	Validators  []abcitypes.ValidatorUpdate
}

// NewABCIApp crea una nueva aplicación ABCI
func NewABCIApp(storage *storage.BlockchainDB, executor *execution.EVMExecutor, validators *ValidatorSet, chainID string) *ABCIApp {
	return &ABCIApp{
		storage:    storage,
		executor:   executor,
		validators: validators,
		chainID:    chainID,
		state: &AppState{
			Height:     0,
			AppHash:    make([]byte, 32),
			Validators: []abcitypes.ValidatorUpdate{},
		},
		currentBlockTxs:     make([]*Transaction, 0),
		currentBlockReceipts: make([]*TransactionReceipt, 0),
		getMempool:          nil, // Se establecerá después
		clearMempoolTx:      nil, // Se establecerá después
	}
}

// SetGetMempool establece la función para obtener el mempool local
func (app *ABCIApp) SetGetMempool(getMempool func() []*Transaction) {
	app.getMempool = getMempool
}

// SetClearMempoolTx establece la función para limpiar una transacción del mempool
func (app *ABCIApp) SetClearMempoolTx(clearMempoolTx func(string)) {
	app.clearMempoolTx = clearMempoolTx
}

// SetMetrics establece la referencia a las métricas
func (app *ABCIApp) SetMetrics(m *metrics.Metrics) {
	app.metrics = m
}

// Info retorna información sobre el estado de la aplicación (nueva API v1.0.1)
func (app *ABCIApp) Info(ctx context.Context, req *abcitypes.InfoRequest) (*abcitypes.InfoResponse, error) {
	return &abcitypes.InfoResponse{
		Data:             fmt.Sprintf("oxy-blockchain-v0.1.0"),
		Version:          "0.1.0",
		AppVersion:       1,
		LastBlockHeight:  app.state.Height,
		LastBlockAppHash: app.state.AppHash,
	}, nil
}

// InitChain inicializa la blockchain (nueva API v1.0.1)
func (app *ABCIApp) InitChain(ctx context.Context, req *abcitypes.InitChainRequest) (*abcitypes.InitChainResponse, error) {
	fmt.Fprintf(os.Stdout, "[ABCI] InitChain iniciado\n")
	os.Stdout.Sync()
	logger.Info("Inicializando blockchain")
	
	// Cargar validadores guardados
	fmt.Fprintf(os.Stdout, "[ABCI] Verificando validadores...\n")
	os.Stdout.Sync()
	if app.validators != nil {
		fmt.Fprintf(os.Stdout, "[ABCI] Validadores disponibles, cargando...\n")
		os.Stdout.Sync()
		if err := app.validators.LoadValidators(); err != nil {
			logger.Warn("Error cargando validadores: " + err.Error())
		}
		
		fmt.Fprintf(os.Stdout, "[ABCI] Verificando validadores activos...\n")
		os.Stdout.Sync()
		activeValidators := app.validators.GetActiveValidators()
		fmt.Fprintf(os.Stdout, "[ABCI] Validadores activos: %d\n", len(activeValidators))
		os.Stdout.Sync()
		
		// Usar validadores del set en lugar de los del genesis
		// Si no hay validadores guardados, usar los del genesis
		if len(activeValidators) == 0 {
			fmt.Fprintf(os.Stdout, "[ABCI] No hay validadores activos, inicializando desde genesis...\n")
			os.Stdout.Sync()
			// Convertir validadores del genesis al formato interno
			genesisValidators := make([]GenesisValidator, 0, len(req.Validators))
			for _, v := range req.Validators {
				// Extraer dirección desde la clave pública (simplificado)
				// Nueva API v1.0.1: usar PubKeyBytes en lugar de PubKey.Data
				address := common.BytesToAddress(v.PubKeyBytes).Hex()
				
				// Convertir power a stake usando big.Int para evitar overflow
				powerBig := big.NewInt(int64(v.Power))
				multiplier := big.NewInt(1e18)
				stake := new(big.Int).Mul(powerBig, multiplier)
				
				genesisValidators = append(genesisValidators, GenesisValidator{
					Address: address,
					PubKey:  v.PubKeyBytes,
					Stake:   stake,
				})
			}
			
			fmt.Fprintf(os.Stdout, "[ABCI] Validadores genesis convertidos: %d\n", len(genesisValidators))
			os.Stdout.Sync()
			
			if err := app.validators.InitializeGenesisValidators(genesisValidators); err != nil {
				fmt.Fprintf(os.Stderr, "[ABCI] ERROR inicializando validadores genesis: %v\n", err)
				os.Stderr.Sync()
				logger.Error("Error inicializando validadores genesis: " + err.Error())
			} else {
				fmt.Fprintf(os.Stdout, "[ABCI] Validadores genesis inicializados exitosamente\n")
				os.Stdout.Sync()
			}
		}
		
		fmt.Fprintf(os.Stdout, "[ABCI] Obteniendo validadores actualizados...\n")
		os.Stdout.Sync()
		
		// Si el genesis ya tiene validadores, usar los del genesis directamente (vienen con formato correcto)
		// Estos validadores ya tienen el formato correcto de CometBFT y no causarán error de encoding
		if len(req.Validators) > 0 {
			fmt.Fprintf(os.Stdout, "[ABCI] Genesis tiene %d validadores, usando directamente del genesis (formato correcto)\n", len(req.Validators))
			os.Stdout.Sync()
			// Usar los validadores del genesis directamente - ya vienen con el formato correcto de CometBFT
			app.state.Validators = req.Validators
			fmt.Fprintf(os.Stdout, "[ABCI] Validadores InitChain: %d (del genesis)\n", len(app.state.Validators))
			os.Stdout.Sync()
		} else {
			// Si el genesis no tiene validadores, usar los del ValidatorSet
			allValidators := app.validators.GetValidators()
			fmt.Fprintf(os.Stdout, "[ABCI] Total validadores en set: %d\n", len(allValidators))
			os.Stdout.Sync()
			
			activeValidators = app.validators.GetActiveValidators()
			fmt.Fprintf(os.Stdout, "[ABCI] Validadores activos: %d\n", len(activeValidators))
			os.Stdout.Sync()
			
			app.state.Validators = app.validators.ToCometBFTValidators()
			fmt.Fprintf(os.Stdout, "[ABCI] Validadores actualizados obtenidos: %d\n", len(app.state.Validators))
			os.Stdout.Sync()
		}
	} else {
		fmt.Fprintf(os.Stdout, "[ABCI] No hay validadores, usando genesis directamente\n")
		os.Stdout.Sync()
		// Fallback: usar validadores del genesis directamente
		app.state.Validators = req.Validators
	}
	
	fmt.Fprintf(os.Stdout, "[ABCI] Preparando respuesta InitChain...\n")
	os.Stdout.Sync()
	response := &abcitypes.InitChainResponse{
		Validators: app.state.Validators,
		AppHash:    app.state.AppHash,
	}
	fmt.Fprintf(os.Stdout, "[ABCI] InitChain completado, retornando respuesta\n")
	os.Stdout.Sync()
	return response, nil
}

// FinalizeBlock procesa todas las transacciones del bloque y finaliza el bloque
// Reemplaza BeginBlock, DeliverTx y EndBlock en la nueva API v1.0.1
func (app *ABCIApp) FinalizeBlock(ctx context.Context, req *abcitypes.FinalizeBlockRequest) (*abcitypes.FinalizeBlockResponse, error) {
		fmt.Fprintf(os.Stdout, "[ABCI] FinalizeBlock llamado: height=%d, txs=%d\n", req.Height, len(req.Txs))
		os.Stdout.Sync()
		logger.Info(fmt.Sprintf("Finalizando bloque: height=%d, txs=%d", req.Height, len(req.Txs)))
		
		// Log detallado de transacciones recibidas
		if len(req.Txs) > 0 {
			fmt.Fprintf(os.Stdout, "[ABCI] Procesando %d transacciones en bloque %d\n", len(req.Txs), req.Height)
			os.Stdout.Sync()
		} else {
			fmt.Fprintf(os.Stdout, "[ABCI] Bloque %d sin transacciones\n", req.Height)
			os.Stdout.Sync()
		}
	
	// Guardar altura y timestamp actuales para uso en ejecución EVM
	app.state.Height = req.Height
	app.currentBlockHeight = uint64(req.Height)
	app.currentBlockTime = req.Time.Unix()
	
	// Limpiar transacciones del bloque anterior
	app.currentBlockTxs = make([]*Transaction, 0)
	app.currentBlockReceipts = make([]*TransactionReceipt, 0)
	
	// Procesar todas las transacciones del bloque
	txResults := make([]*abcitypes.ExecTxResult, 0, len(req.Txs))
	
	// Establecer información del bloque actual en el ejecutor
	app.executor.SetCurrentBlockInfo(uint64(req.Height), app.currentBlockTime)
	
		// Procesar cada transacción
		for i, txBytes := range req.Txs {
			fmt.Fprintf(os.Stdout, "[ABCI] Procesando transacción %d de %d (bytes: %d)\n", i+1, len(req.Txs), len(txBytes))
			os.Stdout.Sync()
			
			// Decodificar transacción
			var tx Transaction
			if err := json.Unmarshal(txBytes, &tx); err != nil {
				fmt.Fprintf(os.Stderr, "[ABCI] ERROR decodificando transacción %d: %v\n", i+1, err)
				os.Stderr.Sync()
				txResults = append(txResults, &abcitypes.ExecTxResult{
					Code: 1,
					Log:  fmt.Sprintf("Error decodificando transacción: %v", err),
				})
				continue
			}
			
			fmt.Fprintf(os.Stdout, "[ABCI] Transacción decodificada: hash=%s, from=%s, to=%s\n", tx.Hash, tx.From, tx.To)
			os.Stdout.Sync()

		// Validar transacción básica
		fmt.Fprintf(os.Stdout, "[ABCI] Validando transacción: hash=%s\n", tx.Hash)
		os.Stdout.Sync()
		if err := app.validateTransaction(&tx); err != nil {
			fmt.Fprintf(os.Stderr, "[ABCI] ERROR validación falló: %v\n", err)
			os.Stderr.Sync()
			txResults = append(txResults, &abcitypes.ExecTxResult{
				Code: 2,
				Log:  fmt.Sprintf("Transacción inválida: %v", err),
			})
			continue
		}
		fmt.Fprintf(os.Stdout, "[ABCI] Validación exitosa: hash=%s\n", tx.Hash)
		os.Stdout.Sync()

		// Convertir a formato execution.Transaction
		fmt.Fprintf(os.Stdout, "[ABCI] Convirtiendo a formato execution: hash=%s\n", tx.Hash)
		os.Stdout.Sync()
		executionTx := &execution.Transaction{
			Hash:     tx.Hash,
			From:     tx.From,
			To:       tx.To,
			Value:    tx.Value,
			Data:     tx.Data,
			GasLimit: tx.GasLimit,
			GasPrice: tx.GasPrice,
			Nonce:    tx.Nonce,
		}

		// Ejecutar transacción con EVM
		fmt.Fprintf(os.Stdout, "[ABCI] Ejecutando transacción con EVM: hash=%s\n", tx.Hash)
		os.Stdout.Sync()
		result, err := app.executor.ExecuteTransaction(executionTx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ABCI] ERROR ejecutando transacción: %v\n", err)
			os.Stderr.Sync()
			txResults = append(txResults, &abcitypes.ExecTxResult{
				Code: 3,
				Log:  fmt.Sprintf("Error ejecutando transacción: %v", err),
			})
			continue
		}
		fmt.Fprintf(os.Stdout, "[ABCI] Ejecución completada: hash=%s, success=%v\n", tx.Hash, result.Success)
		os.Stdout.Sync()

		// Crear resultado de ejecución
		execTxResult := &abcitypes.ExecTxResult{
			Code:    0,
			Log:     "OK",
			GasUsed: int64(result.GasUsed),
			Events:  app.buildEvents(result),
		}

		if !result.Success {
			fmt.Fprintf(os.Stderr, "[ABCI] Transacción falló en ejecución: hash=%s, error=%s\n", tx.Hash, result.Error)
			os.Stderr.Sync()
			execTxResult.Code = 4
			execTxResult.Log = result.Error
			
			// Actualizar métricas para transacción rechazada
			if app.metrics != nil {
				app.metrics.IncrementRejectedTransactions()
			}
			
			// IMPORTANTE: Remover transacción del mempool incluso si falla
			// Esto evita que se reintente infinitamente
			if app.clearMempoolTx != nil {
				app.clearMempoolTx(tx.Hash)
				fmt.Fprintf(os.Stdout, "[ABCI] Transacción fallida removida del mempool: hash=%s\n", tx.Hash)
				os.Stdout.Sync()
			}
			
			// Guardar transacción fallida en storage para referencia
			// (opcional: solo si quieres trackear transacciones fallidas)
			// txData, _ := json.Marshal(tx)
			// app.storage.SaveTransaction(tx.Hash, txData)
		} else {
			// Guardar transacción solo si fue exitosa
			fmt.Fprintf(os.Stdout, "[ABCI] Transacción exitosa, guardando en storage: hash=%s\n", tx.Hash)
			os.Stdout.Sync()
			
			txData, _ := json.Marshal(tx)
			if err := app.storage.SaveTransaction(tx.Hash, txData); err != nil {
				fmt.Fprintf(os.Stderr, "[ABCI] ERROR guardando transacción en storage: %v\n", err)
				os.Stderr.Sync()
			} else {
				fmt.Fprintf(os.Stdout, "[ABCI] Transacción guardada exitosamente en storage: hash=%s\n", tx.Hash)
				os.Stdout.Sync()
			}
			
			// Actualizar métricas para transacción exitosa
			if app.metrics != nil {
				app.metrics.IncrementTransactions()
				app.metrics.AddGasUsed(result.GasUsed)
			}
			
			// Agregar transacción al bloque actual
			app.currentBlockTxs = append(app.currentBlockTxs, &tx)
			
			// Limpiar transacción del mempool local después de procesarla exitosamente
			if app.clearMempoolTx != nil {
				app.clearMempoolTx(tx.Hash)
				fmt.Fprintf(os.Stdout, "[ABCI] Transacción exitosa removida del mempool: hash=%s\n", tx.Hash)
				os.Stdout.Sync()
			}
			
			// Crear receipt de la transacción
			receipt := &TransactionReceipt{
				TransactionHash: tx.Hash,
				BlockNumber:     app.currentBlockHeight,
				GasUsed:         result.GasUsed,
				Status:          "success",
				Logs:            convertLogs(result.Logs),
				Error:           result.Error,
			}
			
			app.currentBlockReceipts = append(app.currentBlockReceipts, receipt)
		}
		
		txResults = append(txResults, execTxResult)
	}
	
	// Rotar validadores periódicamente (cada 100 bloques)
	// IMPORTANTE: Solo retornar ValidatorUpdates si hay cambios REALES
	// CometBFT puede detenerse si recibe validadores sin cambios
	var validatorUpdates []abcitypes.ValidatorUpdate
	if req.Height > 0 && req.Height%100 == 0 {
		fmt.Fprintf(os.Stdout, "[ABCI] Rotación de validadores en bloque %d\n", req.Height)
		os.Stdout.Sync()
		if app.validators != nil {
			// Obtener validadores actuales para comparar
			currentValidators := app.validators.ToCometBFTValidators()
			fmt.Fprintf(os.Stdout, "[ABCI] Validadores actuales: %d\n", len(currentValidators))
			os.Stdout.Sync()
			
			// OPTIMIZACIÓN: Si solo hay 1 validador, no tiene sentido rotar
			// Solo retornar updates si hay cambios reales (nuevos validadores, power diferente, etc.)
			if len(currentValidators) <= 1 {
				fmt.Fprintf(os.Stdout, "[ABCI] Solo hay 1 validador, saltando rotación (no hay nada que rotar)\n")
				os.Stdout.Sync()
				// NO hacer rotación si solo hay 1 validador - no retornar ValidatorUpdates
			} else {
				// Solo rotar si hay múltiples validadores
				updates, err := app.validators.RotateValidators()
				if err != nil {
					fmt.Fprintf(os.Stderr, "[ABCI] ERROR rotando validadores: %v\n", err)
					os.Stderr.Sync()
					logger.Error("Error rotando validadores: " + err.Error())
					// Si hay error, no retornar actualizaciones (mantener validadores actuales)
				} else if len(updates) > 0 {
					// Comparar si hay cambios REALES antes de retornar updates
					if hasValidatorChanges(currentValidators, updates) {
						validatorUpdates = updates
						fmt.Fprintf(os.Stdout, "[ABCI] Validadores rotados exitosamente con cambios: count=%d\n", len(updates))
						os.Stdout.Sync()
						logger.Info(fmt.Sprintf("Validadores rotados: count=%d", len(updates)))
					} else {
						// No hay cambios, no retornar updates (evita que CometBFT se detenga)
						fmt.Fprintf(os.Stdout, "[ABCI] Rotación completada pero sin cambios, no retornando ValidatorUpdates\n")
						os.Stdout.Sync()
						logger.Info("Rotación de validadores: sin cambios detectados")
					}
				} else {
					// Si no hay validadores después de rotación, NO retornar actualizaciones
					fmt.Fprintf(os.Stderr, "[ABCI] ADVERTENCIA: Rotación retornó 0 validadores, manteniendo validadores actuales\n")
					os.Stderr.Sync()
					logger.Warn("Rotación retornó 0 validadores, manteniendo validadores actuales")
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "[ABCI] ADVERTENCIA: validators es nil en bloque %d\n", req.Height)
			os.Stderr.Sync()
		}
	}
	
	return &abcitypes.FinalizeBlockResponse{
		TxResults:        txResults,
		ValidatorUpdates: validatorUpdates,
	}, nil
}

// Commit confirma el bloque y retorna el AppHash (nueva API v1.0.1)
func (app *ABCIApp) Commit(ctx context.Context, req *abcitypes.CommitRequest) (*abcitypes.CommitResponse, error) {
	// Guardar estado EVM completo (esto persiste el StateDB)
	if err := app.executor.SaveState(); err != nil {
		logger.Warn("Error guardando estado EVM: " + err.Error())
	}
	
	// Obtener root hash del StateDB
	stateRoot := app.executor.GetStateManager().GetRootHash()
	
	// Si no hay root, usar hash del estado de la aplicación
	var appHash []byte
	if stateRoot != (common.Hash{}) {
		appHash = stateRoot[:]
	} else {
		stateData, _ := json.Marshal(app.state)
		hash := crypto.Keccak256(stateData)
		appHash = hash[:32]
	}
	
	// Guardar metadata del estado
	stateData, _ := json.Marshal(map[string]interface{}{
		"root":      stateRoot.Hex(),
		"height":    app.state.Height,
		"app_hash": common.BytesToHash(appHash).Hex(),
	})
	app.storage.SaveState(stateData)
	
	// Guardar bloque completo
	if app.currentBlockHeight > 0 {
		if err := app.saveBlock(appHash); err != nil {
			logger.Warn("Error guardando bloque: " + err.Error())
		}
		
		// Actualizar métricas para bloque procesado
		if app.metrics != nil {
			app.metrics.IncrementBlocks()
			app.metrics.SetBlockHeight(app.currentBlockHeight)
			// Calcular tiempo de procesamiento (aproximado)
			if app.currentBlockTime > 0 {
				processingTime := time.Since(time.Unix(app.currentBlockTime, 0))
				app.metrics.AddBlockProcessingTime(processingTime)
			}
		}
	}
	
	// Actualizar AppHash con root del StateDB
	copy(app.state.AppHash, appHash)
	
	// Nota: En la nueva API v1.0.1, CommitResponse ya no tiene Data (AppHash se maneja de otra forma)
	return &abcitypes.CommitResponse{
		RetainHeight: 0,
	}, nil
}

// saveBlock guarda el bloque completo en storage
func (app *ABCIApp) saveBlock(blockHash []byte) error {
	// Calcular hash del bloque
	blockHashStr := common.BytesToHash(blockHash).Hex()
	
	// Obtener hash del bloque padre
	parentHash := ""
	if app.currentBlockHeight > 0 {
		parentBlockData, err := app.storage.GetBlock(app.currentBlockHeight - 1)
		if err == nil && parentBlockData != nil {
			var parentBlock Block
			if err := json.Unmarshal(parentBlockData, &parentBlock); err == nil {
				parentHash = parentBlock.Header.Hash
			}
		}
	}
	
	// Crear bloque completo
	block := &Block{
		Header: BlockHeader{
			Height:     app.currentBlockHeight,
			Hash:       blockHashStr,
			ParentHash: parentHash,
			Timestamp:  time.Unix(app.currentBlockTime, 0),
			ChainID:    app.chainID,
		},
		Transactions: app.currentBlockTxs,
		Receipts:     app.currentBlockReceipts,
	}
	
	// Guardar bloque
	blockData, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("error serializando bloque: %w", err)
	}
	
	if err := app.storage.SaveBlock(app.currentBlockHeight, blockData); err != nil {
		return fmt.Errorf("error guardando bloque: %w", err)
	}
	
	// Guardar altura del último bloque
	if err := app.storage.SaveLatestHeight(app.currentBlockHeight); err != nil {
		logger.Warn("Error guardando altura: " + err.Error())
	}
	
	logger.Info(fmt.Sprintf("Bloque guardado: height=%d, hash=%s, transactions=%d", 
		app.currentBlockHeight, blockHashStr[:8], len(app.currentBlockTxs)))
	
	return nil
}

// convertLogs convierte logs de execution a consensus
func convertLogs(execLogs []execution.Log) []Log {
	logs := make([]Log, len(execLogs))
	for i, log := range execLogs {
		logs[i] = Log{
			Address:     log.Address,
			Topics:      log.Topics,
			Data:        log.Data,
			BlockNumber: 0, // Se actualizará al guardar el bloque
			TxHash:      "",
		}
	}
	return logs
}

// Query permite consultar el estado de la aplicación (nueva API v1.0.1)
func (app *ABCIApp) Query(ctx context.Context, req *abcitypes.QueryRequest) (*abcitypes.QueryResponse, error) {
	// Parsear path del query
	// Formatos esperados:
	// - "balance/{address}" - Obtener balance de cuenta
	// - "account/{address}" - Obtener estado completo de cuenta
	// - "tx/{hash}" - Obtener transacción por hash
	// - "block/{height}" - Obtener bloque por altura
	// - "height" - Obtener altura actual
	
	path := string(req.Path)
	
	switch {
	case path == "height":
		height := uint64(app.state.Height)
		return &abcitypes.QueryResponse{
			Code:  0,
			Value: []byte(fmt.Sprintf("%d", height)),
		}, nil
	
	case len(path) > 8 && path[:8] == "balance/":
		address := path[8:]
		accountState, err := app.executor.GetState(address)
		if err != nil {
			return &abcitypes.QueryResponse{
				Code:  1,
				Log:   fmt.Sprintf("Error obteniendo balance: %v", err),
			}, nil
		}
		
		result := map[string]interface{}{
			"address": address,
			"balance": accountState.Balance,
		}
		
		resultData, _ := json.Marshal(result)
		return &abcitypes.QueryResponse{
			Code:  0,
			Value: resultData,
		}, nil
	
	case len(path) > 7 && path[:7] == "account/":
		address := path[7:]
		accountState, err := app.executor.GetState(address)
		if err != nil {
			return &abcitypes.QueryResponse{
				Code:  1,
				Log:   fmt.Sprintf("Error obteniendo cuenta: %v", err),
			}, nil
		}
		
		resultData, _ := json.Marshal(accountState)
		return &abcitypes.QueryResponse{
			Code:  0,
			Value: resultData,
		}, nil
	
	case len(path) > 3 && path[:3] == "tx/":
		txHash := path[3:]
		txData, err := app.storage.GetTransaction(txHash)
		if err != nil {
			return &abcitypes.QueryResponse{
				Code:  1,
				Log:   fmt.Sprintf("Transacción no encontrada: %s", txHash),
			}, nil
		}
		
		return &abcitypes.QueryResponse{
			Code:  0,
			Value: txData,
		}, nil
	
	case len(path) > 6 && path[:6] == "block/":
		height := uint64(0)
		fmt.Sscanf(path[6:], "%d", &height)
		
		blockData, err := app.storage.GetBlock(height)
		if err != nil {
			return &abcitypes.QueryResponse{
				Code:  1,
				Log:   fmt.Sprintf("Bloque no encontrado: altura %d", height),
			}, nil
		}
		
		return &abcitypes.QueryResponse{
			Code:  0,
			Value: blockData,
		}, nil
	
	default:
		return &abcitypes.QueryResponse{
			Code:  1,
			Log:   fmt.Sprintf("Query path desconocido: %s", path),
		}, nil
	}
}

// CheckTx valida una transacción sin ejecutarla (nueva API v1.0.1)
func (app *ABCIApp) CheckTx(ctx context.Context, req *abcitypes.CheckTxRequest) (*abcitypes.CheckTxResponse, error) {
	var tx Transaction
	if err := json.Unmarshal(req.Tx, &tx); err != nil {
		return &abcitypes.CheckTxResponse{
			Code: 1,
			Log:  fmt.Sprintf("Error decodificando transacción: %v", err),
		}, nil
	}

	// Validación completa de transacción
	if err := app.validateTransactionComplete(&tx); err != nil {
		return &abcitypes.CheckTxResponse{
			Code: 2,
			Log:  fmt.Sprintf("Transacción inválida: %v", err),
		}, nil
	}

	return &abcitypes.CheckTxResponse{
		Code: 0,
		Log:  "OK",
	}, nil
}

// validateTransaction valida una transacción básica
func (app *ABCIApp) validateTransaction(tx *Transaction) error {
	// Validaciones básicas
	if tx.From == "" {
		return fmt.Errorf("dirección remitente vacía")
	}
	
	if !common.IsHexAddress(tx.From) {
		return fmt.Errorf("dirección remitente inválida: %s", tx.From)
	}
	
	if tx.To != "" && !common.IsHexAddress(tx.To) {
		return fmt.Errorf("dirección destino inválida: %s", tx.To)
	}
	
	return nil
}

// validateTransactionComplete valida una transacción completamente (firma, nonce, balance)
func (app *ABCIApp) validateTransactionComplete(tx *Transaction) error {
	// Validaciones básicas primero
	if err := app.validateTransaction(tx); err != nil {
		return err
	}

	// Validar que tenga hash
	if tx.Hash == "" {
		return fmt.Errorf("transacción sin hash")
	}

	// Validar nonce (obtener nonce actual de la cuenta)
	accountState, err := app.executor.GetState(tx.From)
	if err == nil && accountState != nil {
		if tx.Nonce < accountState.Nonce {
			return fmt.Errorf("nonce inválido: esperado >= %d, tiene %d", accountState.Nonce, tx.Nonce)
		}
	}

	// Validar balance suficiente (si hay transferencia de valor)
	if tx.Value != "" && tx.Value != "0" {
		if accountState == nil {
			return fmt.Errorf("cuenta no encontrada: %s", tx.From)
		}

		// Parsear valor
		value, ok := new(big.Int).SetString(tx.Value, 10)
		if !ok {
			return fmt.Errorf("valor inválido: %s", tx.Value)
		}

		// Parsear balance
		balance, ok := new(big.Int).SetString(accountState.Balance, 10)
		if !ok {
			return fmt.Errorf("balance inválido: %s", accountState.Balance)
		}

		// Calcular gas cost
		gasPrice, ok := new(big.Int).SetString(tx.GasPrice, 10)
		if !ok {
			return fmt.Errorf("gas price inválido: %s", tx.GasPrice)
		}

		gasCost := new(big.Int).Mul(gasPrice, big.NewInt(int64(tx.GasLimit)))
		totalCost := new(big.Int).Add(value, gasCost)

		// Validar balance suficiente
		if balance.Cmp(totalCost) < 0 {
			return fmt.Errorf("balance insuficiente: tiene %s, necesita %s", balance.String(), totalCost.String())
		}
	}

	// Validar firma criptográfica
	if len(tx.Signature) == 0 {
		return fmt.Errorf("transacción sin firma")
	}

	// Convertir transacción a mapa para validación de firma
	txMap := map[string]interface{}{
		"hash":      tx.Hash,
		"from":      tx.From,
		"to":        tx.To,
		"value":     tx.Value,
		"data":      tx.Data,
		"gasLimit":  tx.GasLimit,
		"gasPrice":  tx.GasPrice,
		"nonce":     tx.Nonce,
		"signature": tx.Signature,
	}

	// Verificar firma
	_, err = cryptosigner.VerifyTransactionSignature(txMap)
	if err != nil {
		return fmt.Errorf("firma criptográfica inválida: %w", err)
	}

	// Verificar que el hash de la transacción sea correcto
	// Calcular hash esperado
	expectedHash, err := cryptosigner.CalculateTransactionHash(txMap)
	if err != nil {
		return fmt.Errorf("error calculando hash de transacción: %w", err)
	}

	// Comparar hash
	if tx.Hash != expectedHash.Hex() {
		return fmt.Errorf("hash de transacción inválido: esperado %s, tiene %s", expectedHash.Hex(), tx.Hash)
	}
	
	return nil
}

// buildEvents construye eventos a partir del resultado de ejecución
func (app *ABCIApp) buildEvents(result *execution.ExecutionResult) []abcitypes.Event {
	events := []abcitypes.Event{}
	
	// Evento de ejecución
	events = append(events, abcitypes.Event{
		Type: "execution",
		Attributes: []abcitypes.EventAttribute{
			{Key: "success", Value: fmt.Sprintf("%t", result.Success)},
			{Key: "gas_used", Value: fmt.Sprintf("%d", result.GasUsed)},
		},
	})
	
	// Eventos de logs de contratos
	for _, log := range result.Logs {
		events = append(events, abcitypes.Event{
			Type: "contract_log",
			Attributes: []abcitypes.EventAttribute{
				{Key: "address", Value: log.Address},
			},
		})
	}
	
	return events
}

// PrepareProposal prepara una propuesta de bloque (nueva API v1.0.1)
func (app *ABCIApp) PrepareProposal(ctx context.Context, req *abcitypes.PrepareProposalRequest) (*abcitypes.PrepareProposalResponse, error) {
	fmt.Fprintf(os.Stdout, "[ABCI] PrepareProposal llamado: height=%d, maxTxBytes=%d\n", req.Height, req.MaxTxBytes)
	os.Stdout.Sync()
	
	txs := make([][]byte, 0)
	var totalBytes int64
	
	// Primero, agregar transacciones del mempool local si está disponible
	if app.getMempool != nil {
		localMempool := app.getMempool()
		fmt.Fprintf(os.Stdout, "[ABCI] Mempool local tiene %d transacciones\n", len(localMempool))
		os.Stdout.Sync()
		
		for i, tx := range localMempool {
			// Serializar transacción a JSON
			txBytes, err := json.Marshal(tx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ABCI] ERROR serializando transacción %d: %v\n", i, err)
				os.Stderr.Sync()
				continue // Saltar si no se puede serializar
			}
			
			// Verificar límite de bytes
			if totalBytes+int64(len(txBytes)) > req.MaxTxBytes {
				fmt.Fprintf(os.Stdout, "[ABCI] Límite de bytes alcanzado: %d + %d > %d\n", totalBytes, len(txBytes), req.MaxTxBytes)
				os.Stdout.Sync()
				break
			}
			
			txs = append(txs, txBytes)
			totalBytes += int64(len(txBytes))
			fmt.Fprintf(os.Stdout, "[ABCI] Transacción %s agregada a propuesta (total: %d bytes)\n", tx.Hash, totalBytes)
			os.Stdout.Sync()
		}
	} else {
		fmt.Fprintf(os.Stderr, "[ABCI] ADVERTENCIA: getMempool es nil, no se pueden incluir transacciones del mempool local\n")
		os.Stderr.Sync()
	}
	
	// Luego, agregar transacciones que vienen de CometBFT (si hay espacio)
	for _, tx := range req.Txs {
		if totalBytes+int64(len(tx)) > req.MaxTxBytes {
			break
		}
		
		// Evitar duplicados: verificar si la transacción ya está en la lista
		isDuplicate := false
		for _, existingTx := range txs {
			if string(existingTx) == string(tx) {
				isDuplicate = true
				break
			}
		}
		
		if !isDuplicate {
			txs = append(txs, tx)
			totalBytes += int64(len(tx))
		}
	}
	
	fmt.Fprintf(os.Stdout, "[ABCI] PrepareProposal retornando %d transacciones (total bytes: %d/%d)\n", len(txs), totalBytes, req.MaxTxBytes)
	os.Stdout.Sync()
	
	return &abcitypes.PrepareProposalResponse{Txs: txs}, nil
}

// ProcessProposal procesa una propuesta de bloque (nueva API v1.0.1)
func (app *ABCIApp) ProcessProposal(ctx context.Context, req *abcitypes.ProcessProposalRequest) (*abcitypes.ProcessProposalResponse, error) {
	// Por ahora, aceptamos todas las propuestas
	return &abcitypes.ProcessProposalResponse{
		Status: abcitypes.PROCESS_PROPOSAL_STATUS_ACCEPT,
	}, nil
}

// ExtendVote extiende un voto (nueva API v1.0.1)
func (app *ABCIApp) ExtendVote(ctx context.Context, req *abcitypes.ExtendVoteRequest) (*abcitypes.ExtendVoteResponse, error) {
	// Por ahora, no extendemos votos
	return &abcitypes.ExtendVoteResponse{}, nil
}

// VerifyVoteExtension verifica una extensión de voto (nueva API v1.0.1)
func (app *ABCIApp) VerifyVoteExtension(ctx context.Context, req *abcitypes.VerifyVoteExtensionRequest) (*abcitypes.VerifyVoteExtensionResponse, error) {
	// Por ahora, aceptamos todas las extensiones de voto
	return &abcitypes.VerifyVoteExtensionResponse{
		Status: abcitypes.VERIFY_VOTE_EXTENSION_STATUS_ACCEPT,
	}, nil
}

// ListSnapshots retorna snapshots disponibles (nueva API v1.0.1)
func (app *ABCIApp) ListSnapshots(ctx context.Context, req *abcitypes.ListSnapshotsRequest) (*abcitypes.ListSnapshotsResponse, error) {
	return &abcitypes.ListSnapshotsResponse{}, nil
}

// OfferSnapshot ofrece un snapshot (nueva API v1.0.1)
func (app *ABCIApp) OfferSnapshot(ctx context.Context, req *abcitypes.OfferSnapshotRequest) (*abcitypes.OfferSnapshotResponse, error) {
	return &abcitypes.OfferSnapshotResponse{
		Result: abcitypes.OFFER_SNAPSHOT_RESULT_REJECT,
	}, nil
}

// LoadSnapshotChunk carga un chunk de snapshot (nueva API v1.0.1)
func (app *ABCIApp) LoadSnapshotChunk(ctx context.Context, req *abcitypes.LoadSnapshotChunkRequest) (*abcitypes.LoadSnapshotChunkResponse, error) {
	return &abcitypes.LoadSnapshotChunkResponse{}, nil
}

// ApplySnapshotChunk aplica un chunk de snapshot (nueva API v1.0.1)
func (app *ABCIApp) ApplySnapshotChunk(ctx context.Context, req *abcitypes.ApplySnapshotChunkRequest) (*abcitypes.ApplySnapshotChunkResponse, error) {
	return &abcitypes.ApplySnapshotChunkResponse{
		Result: abcitypes.APPLY_SNAPSHOT_CHUNK_RESULT_REJECT_SNAPSHOT,
	}, nil
}

// hasValidatorChanges compara dos sets de validadores para detectar cambios reales
func hasValidatorChanges(current []abcitypes.ValidatorUpdate, updates []abcitypes.ValidatorUpdate) bool {
	// Si la cantidad es diferente, hay cambios
	if len(current) != len(updates) {
		fmt.Fprintf(os.Stdout, "[ABCI] Cambio detectado: cantidad diferente (actual=%d, updates=%d)\n", len(current), len(updates))
		os.Stdout.Sync()
		return true
	}
	
	// Crear mapas para comparación rápida
	currentMap := make(map[string]int64) // PubKey -> Power
	updatesMap := make(map[string]int64)
	
	for _, v := range current {
		key := string(v.PubKeyBytes)
		currentMap[key] = v.Power
	}
	
	for _, v := range updates {
		key := string(v.PubKeyBytes)
		updatesMap[key] = v.Power
	}
	
	// Verificar si hay validadores nuevos o removidos
	for key, power := range updatesMap {
		if currentPower, exists := currentMap[key]; !exists {
			// Nuevo validador
			fmt.Fprintf(os.Stdout, "[ABCI] Cambio detectado: nuevo validador (power=%d)\n", power)
			os.Stdout.Sync()
			return true
		} else if currentPower != power {
			// Power cambiado
			fmt.Fprintf(os.Stdout, "[ABCI] Cambio detectado: power cambiado (key=%s, old=%d, new=%d)\n", key[:8], currentPower, power)
			os.Stdout.Sync()
			return true
		}
	}
	
	// Verificar si hay validadores removidos
	for key := range currentMap {
		if _, exists := updatesMap[key]; !exists {
			// Validador removido
			fmt.Fprintf(os.Stdout, "[ABCI] Cambio detectado: validador removido (key=%s)\n", key[:8])
			os.Stdout.Sync()
			return true
		}
	}
	
	// No hay cambios
	fmt.Fprintf(os.Stdout, "[ABCI] No se detectaron cambios en validadores\n")
	os.Stdout.Sync()
	return false
}


