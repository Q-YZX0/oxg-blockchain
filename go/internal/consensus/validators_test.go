package consensus

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	execution "github.com/Q-YZX0/oxy-blockchain/internal/execution"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// cleanupTestDir limpia completamente un directorio de test
func cleanupValidatorTestDir(dir string) error {
	pebbleDir := filepath.Join(dir, "evm_state")
	if err := os.RemoveAll(pebbleDir); err != nil {
		return fmt.Errorf("error limpiando directorio Pebble DB: %w", err)
	}
	
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("error limpiando directorio de test: %w", err)
	}
	
	return nil
}

// createTestDir crea un directorio único de test en un directorio temporal del sistema
func createValidatorTestDir(testName string) string {
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, "oxy_blockchain_test", fmt.Sprintf("test_data_validators_%s_%d", testName, os.Getpid()))
}

// TestValidatorSet_RegisterValidator prueba el registro de un validador
func TestValidatorSet_RegisterValidator(t *testing.T) {
	testDir := createValidatorTestDir("register")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 100)
	
	// Generar dirección y clave pública
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	
	// Generar clave pública CometBFT (simplificada, 32 bytes)
	pubKey := make([]byte, 32)
	copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
	
	// Registrar validador con stake suficiente
	stake := new(big.Int).Mul(big.NewInt(5000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	validator, err := validatorSet.RegisterValidator(address, pubKey, stake)
	if err != nil {
		t.Fatalf("Error registrando validador: %v", err)
	}
	
	// Verificar validador
	if validator.Address != address {
		t.Errorf("Dirección incorrecta: esperado %s, obtenido %s", address, validator.Address)
	}
	
	if validator.Stake.Cmp(stake) != 0 {
		t.Errorf("Stake incorrecto: esperado %s, obtenido %s", stake.String(), validator.Stake.String())
	}
	
	if validator.Power <= 0 {
		t.Errorf("Power debería ser mayor que 0: obtenido %d", validator.Power)
	}
	
	// Verificar que está guardado
	retrieved, err := validatorSet.GetValidator(address)
	if err != nil {
		t.Fatalf("Error obteniendo validador: %v", err)
	}
	
	if retrieved.Address != address {
		t.Errorf("Validador recuperado tiene dirección incorrecta")
	}
}

// TestValidatorSet_RegisterValidator_InsufficientStake prueba registro con stake insuficiente
func TestValidatorSet_RegisterValidator_InsufficientStake(t *testing.T) {
	testDir := createValidatorTestDir("insufficient_stake")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet con minStake de 1000 tokens
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 100)
	
	// Generar dirección y clave pública
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	
	pubKey := make([]byte, 32)
	copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
	
	// Intentar registrar con stake insuficiente (500 tokens, menos del mínimo de 1000)
	stake := new(big.Int).Mul(big.NewInt(500), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	_, err = validatorSet.RegisterValidator(address, pubKey, stake)
	if err == nil {
		t.Error("Debería retornar error con stake insuficiente")
	}
	
	if err.Error() == "" {
		t.Error("Error debería tener mensaje descriptivo")
	}
}

// TestValidatorSet_Stake prueba agregar stake a un validador
func TestValidatorSet_Stake(t *testing.T) {
	testDir := createValidatorTestDir("stake")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 100)
	
	// Registrar validador
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	
	pubKey := make([]byte, 32)
	copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
	
	initialStake := new(big.Int).Mul(big.NewInt(5000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	_, err = validatorSet.RegisterValidator(address, pubKey, initialStake)
	if err != nil {
		t.Fatalf("Error registrando validador: %v", err)
	}
	
	// Agregar más stake
	additionalStake := new(big.Int).Mul(big.NewInt(2000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	err = validatorSet.Stake(address, additionalStake)
	if err != nil {
		t.Fatalf("Error agregando stake: %v", err)
	}
	
	// Verificar stake actualizado
	validator, err := validatorSet.GetValidator(address)
	if err != nil {
		t.Fatalf("Error obteniendo validador: %v", err)
	}
	
	expectedStake := new(big.Int).Add(initialStake, additionalStake)
	if validator.Stake.Cmp(expectedStake) != 0 {
		t.Errorf("Stake incorrecto: esperado %s, obtenido %s", expectedStake.String(), validator.Stake.String())
	}
	
	// Verificar que power se actualizó
	if validator.Power <= 0 {
		t.Errorf("Power debería ser mayor que 0: obtenido %d", validator.Power)
	}
}

// TestValidatorSet_Unstake prueba reducir stake de un validador
func TestValidatorSet_Unstake(t *testing.T) {
	testDir := createValidatorTestDir("unstake")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet con minStake de 1000 tokens
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 100)
	
	// Registrar validador con stake de 5000 tokens
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	
	pubKey := make([]byte, 32)
	copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
	
	initialStake := new(big.Int).Mul(big.NewInt(5000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	_, err = validatorSet.RegisterValidator(address, pubKey, initialStake)
	if err != nil {
		t.Fatalf("Error registrando validador: %v", err)
	}
	
	// Reducir stake (2000 tokens, quedará con 3000, que es mayor al mínimo)
	unstakeAmount := new(big.Int).Mul(big.NewInt(2000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	err = validatorSet.Unstake(address, unstakeAmount)
	if err != nil {
		t.Fatalf("Error reduciendo stake: %v", err)
	}
	
	// Verificar stake actualizado
	validator, err := validatorSet.GetValidator(address)
	if err != nil {
		t.Fatalf("Error obteniendo validador: %v", err)
	}
	
	expectedStake := new(big.Int).Sub(initialStake, unstakeAmount)
	if validator.Stake.Cmp(expectedStake) != 0 {
		t.Errorf("Stake incorrecto: esperado %s, obtenido %s", expectedStake.String(), validator.Stake.String())
	}
	
	// Verificar que sigue siendo mayor al mínimo
	if validator.Stake.Cmp(minStake) < 0 {
		t.Errorf("Stake debería ser mayor o igual al mínimo después de unstake")
	}
}

// TestValidatorSet_Unstake_BelowMinimum prueba unstake que deja stake por debajo del mínimo
func TestValidatorSet_Unstake_BelowMinimum(t *testing.T) {
	testDir := createValidatorTestDir("unstake_below_min")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet con minStake de 1000 tokens
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 100)
	
	// Registrar validador con stake de 5000 tokens
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	
	pubKey := make([]byte, 32)
	copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
	
	initialStake := new(big.Int).Mul(big.NewInt(5000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	_, err = validatorSet.RegisterValidator(address, pubKey, initialStake)
	if err != nil {
		t.Fatalf("Error registrando validador: %v", err)
	}
	
	// Intentar reducir stake demasiado (quedaría con 500, menos del mínimo de 1000)
	unstakeAmount := new(big.Int).Mul(big.NewInt(4500), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	err = validatorSet.Unstake(address, unstakeAmount)
	if err == nil {
		t.Error("Debería retornar error al unstake que dejaría stake por debajo del mínimo")
	}
	
	// Verificar que el stake original no cambió
	validator, err := validatorSet.GetValidator(address)
	if err != nil {
		t.Fatalf("Error obteniendo validador: %v", err)
	}
	
	if validator.Stake.Cmp(initialStake) != 0 {
		t.Errorf("Stake no debería cambiar: esperado %s, obtenido %s", initialStake.String(), validator.Stake.String())
	}
}

// TestValidatorSet_Slash prueba el slash de un validador
func TestValidatorSet_Slash(t *testing.T) {
	testDir := createValidatorTestDir("slash")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 100)
	
	// Registrar validador
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	
	pubKey := make([]byte, 32)
	copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
	
	initialStake := new(big.Int).Mul(big.NewInt(10000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	_, err = validatorSet.RegisterValidator(address, pubKey, initialStake)
	if err != nil {
		t.Fatalf("Error registrando validador: %v", err)
	}
	
	// Slash del 10% del stake
	slashPercent := 10
	jailDuration := 24 * time.Hour
	
	err = validatorSet.Slash(address, slashPercent, jailDuration)
	if err != nil {
		t.Fatalf("Error slasheando validador: %v", err)
	}
	
	// Verificar slash
	validator, err := validatorSet.GetValidator(address)
	if err != nil {
		t.Fatalf("Error obteniendo validador: %v", err)
	}
	
	// Calcular stake esperado después del slash (90% del original)
	expectedStake := new(big.Int).Mul(initialStake, big.NewInt(90))
	expectedStake.Div(expectedStake, big.NewInt(100))
	
	if validator.Stake.Cmp(expectedStake) != 0 {
		t.Errorf("Stake después de slash incorrecto: esperado %s, obtenido %s", expectedStake.String(), validator.Stake.String())
	}
	
	// Verificar que está en jail
	if !validator.Jailed {
		t.Error("Validador debería estar en jail después de slash")
	}
	
	if validator.JailedUntil.IsZero() {
		t.Error("JailedUntil debería estar configurado")
	}
	
	// Verificar power se actualizó
	if validator.Power <= 0 {
		t.Errorf("Power debería ser mayor que 0: obtenido %d", validator.Power)
	}
}

// TestValidatorSet_Unjail prueba liberar validador de jail
func TestValidatorSet_Unjail(t *testing.T) {
	testDir := createValidatorTestDir("unjail")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 100)
	
	// Registrar y slashear validador
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Error generando clave: %v", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	
	pubKey := make([]byte, 32)
	copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
	
	initialStake := new(big.Int).Mul(big.NewInt(10000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	
	_, err = validatorSet.RegisterValidator(address, pubKey, initialStake)
	if err != nil {
		t.Fatalf("Error registrando validador: %v", err)
	}
	
	// Slash con jail de 1 segundo (corto para test)
	err = validatorSet.Slash(address, 10, 1*time.Second)
	if err != nil {
		t.Fatalf("Error slasheando validador: %v", err)
	}
	
	// Esperar que termine el jail
	time.Sleep(2 * time.Second)
	
	// Unjail
	err = validatorSet.Unjail(address)
	if err != nil {
		t.Fatalf("Error unjailing validador: %v", err)
	}
	
	// Verificar que ya no está en jail
	validator, err := validatorSet.GetValidator(address)
	if err != nil {
		t.Fatalf("Error obteniendo validador: %v", err)
	}
	
	if validator.Jailed {
		t.Error("Validador debería estar libre de jail")
	}
}

// TestValidatorSet_GetActiveValidators prueba obtener validadores activos
func TestValidatorSet_GetActiveValidators(t *testing.T) {
	testDir := createValidatorTestDir("get_active")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 100)
	
	// Registrar 3 validadores
	var addresses []string
	for i := 0; i < 3; i++ {
		privateKey, err := crypto.GenerateKey()
		if err != nil {
			t.Fatalf("Error generando clave: %v", err)
		}
		address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
		addresses = append(addresses, address)
		
		pubKey := make([]byte, 32)
		copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
		
		stake := new(big.Int).Mul(big.NewInt(int64(5000+i*1000)), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
		
		_, err = validatorSet.RegisterValidator(address, pubKey, stake)
		if err != nil {
			t.Fatalf("Error registrando validador %d: %v", i, err)
		}
	}
	
	// Slash uno (estará en jail)
	err = validatorSet.Slash(addresses[1], 5, 1*time.Hour)
	if err != nil {
		t.Fatalf("Error slasheando validador: %v", err)
	}
	
	// Obtener validadores activos
	activeValidators := validatorSet.GetActiveValidators()
	
	// Debería tener 2 validadores activos (uno está en jail)
	if len(activeValidators) != 2 {
		t.Errorf("Debería haber 2 validadores activos: obtenido %d", len(activeValidators))
	}
	
	// Verificar que ninguno está en jail
	for _, v := range activeValidators {
		if v.Jailed {
			t.Errorf("Validador activo %s no debería estar en jail", v.Address)
		}
	}
}

// TestValidatorSet_RotateValidators prueba la rotación de validadores
func TestValidatorSet_RotateValidators(t *testing.T) {
	testDir := createValidatorTestDir("rotate")
	defer func() {
		if err := cleanupValidatorTestDir(testDir); err != nil {
			t.Logf("Advertencia: error limpiando directorio: %v", err)
		}
	}()
	
	// Limpiar antes de empezar
	if err := cleanupValidatorTestDir(testDir); err != nil && !os.IsNotExist(err) {
		t.Logf("Advertencia: error limpiando antes del test: %v", err)
	}
	
	// Crear storage
	db, err := storage.NewBlockchainDB(testDir)
	if err != nil {
		t.Fatalf("Error creando storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Advertencia: error cerrando storage: %v", err)
		}
	}()
	
	// Crear ejecutor EVM (no lo iniciamos para evitar goroutines de Pebble)
	// Los tests de validators no necesitan el EVM corriendo
	evm := execution.NewEVMExecutor(db)
	
	// Crear ValidatorSet con máximo de 5 validadores
	minStake := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	validatorSet := NewValidatorSet(db, evm, minStake, 5)
	
	// Registrar 3 validadores
	for i := 0; i < 3; i++ {
		privateKey, err := crypto.GenerateKey()
		if err != nil {
			t.Fatalf("Error generando clave: %v", err)
		}
		address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
		
		pubKey := make([]byte, 32)
		copy(pubKey, crypto.FromECDSAPub(&privateKey.PublicKey)[:32])
		
		stake := new(big.Int).Mul(big.NewInt(int64(5000+i*1000)), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
		
		_, err = validatorSet.RegisterValidator(address, pubKey, stake)
		if err != nil {
			t.Fatalf("Error registrando validador %d: %v", i, err)
		}
	}
	
	// Rotar validadores
	updates, err := validatorSet.RotateValidators()
	if err != nil {
		t.Fatalf("Error rotando validadores: %v", err)
	}
	
	// Debería retornar 3 validadores activos
	if len(updates) != 3 {
		t.Errorf("Debería haber 3 validadores en la rotación: obtenido %d", len(updates))
	}
	
	// Verificar que todos tienen power mayor que 0
	for _, update := range updates {
		if update.Power <= 0 {
			t.Errorf("Power debería ser mayor que 0: obtenido %d", update.Power)
		}
	}
}
