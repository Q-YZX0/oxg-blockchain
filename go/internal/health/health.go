package health

import (
	"sync"
	"time"
)

// HealthStatus representa el estado de salud del nodo
type HealthStatus struct {
	Status      string                 `json:"status"`      // "healthy", "degraded", "unhealthy"
	Timestamp   time.Time              `json:"timestamp"`
	Components  map[string]ComponentStatus `json:"components"`
	BlockHeight uint64                 `json:"block_height"`
	Peers       int                    `json:"peers"`
}

// ComponentStatus representa el estado de un componente
type ComponentStatus struct {
	Status    string    `json:"status"`    // "ok", "warning", "error"
	Message   string    `json:"message,omitempty"`
	LastCheck time.Time `json:"last_check"`
}

// HealthChecker maneja el estado de salud del nodo
type HealthChecker struct {
	mu              sync.RWMutex
	components      map[string]ComponentStatus
	blockHeight     uint64
	peers           int
	storageHealthy  bool
	evmHealthy      bool
	consensusHealthy bool
	meshHealthy     bool
}

// NewHealthChecker crea un nuevo verificador de salud
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		components: make(map[string]ComponentStatus),
	}
}

// CheckHealth retorna el estado de salud actual
func (h *HealthChecker) CheckHealth() HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := "healthy"
	
	// Verificar estado general
	allHealthy := true
	anyWarning := false
	
	for _, comp := range h.components {
		if comp.Status == "error" {
			allHealthy = false
			status = "unhealthy"
			break
		} else if comp.Status == "warning" {
			anyWarning = true
		}
	}

	if anyWarning && allHealthy {
		status = "degraded"
	}

	// Verificar componentes críticos
	if !h.storageHealthy || !h.evmHealthy || !h.consensusHealthy {
		status = "unhealthy"
	}

	return HealthStatus{
		Status:      status,
		Timestamp:   time.Now(),
		Components:  h.components,
		BlockHeight: h.blockHeight,
		Peers:       h.peers,
	}
}

// UpdateComponent actualiza el estado de un componente
func (h *HealthChecker) UpdateComponent(name string, status string, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.components[name] = ComponentStatus{
		Status:    status,
		Message:   message,
		LastCheck: time.Now(),
	}
}

// SetBlockHeight actualiza la altura del bloque
func (h *HealthChecker) SetBlockHeight(height uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.blockHeight = height
}

// SetPeers actualiza el número de peers
func (h *HealthChecker) SetPeers(count int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.peers = count
}

// SetStorageHealth actualiza el estado del storage
func (h *HealthChecker) SetStorageHealth(healthy bool) {
	h.mu.Lock()
	h.storageHealthy = healthy
	// Actualizar componente sin adquirir el lock de nuevo (ya lo tenemos)
	name := "storage"
	if healthy {
		h.components[name] = ComponentStatus{
			Status:    "ok",
			Message:   "Storage operativo",
			LastCheck: time.Now(),
		}
	} else {
		h.components[name] = ComponentStatus{
			Status:    "error",
			Message:   "Storage no disponible",
			LastCheck: time.Now(),
		}
	}
	h.mu.Unlock()
}

// SetEVMHealth actualiza el estado del EVM
func (h *HealthChecker) SetEVMHealth(healthy bool) {
	h.mu.Lock()
	h.evmHealthy = healthy
	// Actualizar componente sin adquirir el lock de nuevo (ya lo tenemos)
	name := "evm"
	if healthy {
		h.components[name] = ComponentStatus{
			Status:    "ok",
			Message:   "EVM operativo",
			LastCheck: time.Now(),
		}
	} else {
		h.components[name] = ComponentStatus{
			Status:    "error",
			Message:   "EVM no disponible",
			LastCheck: time.Now(),
		}
	}
	h.mu.Unlock()
}

// SetConsensusHealth actualiza el estado del consenso
func (h *HealthChecker) SetConsensusHealth(healthy bool) {
	h.mu.Lock()
	h.consensusHealthy = healthy
	// Actualizar componente sin adquirir el lock de nuevo (ya lo tenemos)
	name := "consensus"
	if healthy {
		h.components[name] = ComponentStatus{
			Status:    "ok",
			Message:   "Consenso operativo",
			LastCheck: time.Now(),
		}
	} else {
		h.components[name] = ComponentStatus{
			Status:    "error",
			Message:   "Consenso no disponible",
			LastCheck: time.Now(),
		}
	}
	h.mu.Unlock()
}

// SetMeshHealth actualiza el estado de la mesh network
func (h *HealthChecker) SetMeshHealth(healthy bool) {
	h.mu.Lock()
	h.meshHealthy = healthy
	// Actualizar componente sin adquirir el lock de nuevo (ya lo tenemos)
	name := "mesh"
	if healthy {
		h.components[name] = ComponentStatus{
			Status:    "ok",
			Message:   "Mesh network operativa",
			LastCheck: time.Now(),
		}
	} else {
		h.components[name] = ComponentStatus{
			Status:    "warning",
			Message:   "Mesh network degradada",
			LastCheck: time.Now(),
		}
	}
	h.mu.Unlock()
}

// IsHealthy retorna si el nodo está saludable
func (h *HealthChecker) IsHealthy() bool {
	status := h.CheckHealth()
	return status.Status == "healthy"
}

// IsLive retorna si el nodo está vivo (liveness check)
// Un nodo está "live" si no está completamente caído
func (h *HealthChecker) IsLive() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	// Liveness: solo verificar que los componentes críticos no estén todos caídos
	// Si al menos uno está funcionando, el nodo está "live"
	return h.storageHealthy || h.evmHealthy || h.consensusHealthy
}

// IsReady retorna si el nodo está listo para recibir tráfico (readiness check)
// Un nodo está "ready" si todos los componentes críticos están operativos
func (h *HealthChecker) IsReady() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	// Readiness: todos los componentes críticos deben estar operativos
	return h.storageHealthy && h.evmHealthy && h.consensusHealthy
}

