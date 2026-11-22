#!/usr/bin/env bash
# 下载并按固定版本运行 TLA+ 模型检查；JAR 缓存在用户缓存目录。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/nebula"
JAR="${TLA2TOOLS_JAR:-$CACHE_DIR/tla2tools-1.7.4.jar}"
METADIR="$CACHE_DIR/tlc-states"

if [[ ! -f "$JAR" ]]; then
  mkdir -p "$(dirname "$JAR")"
  echo "downloading tla2tools 1.7.4 -> $JAR" >&2
  curl -fsSL -o "$JAR.tmp" \
    https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar
  mv "$JAR.tmp" "$JAR"
fi

mkdir -p "$METADIR"
cd "$ROOT/docs/tla"
exec java -XX:+UseParallelGC -cp "$JAR" tlc2.TLC \
  -metadir "$METADIR" -config RefreshFamily.cfg RefreshFamily.tla
