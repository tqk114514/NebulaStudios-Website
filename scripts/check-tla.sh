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

cd "$ROOT/docs/tla"

# 每个模型使用独立的状态目录，避免两个配置的指纹缓存互相干扰。
run_tlc() {
  local module="$1"
  echo "==> TLC: $module" >&2
  mkdir -p "$METADIR/$module"
  java -XX:+UseParallelGC -cp "$JAR" tlc2.TLC \
    -metadir "$METADIR/$module" -config "$module.cfg" "$module.tla"
}

run_tlc RefreshFamily
run_tlc OAuthCode
