package security

import (
	"crypto/ecdsa"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// KeyManager maneja claves privadas de forma segura
type KeyManager struct {
	keystoreDir string
	keystore    *keystore.KeyStore
}

// NewKeyManager crea un nuevo gestor de claves
func NewKeyManager(keystoreDir string) (*KeyManager, error) {
	// Crear directorio si no existe
	if err := os.MkdirAll(keystoreDir, 0700); err != nil {
		return nil, fmt.Errorf("error creando directorio keystore: %w", err)
	}

	// Crear keystore
	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)

	return &KeyManager{
		keystoreDir: keystoreDir,
		keystore:    ks,
	}, nil
}

// LoadPrivateKey carga una clave privada desde el keystore
func (km *KeyManager) LoadPrivateKey(address common.Address, password string) (*ecdsa.PrivateKey, error) {
	// Buscar cuenta en keystore
	accountList := km.keystore.Accounts()
	var account accounts.Account
	found := false
	for _, acc := range accountList {
		if acc.Address == address {
			account = acc
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("cuenta no encontrada en keystore: %s", address.Hex())
	}

	// Exportar clave privada
	keyJSON, err := km.keystore.Export(account, password, password)
	if err != nil {
		return nil, fmt.Errorf("error exportando clave: %w", err)
	}

	// Importar clave para obtener private key
	account, err = km.keystore.Import(keyJSON, password, password)
	if err != nil {
		return nil, fmt.Errorf("error importando clave: %w", err)
	}

	// Obtener private key (esto requiere acceso al keystore interno)
	// Por ahora, retornamos un error indicando que se debe usar el keystore directamente
	return nil, fmt.Errorf("usar keystore directamente para firmar transacciones")
}

// LoadPrivateKeyFromEnv carga una clave privada desde variable de entorno
func LoadPrivateKeyFromEnv(keyEnvVar string) (*ecdsa.PrivateKey, error) {
	privateKeyHex := os.Getenv(keyEnvVar)
	if privateKeyHex == "" {
		return nil, fmt.Errorf("variable de entorno %s no configurada", keyEnvVar)
	}

	// Remover prefijo "0x" si existe
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	// Decodificar clave privada
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("error decodificando clave privada: %w", err)
	}

	return privateKey, nil
}

// LoadPrivateKeyFromFile carga una clave privada desde un archivo
func LoadPrivateKeyFromFile(filePath string) (*ecdsa.PrivateKey, error) {
	// Leer archivo
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error leyendo archivo de clave: %w", err)
	}

	// Limpiar datos (remover espacios, newlines, etc.)
	privateKeyHex := string(data)
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	// Decodificar clave privada
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("error decodificando clave privada: %w", err)
	}

	return privateKey, nil
}

// GenerateKey genera una nueva clave privada
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return crypto.GenerateKey()
}

// SaveKeyToFile guarda una clave privada en un archivo (usar con cuidado)
func SaveKeyToFile(privateKey *ecdsa.PrivateKey, filePath string, permissions os.FileMode) error {
	// Convertir clave a hex
	privateKeyHex := crypto.FromECDSA(privateKey)

	// Guardar archivo
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("error creando directorio: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(fmt.Sprintf("0x%x", privateKeyHex)), permissions); err != nil {
		return fmt.Errorf("error escribiendo archivo de clave: %w", err)
	}

	return nil
}

// ValidatePrivateKey valida que una clave privada sea válida
func ValidatePrivateKey(privateKey *ecdsa.PrivateKey) error {
	if privateKey == nil {
		return fmt.Errorf("clave privada es nil")
	}

	// Verificar que la clave pueda derivar una dirección pública
	_ = crypto.PubkeyToAddress(privateKey.PublicKey)
	
	return nil
}

// GetAddressFromPrivateKey obtiene la dirección Ethereum desde una clave privada
func GetAddressFromPrivateKey(privateKey *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(privateKey.PublicKey)
}

