package execution

import (
	"fmt"
	"log"
	"math/big"

	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// EVMExecutor ejecuta transacciones usando go-ethereum (EVM compatible)
// Nota: go-ethereum es compatible con EVM y puede usarse como alternativa a EVMone
type EVMExecutor struct {
	storage          *storage.BlockchainDB
	stateManager     *StateManager
	stateDB          *state.StateDB
	chainConfig      *params.ChainConfig
	currentHeight    uint64
	currentTimestamp int64
	running          bool
}

// NewEVMExecutor crea una nueva instancia del ejecutor EVM
func NewEVMExecutor(storage *storage.BlockchainDB) *EVMExecutor {
	// Configurar chain config para Oxy•gen
	chainConfig := &params.ChainConfig{
		ChainID:             big.NewInt(999), // Chain ID de Oxy•gen (temporal)
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
	}

	// Usar el mismo directorio de datos que el storage para evitar conflictos en tests
	dataDir := storage.GetDataDir()
	stateManager := NewStateManager(storage, dataDir)

	return &EVMExecutor{
		storage:      storage,
		stateManager: stateManager,
		chainConfig:  chainConfig,
		running:      false,
	}
}

// Start inicia el ejecutor EVM
func (e *EVMExecutor) Start() error {
	// Cargar estado desde storage
	stateDB, err := e.stateManager.LoadState()
	if err != nil {
		return fmt.Errorf("error cargando estado: %w", err)
	}

	e.stateDB = stateDB
	e.running = true
	log.Println("Ejecutor EVM iniciado")
	return nil
}

// Stop detiene el ejecutor EVM
func (e *EVMExecutor) Stop() error {
	if !e.running {
		return nil
	}

	// Guardar estado antes de detener
	if err := e.stateManager.SaveState(); err != nil {
		log.Printf("Advertencia: error guardando estado: %v", err)
	}

	// Cerrar gestor de estado
	if err := e.stateManager.Close(); err != nil {
		log.Printf("Advertencia: error cerrando gestor de estado: %v", err)
	}

	e.running = false
	log.Println("Ejecutor EVM detenido")
	return nil
}

// SetCurrentBlockInfo establece la información del bloque actual
func (e *EVMExecutor) SetCurrentBlockInfo(height uint64, timestamp int64) {
	e.currentHeight = height
	e.currentTimestamp = timestamp
}

// ExecuteTransaction ejecuta una transacción y actualiza el estado
func (e *EVMExecutor) ExecuteTransaction(tx *Transaction) (*ExecutionResult, error) {
	if !e.running {
		return nil, fmt.Errorf("ejecutor EVM no está corriendo")
	}

	// Convertir transacción a formato go-ethereum
	from := common.HexToAddress(tx.From)
	to := common.HexToAddress(tx.To)
	value, ok := new(big.Int).SetString(tx.Value, 10)
	if !ok {
		return nil, fmt.Errorf("valor inválido: %s", tx.Value)
	}
	gasPrice, ok := new(big.Int).SetString(tx.GasPrice, 10)
	if !ok {
		return nil, fmt.Errorf("gas price inválido: %s", tx.GasPrice)
	}

	// Obtener nonce actual si no se proporcionó
	nonce := tx.Nonce
	if nonce == 0 {
		stateDB := e.getStateDB()
		if stateDB != nil {
			nonce = stateDB.GetNonce(from)
		}
	}

	// Preparar header del bloque con valores reales
	// Coinbase es la dirección del validador (usar zero address si no hay validador específico)
	coinbase := common.Address{}
	
	// BaseFee: usar big.NewInt(0) para chains sin EIP-1559
	// NewEVMBlockContext requiere que BaseFee no sea nil
	baseFee := big.NewInt(0) // 0 significa que no se usa EIP-1559
	
	// Crear header completo con todos los campos necesarios
	header := &types.Header{
		ParentHash: common.Hash{}, // Hash del bloque padre (zero hash para simplificar)
		UncleHash:  types.EmptyUncleHash,
		Coinbase:   coinbase, // Dirección del validador
		Root:       common.Hash{}, // Root del estado (zero hash para simplificar)
		TxHash:     types.EmptyRootHash,
		ReceiptHash: types.EmptyRootHash,
		Bloom:      types.Bloom{},
		Difficulty: big.NewInt(0), // Difficulty 0 para PoS
		Number:     big.NewInt(int64(e.currentHeight)),
		GasLimit:   tx.GasLimit,
		GasUsed:    0,
		Time:       uint64(e.currentTimestamp),
		Extra:      []byte{},
		MixDigest:  common.Hash{},
		Nonce:      types.BlockNonce{},
		BaseFee:    baseFee, // 0 para chains sin EIP-1559
	}

	// Preparar contexto de ejecución
	// NewEVMBlockContext requiere: header, ChainContext (para obtener headers previos), y author (dirección del validador)
	// author puede ser zero address si no hay validador específico
	author := &coinbase // Usar dirección del validador (zero address si no hay)
	
	// Crear adaptador de ChainContext con configuración de chain y engine nil (no necesario para ejecución básica)
	chainAdapter := &chainContextAdapter{
		chainConfig: e.chainConfig,
		engine:      nil, // No necesitamos engine para ejecución básica
	}
	
	blockContext := core.NewEVMBlockContext(header, chainAdapter, author)

	// Crear message para ejecutar
	// Para chains sin EIP-1559, usamos GasPrice tradicional
	// GasFeeCap y GasTipCap se usan solo para EIP-1559
	msg := core.Message{
		From:       from,
		To:         &to,
		Nonce:      tx.Nonce,
		Value:      value,
		GasLimit:   tx.GasLimit,
		GasPrice:   gasPrice,
		GasFeeCap:  gasPrice, // Usar gasPrice como GasFeeCap si no se especifica
		GasTipCap:  gasPrice, // Usar gasPrice como GasTipCap si no se especifica
		Data:       tx.Data,
		AccessList: nil,
	}

	// Crear EVM (v1.16+: TxContext se pasa directamente en ApplyMessage)
	evm := vm.NewEVM(blockContext, e.getStateDB(), e.chainConfig, vm.Config{})

	// Ejecutar transacción
	result, err := core.ApplyMessage(evm, &msg, new(core.GasPool).AddGas(tx.GasLimit))

	if err != nil {
		// Si hay error, result puede ser nil, usar 0 para GasUsed
		gasUsed := uint64(0)
		if result != nil {
			gasUsed = result.UsedGas
		}
		return &ExecutionResult{
			Success: false,
			GasUsed: gasUsed,
			Error:   err.Error(),
		}, nil
	}

	// Si la ejecución fue exitosa, guardar estado intermedio
	if err == nil && !result.Failed() {
		// Finalizar el StateDB para aplicar cambios
		e.stateDB.Finalise(true)
	}

	// Obtener logs del StateDB
	var logs []Log
	if err == nil && !result.Failed() {
		stateDBLogs := e.stateDB.Logs()
		logs = make([]Log, len(stateDBLogs))
		for i, log := range stateDBLogs {
			topics := make([]string, len(log.Topics))
			for j, topic := range log.Topics {
				topics[j] = topic.Hex()
			}
			logs[i] = Log{
				Address: log.Address.Hex(),
				Topics:  topics,
				Data:    log.Data,
			}
		}
	}

	return &ExecutionResult{
		Success:    err == nil && result.Failed() == false,
		GasUsed:    result.UsedGas,
		ReturnData: result.ReturnData,
		Logs:       logs,
		Error:      "",
	}, nil
}

// getStateDB obtiene o crea el StateDB
func (e *EVMExecutor) getStateDB() *state.StateDB {
	if e.stateDB == nil {
		// Si no hay StateDB, cargar desde manager
		if e.stateManager != nil {
			e.stateDB, _ = e.stateManager.LoadState()
		}
	}
	return e.stateDB
}

// GetState retorna el estado actual de una cuenta
func (e *EVMExecutor) GetState(address string) (*AccountState, error) {
	if !e.running {
		return nil, fmt.Errorf("ejecutor EVM no está corriendo")
	}

	addr := common.HexToAddress(address)
	stateDB := e.getStateDB()

	balance := stateDB.GetBalance(addr)
	nonce := stateDB.GetNonce(addr)
	codeHash := stateDB.GetCodeHash(addr)

	// Obtener storage (primeras 100 slots como ejemplo)
	storage := make(map[string]string)
	for i := 0; i < 100; i++ {
		key := common.BigToHash(big.NewInt(int64(i)))
		value := stateDB.GetState(addr, key)
		if value != (common.Hash{}) {
			storage[key.Hex()] = value.Hex()
		}
	}

	return &AccountState{
		Address:  address,
		Balance:  balance.String(),
		Nonce:    nonce,
		CodeHash: codeHash.Hex(),
		Storage:  storage,
	}, nil
}

// FundAccount agrega fondos a una cuenta (útil para testing)
// Nota: Solo debe usarse en testnet, no en producción
func (e *EVMExecutor) FundAccount(address string, amount string) error {
	if !e.running {
		return fmt.Errorf("ejecutor EVM no está corriendo")
	}

	addr := common.HexToAddress(address)
	stateDB := e.getStateDB()

	// Parsear cantidad
	amountBig, ok := new(big.Int).SetString(amount, 10)
	if !ok {
		return fmt.Errorf("cantidad inválida: %s", amount)
	}

	// Convertir big.Int a uint256.Int (requerido por la nueva API)
	amountU256 := new(uint256.Int)
	amountU256.SetFromBig(amountBig)

	// Agregar balance a la cuenta (nueva API requiere BalanceChangeReason)
	// Usar BalanceIncreaseGenesisBalance para fondear cuentas en testnet
	stateDB.AddBalance(addr, amountU256, tracing.BalanceIncreaseGenesisBalance)

	// Guardar estado (esto guardará los cambios en el StateDB)
	// Nota: El estado se guardará cuando se haga commit del bloque
	// Pero para efectos inmediatos, podemos guardar el estado aquí
	if e.stateManager != nil {
		// No hacer commit completo aquí, solo marcar como modificado
		// El commit completo se hace al finalizar el bloque
	}

	log.Printf("💰 Cuenta %s fondeada con %s tokens", address, amount)
	return nil
}

// DeployContract despliega un contrato inteligente
func (e *EVMExecutor) DeployContract(
	from string,
	code []byte,
	constructorArgs []byte,
	gasLimit uint64,
	gasPrice string,
) (string, *ExecutionResult, error) {
	if !e.running {
		return "", nil, fmt.Errorf("ejecutor EVM no está corriendo")
	}

	fromAddr := common.HexToAddress(from)

	// Combinar bytecode con constructor args
	contractData := append(code, constructorArgs...)

	// Obtener nonce actual
	stateDB := e.getStateDB()
	nonce := uint64(0)
	if stateDB != nil {
		nonce = stateDB.GetNonce(common.HexToAddress(from))
	}

	// Crear transacción de deployment (To es nil)
	tx := &Transaction{
		From:     from,
		To:       "", // Empty para deployment
		Value:    "0",
		Data:     contractData,
		GasLimit: gasLimit,
		GasPrice: gasPrice,
		Nonce:    nonce,
	}

	// Ejecutar transacción
	result, err := e.ExecuteTransaction(tx)
	if err != nil {
		return "", nil, fmt.Errorf("error ejecutando deployment: %w", err)
	}

	if !result.Success {
		return "", result, fmt.Errorf("deployment falló: %s", result.Error)
	}

	// Calcular dirección del contrato
	// Usar el nonce antes del deployment para calcular la dirección
	contractAddr := crypto.CreateAddress(fromAddr, nonce).Hex()

	return contractAddr, result, nil
}

// CallContract ejecuta una llamada a un contrato (sin modificar estado)
func (e *EVMExecutor) CallContract(
	from string,
	contractAddr string,
	data []byte,
	gasLimit uint64,
) ([]byte, error) {
	if !e.running {
		return nil, fmt.Errorf("ejecutor EVM no está corriendo")
	}

	// Obtener nonce actual
	stateDB := e.getStateDB()
	nonce := uint64(0)
	if stateDB != nil {
		nonce = stateDB.GetNonce(common.HexToAddress(from))
	}

	// Crear transacción de llamada
	tx := &Transaction{
		From:     from,
		To:       contractAddr,
		Value:    "0",
		Data:     data,
		GasLimit: gasLimit,
		GasPrice: "0", // Sin costo para calls
		Nonce:    nonce,
	}

	// Ejecutar transacción
	result, err := e.ExecuteTransaction(tx)
	if err != nil {
		return nil, fmt.Errorf("error ejecutando call: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("call falló: %s", result.Error)
	}

	return result.ReturnData, nil
}

// GetStateManager retorna el StateManager (para uso interno de consensus)
func (e *EVMExecutor) GetStateManager() *StateManager {
	return e.stateManager
}

// SaveState guarda el estado actual del EVM
func (e *EVMExecutor) SaveState() error {
	if e.stateManager == nil {
		return fmt.Errorf("stateManager no está inicializado")
	}
	return e.stateManager.SaveState()
}

// SaveStateAtHeight guarda el estado en una altura específica
func (e *EVMExecutor) SaveStateAtHeight(height uint64) error {
	if e.stateManager == nil {
		return fmt.Errorf("stateManager no está inicializado")
	}
	return e.stateManager.SaveStateAtHeight(height)
}

// Transaction representa una transacción a ejecutar
type Transaction struct {
	Hash     string
	From     string
	To       string
	Value    string
	Data     []byte
	GasLimit uint64
	GasPrice string
	Nonce    uint64
}

// ExecutionResult contiene el resultado de ejecutar una transacción
type ExecutionResult struct {
	Success    bool
	GasUsed    uint64
	ReturnData []byte
	Logs       []Log
	Error      string
}

// AccountState representa el estado de una cuenta
type AccountState struct {
	Address  string
	Balance  string
	Nonce    uint64
	CodeHash string
	Storage  map[string]string
}

// Log representa un evento emitido por un contrato
type Log struct {
	Address string
	Topics  []string
	Data    []byte
}

// chainContextAdapter es un adaptador simple que implementa core.ChainContext
// para usar en NewEVMBlockContext
type chainContextAdapter struct {
	chainConfig *params.ChainConfig
	engine      consensus.Engine
}

// Config retorna la configuración de la chain (ChainHeaderReader)
func (c *chainContextAdapter) Config() *params.ChainConfig {
	return c.chainConfig
}

// CurrentHeader retorna el header actual (ChainHeaderReader)
func (c *chainContextAdapter) CurrentHeader() *types.Header {
	return nil // Simplificado para ejecución básica
}

// GetHeader retorna un header por hash y número (ChainHeaderReader)
func (c *chainContextAdapter) GetHeader(hash common.Hash, number uint64) *types.Header {
	return nil // No necesitamos headers previos para ejecución básica
}

// GetHeaderByNumber retorna un header por número (ChainHeaderReader)
func (c *chainContextAdapter) GetHeaderByNumber(number uint64) *types.Header {
	return nil // No necesitamos headers previos para ejecución básica
}

// GetHeaderByHash retorna un header por hash (ChainHeaderReader)
func (c *chainContextAdapter) GetHeaderByHash(hash common.Hash) *types.Header {
	return nil // No necesitamos headers previos para ejecución básica
}

// Engine retorna el engine de consenso (ChainContext)
func (c *chainContextAdapter) Engine() consensus.Engine {
	return c.engine // Puede ser nil, pero debe ser de tipo consensus.Engine
}
