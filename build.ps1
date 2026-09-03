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
    # 构建 SPA 前端（Vue 3 + Vite）：vue-tsc 类型检查 + vite build，
    # 产物输出到项目根 dist/（含 index.html/assets/data/policy，供 server 二进制同目录部署）

    Push-Location frontend
    try {
        if (-not (Test-Path "node_modules")) {
            Write-Host "=== Installing frontend dependencies (npm ci) ===" -ForegroundColor Cyan
            npm ci
            if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
        }

        # locales 由 package.json 的 prebuild 钩子生成（npm run build 前自动执行，无需在此调用）

        Write-Host "=== Building frontend (Vite) ===" -ForegroundColor Cyan
        npm run build
        if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
    } finally {
        Pop-Location
    }

    # Brotli 预压缩：服务端 PreCompressedStatic 对 /assets/* 与 /policy-content/* 优先服务 .br 副本
    # （中间件已支持无 .br 时回退原文件，此步骤保证压缩收益）
    Write-Host "=== Pre-compressing dist (Brotli) ===" -ForegroundColor Cyan
    Add-Type -AssemblyName System.IO.Compression
    $brCount = 0
    $compressible = "*.js", "*.css", "*.svg", "*.json", "*.md", "*.html", "*.woff", "*.woff2"
    Get-ChildItem -Path (Join-Path (Get-Location) "dist") -Recurse -File | Where-Object {
        $compressible -contains ("*" + $_.Extension)
    } | ForEach-Object {
        $src = $_.FullName
        $dst = "$src.br"
        if (-not (Test-Path $dst)) {
            $in = [System.IO.File]::OpenRead($src)
            $out = [System.IO.File]::Create($dst)
            try {
                $br = [System.IO.Compression.BrotliStream]::new($out, [System.IO.Compression.CompressionLevel]::Optimal)
                $in.CopyTo($br)
                $br.Dispose()
                $script:brCount++
            } finally {
                $in.Dispose()
                $out.Dispose()
            }
        }
    }
    Write-Host "Brotli pre-compressed: $brCount files" -ForegroundColor Green
    Write-Host "Frontend build OK -> dist/" -ForegroundColor Green
}