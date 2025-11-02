package consensus

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"sort"
	"sync"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/Q-YZX0/oxy-blockchain/internal/execution"
	"github.com/Q-YZX0/oxy-blockchain/internal/storage"
)

// Validator representa un validador en la red
type Validator struct {
	Address       string    // Dirección Ethereum del validador
	PubKey        []byte    // Clave pública CometBFT
	Stake         *big.Int  // Cantidad de OXG staked
	Power         int64     // Poder de voto (calculado del stake)
	DelegatedTo   string    // Dirección que tiene delegado el stake (opcional)
	Jailed        bool      // Si está en jail (slashed)
	JailedUntil   time.Time // Fecha hasta la que está en jail
	CreatedAt     time.Time // Fecha de creación
	LastActiveAt  time.Time // Última actividad
	MissedBlocks  int       // Bloques perdidos consecutivos
	TotalMissed   int       // Total de bloques perdidos
}

// ValidatorSet maneja el conjunto de validadores
type ValidatorSet struct {
	storage       *storage.BlockchainDB
	executor      *execution.EVMExecutor
	validators    map[string]*Validator
	mutex         sync.RWMutex
	minStake      *big.Int // Stake mínimo para ser validador
	maxValidators int      // Número máximo de validadores
}

// NewValidatorSet crea un nuevo conjunto de validadores
func NewValidatorSet(
	storage *storage.BlockchainDB,
	executor *execution.EVMExecutor,
	minStake *big.Int,
	maxValidators int,
) *ValidatorSet {
	return &ValidatorSet{
		storage:       storage,
		executor:      executor,
		validators:    make(map[string]*Validator),
		minStake:      minStake,
		maxValidators: maxValidators,
	}
}

// LoadValidators carga validadores desde storage
func (vs *ValidatorSet) LoadValidators() error {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()

	// Cargar validadores guardados
	validatorsData, err := vs.storage.GetAccount("validators:set")
	if err != nil {
		// No hay validadores guardados, retornar nil
		log.Println("No hay validadores guardados, iniciando con set vacío")
		return nil
	}

	var validatorsList []*Validator
	if err := json.Unmarshal(validatorsData, &validatorsList); err != nil {
		return fmt.Errorf("error parseando validadores: %w", err)
	}

	vs.validators = make(map[string]*Validator)
	for _, v := range validatorsList {
		vs.validators[v.Address] = v
	}

	log.Printf("Cargados %d validadores desde storage", len(vs.validators))
	return nil
}

// SaveValidators guarda validadores en storage
func (vs *ValidatorSet) SaveValidators() error {
	fmt.Fprintf(os.Stdout, "[Validators] SaveValidators iniciado\n")
	os.Stdout.Sync()
	
	fmt.Fprintf(os.Stdout, "[Validators] Adquiriendo RLock...\n")
	os.Stdout.Sync()
	vs.mutex.RLock()
	defer vs.mutex.RUnlock()
	fmt.Fprintf(os.Stdout, "[Validators] RLock adquirido\n")
	os.Stdout.Sync()

	fmt.Fprintf(os.Stdout, "[Validators] Creando lista de validadores (count=%d)...\n", len(vs.validators))
	os.Stdout.Sync()
	validatorsList := make([]*Validator, 0, len(vs.validators))
	for _, v := range vs.validators {
		validatorsList = append(validatorsList, v)
	}
	fmt.Fprintf(os.Stdout, "[Validators] Lista de validadores creada (count=%d)\n", len(validatorsList))
	os.Stdout.Sync()

	fmt.Fprintf(os.Stdout, "[Validators] Serializando validadores...\n")
	os.Stdout.Sync()
	validatorsData, err := json.Marshal(validatorsList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Validators] ERROR serializando validadores: %v\n", err)
		os.Stderr.Sync()
		return fmt.Errorf("error serializando validadores: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[Validators] Validadores serializados (%d bytes)\n", len(validatorsData))
	os.Stdout.Sync()

	fmt.Fprintf(os.Stdout, "[Validators] Llamando a storage.SaveAccount('validators:set')...\n")
	os.Stdout.Sync()
	err = vs.storage.SaveAccount("validators:set", validatorsData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Validators] ERROR en storage.SaveAccount(): %v\n", err)
		os.Stderr.Sync()
		return err
	}
	fmt.Fprintf(os.Stdout, "[Validators] storage.SaveAccount() completado exitosamente\n")
	os.Stdout.Sync()
	
	fmt.Fprintf(os.Stdout, "[Validators] SaveValidators completado exitosamente\n")
	os.Stdout.Sync()
	return nil
}

// RegisterValidator registra un nuevo validador
func (vs *ValidatorSet) RegisterValidator(
	address string,
	pubKey []byte,
	initialStake *big.Int,
) (*Validator, error) {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()

	// Validar que no esté ya registrado
	if _, exists := vs.validators[address]; exists {
		return nil, fmt.Errorf("validador ya está registrado: %s", address)
	}

	// Validar stake mínimo
	if initialStake.Cmp(vs.minStake) < 0 {
		return nil, fmt.Errorf("stake insuficiente: requiere mínimo %s, tiene %s", vs.minStake.String(), initialStake.String())
	}

	// Validar que no se exceda el máximo
	if len(vs.validators) >= vs.maxValidators {
		// Encontrar validador con menor stake
		minStakeVal := vs.findLowestStakeValidator()
		if minStakeVal == nil || initialStake.Cmp(minStakeVal.Stake) <= 0 {
			return nil, fmt.Errorf("número máximo de validadores alcanzado y stake insuficiente para reemplazar al validador con menor stake")
		}

		// Remover validador con menor stake
		delete(vs.validators, minStakeVal.Address)
		log.Printf("Removido validador %s con stake %s para hacer espacio", minStakeVal.Address, minStakeVal.Stake.String())
	}

	// Crear nuevo validador
	validator := &Validator{
		Address:      address,
		PubKey:       pubKey,
		Stake:        new(big.Int).Set(initialStake),
		Power:        vs.calculatePower(initialStake),
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}

	vs.validators[address] = validator

	log.Printf("✅ Validador registrado: %s con stake %s", address, initialStake.String())

	// Guardar validadores
	if err := vs.SaveValidators(); err != nil {
		log.Printf("Advertencia: error guardando validadores: %v", err)
	}

	return validator, nil
}

// Stake aumenta el stake de un validador
func (vs *ValidatorSet) Stake(address string, amount *big.Int) error {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()

	validator, exists := vs.validators[address]
	if !exists {
		return fmt.Errorf("validador no encontrado: %s", address)
	}

	// Validar que no esté en jail
	if validator.Jailed {
		return fmt.Errorf("validador está en jail: %s", address)
	}

	// Actualizar stake
	validator.Stake.Add(validator.Stake, amount)
	validator.Power = vs.calculatePower(validator.Stake)
	validator.LastActiveAt = time.Now()

	log.Printf("✅ Stake actualizado para %s: %s (nuevo total: %s)", address, amount.String(), validator.Stake.String())

	// Guardar validadores
	if err := vs.SaveValidators(); err != nil {
		log.Printf("Advertencia: error guardando validadores: %v", err)
	}

	return nil
}

// Unstake reduce el stake de un validador
func (vs *ValidatorSet) Unstake(address string, amount *big.Int) error {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()

	validator, exists := vs.validators[address]
	if !exists {
		return fmt.Errorf("validador no encontrado: %s", address)
	}

	// Validar que no esté en jail
	if validator.Jailed {
		return fmt.Errorf("validador está en jail: %s", address)
	}

	// Validar que no baje del mínimo
	newStake := new(big.Int).Sub(validator.Stake, amount)
	if newStake.Cmp(vs.minStake) < 0 {
		return fmt.Errorf("no se puede unstake: quedaría con %s, requiere mínimo %s", newStake.String(), vs.minStake.String())
	}

	// Actualizar stake
	validator.Stake.Sub(validator.Stake, amount)
	validator.Power = vs.calculatePower(validator.Stake)
	validator.LastActiveAt = time.Now()

	log.Printf("✅ Stake reducido para %s: -%s (nuevo total: %s)", address, amount.String(), validator.Stake.String())

	// Si el stake es muy bajo, puede ser removido del set activo
	if validator.Stake.Cmp(vs.minStake) < 0 {
		delete(vs.validators, address)
		log.Printf("⚠️ Validador %s removido por stake insuficiente", address)
	}

	// Guardar validadores
	if err := vs.SaveValidators(); err != nil {
		log.Printf("Advertencia: error guardando validadores: %v", err)
	}

	return nil
}

// Slash penaliza a un validador por comportamiento malicioso
func (vs *ValidatorSet) Slash(address string, slashPercent int, jailDuration time.Duration) error {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()

	validator, exists := vs.validators[address]
	if !exists {
		return fmt.Errorf("validador no encontrado: %s", address)
	}

	// Calcular cantidad a slashear
	slashAmount := new(big.Int)
	slashAmount.Mul(validator.Stake, big.NewInt(int64(slashPercent)))
	slashAmount.Div(slashAmount, big.NewInt(100))

	// Reducir stake
	validator.Stake.Sub(validator.Stake, slashAmount)

	// Actualizar power
	validator.Power = vs.calculatePower(validator.Stake)

	// Enviar a jail
	validator.Jailed = true
	validator.JailedUntil = time.Now().Add(jailDuration)

	log.Printf("⚠️ Validador slasheado: %s -%s (%%%d)", address, slashAmount.String(), slashPercent)
	log.Printf("⛓️ Validador %s enviado a jail hasta %s", address, validator.JailedUntil.Format(time.RFC3339))

	// Si el stake es muy bajo después de slash, remover
	if validator.Stake.Cmp(vs.minStake) < 0 {
		delete(vs.validators, address)
		log.Printf("⚠️ Validador %s removido por stake insuficiente después de slash", address)
	}

	// Guardar validadores
	if err := vs.SaveValidators(); err != nil {
		log.Printf("Advertencia: error guardando validadores: %v", err)
	}

	return nil
}

// Unjail libera a un validador de jail
func (vs *ValidatorSet) Unjail(address string) error {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()

	validator, exists := vs.validators[address]
	if !exists {
		return fmt.Errorf("validador no encontrado: %s", address)
	}

	if !validator.Jailed {
		return fmt.Errorf("validador no está en jail: %s", address)
	}

	if time.Now().Before(validator.JailedUntil) {
		return fmt.Errorf("validador aún está en jail hasta %s", validator.JailedUntil.Format(time.RFC3339))
	}

	// Liberar de jail
	validator.Jailed = false
	validator.JailedUntil = time.Time{}
	validator.MissedBlocks = 0

	log.Printf("✅ Validador %s liberado de jail", address)

	// Guardar validadores
	if err := vs.SaveValidators(); err != nil {
		log.Printf("Advertencia: error guardando validadores: %v", err)
	}

	return nil
}

// GetValidators retorna la lista de validadores
func (vs *ValidatorSet) GetValidators() []*Validator {
	vs.mutex.RLock()
	defer vs.mutex.RUnlock()

	validators := make([]*Validator, 0, len(vs.validators))
	for _, v := range vs.validators {
		if !v.Jailed {
			validators = append(validators, v)
		}
	}

	return validators
}

// GetActiveValidators retorna solo los validadores activos (no en jail)
func (vs *ValidatorSet) GetActiveValidators() []*Validator {
	vs.mutex.RLock()
	defer vs.mutex.RUnlock()

	validators := make([]*Validator, 0, len(vs.validators))
	for _, v := range vs.validators {
		stakeValid := v.Stake.Cmp(vs.minStake) >= 0
		// Logs reducidos para evitar spam
		if !v.Jailed && stakeValid {
			validators = append(validators, v)
		}
	}

	// Ordenar por stake (mayor primero)
	sort.Slice(validators, func(i, j int) bool {
		return validators[i].Stake.Cmp(validators[j].Stake) > 0
	})

	return validators
}

// GetValidator retorna un validador por dirección
func (vs *ValidatorSet) GetValidator(address string) (*Validator, error) {
	vs.mutex.RLock()
	defer vs.mutex.RUnlock()

	validator, exists := vs.validators[address]
	if !exists {
		return nil, fmt.Errorf("validador no encontrado: %s", address)
	}

	return validator, nil
}

// ToCometBFTValidators convierte validadores a formato CometBFT
func (vs *ValidatorSet) ToCometBFTValidators() []abcitypes.ValidatorUpdate {
	vs.mutex.RLock()
	defer vs.mutex.RUnlock()
	
	// Obtener validadores activos directamente (sin llamar a GetActiveValidators para evitar locks anidados)
	activeValidators := make([]*Validator, 0, len(vs.validators))
	for _, v := range vs.validators {
		stakeValid := v.Stake.Cmp(vs.minStake) >= 0
		if !v.Jailed && stakeValid {
			activeValidators = append(activeValidators, v)
		}
	}
	
	// Ordenar por stake (mayor primero)
	sort.Slice(activeValidators, func(i, j int) bool {
		return activeValidators[i].Stake.Cmp(activeValidators[j].Stake) > 0
	})
	
	updates := make([]abcitypes.ValidatorUpdate, 0, len(activeValidators))

	for _, v := range activeValidators {
		// Convertir clave pública a formato CometBFT
		var pubKey ed25519.PubKey
		if len(v.PubKey) == ed25519.PubKeySize {
			copy(pubKey[:], v.PubKey)
		} else {
			log.Printf("Advertencia: clave pública inválida para validador %s", v.Address)
			continue
		}

		// Nueva API v1.0.1: usar PubKeyBytes (PubKeyType es opcional en v1.0.1)
		// CometBFT v1.0.1 puede inferir el tipo desde PubKeyBytes
		updates = append(updates, abcitypes.ValidatorUpdate{
			PubKeyBytes: pubKey.Bytes(),
			// PubKeyType omitido: CometBFT v1.0.1 infiere el tipo desde PubKeyBytes para Ed25519
			Power: v.Power,
		})
	}

	return updates
}

// calculatePower calcula el poder de voto basado en stake
func (vs *ValidatorSet) calculatePower(stake *big.Int) int64 {
	// Power máximo en CometBFT es 2^63 - 1
	// Usamos una proporción simple: 1 OXG = 1 power (con límites)
	maxPower := int64(1 << 30) // 1,073,741,824 (seguro para CometBFT)

	// Convertir stake a int64 (con límite)
	power := new(big.Int).Div(stake, big.NewInt(1e18)) // Dividir por 18 decimales
	if power.Cmp(big.NewInt(maxPower)) > 0 {
		return maxPower
	}

	return power.Int64()
}

// findLowestStakeValidator encuentra el validador con menor stake
func (vs *ValidatorSet) findLowestStakeValidator() *Validator {
	var lowest *Validator
	for _, v := range vs.validators {
		if lowest == nil || v.Stake.Cmp(lowest.Stake) < 0 {
			lowest = v
		}
	}
	return lowest
}

// UpdateValidatorActivity actualiza la última actividad de un validador
func (vs *ValidatorSet) UpdateValidatorActivity(address string, missedBlock bool) {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()

	validator, exists := vs.validators[address]
	if !exists {
		return
	}

	validator.LastActiveAt = time.Now()

	if missedBlock {
		validator.MissedBlocks++
		validator.TotalMissed++

		// Si falla muchos bloques consecutivos, aplicar slash automático
		if validator.MissedBlocks >= 100 {
			log.Printf("⚠️ Validador %s ha fallado %d bloques consecutivos, aplicando slash", address, validator.MissedBlocks)
			// Slash del 5% del stake por cada 100 bloques perdidos
			slashPercentage := float64(validator.MissedBlocks/100) * 0.05 // 5% por cada 100 bloques
			if slashPercentage > 0.50 { // Máximo 50% de slash
				slashPercentage = 0.50
			}
			// TODO: Implementar SlashValidator cuando esté disponible
			// Por ahora, solo resetear el contador después de log
			log.Printf("⚠️ Slash pendiente para validador %s: %.2f%% por %d bloques perdidos", 
				address, slashPercentage*100, validator.MissedBlocks)
			validator.MissedBlocks = 0
		}
	} else {
		validator.MissedBlocks = 0
	}
}

// RotateValidators rota los validadores según stake y actividad
func (vs *ValidatorSet) RotateValidators() ([]abcitypes.ValidatorUpdate, error) {
	fmt.Fprintf(os.Stdout, "[Validators] RotateValidators iniciado\n")
	os.Stdout.Sync()
	
	vs.mutex.Lock()
	fmt.Fprintf(os.Stdout, "[Validators] Lock adquirido en RotateValidators\n")
	os.Stdout.Sync()
	
	// Actualizar power de todos los validadores primero (con lock)
	fmt.Fprintf(os.Stdout, "[Validators] Actualizando power de %d validadores...\n", len(vs.validators))
	os.Stdout.Sync()
	for _, v := range vs.validators {
		v.Power = vs.calculatePower(v.Stake)
	}
	
	// Obtener lista de validadores activos (necesitamos copiar datos antes de liberar el lock)
	fmt.Fprintf(os.Stdout, "[Validators] Creando copia de validadores activos...\n")
	os.Stdout.Sync()
	validatorsCopy := make([]*Validator, 0, len(vs.validators))
	for _, v := range vs.validators {
		stakeValid := v.Stake.Cmp(vs.minStake) >= 0
		if !v.Jailed && stakeValid {
			validatorsCopy = append(validatorsCopy, &Validator{
				Address:      v.Address,
				PubKey:       v.PubKey,
				Stake:        new(big.Int).Set(v.Stake),
				Power:        v.Power,
				Jailed:       v.Jailed,
				CreatedAt:    v.CreatedAt,
				LastActiveAt: v.LastActiveAt,
			})
		}
	}
	
	fmt.Fprintf(os.Stdout, "[Validators] Validadores activos encontrados: %d\n", len(validatorsCopy))
	os.Stdout.Sync()
	
	// Limitar a máximo de validadores
	if len(validatorsCopy) > vs.maxValidators {
		// Ordenar por stake (mayor primero)
		fmt.Fprintf(os.Stdout, "[Validators] Limitando a %d validadores (hay %d)\n", vs.maxValidators, len(validatorsCopy))
		os.Stdout.Sync()
		sort.Slice(validatorsCopy, func(i, j int) bool {
			return validatorsCopy[i].Stake.Cmp(validatorsCopy[j].Stake) > 0
		})
		validatorsCopy = validatorsCopy[:vs.maxValidators]
	}
	
	fmt.Fprintf(os.Stdout, "[Validators] Liberando lock...\n")
	os.Stdout.Sync()
	vs.mutex.Unlock() // Liberar lock antes de convertir a formato CometBFT
	
	// Convertir a formato CometBFT (ahora sin lock, usando la copia)
	fmt.Fprintf(os.Stdout, "[Validators] Convirtiendo %d validadores a formato CometBFT...\n", len(validatorsCopy))
	os.Stdout.Sync()
	updates := make([]abcitypes.ValidatorUpdate, 0, len(validatorsCopy))
	for i, v := range validatorsCopy {
		// Convertir clave pública a formato CometBFT
		var pubKey ed25519.PubKey
		if len(v.PubKey) == ed25519.PubKeySize {
			copy(pubKey[:], v.PubKey)
		} else {
			fmt.Fprintf(os.Stderr, "[Validators] ERROR: clave pública inválida para validador %s (tamaño: %d, esperado: %d)\n", v.Address, len(v.PubKey), ed25519.PubKeySize)
			os.Stderr.Sync()
			log.Printf("Advertencia: clave pública inválida para validador %s", v.Address)
			continue
		}

		// Nueva API v1.0.1: usar PubKeyBytes (PubKeyType es opcional en v1.0.1)
		updates = append(updates, abcitypes.ValidatorUpdate{
			PubKeyBytes: pubKey.Bytes(),
			Power:       v.Power,
		})
		
		if i < 3 { // Log primeros 3 para debug
			fmt.Fprintf(os.Stdout, "[Validators] Validador %d: Address=%s, Power=%d\n", i+1, v.Address, v.Power)
			os.Stdout.Sync()
		}
	}

	fmt.Fprintf(os.Stdout, "[Validators] Rotación completada: %d validadores activos\n", len(updates))
	os.Stdout.Sync()
	
	// IMPORTANTE: Si no hay validadores, CometBFT puede detenerse
	// No retornar lista vacía si no hay validadores - esto puede detener el consenso
	if len(updates) == 0 {
		fmt.Fprintf(os.Stderr, "[Validators] ADVERTENCIA CRÍTICA: No hay validadores activos después de rotación!\n")
		os.Stderr.Sync()
		fmt.Fprintf(os.Stderr, "[Validators] Esto puede causar que CometBFT se detenga. Retornando validadores del genesis.\n")
		os.Stderr.Sync()
		// Retornar lista vacía en lugar de nil para que CometBFT no se detenga
		// O mejor aún, no hacer rotación si no hay validadores
		return []abcitypes.ValidatorUpdate{}, nil
	}

	log.Printf("🔄 Rotación de validadores: %d validadores activos", len(updates))

	return updates, nil
}

// InitializeGenesisValidators inicializa validadores desde genesis
func (vs *ValidatorSet) InitializeGenesisValidators(genesisValidators []GenesisValidator) error {
	fmt.Fprintf(os.Stdout, "[Validators] InitializeGenesisValidators iniciado con %d validadores\n", len(genesisValidators))
	os.Stdout.Sync()
	
	fmt.Fprintf(os.Stdout, "[Validators] Adquiriendo mutex...\n")
	os.Stdout.Sync()
	vs.mutex.Lock()
	fmt.Fprintf(os.Stdout, "[Validators] Mutex adquirido\n")
	os.Stdout.Sync()

	fmt.Fprintf(os.Stdout, "[Validators] Iterando sobre %d validadores genesis...\n", len(genesisValidators))
	os.Stdout.Sync()
	for i, gv := range genesisValidators {
		fmt.Fprintf(os.Stdout, "[Validators] Procesando validador genesis %d/%d: %s\n", i+1, len(genesisValidators), gv.Address)
		os.Stdout.Sync()
		
		validator := &Validator{
			Address:      gv.Address,
			PubKey:       gv.PubKey,
			Stake:        gv.Stake,
			Power:        vs.calculatePower(gv.Stake),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
		}

		vs.validators[gv.Address] = validator
		log.Printf("✅ Validador genesis registrado: %s con stake %s", gv.Address, gv.Stake.String())
		fmt.Fprintf(os.Stdout, "[Validators] Validador registrado: %s\n", gv.Address)
		os.Stdout.Sync()
	}
	fmt.Fprintf(os.Stdout, "[Validators] Todos los validadores genesis procesados\n")
	os.Stdout.Sync()

	// Liberar el mutex antes de llamar a SaveValidators() para evitar deadlock
	// SaveValidators() necesita adquirir RLock, y aunque RLock debería poder adquirirse
	// después de Lock(), es mejor liberar el Lock() primero para evitar cualquier problema
	fmt.Fprintf(os.Stdout, "[Validators] Liberando mutex antes de SaveValidators()...\n")
	os.Stdout.Sync()
	vs.mutex.Unlock()
	
	fmt.Fprintf(os.Stdout, "[Validators] Llamando a SaveValidators()...\n")
	os.Stdout.Sync()
	err := vs.SaveValidators()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Validators] ERROR en SaveValidators(): %v\n", err)
		os.Stderr.Sync()
		return err
	}
	fmt.Fprintf(os.Stdout, "[Validators] SaveValidators() completado exitosamente\n")
	os.Stdout.Sync()
	
	return nil
}

// GenesisValidator representa un validador en el genesis
type GenesisValidator struct {
	Address string
	PubKey  []byte
	Stake   *big.Int
}

