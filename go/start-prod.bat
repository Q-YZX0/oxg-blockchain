@echo off
setlocal EnableDelayedExpansion

REM === Configuración por defecto (puedes sobreescribir con variables de entorno antes de llamar el script) ===
if "%OXY_ENV%"=="" set OXY_ENV=production
if "%OXY_CHAIN_ID%"=="" set OXY_CHAIN_ID=oxy-mainnet
if "%OXY_DATA_DIR%"=="" set OXY_DATA_DIR=C:\\OxygenData\\mainnet_data
if "%OXY_LOG_LEVEL%"=="" set OXY_LOG_LEVEL=info
if "%OXY_LOG_JSON%"=="" set OXY_LOG_JSON=true
if "%BLOCKCHAIN_API_ENABLED%"=="" set BLOCKCHAIN_API_ENABLED=true
if "%BLOCKCHAIN_API_HOST%"=="" set BLOCKCHAIN_API_HOST=0.0.0.0
if "%BLOCKCHAIN_API_PORT%"=="" set BLOCKCHAIN_API_PORT=8080
if "%OXY_METRICS_ENABLE%"=="" set OXY_METRICS_ENABLE=true
if "%OXY_METRICS_PORT%"=="" set OXY_METRICS_PORT=9102

REM Peers/Seeds (opcional)
if "%OXY_PERSISTENT_PEERS%"=="" set OXY_PERSISTENT_PEERS=
if "%OXY_SEEDS%"=="" set OXY_SEEDS=

REM Convertir OXY_DATA_DIR a ruta absoluta (si ya lo es, no cambia)
for %%I in (.) do set CUR_DIR=%%~fI
pushd %~dp0
cd cmd\oxy-blockchain
for %%I in (.) do set CMD_DIR=%%~fI
popd
if not exist "%OXY_DATA_DIR%" (
    mkdir "%OXY_DATA_DIR%" 2>nul
)

REM Recalcular COMETBFT_HOME a partir de OXY_DATA_DIR
set COMETBFT_HOME=%OXY_DATA_DIR%\cometbft
if not exist "%COMETBFT_HOME%\config" (
    mkdir "%COMETBFT_HOME%\config" 2>nul
)
if not exist "%COMETBFT_HOME%\data" (
    mkdir "%COMETBFT_HOME%\data" 2>nul
)

REM Mostrar configuración clave
echo OXY_ENV=%OXY_ENV%
echo OXY_CHAIN_ID=%OXY_CHAIN_ID%
echo OXY_DATA_DIR=%OXY_DATA_DIR%
echo COMETBFT_HOME=%COMETBFT_HOME%
echo BLOCKCHAIN_API_HOST=%BLOCKCHAIN_API_HOST%
echo BLOCKCHAIN_API_PORT=%BLOCKCHAIN_API_PORT%
echo OXY_PERSISTENT_PEERS=%OXY_PERSISTENT_PEERS%
echo OXY_SEEDS=%OXY_SEEDS%

REM Ejecutar binario
pushd go\cmd\oxy-blockchain
if not exist testnet.exe (
    echo Compilando binario...
    go build -o testnet.exe .
)

echo Iniciando nodo en modo PRODUCCION...
set OXY_ENV=%OXY_ENV%
set OXY_CHAIN_ID=%OXY_CHAIN_ID%
set OXY_DATA_DIR=%OXY_DATA_DIR%
set COMETBFT_HOME=%COMETBFT_HOME%
set OXY_LOG_LEVEL=%OXY_LOG_LEVEL%
set OXY_LOG_JSON=%OXY_LOG_JSON%
set BLOCKCHAIN_API_ENABLED=%BLOCKCHAIN_API_ENABLED%
set BLOCKCHAIN_API_HOST=%BLOCKCHAIN_API_HOST%
set BLOCKCHAIN_API_PORT=%BLOCKCHAIN_API_PORT%
set OXY_METRICS_ENABLE=%OXY_METRICS_ENABLE%
set OXY_METRICS_PORT=%OXY_METRICS_PORT%
set OXY_PERSISTENT_PEERS=%OXY_PERSISTENT_PEERS%
set OXY_SEEDS=%OXY_SEEDS%

start "oxy-node" testnet.exe
popd

endlocal
