# Go 1.27.1 升级 + pprof 诊断接入（发布说明与 PR 描述）

日期：2026-09-11　·　分支：`main`（基线 `bee7535`）　·　建议 commit type：`feat`

本文包含两部分，均可直接复制使用：**CHANGELOG 片段**（粘到 GitHub Release 或 CHANGELOG）与 **PR 描述**。

---

## 一、CHANGELOG 片段

### 升级

- 工具链升级至 **Go 1.27.1**，`go.mod` 的 go 指令 `1.26.5` → `1.27.1`。CI 通过 `setup-go` 的 `go-version-file: go.mod` 自动跟随，无需改动 workflow。

### 新增

- **pprof 诊断服务**（默认关闭）：`PPROF_ENABLED=true` 时在 `PPROF_ADDR`（默认 `127.0.0.1:6060`）独立监听，暴露 `/debug/pprof/` 系列端点，含 Go 1.27 转正的 `/debug/pprof/goroutineleak`，用于排查后台任务与 img-processor 常驻 goroutine 泄漏。
- **配置校验**：`PPROF_ADDR` 为非回环地址且未设置 `PPROF_ALLOW_REMOTE=true` 时拒绝启动，防止诊断接口被误暴露到公网。
- 新增测试：`cmd/server`（pprof mux 与生命周期）、`internal/config`（回环地址判定与 pprof 校验规则，该包此前无测试）。

### 变更

- 依赖升级（含安全修复与常规补丁）：
  - `golang.org/x/crypto` v0.54.0 → v0.57.0（连带 `x/net` v0.58.0、`x/sync` v0.23.0、`x/sys` v0.48.0、`x/text` v0.42.0）。govulncheck 报出 `x/crypto/ssh` 的 GO-2026-6355 / GO-2026-6354 / GO-2026-6303 三个漏洞，本服务未调用这些包，升级后消除；`openpgp` 的 GO-2026-5932 无修复版本（包已停止维护），同样未被引用。
  - `github.com/andybalholm/brotli` v1.2.2 → v1.2.4、`github.com/jackc/pgx/v5` v5.10.0 → v5.11.0、`golang.org/x/time` v0.15.0 → v0.16.0。
- `NOTICES` 同步上述 8 处版本号（该文件以许可证链接里的 tag 记录版本）。
- CI：新增 `.github/workflows/race.yml`，每日夜间跑 `go test -race ./...`（`-race` 需重编全部依赖，不挂到每次 PR；可在 workflow 里放开 `pull_request` 触发器改为 PR 拦截）。
- `errors.As` → `errors.AsType[T]`（4 处：`models.IsUniqueViolation`、`utils.IsDatabaseNotFound` 及两个测试断言）。
- `interface{}` → `any`（2 处：`utils.BindJSON` / `BindJSONOrError`；全仓其余 147 处已是 `any`）。
- 文档：技术栈与环境要求改为 Go 1.27.1，新增 pprof 配置项说明与「诊断接口（pprof）」小节。

### 行为变更（随工具链自动生效，无需改代码）

- `encoding/json` v1 改由 v2 实现支撑：反序列化显著变快，重复对象名与非法 UTF-8 被拒绝。本服务 25 处 JSON 解码路径直接受益；错误文案匹配逻辑（`utils/bind.go` 只匹配 `request body too large`，来自 `http.MaxBytesReader`）不受影响。
- `net.UnixConn` 读 EOF 不再包装为 `net.OpError`：img-processor 客户端仅做 `fmt.Errorf("%w")` 包装、无类型断言，仅日志文案变化。
- go 指令 ≥1.27 后 traceback 携带 pprof goroutine 标签；如需关闭用 `GODEBUG=tracebacklabels=0`。
- `go test` 默认启用 `stdversion` vet 检查；`go mod tidy` 对 `go 1.27+` 模块强制 direct / indirect 两块布局（本仓已符合，`go mod tidy -diff` 无差异）。

---

## 二、PR 描述

### 标题

```
feat(diag): 升级 Go 1.27.1 并接入 pprof 诊断服务
```

### 正文

**背景**

当前仓库未注册任何 `net/http/pprof`，后台任务（Token 清理、OAuth state 清理、日志清理、SMTP 保活）与 img-processor 常驻 goroutine 一旦泄漏，只能靠外部现象推断。Go 1.27 把 goroutine leak profile 转正，是这次升级唯一"必须写代码才能拿到"的能力，其余特性（json v2 性能、小对象分配提速）升级后自动生效。

**改动**

1. `go.mod`：go 指令 `1.26.5` → `1.27.1`。
2. `cmd/server/pprof.go`（新增）：`newPprofMux()` 显式注册 pprof 端点（不用 `import _ "net/http/pprof"`，避免把处理器挂到 `http.DefaultServeMux` 被其他包误 Serve）；`startPprofServer()` 独立监听；`shutdownPprofServer()` 支持 nil，关闭超时 5s。
3. `cmd/server/main.go`：`run()` 按配置启动；`gracefulShutdown` 新增 `pprofSrv` 参数并**最先关闭**，避免 profile 采集拖慢主服务收尾。
4. `internal/config/config.go`：新增 `PPROF_ENABLED` / `PPROF_ADDR` / `PPROF_ALLOW_REMOTE` 与 `isLoopbackAddr()`；`validateConfig` 对非回环且未显式放行的配置直接报错。
5. Go 1.27 现代化：`errors.As` → `errors.AsType[T]`（4 处）、`interface{}` → `any`（2 处）。
6. 测试与文档：`cmd/server/pprof_test.go`、`internal/config/config_test.go`；README 同步。

**为什么 pprof 独立监听而不是挂主路由**

pprof 端点不经过 Gin 的鉴权 / 限流 / CSP 中间件，访问面只能靠监听地址收敛，因此默认只绑回环、非回环需显式声明 `PPROF_ALLOW_REMOTE=true`。远程排查走 SSH 隧道：`ssh -L 6060:127.0.0.1:6060 <server>`。

**验证**

- `gofmt -l cmd internal` 无输出；`go vet ./...`、`go build ./...`、`go test ./...` 全部通过（go1.27.1 windows/amd64）。
- `go mod tidy -diff` 无差异。
- goroutineleak 实测有效：构造一个永久阻塞在无人引用 channel 上的 goroutine，profile 输出 `total 1` 并精确到泄漏点的源码行。
- 依赖审计：`govulncheck ./...` 报告调用链 0 漏洞；模块级 4 项（`x/crypto/ssh` ×3、`x/crypto/openpgp` ×1）均未被本服务调用，已通过升级 `x/crypto` 消除其中 3 项。
- 前端：`npm run typecheck`（`vue-tsc --noEmit`）与 `npm run build`（含 `data/`、`policy/` 复制与 59 个 `.br` 预压缩产物）均通过；`npm audit --omit=dev` 生产依赖 0 漏洞（38 个）。
- 竞态：`go test -race ./...` 全绿，未发现数据竞争（限流器分片、LRU、img-processor 信号量等并发路径均已覆盖）。
- Zig：`zig build test` 通过（zig 0.16.0）。注：Windows 沙箱环境下会因 `AccessDenied` 失败，需放沙箱外执行，CI（ubuntu）不受影响。
- 发布构建：`go build -trimpath -ldflags "-s -w -X auth-system/internal/version.ServerCommit=$(git rev-parse --short HEAD)"` 成功，产物 37 MB（含嵌入的 img-processor）。

**风险与回滚**

- 默认 `PPROF_ENABLED=false`，不配置即行为不变，无用户可感知影响。
- 唯一外部依赖变化是工具链版本：CI 与部署环境需提供 Go 1.27.1（`setup-go` 会按 go.mod 自动拉取）。
- 回滚：`git revert` 本 PR 即可；`go.mod` 回退后需重新编译。
- 观察项：一次全量 `go test ./...` 在与 govulncheck、前端构建并行的高负载窗口内出现 `internal/services` 包失败；随后 8 次复跑（含 351 个用例的 `-v` 全量运行、services 包 10 次压测）均未复现。若 CI 出现同类偶发，先怀疑时间/调度敏感用例，可用 `go test -p 1` 降并行度验证。

**部署注意**

- 无需新增必填配置；按需设置 `PPROF_ENABLED=true` 开启诊断。
- 若确需远程访问 pprof，必须同时设置 `PPROF_ALLOW_REMOTE=true`，并自行在外层（防火墙 / Cloudflare Access）限制来源。

**待确认**

- `.gitignore` 中新增的 `.workbuddy-ai/` 条目由本地工具链写入，与本 PR 无关，提交前需确认是否一并纳入或还原。

**后续可选**

- 唯一残留的模块级告警 GO-2026-5932（`x/crypto/openpgp`，官方无修复版本）无法通过升级消除，项目也未引用该包，建议记录即可。
- 构建产物 `HomePage` chunk 543 KB（gzip 139 KB）触发 Vite 体积告警，主要来自 three.js；如需优化，可考虑路由级动态导入。
