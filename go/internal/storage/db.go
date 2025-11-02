package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
)

// BlockchainDB maneja el almacenamiento de la blockchain
type BlockchainDB struct {
	db      *leveldb.DB
	dataDir string
}

// NewBlockchainDB crea una nueva instancia de la base de datos
func NewBlockchainDB(dataDir string) (*BlockchainDB, error) {
	dbPath := filepath.Join(dataDir, "blockchain.db")
	fmt.Fprintf(os.Stdout, "[Storage] Abriendo LevelDB en: %s\n", dbPath)
	os.Stdout.Sync()
	
	fmt.Fprintf(os.Stdout, "[Storage] Llamando a leveldb.OpenFile()...\n")
	os.Stdout.Sync()
	db, err := leveldb.OpenFile(dbPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Storage] ERROR abriendo LevelDB: %v\n", err)
		os.Stderr.Sync()
		return nil, fmt.Errorf("error abriendo base de datos: %w", err)
	}

	fmt.Fprintf(os.Stdout, "[Storage] LevelDB abierto exitosamente\n")
	os.Stdout.Sync()
	return &BlockchainDB{
		db:      db,
		dataDir: dataDir,
	}, nil
}

// Close cierra la base de datos y espera a que las goroutines terminen
func (b *BlockchainDB) Close() error {
	if b.db == nil {
		return nil
	}
	
	// Cerrar la base de datos
	// LevelDB.Close() debería detener todas las goroutines de background
	// (compaction, memory pool drain, etc.)
	err := b.db.Close()
	b.db = nil
	
	// Dar tiempo para que las goroutines de LevelDB terminen
	// LevelDB tiene goroutines de compaction (mCompaction, tCompaction) y 
	// memory pool drain (mpoolDrain) que necesitan tiempo para terminar
	// Nota: Estas goroutines pueden seguir ejecutándose después de Close()
	// pero se cerrarán cuando el proceso termine (comportamiento conocido de LevelDB)
	time.Sleep(200 * time.Millisecond)
	
	// Forzar garbage collection para ayudar a liberar recursos
	runtime.GC()
	
	return err
}

// GetDataDir retorna el directorio de datos
func (b *BlockchainDB) GetDataDir() string {
	return b.dataDir
}

// SaveBlock guarda un bloque en la base de datos
func (b *BlockchainDB) SaveBlock(height uint64, blockData []byte) error {
	key := []byte(fmt.Sprintf("block:%d", height))
	return b.db.Put(key, blockData, nil)
}

// GetBlock obtiene un bloque por altura
func (b *BlockchainDB) GetBlock(height uint64) ([]byte, error) {
	key := []byte(fmt.Sprintf("block:%d", height))
	return b.db.Get(key, nil)
}

// SaveState guarda el estado de la blockchain
func (b *BlockchainDB) SaveState(stateData []byte) error {
	return b.db.Put([]byte("state:latest"), stateData, nil)
}

// GetState obtiene el estado actual
func (b *BlockchainDB) GetState() ([]byte, error) {
	return b.db.Get([]byte("state:latest"), nil)
}

// SaveTransaction guarda una transacción
func (b *BlockchainDB) SaveTransaction(txHash string, txData []byte) error {
	key := []byte(fmt.Sprintf("tx:%s", txHash))
	return b.db.Put(key, txData, nil)
}

// GetTransaction obtiene una transacción por hash
func (b *BlockchainDB) GetTransaction(txHash string) ([]byte, error) {
	key := []byte(fmt.Sprintf("tx:%s", txHash))
	return b.db.Get(key, nil)
}

// SaveAccount guarda el estado de una cuenta
func (b *BlockchainDB) SaveAccount(address string, accountData []byte) error {
	key := []byte(fmt.Sprintf("account:%s", address))
	return b.db.Put(key, accountData, nil)
}

// GetAccount obtiene el estado de una cuenta
func (b *BlockchainDB) GetAccount(address string) ([]byte, error) {
	key := []byte(fmt.Sprintf("account:%s", address))
	return b.db.Get(key, nil)
}

// SaveLatestHeight guarda la altura del último bloque
func (b *BlockchainDB) SaveLatestHeight(height uint64) error {
	heightBytes := []byte(fmt.Sprintf("%d", height))
	return b.db.Put([]byte("height:latest"), heightBytes, nil)
}

// GetLatestHeight obtiene la altura del último bloque
func (b *BlockchainDB) GetLatestHeight() (uint64, error) {
	heightBytes, err := b.db.Get([]byte("height:latest"), nil)
	if err != nil {
		return 0, err
	}
	
	var height uint64
	fmt.Sscanf(string(heightBytes), "%d", &height)
	return height, nil
}

