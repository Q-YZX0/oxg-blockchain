#!/bin/bash
# Script para iniciar Oxy•gen Blockchain Testnet en Linux/Mac

set -e

echo "========================================"
echo "  Oxy•gen Blockchain Testnet Launcher"
echo "========================================"
echo ""

# Configuración de Testnet
export OXY_DATA_DIR="./testnet_data"
export OXY_CHAIN_ID="oxy-gen-testnet"
export OXY_LOG_LEVEL="info"
export OXY_LOG_JSON="false"
export BLOCKCHAIN_API_ENABLED="true"
export BLOCKCHAIN_API_HOST="0.0.0.0"
export BLOCKCHAIN_API_PORT="8080"
export OXY_MESH_ENDPOINT="ws://localhost:3001"
export COMETBFT_HOME="${OXY_DATA_DIR}/cometbft"

echo "Configuración:"
echo "  Chain ID: ${OXY_CHAIN_ID}"
echo "  Data Dir: ${OXY_DATA_DIR}"
echo "  API REST: http://${BLOCKCHAIN_API_HOST}:${BLOCKCHAIN_API_PORT}"
echo "  Mesh: ${OXY_MESH_ENDPOINT}"
echo ""

# Verificar que Go está instalado
if ! command -v go &> /dev/null; then
    echo "ERROR: Go no está instalado o no está en PATH"
    exit 1
fi

echo "✅ Go detectado: $(go version)"
echo ""

# Ir al directorio del proyecto
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if [ ! -f "cmd/oxy-blockchain/main.go" ]; then
    echo "ERROR: No se encontró el archivo main.go"
    echo "Asegúrate de ejecutar este script desde el directorio raíz del proyecto"
    exit 1
fi

echo "✅ Directorio verificado"
echo ""

# Crear directorio de datos si no existe
if [ ! -d "$OXY_DATA_DIR" ]; then
    echo "📁 Creando directorio de datos: ${OXY_DATA_DIR}"
    mkdir -p "$OXY_DATA_DIR"
fi

echo ""
echo "🚀 Iniciando Oxy•gen Blockchain Testnet..."
echo ""
echo "Para detener, presiona Ctrl+C"
echo ""

# Ejecutar
cd cmd/oxy-blockchain
go run main.go

