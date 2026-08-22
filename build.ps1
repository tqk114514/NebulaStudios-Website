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

# Zig 编译 img-processor 并复制到 internal/services/img-processor-bin（供 //go:embed 使用）
# 目标平台由 -Dtarget 指定；默认 x86_64-linux-gnu 与后端部署目标一致
function Build-ImgProcessor {
    $zig = Get-Command zig -ErrorAction SilentlyContinue
    if (-not $zig) {
        throw "zig 未安装（或在 PATH 中）。img-processor-bin 是后端编译的硬依赖（//go:embed），请先安装 Zig 0.16.0。"
    }

    $target = if ($env:IMG_PROCESSOR_TARGET) { $env:IMG_PROCESSOR_TARGET } else { "x86_64-linux-gnu" }

    Write-Host "=== Building img-processor (target: $target) ===" -ForegroundColor Cyan
    Push-Location img-processor
    try {
        zig build -Doptimize=ReleaseFast "-Dtarget=$target"
        if ($LASTEXITCODE -ne 0) { throw "img-processor build failed" }
    } finally {
        Pop-Location
    }

    $src = Join-Path (Get-Location) "img-processor\zig-out\bin\img-processor"
    $dst = Join-Path (Get-Location) "internal\services\img-processor-bin"
    Copy-Item $src $dst -Force
    Write-Host "img-processor OK -> $dst" -ForegroundColor Green
}

if (-not $Backend -and -not $Frontend) {
    $Backend = $true
    $Frontend = $true
}

if ($Backend) {
    Build-ImgProcessor

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
    # 确保依赖已安装（npm ci 需要 package-lock.json，与 CI 一致）。
    # 不要依赖 npx 自动下载——本地没有 tsc 时 npx 会拉到抢注的废弃包 tsc@2.0.4。
    if (-not (Test-Path "node_modules\.bin\tsc")) {
        Write-Host "=== Installing frontend dependencies (npm ci) ===" -ForegroundColor Cyan
        npm ci
        if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
    }

    # 先构建再类型检查：cmd/build 会依据 shared/js/lib 目录重新生成 vendor.ts
    # 并补齐 ts-nocheck 头，tsc 必须在生成之后跑才能校验到最新引用
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

    Write-Host "=== Type-checking frontend (tsc --noEmit) ===" -ForegroundColor Cyan
    # --no-install：npx 只使用本地已装的包，绝不联网下载
    npx --no-install tsc --noEmit
    if ($LASTEXITCODE -ne 0) { throw "TypeScript type-check failed" }
    Write-Host "Type-check OK" -ForegroundColor Green
}