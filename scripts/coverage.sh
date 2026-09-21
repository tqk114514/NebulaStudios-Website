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

# 分包下限：合计门槛会被"往易处刷"抵消，故按包再设一道。
# 注意聚合粒度是三级前缀（internal/handlers 而非 internal/handlers/auth）。
# 数值取"当前值略低"作棘轮：只防回退，后续随覆盖率上调。
PKG_FLOORS="${PKG_FLOORS:-internal/middleware:85 internal/handlers:80 internal/services:50 cmd/server:65}"

# 关键文件（鉴权 / PKCE）要求每个函数至少被调用一次。
# 只抓 0%、不抓"没到 100%"：go tool cover -func 报的是函数内语句占比，
# 90% 通常意味着 rand.Read 失败之类的防御分支没走，硬凑没有价值；
# 而 0% 恰好对应两类真实病灶——从没进过的降级分支、以及没人调用的死代码。
# 合计覆盖率对这两类完全隐形（分母小），故单列一道闸。
CRITICAL_FILES="${CRITICAL_FILES:-internal/middleware/auth.go internal/utils/crypto.go}"

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
awk -v min="$MIN_COVERAGE" -v floors="$PKG_FLOORS" '
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

  ok = 1;
  if (pct + 0.0001 < min + 0) {
    print "  FAIL: 合计覆盖率低于门槛";
    ok = 0;
  }

  # 分包门槛：防止强包补贴弱包
  nf = split(floors, f, " ");
  for (i = 1; i <= nf; i++) {
    if (split(f[i], kv, ":") != 2) continue;
    pat = kv[1]; need = kv[2] + 0;
    for (d in total) {
      if (total[d] == 0) continue;
      if (index(d, pat) == 0) continue;
      ppct = covered[d] * 100 / total[d];
      if (ppct + 0.0001 < need) {
        printf "  FAIL: %s 覆盖率 %.1f%% < 分包门槛 %d%%\n", d, ppct, need;
        ok = 0;
      } else {
        printf "  分包: %s %.1f%% >= %d%%\n", d, ppct, need;
      }
      break;
    }
  }

  if (!ok) exit 1;
  print "  OK";
}' "$PROFILE"

# 关键文件的函数级门槛：抓"函数存在但从没被调用过"（降级分支、死代码）
echo
echo "==> 关键文件函数覆盖（不得存在从未被调用的函数）"
go tool cover -func="$PROFILE" | awk -v files="$CRITICAL_FILES" '
BEGIN { n = split(files, want, " "); for (i = 1; i <= n; i++) need[i] = want[i] }
{
  path = $1; sub(/:[0-9]+:$/, "", path);
  fn = $2;
  pct = $3; sub(/%$/, "", pct); pct = pct + 0;
  for (i = 1; i <= n; i++) {
    if (index(path, need[i]) == 0) continue;
    seen++;
    if (pct == 0) {
      printf "  FAIL: %s %s() 从未被任何测试调用\n", path, fn;
      bad = 1;
    }
    break;
  }
}
END {
  if (seen == 0) { print "  警告: 未匹配到任何关键文件函数，检查 CRITICAL_FILES 配置"; exit 1 }
  if (bad) exit 1;
  printf "  OK: %d 个函数均被调用过\n", seen;
}'
