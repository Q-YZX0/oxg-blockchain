package crypto

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// VerifyTransactionSignature verifica la firma de una transacción Ethereum
// Retorna la dirección derivada de la firma y error si la firma es inválida
func VerifyTransactionSignature(txData map[string]interface{}) (common.Address, error) {
	// Extraer campos de la transacción
	hashHex, ok := txData["hash"].(string)
	if !ok || hashHex == "" {
		return common.Address{}, fmt.Errorf("hash de transacción requerido")
	}

	signatureBytes, ok := txData["signature"].([]byte)
	if !ok || len(signatureBytes) == 0 {
		return common.Address{}, fmt.Errorf("firma requerida para validación")
	}

	fromAddrStr, ok := txData["from"].(string)
	if !ok || fromAddrStr == "" {
		return common.Address{}, fmt.Errorf("dirección remitente requerida")
	}

	fromAddr := common.HexToAddress(fromAddrStr)

	// Convertir hash de string a bytes
	hashBytes := common.HexToHash(hashHex).Bytes()

	// Verificar firma ECDSA
	// La firma debe tener 65 bytes: [R][S][V]
	if len(signatureBytes) != 65 {
		return common.Address{}, fmt.Errorf("firma inválida: debe tener 65 bytes, tiene %d", len(signatureBytes))
	}

	// Ajustar V (último byte) para recuperación de clave pública
	// crypto.Sign() retorna V en rango 0-1, crypto.SigToPub espera 27-28
	v := signatureBytes[64]
	// Normalizar V: crypto.Sign() retorna 0-1, pero SigToPub necesita 27-28
	if v < 27 {
		// Si V está en 0-1, ajustarlo a 27-28
		v = v + 27
	}

	// Recuperar clave pública desde la firma usando Ecrecover (más directo)
	// Ecrecover retorna la clave pública sin necesidad de ajustar V manualmente
	pubKeyBytes, err := crypto.Ecrecover(hashBytes, signatureBytes)
	if err != nil {
		return common.Address{}, fmt.Errorf("error recuperando clave pública: %w", err)
	}
	
	// Convertir bytes de clave pública a *ecdsa.PublicKey
	pubKey, err := crypto.UnmarshalPubkey(pubKeyBytes)
	if err != nil {
		return common.Address{}, fmt.Errorf("error parseando clave pública: %w", err)
	}

	// Derivar dirección desde la clave pública
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)

	// Verificar que la dirección recuperada coincida con tx.From
	if recoveredAddr != fromAddr {
		return common.Address{}, fmt.Errorf("firma inválida: dirección recuperada %s no coincide con remitente %s", recoveredAddr.Hex(), fromAddr.Hex())
	}

	return recoveredAddr, nil
}

// VerifyTransactionSignatureFromBytes verifica una firma directamente desde bytes
func VerifyTransactionSignatureFromBytes(txHash []byte, signature []byte, fromAddr common.Address) error {
	if len(signature) != 65 {
		return fmt.Errorf("firma inválida: debe tener 65 bytes, tiene %d", len(signature))
	}

	// Ajustar V (último byte) para recuperación de clave pública
	// crypto.Sign() retorna V en rango 0-1, crypto.SigToPub espera 27-28
	v := signature[64]
	// Normalizar V: crypto.Sign() retorna 0-1, pero SigToPub necesita 27-28
	if v < 27 {
		// Si V está en 0-1, ajustarlo a 27-28
		v = v + 27
	}

	// Recuperar clave pública desde la firma usando Ecrecover (más directo)
	// Ecrecover retorna la clave pública sin necesidad de ajustar V manualmente
	pubKeyBytes, err := crypto.Ecrecover(txHash, signature)
	if err != nil {
		return fmt.Errorf("error recuperando clave pública: %w", err)
	}
	
	// Convertir bytes de clave pública a *ecdsa.PublicKey
	pubKey, err := crypto.UnmarshalPubkey(pubKeyBytes)
	if err != nil {
		return fmt.Errorf("error parseando clave pública: %w", err)
	}

	// Derivar dirección
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)

	// Verificar coincidencia
	if recoveredAddr != fromAddr {
		return fmt.Errorf("firma inválida: dirección recuperada %s no coincide con remitente %s", recoveredAddr.Hex(), fromAddr.Hex())
	}

	return nil
}

// SignTransaction firma una transacción con una clave privada
func SignTransaction(txData map[string]interface{}, privateKey *ecdsa.PrivateKey) ([]byte, error) {
	// Crear hash de la transacción (excluyendo signature)
	txCopy := make(map[string]interface{})
	for k, v := range txData {
		if k != "signature" {
			txCopy[k] = v
		}
	}

	// Serializar transacción para hashing
	txBytes, err := json.Marshal(txCopy)
	if err != nil {
		return nil, fmt.Errorf("error serializando transacción: %w", err)
	}

	// Hashear con Keccak256 (hash Ethereum)
	hash := crypto.Keccak256Hash(txBytes)

	// Firmar hash
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		return nil, fmt.Errorf("error firmando transacción: %w", err)
	}

	return signature, nil
}

// CalculateTransactionHash calcula el hash de una transacción para firma
func CalculateTransactionHash(txData map[string]interface{}) (common.Hash, error) {
	// Crear copia sin signature
	txCopy := make(map[string]interface{})
	for k, v := range txData {
		if k != "signature" {
			txCopy[k] = v
		}
	}

	// Serializar
	txBytes, err := json.Marshal(txCopy)
	if err != nil {
		return common.Hash{}, fmt.Errorf("error serializando transacción: %w", err)
	}

	// Hashear
	hash := crypto.Keccak256Hash(txBytes)
	return hash, nil
}

// CreateTransactionHash crea un hash de transacción compatible con Ethereum
// Usa el formato RLP estándar de Ethereum
func CreateTransactionHash(
	nonce uint64,
	to common.Address,
	value *big.Int,
	gasLimit uint64,
	gasPrice *big.Int,
	data []byte,
	chainID *big.Int,
) common.Hash {
	// Crear transacción interna de Ethereum
	tx := types.NewTransaction(
		nonce,
		to,
		value,
		gasLimit,
		gasPrice,
		data,
	)

	// Hashear transacción con chain ID
	signer := types.NewEIP155Signer(chainID)
	return signer.Hash(tx)
}

