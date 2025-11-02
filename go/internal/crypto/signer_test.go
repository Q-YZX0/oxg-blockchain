package crypto

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestVerifyTransactionSignature prueba la verificación de firmas
func TestVerifyTransactionSignature(t *testing.T) {
	// Generar clave privada para testing
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}

	fromAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Crear datos de transacción
	txData := map[string]interface{}{
		"hash":      "0x1234567890abcdef",
		"from":      fromAddr.Hex(),
		"to":        common.HexToAddress("0x0000000000000000000000000000000000000000").Hex(),
		"value":     "1000000000000000000",
		"gasLimit":  21000,
		"gasPrice":  "1000000000",
		"nonce":     0,
	}

	// Calcular hash
	hash, err := CalculateTransactionHash(txData)
	if err != nil {
		t.Fatalf("Error calculando hash: %v", err)
	}

	// Firmar transacción
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		t.Fatalf("Error firmando: %v", err)
	}

	// Agregar firma a txData
	txData["signature"] = signature
	txData["hash"] = hash.Hex()

	// Verificar firma
	recoveredAddr, err := VerifyTransactionSignature(txData)
	if err != nil {
		t.Fatalf("Error verificando firma: %v", err)
	}

	if recoveredAddr != fromAddr {
		t.Errorf("Dirección recuperada %s no coincide con original %s", recoveredAddr.Hex(), fromAddr.Hex())
	}
}

// TestVerifyTransactionSignatureFromBytes prueba verificación directa desde bytes
func TestVerifyTransactionSignatureFromBytes(t *testing.T) {
	// Generar clave privada
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}

	fromAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Crear hash de prueba
	hash := crypto.Keccak256Hash([]byte("test transaction"))

	// Firmar
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		t.Fatalf("Error firmando: %v", err)
	}

	// Verificar
	err = VerifyTransactionSignatureFromBytes(hash.Bytes(), signature, fromAddr)
	if err != nil {
		t.Fatalf("Error verificando firma: %v", err)
	}
}

// TestCreateTransactionHash prueba creación de hash compatible con Ethereum
func TestCreateTransactionHash(t *testing.T) {
	chainID := big.NewInt(999)
	nonce := uint64(0)
	to := common.HexToAddress("0x0000000000000000000000000000000000000000")
	value := big.NewInt(1000000000000000000) // 1 OXG
	gasLimit := uint64(21000)
	gasPrice := big.NewInt(1000000000) // 1 gwei
	data := []byte{}

	hash := CreateTransactionHash(nonce, to, value, gasLimit, gasPrice, data, chainID)

	if hash == (common.Hash{}) {
		t.Error("Hash no debería estar vacío")
	}
}

