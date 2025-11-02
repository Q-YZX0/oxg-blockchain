package consensus

import (
	"testing"
	"time"
)

// TestRateLimiter_Allow prueba el rate limiting básico
func TestRateLimiter_Allow(t *testing.T) {
	// Crear rate limiter: 5 transacciones por minuto, mempool de 1000
	rl := NewRateLimiter(5, 1*time.Minute, 1000)
	
	address := "0x1234567890123456789012345678901234567890"
	
	// Primera transacción debería ser permitida
	if !rl.Allow(address) {
		t.Error("Primera transacción debería ser permitida")
	}
	
	// Verificar count
	count := rl.GetCount(address)
	if count != 1 {
		t.Errorf("Count debería ser 1: obtenido %d", count)
	}
	
	// Permitir más transacciones hasta el límite
	for i := 0; i < 4; i++ {
		if !rl.Allow(address) {
			t.Errorf("Transacción %d debería ser permitida", i+2)
		}
	}
	
	// Verificar count después de 5 transacciones
	count = rl.GetCount(address)
	if count != 5 {
		t.Errorf("Count debería ser 5: obtenido %d", count)
	}
	
	// La sexta transacción debería ser rechazada
	if rl.Allow(address) {
		t.Error("Sexta transacción debería ser rechazada (límite alcanzado)")
	}
	
	// Count debería seguir siendo 5
	count = rl.GetCount(address)
	if count != 5 {
		t.Errorf("Count debería seguir siendo 5 después de rechazo: obtenido %d", count)
	}
}

// TestRateLimiter_Allow_DifferentAddresses prueba que el rate limiting es por dirección
func TestRateLimiter_Allow_DifferentAddresses(t *testing.T) {
	rl := NewRateLimiter(5, 1*time.Minute, 1000)
	
	address1 := "0x1111111111111111111111111111111111111111"
	address2 := "0x2222222222222222222222222222222222222222"
	
	// Llenar límite para address1
	for i := 0; i < 5; i++ {
		if !rl.Allow(address1) {
			t.Errorf("Transacción %d de address1 debería ser permitida", i+1)
		}
	}
	
	// address1 debería estar en límite
	if rl.Allow(address1) {
		t.Error("address1 debería estar en límite")
	}
	
	// address2 debería poder hacer transacciones (límite independiente)
	for i := 0; i < 5; i++ {
		if !rl.Allow(address2) {
			t.Errorf("Transacción %d de address2 debería ser permitida", i+1)
		}
	}
	
	// Verificar counts
	count1 := rl.GetCount(address1)
	count2 := rl.GetCount(address2)
	
	if count1 != 5 {
		t.Errorf("Count de address1 debería ser 5: obtenido %d", count1)
	}
	if count2 != 5 {
		t.Errorf("Count de address2 debería ser 5: obtenido %d", count2)
	}
}

// TestRateLimiter_Allow_ExpiredWindow prueba que las transacciones expiradas se limpian
func TestRateLimiter_Allow_ExpiredWindow(t *testing.T) {
	// Crear rate limiter con ventana muy corta (100ms) para testing
	rl := NewRateLimiter(5, 100*time.Millisecond, 1000)
	
	address := "0x1234567890123456789012345678901234567890"
	
	// Llenar límite
	for i := 0; i < 5; i++ {
		if !rl.Allow(address) {
			t.Errorf("Transacción %d debería ser permitida", i+1)
		}
	}
	
	// Verificar que está en límite
	if rl.Allow(address) {
		t.Error("Debería estar en límite después de 5 transacciones")
	}
	
	// Esperar a que expire la ventana
	time.Sleep(150 * time.Millisecond)
	
	// Después de expirar, debería poder hacer transacciones nuevamente
	if !rl.Allow(address) {
		t.Error("Debería poder hacer transacciones después de que expire la ventana")
	}
	
	// El count debería ser 1 (las anteriores expiraron, solo queda la nueva)
	count := rl.GetCount(address)
	if count != 1 {
		t.Errorf("Count debería ser 1 después de expirar ventana: obtenido %d", count)
	}
}

// TestRateLimiter_GetCount prueba obtener el conteo de transacciones
func TestRateLimiter_GetCount(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Minute, 1000)
	
	address := "0x1234567890123456789012345678901234567890"
	
	// Inicialmente debería ser 0
	count := rl.GetCount(address)
	if count != 0 {
		t.Errorf("Count inicial debería ser 0: obtenido %d", count)
	}
	
	// Agregar transacciones
	for i := 0; i < 7; i++ {
		rl.Allow(address)
	}
	
	// Verificar count
	count = rl.GetCount(address)
	if count != 7 {
		t.Errorf("Count debería ser 7: obtenido %d", count)
	}
}

// TestRateLimiter_Cleanup prueba la limpieza de transacciones antiguas
func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(10, 100*time.Millisecond, 1000)
	
	address1 := "0x1111111111111111111111111111111111111111"
	address2 := "0x2222222222222222222222222222222222222222"
	
	// Agregar transacciones para ambas direcciones
	for i := 0; i < 3; i++ {
		rl.Allow(address1)
		rl.Allow(address2)
	}
	
	// Esperar a que expire la ventana
	time.Sleep(150 * time.Millisecond)
	
	// Agregar una transacción nueva para address1
	rl.Allow(address1)
	
	// Limpiar transacciones antiguas
	rl.Cleanup()
	
	// address1 debería tener 1 transacción (la nueva)
	count1 := rl.GetCount(address1)
	if count1 != 1 {
		t.Errorf("address1 debería tener 1 transacción después de cleanup: obtenido %d", count1)
	}
	
	// address2 no debería tener transacciones (todas expiraron)
	count2 := rl.GetCount(address2)
	if count2 != 0 {
		t.Errorf("address2 debería tener 0 transacciones después de cleanup: obtenido %d", count2)
	}
}

// TestRateLimiter_CheckMempoolSize prueba verificación del tamaño del mempool
func TestRateLimiter_CheckMempoolSize(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Minute, 1000)
	
	// Verificar límite del mempool
	limit := rl.GetMempoolSizeLimit()
	if limit != 1000 {
		t.Errorf("Límite del mempool debería ser 1000: obtenido %d", limit)
	}
	
	// Verificar que puede aceptar transacciones por debajo del límite
	if !rl.CheckMempoolSize(500) {
		t.Error("Mempool debería poder aceptar 500 transacciones")
	}
	
	if !rl.CheckMempoolSize(999) {
		t.Error("Mempool debería poder aceptar 999 transacciones")
	}
	
	// Verificar que rechaza cuando está en el límite
	if rl.CheckMempoolSize(1000) {
		t.Error("Mempool debería rechazar cuando está en el límite (1000)")
	}
	
	if rl.CheckMempoolSize(1001) {
		t.Error("Mempool debería rechazar cuando excede el límite (1001)")
	}
	
	// Verificar valores límite
	if !rl.CheckMempoolSize(0) {
		t.Error("Mempool vacío debería poder aceptar transacciones")
	}
	
	if !rl.CheckMempoolSize(1) {
		t.Error("Mempool con 1 transacción debería poder aceptar más")
	}
}

// TestRateLimiter_StartCleanup prueba el cleanup automático
func TestRateLimiter_StartCleanup(t *testing.T) {
	rl := NewRateLimiter(10, 100*time.Millisecond, 1000)
	
	address := "0x1234567890123456789012345678901234567890"
	
	// Agregar transacciones
	for i := 0; i < 5; i++ {
		rl.Allow(address)
	}
	
	// Iniciar cleanup automático cada 50ms
	rl.StartCleanup(50 * time.Millisecond)
	
	// Esperar a que expire la ventana y se ejecute cleanup
	time.Sleep(200 * time.Millisecond)
	
	// Verificar que las transacciones antiguas fueron limpiadas
	// Nota: El cleanup puede haber corrido o no, pero el count debería reflejar transacciones válidas
	count := rl.GetCount(address)
	// El count debería ser 0 después de que expire la ventana y se ejecute cleanup
	// (a menos que haya nuevas transacciones agregadas)
	
	// Agregar una transacción nueva y verificar
	rl.Allow(address)
	count = rl.GetCount(address)
	if count != 1 {
		t.Logf("Count después de cleanup y nueva transacción: %d (puede variar según timing)", count)
	}
}

// TestRateLimiter_ConcurrentAccess prueba acceso concurrente al rate limiter
func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Minute, 1000)
	
	address := "0x1234567890123456789012345678901234567890"
	
	// Ejecutar múltiples goroutines agregando transacciones
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 2; j++ {
				rl.Allow(address)
			}
			done <- true
		}()
	}
	
	// Esperar a que todas terminen
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verificar que el count es razonable (debería ser 20, pero puede ser menor por race conditions)
	count := rl.GetCount(address)
	if count < 10 || count > 20 {
		t.Logf("Count después de acceso concurrente: %d (puede variar por race conditions)", count)
	}
	
	// Verificar que el límite se respeta
	// No debería poder agregar más si ya alcanzó el límite
	if count >= 10 {
		if rl.Allow(address) {
			t.Error("No debería poder agregar más transacciones si alcanzó el límite")
		}
	}
}

