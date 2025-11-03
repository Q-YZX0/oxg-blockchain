package config

import (
	"os"
	"path/filepath"
)

// Config contiene toda la configuración del nodo blockchain
type Config struct {
	// Directorio de datos
	DataDir string

	// Chain ID
	ChainID string

	// Configuración de validador
	ValidatorAddr string
	ValidatorKey  string

	// Configuración de red mesh
	MeshEndpoint string

	// Configuración de peers P2P (CometBFT)
	PersistentPeers string // Formato: "nodeid@host:port,nodeid2@host2:port2"
	Seeds           string // Formato: "nodeid@host:port,nodeid2@host2:port2"

	// Configuración de logging
	LogLevel string

	// Configuración de CometBFT
	CometBFTHome string

	// Configuración de EVMone
	EVMoneTrace bool

	// Configuración del API REST
	APIEnabled bool
	APIPort    string
	APIHost    string
}

// LoadConfig carga la configuración desde variables de entorno
func LoadConfig() *Config {
	dataDir := getEnv("OXY_DATA_DIR", "./data")
	
	return &Config{
		DataDir:        dataDir,
		ChainID:        getEnv("OXY_CHAIN_ID", "oxy-gen-chain"),
		ValidatorAddr:  getEnv("OXY_VALIDATOR_ADDR", ""),
		ValidatorKey:   getEnv("OXY_VALIDATOR_KEY", ""),
		MeshEndpoint:    getEnv("OXY_MESH_ENDPOINT", "ws://localhost:3001"),
		PersistentPeers: getEnv("OXY_PERSISTENT_PEERS", ""),
		Seeds:           getEnv("OXY_SEEDS", ""),
		LogLevel:        getEnv("OXY_LOG_LEVEL", "info"),
		CometBFTHome:   getEnv("COMETBFT_HOME", filepath.Join(dataDir, "cometbft")),
		EVMoneTrace:    getEnvBool("EVMONE_TRACE", false),
		APIEnabled:     getEnvBool("BLOCKCHAIN_API_ENABLED", true),
		APIPort:         getEnv("BLOCKCHAIN_API_PORT", "8080"),
		APIHost:         getEnv("BLOCKCHAIN_API_HOST", "localhost"),
	}
}

// getEnv obtiene una variable de entorno o retorna el valor por defecto
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool obtiene una variable de entorno booleana
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

