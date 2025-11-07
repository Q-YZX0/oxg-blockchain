@echo off
setlocal EnableDelayedExpansion

if "%COMETBFT_HOME%"=="" (
  echo COMETBFT_HOME no establecido. Saliendo.
  exit /b 1
)
if "%OXY_DATA_DIR%"=="" (
  echo OXY_DATA_DIR no establecido. Saliendo.
  exit /b 1
)

set TS=%DATE:~10,4%-%DATE:~4,2%-%DATE:~7,2%_%TIME:~0,2%-%TIME:~3,2%-%TIME:~6,2%
set TS=%TS: =0%
set OUT=snapshot-%TS%.zip

echo Creando snapshot %OUT% ...
powershell -Command "Compress-Archive -LiteralPath '%COMETBFT_HOME%\config','%COMETBFT_HOME%\data','%OXY_DATA_DIR%\evm' -DestinationPath '%OUT%' -Force" || (
  echo Error creando snapshot.
  exit /b 1
)

echo Snapshot creado: %OUT%
endlocal
