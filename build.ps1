# Build script
# Usage:
#   .\build.ps1              # Build backend + frontend
#   .\build.ps1 -Backend     # Backend only
#   .\build.ps1 -Frontend    # Frontend only

param(
    [switch]$Backend,
    [switch]$Frontend
)

$ErrorActionPreference = "Stop"

if (-not $Backend -and -not $Frontend) {
    $Backend = $true
    $Frontend = $true
}

if ($Backend) {
    Write-Host "=== Building backend ===" -ForegroundColor Cyan
    $origGOOS = $env:GOOS
    $origGOARCH = $env:GOARCH
    $env:CGO_ENABLED = 0
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -trimpath -ldflags="-s -w -X auth-system/internal/version.ServerCommit=$(git rev-parse --short HEAD)" -o server ./cmd/server
    $env:GOOS = $origGOOS
    $env:GOARCH = $origGOARCH
    if ($LASTEXITCODE -ne 0) { throw "Backend build failed" }
    Write-Host "Backend build OK" -ForegroundColor Green
}

if ($Frontend) {
    Write-Host "=== Type-checking frontend (tsc --noEmit) ===" -ForegroundColor Cyan
    npx tsc --noEmit
    if ($LASTEXITCODE -ne 0) { throw "TypeScript type-check failed" }
    Write-Host "Type-check OK" -ForegroundColor Green

    Write-Host "=== Building frontend ===" -ForegroundColor Cyan
    # go run 的 log.Printf 输出到 stderr，PowerShell 的 Stop 策略会误判为终止错误，
    # 临时放宽策略，通过 LASTEXITCODE 判断真实结果
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    go run ./cmd/build
    $buildExit = $LASTEXITCODE
    $ErrorActionPreference = $prevEAP
    if ($buildExit -ne 0) { throw "Frontend build failed" }
    Write-Host "Frontend build OK" -ForegroundColor Green
}