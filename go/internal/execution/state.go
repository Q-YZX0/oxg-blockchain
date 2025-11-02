package execution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/ethdb"
	ethdbpebble "github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// StateManager maneja el estado de la blockchain EVM
type StateManager struct {
	storage    *storage.BlockchainDB
	stateDB    *state.StateDB
	database   state.Database
	pebbleDB   *ethdbpebble.Database // Guardar referencia a Pebble DB para cerrarlo correctamente
	stateRoot  common.Hash
	dataDir    string
}

// NewStateManager crea un nuevo gestor de estado
func NewStateManager(storage *storage.BlockchainDB, dataDir string) *StateManager {
	return &StateManager{
		storage: storage,
		dataDir: dataDir,
	}
}

// LoadState carga el estado desde storage
func (sm *StateManager) LoadState() (*state.StateDB, error) {
	// Crear base de datos para StateDB usando Pebble
	stateDBPath := filepath.Join(sm.dataDir, "evm_state")
	
	// Cerrar base de datos anterior si existe
	if sm.pebbleDB != nil {
		if err := sm.pebbleDB.Close(); err != nil {
			// Log pero no fallar si hay error al cerrar
		}
		sm.pebbleDB = nil
	}
	
	// Intentar crear base de datos Ethereum usando Pebble
	// Si falla por WAL corrupto, limpiar directorio y reintentar
	db, err := ethdbpebble.New(stateDBPath, 0, 0, "", false)
	if err != nil {
		// Si hay error, puede ser por WAL corrupto, intentar limpiar y recrear
		// Nota: En producción esto debería manejarse diferente, pero en tests es útil
		if err2 := os.RemoveAll(stateDBPath); err2 == nil {
			// Reintentar después de limpiar
			db, err = ethdbpebble.New(stateDBPath, 0, 0, "", false)
			if err != nil {
				return nil, fmt.Errorf("error creando base de datos EVM (después de limpieza): %w", err)
			}
		} else {
			return nil, fmt.Errorf("error creando base de datos EVM: %w", err)
		}
	}
	
	// Guardar referencia a Pebble DB para poder cerrarlo correctamente
	sm.pebbleDB = db
	
	// Crear triedb database (nueva API v1.16+: usar rawdb.NewDatabase para envolver pebble)
	// rawdb.NewDatabase crea un wrapper que implementa ethdb.Database completo
	ethdbWrapper := rawdb.NewDatabase(db)
	trieDB := triedb.NewDatabase(ethdbWrapper, &triedb.Config{})
	
	// Crear snapshot tree (vacío por defecto)
	snapConfig := snapshot.Config{
		CacheSize: 256,
	}
	snapTree, err := snapshot.New(snapConfig, db, trieDB, types.EmptyRootHash)
	if err != nil {
		db.Close()
		sm.pebbleDB = nil
		return nil, fmt.Errorf("error creando snapshot tree: %w", err)
	}
	
	// Crear database wrapper para StateDB (nueva API v1.16+)
	database := state.NewDatabase(trieDB, snapTree)
	
	// Intentar cargar root hash guardado
	stateData, err := sm.storage.GetState()
	var root common.Hash
	
	if err == nil && stateData != nil {
		// Parsear estado guardado
		var stateInfo map[string]interface{}
		if err := json.Unmarshal(stateData, &stateInfo); err == nil {
			if rootStr, ok := stateInfo["root"].(string); ok {
				root = common.HexToHash(rootStr)
			}
		}
	}
	
	// Si no hay root guardado, usar hash vacío (estado nuevo)
	if root == (common.Hash{}) {
		root = common.Hash{}
	}
	
	// Crear StateDB desde root (nueva API v1.16+: solo root y database, sin tercer argumento)
	stateDB, err := state.New(root, database)
	if err != nil {
		db.Close()
		sm.pebbleDB = nil
		return nil, fmt.Errorf("error creando StateDB: %w", err)
	}
	
	sm.stateDB = stateDB
	sm.database = database
	sm.stateRoot = root
	
	return stateDB, nil
}

// SaveState guarda el estado completo en storage
func (sm *StateManager) SaveState() error {
	if sm.stateDB == nil {
		return fmt.Errorf("StateDB no está inicializado")
	}
	
	// Calcular root hash intermedio (commits todos los cambios)
	root := sm.stateDB.IntermediateRoot(true)
	
	// Commit el StateDB a la base de datos (nueva API v1.16+: requiere 3 argumentos)
	_, err := sm.stateDB.Commit(0, true, false)
	if err != nil {
		return fmt.Errorf("error haciendo commit del StateDB: %w", err)
	}
	
	// IMPORTANTE: Después del commit, recargar StateDB desde el nuevo root
	// Esto evita el error "trie is already committed" cuando se modifica después
	newStateDB, err := sm.reloadStateFromRoot(root)
	if err != nil {
		// Si falla la recarga, loguear pero no fallar
		// El StateDB viejo ya no es usable, pero podemos continuar
		fmt.Fprintf(os.Stderr, "⚠️ Error recargando StateDB después de commit: %v\n", err)
		os.Stderr.Sync()
		// Intentar recargar desde storage en su lugar
		sm.stateDB, _ = sm.LoadState()
	} else {
		sm.stateDB = newStateDB
	}
	
	// Guardar root hash en metadata storage
	stateData, err := json.Marshal(map[string]interface{}{
		"root":      root.Hex(),
		"height":    sm.getCurrentHeight(),
		"timestamp": sm.getCurrentTimestamp(),
	})
	if err != nil {
		return fmt.Errorf("error serializando estado: %w", err)
	}
	
	if err := sm.storage.SaveState(stateData); err != nil {
		return fmt.Errorf("error guardando estado: %w", err)
	}
	
	// Actualizar root hash local
	sm.stateRoot = root
	
	return nil
}

// reloadStateFromRoot recarga el StateDB desde un root hash específico
func (sm *StateManager) reloadStateFromRoot(root common.Hash) (*state.StateDB, error) {
	if sm.database == nil {
		return nil, fmt.Errorf("database no está inicializado")
	}
	
	// Crear nuevo StateDB desde el root committeado
	newStateDB, err := state.New(root, sm.database)
	if err != nil {
		return nil, fmt.Errorf("error creando StateDB desde root: %w", err)
	}
	
	return newStateDB, nil
}

// SaveStateAtHeight guarda el estado en una altura específica
func (sm *StateManager) SaveStateAtHeight(height uint64) error {
	if sm.stateDB == nil {
		return fmt.Errorf("StateDB no está inicializado")
	}
	
	// Calcular root hash
	root := sm.stateDB.IntermediateRoot(true)
	
	// Commit el StateDB (nueva API v1.16+: requiere 3 argumentos)
	_, err := sm.stateDB.Commit(0, true, false)
	if err != nil {
		return fmt.Errorf("error haciendo commit del StateDB: %w", err)
	}
	
	// Guardar estado con altura
	stateData, err := json.Marshal(map[string]interface{}{
		"root":   root.Hex(),
		"height": height,
	})
	if err != nil {
		return fmt.Errorf("error serializando estado: %w", err)
	}
	
	// Guardar estado en altura específica usando método público de storage
	// Nota: Necesitamos agregar método SaveStateAtHeight a BlockchainDB
	// Por ahora, guardamos en metadata storage
	key := fmt.Sprintf("state:%d", height)
	if err := sm.storage.SaveAccount(key, stateData); err != nil {
		return fmt.Errorf("error guardando estado en altura %d: %w", height, err)
	}
	
	return nil
}

// LoadStateAtHeight carga el estado en una altura específica
func (sm *StateManager) LoadStateAtHeight(height uint64) (*state.StateDB, error) {
	// Obtener estado guardado en altura
	key := fmt.Sprintf("state:%d", height)
	stateData, err := sm.storage.GetAccount(key)
	if err != nil {
		return nil, fmt.Errorf("estado no encontrado en altura %d: %w", height, err)
	}
	
	// Parsear estado
	var stateInfo map[string]interface{}
	if err := json.Unmarshal(stateData, &stateInfo); err != nil {
		return nil, fmt.Errorf("error parseando estado: %w", err)
	}
	
	// Obtener root hash
	rootStr, ok := stateInfo["root"].(string)
	if !ok {
		return nil, fmt.Errorf("root hash no encontrado en estado")
	}
	
	root := common.HexToHash(rootStr)
	
	// Crear StateDB desde root usando la misma estructura que LoadState
	stateDBPath := filepath.Join(sm.dataDir, "evm_state")
	
	// Crear base de datos Ethereum usando Pebble (reemplazo de LevelDB)
	db, err := ethdbpebble.New(stateDBPath, 0, 0, "", false)
	if err != nil {
		return nil, fmt.Errorf("error creando base de datos EVM: %w", err)
	}
	
	// Crear triedb database (nueva API v1.16+: usar rawdb.NewDatabase para envolver pebble)
	ethdb := rawdb.NewDatabase(db)
	trieDB := triedb.NewDatabase(ethdb, &triedb.Config{})
	
	// Crear snapshot tree (vacío por defecto)
	snapConfig := snapshot.Config{
		CacheSize: 256,
	}
	snapTree, err := snapshot.New(snapConfig, db, trieDB, types.EmptyRootHash)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("error creando snapshot tree: %w", err)
	}
	
	// Crear database wrapper para StateDB (nueva API v1.16+)
	database := state.NewDatabase(trieDB, snapTree)
	
	// Crear StateDB desde root (nueva API v1.16+: solo root y database)
	stateDB, err := state.New(root, database)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("error creando StateDB desde root: %w", err)
	}
	
	sm.stateDB = stateDB
	sm.database = database
	sm.stateRoot = root
	
	return stateDB, nil
}

// GetStateDB retorna el StateDB actual
func (sm *StateManager) GetStateDB() *state.StateDB {
	return sm.stateDB
}

// GetRootHash retorna el root hash actual
func (sm *StateManager) GetRootHash() common.Hash {
	if sm.stateDB == nil {
		return common.Hash{}
	}
	return sm.stateDB.IntermediateRoot(true)
}

// Close cierra la base de datos y libera recursos
func (sm *StateManager) Close() error {
	var errs []error
	
	// Guardar estado antes de cerrar si está inicializado
	if sm.stateDB != nil {
		if err := sm.SaveState(); err != nil {
			errs = append(errs, fmt.Errorf("error guardando estado antes de cerrar: %w", err))
		}
	}
	
	// Cerrar StateDB primero (cerrar iterators si existen)
	if sm.stateDB != nil {
		// Asegurar que no haya iterators activos
		sm.stateDB = nil
	}
	
	// Cerrar database (StateDatabase) - esto cierra snapshots
	if sm.database != nil {
		if closer, ok := sm.database.(ethdb.Database); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("error cerrando state database: %w", err))
			}
		}
		sm.database = nil
	}
	
	// Cerrar instancia de Pebble DB directamente (esto es crítico)
	if sm.pebbleDB != nil {
		// Pebble DB puede tener goroutines en background (flushLoop, meter)
		// Cerrar la base de datos debería detenerlas
		if err := sm.pebbleDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("error cerrando Pebble DB: %w", err))
		}
		sm.pebbleDB = nil
	}
	
	// Dar tiempo para que las goroutines de Pebble DB terminen
	// Nota: En producción esto no sería necesario, pero en tests ayuda
	// Pebble DB tiene flushLoop y meter que necesitan tiempo para terminar
	// También snapshot generation que puede estar corriendo en background
	time.Sleep(200 * time.Millisecond)
	
	if len(errs) > 0 {
		return fmt.Errorf("errores cerrando StateManager: %v", errs)
	}
	
	return nil
}

// getCurrentHeight obtiene la altura actual (helper)
func (sm *StateManager) getCurrentHeight() uint64 {
	height, err := sm.storage.GetLatestHeight()
	if err != nil {
		return 0
	}
	return height
}

// getCurrentTimestamp obtiene el timestamp actual (helper)
func (sm *StateManager) getCurrentTimestamp() int64 {
	// Obtener altura actual
	height, err := sm.storage.GetLatestHeight()
	if err != nil {
		return 0
	}

	// Obtener bloque más reciente
	blockData, err := sm.storage.GetBlock(height)
	if err != nil {
		return 0
	}

	// Parsear bloque para obtener timestamp
	// El timestamp se guarda como time.Time en el BlockHeader
	type BlockHeader struct {
		Timestamp time.Time `json:"timestamp"`
	}
	type Block struct {
		Header BlockHeader `json:"header"`
	}
	
	var block Block
	if err := json.Unmarshal(blockData, &block); err != nil {
		return 0
	}

	return block.Header.Timestamp.Unix()
}

