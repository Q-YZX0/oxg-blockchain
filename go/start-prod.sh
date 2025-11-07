#!/usr/bin/env bash
set -euo pipefail

# Config por defecto (puedes sobreescribir por entorno antes de llamar)
: "${OXY_ENV:=production}"
: "${OXY_CHAIN_ID:=oxy-mainnet}"
: "${OXY_DATA_DIR:=/var/lib/oxygen/mainnet_data}"
: "${OXY_LOG_LEVEL:=info}"
: "${OXY_LOG_JSON:=true}"
: "${BLOCKCHAIN_API_ENABLED:=true}"
: "${BLOCKCHAIN_API_HOST:=0.0.0.0}"
: "${BLOCKCHAIN_API_PORT:=8080}"
: "${OXY_METRICS_ENABLE:=true}"
: "${OXY_METRICS_PORT:=9102}"
: "${OXY_PERSISTENT_PEERS:=}"
: "${OXY_SEEDS:=}"

export COMETBFT_HOME="${COMETBFT_HOME:-$OXY_DATA_DIR/cometbft}"

# Preparar dirs
mkdir -p "$COMETBFT_HOME/config" "$COMETBFT_HOME/data"
mkdir -p "$(dirname "$OXY_DATA_DIR")" "$OXY_DATA_DIR"

# Mostrar configuración clave
echo "OXY_ENV=$OXY_ENV"
echo "OXY_CHAIN_ID=$OXY_CHAIN_ID"
echo "OXY_DATA_DIR=$OXY_DATA_DIR"
echo "COMETBFT_HOME=$COMETBFT_HOME"
echo "BLOCKCHAIN_API_HOST=$BLOCKCHAIN_API_HOST"
echo "BLOCKCHAIN_API_PORT=$BLOCKCHAIN_API_PORT"
echo "OXY_PERSISTENT_PEERS=$OXY_PERSISTENT_PEERS"
echo "OXY_SEEDS=$OXY_SEEDS"

# Ubicación del binario
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="$SCRIPT_DIR/cmd/oxy-blockchain"
BIN_PATH="$BIN_DIR/oxy-node"

# Build si falta
if [[ ! -x "$BIN_PATH" ]]; then
  echo "Compilando binario..."
  (cd "$BIN_DIR" && go build -o oxy-node .)
fi

# Exportar entorno requerido
export OXY_ENV OXY_CHAIN_ID OXY_DATA_DIR COMETBFT_HOME
export OXY_LOG_LEVEL OXY_LOG_JSON BLOCKCHAIN_API_ENABLED BLOCKCHAIN_API_HOST BLOCKCHAIN_API_PORT
export OXY_METRICS_ENABLE OXY_METRICS_PORT OXY_PERSISTENT_PEERS OXY_SEEDS

echo "Iniciando nodo en modo PRODUCCION (Linux)..."
exec "$BIN_PATH"
