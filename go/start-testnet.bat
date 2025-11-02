@echo off
REM Script para iniciar Oxy•gen Blockchain Testnet en Windows

echo ========================================
echo   Oxy•gen Blockchain Testnet Launcher
echo ========================================
echo.

REM Configuración de Testnet
set OXY_DATA_DIR=testnet_data
set OXY_CHAIN_ID=oxy-gen-testnet
set OXY_LOG_LEVEL=info
set OXY_LOG_JSON=false
REM API REST - Usar otro puerto para evitar conflictos
REM Puedes cambiar el puerto si el 8081 también está ocupado
set BLOCKCHAIN_API_ENABLED=true
set BLOCKCHAIN_API_HOST=0.0.0.0
set BLOCKCHAIN_API_PORT=8081
set OXY_MESH_ENDPOINT=ws://localhost:3001
set OXY_VALIDATOR_KEY=
set OXY_VALIDATOR_ADDR=
set COMETBFT_HOME=%OXY_DATA_DIR%\cometbft

echo Configuración:
echo   Chain ID: %OXY_CHAIN_ID%
echo   Data Dir: %OXY_DATA_DIR%
echo   API REST: http://%BLOCKCHAIN_API_HOST%:%BLOCKCHAIN_API_PORT%
echo   Mesh: %OXY_MESH_ENDPOINT%
echo   Validator Key: (se generará automáticamente si está vacío)
echo   Log Level: %OXY_LOG_LEVEL%
echo   Log JSON: %OXY_LOG_JSON%
echo.

REM Verificar que Go está instalado
echo Verificando Go...
go version >nul 2>&1
if errorlevel 1 (
    echo ❌ ERROR: Go no está instalado o no está en PATH
    echo    Instala Go desde https://golang.org/dl/
    pause
    exit /b 1
)

for /f "tokens=*" %%i in ('go version') do set GO_VERSION=%%i
echo ✅ Go detectado: %GO_VERSION%
echo.

REM Ir al directorio del proyecto
cd /d "%~dp0"
if not exist "cmd\oxy-blockchain\main.go" (
    echo ❌ ERROR: No se encontró el archivo main.go
    echo    Asegúrate de ejecutar este script desde el directorio raíz del proyecto
    echo    Directorio actual: %CD%
    pause
    exit /b 1
)

echo ✅ Directorio verificado
echo.

REM Verificar módulos de Go
echo Verificando módulos de Go...
if not exist "go.mod" (
    echo ❌ ERROR: No se encontró go.mod
    echo    Asegúrate de estar en el directorio correcto
    pause
    exit /b 1
)
echo ✅ go.mod encontrado
echo.

REM Crear directorio de datos si no existe
if not exist "%OXY_DATA_DIR%" (
    echo 📁 Creando directorio de datos: %OXY_DATA_DIR%
    mkdir "%OXY_DATA_DIR%"
) else (
    echo ✅ Directorio de datos existe: %OXY_DATA_DIR%
)
echo.

REM Verificar si CometBFT está inicializado
if exist "%OXY_DATA_DIR%\cometbft\config\genesis.json" (
    echo ✅ CometBFT ya está inicializado
) else (
    echo ⚠️ CometBFT no está inicializado (se inicializará al iniciar)
    echo    La primera vez puede tardar mientras genera claves y configuración
)
echo.

REM Verificar puertos (opcional - puede saltarse si hay falsos positivos)
echo Verificando puerto API REST (%BLOCKCHAIN_API_PORT%)...
echo.
echo ¿Verificar si el puerto está libre?
echo   [1] Sí, verificar
echo   [2] No, saltar verificación (recomendado si hay falsos positivos)
echo.
set /p VERIFY_PORT="Opción (1 o 2, presiona Enter para saltar): "
if "%VERIFY_PORT%"=="1" goto verify_port
if "%VERIFY_PORT%"=="2" goto skip_port_check
if "%VERIFY_PORT%"=="" goto skip_port_check
goto verify_port

:verify_port
echo Verificando puerto %BLOCKCHAIN_API_PORT%...
REM Solo verificar LISTENING real con PowerShell (más preciso que netstat)
powershell -NoProfile -Command "$conn = Get-NetTCPConnection -LocalPort %BLOCKCHAIN_API_PORT% -State Listen -ErrorAction SilentlyContinue; if ($conn) { Write-Host 'Puerto ocupado'; exit 1 } else { Write-Host 'Puerto libre'; exit 0 }" >nul 2>&1
if errorlevel 1 (
    echo ⚠️ Puerto %BLOCKCHAIN_API_PORT% está en uso (LISTEN), buscando alternativo...
    
    REM Intentar puertos alternativos automáticamente
    set ALT_PORT=8082
    :find_free_port
    powershell -NoProfile -Command "$conn = Get-NetTCPConnection -LocalPort %ALT_PORT% -State Listen -ErrorAction SilentlyContinue; if ($conn) { exit 1 } else { exit 0 }" >nul 2>&1
    if errorlevel 1 (
        set /a ALT_PORT+=1
        if %ALT_PORT% GTR 8095 (
            echo ⚠️ No se encontró puerto libre automáticamente (8081-8095)
            echo    Usando puerto configurado (%BLOCKCHAIN_API_PORT%)
            echo    Si hay conflicto, Go mostrará el error al iniciar
            goto port_ok
        )
        goto find_free_port
    )
    
    echo ✅ Puerto %ALT_PORT% disponible, usando ese puerto
    set BLOCKCHAIN_API_PORT=%ALT_PORT%
) else (
    echo ✅ Puerto %BLOCKCHAIN_API_PORT% disponible
)
goto port_ok

:skip_port_check
echo ⏭️ Verificación de puerto saltada
echo    Usando puerto configurado: %BLOCKCHAIN_API_PORT%
echo    Si hay conflicto, Go mostrará el error al iniciar

:port_ok
echo ✅ Puerto %BLOCKCHAIN_API_PORT% configurado para API REST
echo.

echo ========================================
echo   Iniciando Testnet...
echo ========================================
echo.
echo 📝 NOTAS:
echo    - Los logs aparecerán aquí abajo
echo    - La primera vez puede tardar inicializando CometBFT
echo    - Verifica que el mesh network esté corriendo en %OXY_MESH_ENDPOINT%
echo    - Para detener, presiona Ctrl+C
echo.
echo ========================================
echo.

REM Guardar el directorio actual antes de cambiar
set CURRENT_DIR=%CD%

REM Ejecutar con las variables de entorno configuradas
echo Iniciando testnet con configuración:
echo   BLOCKCHAIN_API_ENABLED=%BLOCKCHAIN_API_ENABLED%
echo   BLOCKCHAIN_API_PORT=%BLOCKCHAIN_API_PORT%
echo   BLOCKCHAIN_API_HOST=%BLOCKCHAIN_API_HOST%
echo   OXY_DATA_DIR=%OXY_DATA_DIR%
echo   OXY_CHAIN_ID=%OXY_CHAIN_ID%
echo   OXY_MESH_ENDPOINT=%OXY_MESH_ENDPOINT%
echo.
echo ⚠️ IMPORTANTE: Verifica en los logs del testnet que aparezca:
echo    "Configuración API REST: APIEnabled=true, APIPort=8081, APIHost=0.0.0.0"
echo    "Iniciando servidor REST local en 0.0.0.0:8081"
echo.

REM Compilar primero para asegurar que los cambios se apliquen
echo Compilando testnet...
cd "%CURRENT_DIR%\cmd\oxy-blockchain"
go build -o testnet.exe main.go
if errorlevel 1 (
    echo ❌ ERROR: Fallo al compilar
    pause
    exit /b 1
)
echo ✅ Compilación exitosa
echo.

REM Configurar variables de entorno y ejecutar directamente
REM Las variables SET en batch están disponibles en el mismo proceso
cd "%CURRENT_DIR%\cmd\oxy-blockchain"

REM Configurar variables de entorno antes de ejecutar
set BLOCKCHAIN_API_ENABLED=%BLOCKCHAIN_API_ENABLED%
set BLOCKCHAIN_API_PORT=%BLOCKCHAIN_API_PORT%
set BLOCKCHAIN_API_HOST=%BLOCKCHAIN_API_HOST%
set OXY_DATA_DIR=%OXY_DATA_DIR%
set OXY_CHAIN_ID=%OXY_CHAIN_ID%
set OXY_MESH_ENDPOINT=%OXY_MESH_ENDPOINT%
set OXY_LOG_LEVEL=%OXY_LOG_LEVEL%
set OXY_LOG_JSON=%OXY_LOG_JSON%
set COMETBFT_HOME=%COMETBFT_HOME%

REM Ejecutar directamente (sin PowerShell) para que los logs se vean
echo.
echo Ejecutando testnet.exe con las siguientes variables:
echo   BLOCKCHAIN_API_ENABLED=%BLOCKCHAIN_API_ENABLED%
echo   BLOCKCHAIN_API_PORT=%BLOCKCHAIN_API_PORT%
echo   OXY_DATA_DIR=%OXY_DATA_DIR%
echo   OXY_CHAIN_ID=%OXY_CHAIN_ID%
echo.
echo Los logs aparecerán aquí abajo:
echo ========================================
echo.

.\testnet.exe

REM Si llegamos aquí, el proceso terminó
echo.
echo ========================================
echo   Testnet detenido
echo ========================================
echo.

pause

