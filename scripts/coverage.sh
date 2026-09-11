#!/usr/bin/env bash
# 覆盖率统计与门槛校验
#
# 统计口径：
#   - 排除 internal/models：仓储实现依赖真实 PostgreSQL，本机/CI 无库时无法覆盖（见 docs/releases/go-1.27.1.md）
#   - 排除 internal/testutil：测试辅助代码（fake），计入分母会稀释真实覆盖率
#   - 排除 frontend/node_modules：依赖里夹带的 Go 包，与本项目无关
#
# 用法：
#   scripts/coverage.sh                # 默认门槛 MIN_COVERAGE（见下方默认值）
#   MIN_COVERAGE=70 scripts/coverage.sh
set -euo pipefail

MIN_COVERAGE="${MIN_COVERAGE:-50}"
PROFILE="${PROFILE:-coverage.out}"

EXCLUDE_PATTERNS='node_modules|internal/testutil|internal/models'

echo "==> 运行测试并采集覆盖率"
# shellcheck disable=SC2046
go test -covermode=atomic -coverprofile="$PROFILE" \
  $(go list ./cmd/... ./internal/... | grep -Ev "$EXCLUDE_PATTERNS")

echo
echo "==> 各包覆盖率"
go test -cover $(go list ./cmd/... ./internal/... | grep -Ev "$EXCLUDE_PATTERNS") 2>/dev/null \
  | grep -E "coverage:" || true

echo
echo "==> 汇总（已排除 models / testutil / node_modules）"
awk -v min="$MIN_COVERAGE" '
{
  split($0, a, " ");
  meta = a[1]; stmt = a[2] + 0; cnt = a[3] + 0;
  split(meta, m, ":");
  file = m[1];
  if (file ~ /node_modules/) next;
  split(file, p, "/");
  pkg = p[1] "/" p[2] "/" p[3];
  total[pkg] += stmt;
  if (cnt > 0) covered[pkg] += stmt;
  T += stmt;
  if (cnt > 0) C += stmt;
}
END {
  for (d in total) {
    if (total[d] == 0) continue;
    printf "  %6.1f%%  %s\n", covered[d] * 100 / total[d], d;
  }
  pct = (T > 0) ? C * 100 / T : 0;
  printf "\n  合计: %.1f%% (已覆盖 %d / 总 %d)，门槛 %s%%\n", pct, C, T, min;
  if (pct + 0.0001 < min + 0) {
    print "  FAIL: 覆盖率低于门槛";
    exit 1;
  }
  print "  OK";
}' "$PROFILE"
