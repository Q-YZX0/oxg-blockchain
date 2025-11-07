# Script de rotación de logs para Windows
# Uso: powershell.exe -File rotate-logs.ps1

param(
    [string]$LogDir = "C:\OxygenData\logs",
    [int]$MaxSizeMB = 100,
    [int]$KeepDays = 7
)

# Crear directorio de archivos si no existe
$archiveDir = Join-Path $LogDir "archive"
if (-not (Test-Path $archiveDir)) {
    New-Item -ItemType Directory -Path $archiveDir | Out-Null
}

# Rotar logs por tamaño o antigüedad
Get-ChildItem "$LogDir\*.log" -ErrorAction SilentlyContinue | Where-Object {
    $_.Length -gt ($MaxSizeMB * 1MB) -or
    $_.LastWriteTime -lt (Get-Date).AddDays(-$KeepDays)
} | ForEach-Object {
    $archiveName = "$($_.BaseName)-$(Get-Date -Format 'yyyyMMdd-HHmmss').zip"
    $archivePath = Join-Path $archiveDir $archiveName
    
    Write-Host "Rotando log: $($_.Name) -> $archiveName"
    Compress-Archive -Path $_.FullName -DestinationPath $archivePath -Force
    Remove-Item $_.FullName -Force
}

Write-Host "Rotación de logs completada."

