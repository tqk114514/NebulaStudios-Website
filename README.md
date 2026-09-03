# Nebula Studios Website

Nebula Studios 网站的前后端源码，包含用户系统、OAuth 认证、管理后台以及一个用 Zig 编写的高性能图片处理服务。

代码开源主要为了方便备份和审查，如果你刚好需要一套基于 Go 的身份认证方案或者想看一个 Zig + Go 协作的实践，可以参考看看。不过这不是什么通用框架，很多设计都是为了满足我们自己的需求。

## 技术栈

| 层面 | 技术 |
|------|------|
| 后端语言 | Go 1.26.5 |
| Web 框架 | Gin |
| 数据库 | PostgreSQL（pgx 驱动，连接池管理） |
| 图片处理 | Zig 0.16.0（调用 libwebp + stb_image） |
| 前端框架 | Vue 3（Composition API + TypeScript） |
| 前端构建 | Vite |
| 头像存储 | 本地文件系统（./data/avatars） |
| 缓存 | hashicorp/golang-lru/v2（分片 LRU） |
| 日志 | go.uber.org/zap |

## 功能概览

### 用户认证

- 邮箱注册，支持邮箱白名单限制注册域名（管理员可在后台配置）
- 邮箱 + 密码登录（也支持用户名登录）
- "发送验证邮件 -> 点击链接 -> 输入验证码 -> 完成"的标准验证流程
- 密码重置、已登录状态下修改密码
- 账户注销（需邮件验证码确认）
- 会话基于 JWT Access Token + Refresh Token 双令牌：Access Token 使用 ES256 / ECDSA P-256（默认 1 小时），Refresh Token 以 SHA-256 哈希存库（默认 30 天），支持轮转刷新，检测到重放时撤销整个 token 家族；通过 HttpOnly Secure SameSite Cookie 存储，同时支持 Authorization Header
- 用户数据导出（打包为 JSON，生成一次性下载令牌，5 分钟有效、单次使用，24 小时内限导出 1 次）

### 安全机制

- **分片限流器**：基于 IP 的令牌桶限流，16 个分片降低锁竞争，LRU 淘汰策略防止内存增长。覆盖登录（burst 5，每 12 秒补充 1 个）、注册（burst 3，每 20 秒补充 1 个）、密码重置（burst 3，每 20 秒补充 1 个）、验证码校验（burst 5，每 10 秒补充 1 个）、OAuth Token 端点（burst 10，每 2 秒补充 1 个）、数据导出（同一用户 24 小时限 1 次）
- **邮件限流器**：同一邮箱 60 秒内只能发送一封邮件，16 分片 LRU
- **封禁系统**：支持临时封禁和永久封禁，BanCheckMiddleware 拦截所有需要登录的接口
- **CSRF 防护**：Double Submit Cookie 模式，状态变更请求需提供 X-CSRF-Token 头或表单字段，使用恒定时间比较防止时序攻击
- **CSP（Content Security Policy）**：所有 HTML 页面注入随机 nonce，限制脚本、样式、字体、图片、连接等来源
- **安全响应头**：X-Content-Type-Options、Referrer-Policy、Permissions-Policy
- **请求体大小限制**：全局 1MB，API 路由 64KB，上传路由 5MB
- **路径遍历防护**：静态文件服务中对所有路径做规范化检查

### OAuth 2.0

**作为 Provider（允许第三方接入）：**

- 支持标准 Authorization Code 流程，强制 PKCE（code_challenge_method 支持 S256 与 plain）
- client_secret 使用 Argon2id 哈希存储
- Access Token / Refresh Token 使用 SHA-256 哈希存储，只返回明文一次
- redirect_uri 精确匹配，不支持通配符
- 授权码单次使用，有效期 10 分钟
- Access Token 有效期 1 小时，Refresh Token 有效期 30 天
- 用户可在 Dashboard 查看和撤销已授权的第三方应用
- 管理员可在后台管理 OAuth 客户端（创建、编辑、启用/禁用、重新生成密钥、删除）
- 禁用或删除客户端时自动撤销所有关联 Token

**作为 Client（Microsoft / Google 登录）：**

- 支持 Microsoft 账号登录、绑定、解绑
- 已有账号绑定 Microsoft 或 Google 时，需邮件验证确认（待绑定数据存内存，限时确认）
- Microsoft 支持头像自动同步（登录/绑定时异步转存为 WebP 并更新头像，可关闭）
- Google 通过 Cloudflare Worker 代理调用 API（解决国内无法直连），id_token 由 Worker 现场验签后以 Ed25519 背书
- 均使用 PKCE 流程保护

### 管理后台

基于角色的权限控制，三级角色：

- **普通用户（role 0）**：仅可访问前台功能
- **管理员（role 1）**：可查看统计面板、用户列表、封禁/解封用户
- **超级管理员（role 2）**：拥有全部权限，包括修改用户角色、删除用户、查看管理日志、管理 OAuth 客户端、管理邮箱白名单

管理功能包括：

- 用户管理：分页列表、搜索（用户名/邮箱模糊匹配）、查看详情、封禁/解封
- OAuth 客户端管理：CRUD、重新生成密钥、启用/禁用
- 邮箱白名单管理：配置允许注册的邮箱域名及对应注册链接
- 操作日志：所有管理操作均记录审计日志（admin_id、action、target_uid、details JSONB）
- 数据面板：总用户数、今日新增、管理员数、封禁数
- 数据导出/导入：数据导出打包为加密 JSON（OTAC 一次性授权码下载），导入支持合并 / 覆盖两种策略，可撤销 OTAC

### 验证码

通过 `CAPTCHA_ENABLED` 开关控制（必填配置）：

- `true`：启用 Cloudflare Turnstile 人机验证，登录、注册发码、密码重置发码、修改密码、修改用户名、注销账户等动作均需通过验证，前端展示验证组件
- `false`：上述动作全部跳过人机验证，前端不展示验证组件（登录等页面直接提交）

### 图片处理

独立的 Zig 程序，在 Go 启动时释放并通过 Unix Socket 通信：

- 接收任意格式图片（PNG、JPG、BMP 等，通过 stb_image 解码）
- 转码为 WebP（libwebp，质量 85，压缩方法 6）
- 头像上传流程：用户上传 -> Zig 转 WebP -> 保存到本地文件系统，由 `/avatars/` 路由直接提供（同时生成 Brotli 压缩副本）
- 二进制文件通过 `//go:embed` 嵌入 Go 编译产物，无需单独部署
- 支持自动重启（进程崩溃或 Socket 断开时）
- 每连接一个线程并发处理，信号量限制最多 2 张图同时处理（超出排队，防连接洪泛）；图片大小限制 10MB
- Zig 端包含完整的单元测试（编解码、协议格式）

### 国际化

支持 5 种语言：简体中文、繁体中文、英语、日语、韩语。

- 前端使用 vue-i18n，5 语言文案按语言组织，支持运行时切换与偏好持久化
- 邮件模板支持多语言，文案从 `email-texts.json` 读取
- 政策文档（隐私政策、服务条款、Cookie 政策）支持多语言多版本，前端按生效日期展示

### 前端

Vue 3（Composition API + TypeScript）+ Vite 构建的纯 SPA，路由由 `frontend/` 下的 vue-router 管理。组件化开发（`AppModal`/`ConfirmDialog`/`Toast`/`FormField` 等原子组件全局复用）。

构建流程：

```bash
cd frontend
npm ci
npm run build   # vue-tsc 类型检查 + vite build，产物输出到项目根 dist/
```

`dist/` 包含 `index.html`、`assets/`（前端编译产物）以及后端运行时依赖 `data/`（邮件模板）、`policy/`（政策文档与 manifest），随 server 二进制同目录部署。

页面路由（history 模式，服务端对未匹配路由做 SPA fallback 返回 `index.html`）：

- `/` -- 首页
- `/account/login`、`/account/register`、`/account/verify`、`/account/forgot`、`/account/dashboard`、`/account/link`、`/account/oauth`
- `/policy` -- 政策中心
- `/admin` -- 管理后台（需管理员权限）

旧版路由（如 `/login`、`/register` 等）通过 301 重定向到新版路径。

### 版本信息

- `GET /api/version`：返回编译时注入的 Git commit

### 后台任务

服务启动时自动拉起以下后台任务：

- Token 清理：每 5 分钟清理过期的 Token、验证码、OAuth 授权码/Token
- OAuth State 清理：每 5 分钟清理过期的 OAuth state 和待绑定数据
- 用户日志清理：每 24 小时清理超过 6 个月的日志（首次启动立即执行）
- 邮件 SMTP 连接保活：每 30 秒检查空闲连接，超过 5 分钟未使用则关闭

## 目录结构

```
.
├── cmd/
│   └── server/            # 后端服务入口（main.go、routes.go、tasks.go）
├── frontend/              # 前端（Vue 3 + Vite SPA）
│   ├── src/               # 源码（components 原子组件、pages、router、stores、i18n）
│   └── vite.config.ts     # Vite 配置（产物输出到项目根 dist/）
├── img-processor/         # Zig 图片处理服务
│   ├── src/main.zig       # 主逻辑（Socket 监听、图片编解码）
│   ├── src/stb_impl.c     # stb_image 实现
│   ├── vendor/            # libwebp + stb_image 源码
│   └── build.zig          # Zig 构建脚本
├── internal/
│   ├── cache/             # 用户 LRU 缓存
│   ├── config/            # 配置加载（环境变量、验证）
│   ├── handlers/          # HTTP Handler（auth、user、admin、oauth、static）
│   ├── middleware/        # Gin 中间件（auth、admin、ban、compress、cors、ratelimit、security）
│   ├── models/            # 数据库模型（CRUD、Schema 定义、golang-migrate 版本化迁移）
│   ├── paths/             # 路由路径常量
│   ├── services/          # 业务服务（token、session、captcha、email、localstorage、imgprocessor、oauth）
│   ├── utils/             # 工具函数（加密、验证、日志、Cookie、响应格式）
│   └── version/           # 版本信息（ldflags 注入 Git commit）
├── data/                  # 后端数据文件（邮件模板、文案）→ 构建时复制到 dist/data
├── shared/                # 政策文档源（i18n/policy Markdown + manifest）→ 构建时复制到 dist/policy
├── docs/                  # 文档
├── dist/                  # 构建产物（server 二进制同目录部署：index.html/assets/data/policy）
├── build.ps1              # 一键构建脚本（Zig 交叉编译 + 后端 + 前端）
├── google-oauth-proxy.js  # Google OAuth 代理 Worker（Cloudflare，Ed25519 背书）
└── go.mod / go.sum        # Go 依赖
```

## 快速开始

### 环境要求

- Go 1.26.5
- Zig 0.16.0（如果不需要图片处理可以不安装，但头像上传功能将不可用）
- PostgreSQL 14+

### 配置环境变量

项目通过环境变量配置，支持当前目录下的 `.env` 文件（服务端无法读取其他路径，需自行放置或直接注入系统环境变量）。以下是主要配置项：

**必需配置：**

```bash
DATABASE_URL="postgres://user:password@localhost:5432/dbname"  # PostgreSQL 连接字符串
JWT_PRIVATE_KEY="-----BEGIN EC PRIVATE KEY-----..."              # ECDSA P-256 PEM 私钥（用于 JWT ES256 签名）
EMAIL_WHITELIST_DOMAINS="example.com"                          # 允许注册的邮箱域名白名单（逗号分隔，必需）

# SMTP 邮件（必需，未配置时服务拒绝启动；网易 163 邮箱默认使用 SSL 465 端口 + LOGIN 认证）
SMTP_HOST="smtp.163.com"
SMTP_PORT=465
SMTP_USER="your-email@163.com"
SMTP_PASSWORD="your-smtp-password"
SMTP_FROM="your-email@163.com"

# 人机验证开关（必需，仅接受 true/false）：false 时登录/注册/重置密码/删除账户等
# 全部跳过人机验证且前端不展示验证组件
CAPTCHA_ENABLED=true
# Turnstile 密钥（CAPTCHA_ENABLED=true 时必需；false 时可省略）
TURNSTILE_SITE_KEY="your-site-key"
TURNSTILE_SECRET_KEY="your-secret-key"
```

**建议配置：**

```bash
PORT=3000                                                      # 服务端口（默认 3000）
BASE_URL="https://your-domain.com"                             # 基础 URL（用于重定向等）
CORS_ALLOW_ORIGINS="https://your-domain.com"                   # 允许的跨域来源

# 日志（应用日志默认输出 JSON 结构化日志到 stderr，字段：time/level/msg/category/caller + 业务字段）
LOG_ENCODING="json"                                            # 日志编码（json，开发时可设为 console）
LOG_LEVEL="info"                                               # 日志级别（debug/info/warn/error）

# Microsoft OAuth 登录（回调地址由 BASE_URL 派生，无需单独配置）
MICROSOFT_CLIENT_ID="your-client-id"
MICROSOFT_CLIENT_SECRET="your-client-secret"

# Google OAuth 登录
GOOGLE_CLIENT_ID="your-client-id"
GOOGLE_CLIENT_SECRET="your-client-secret"
GOOGLE_PROXY_URL="https://your-proxy.workers.dev"

# 代理的 Cloudflare Access Service Token（必需，配合 Access 应用限制代理仅本服务器可访问）
GOOGLE_PROXY_ACCESS_CLIENT_ID="your-access-client-id"
GOOGLE_PROXY_ACCESS_CLIENT_SECRET="your-access-client-secret"

# Google id_token 验签（代理 Worker 签名背书的 Ed25519 验签公钥，PEM 全文，与 JWT_PRIVATE_KEY 配置方式一致）
WORKER_SIGNING_PUBLIC_KEY="-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----"

# 头像存储与 CDN（可选）
AVATAR_DIR="./data/avatars"        # 本地头像存储目录（默认 ./data/avatars）
CDN_URL="https://your-cdn-domain"  # CDN 域名（用于 CSP img-src 白名单，可选）

# JWT / 会话配置（可选）
JWT_ISSUER="your-issuer"
JWT_AUDIENCE="your-audience"
ACCESS_TOKEN_EXPIRY="1h"     # Access Token 有效期，默认 1 小时（1 分钟 ~ 24 小时）
REFRESH_TOKEN_EXPIRY="720h"  # Refresh Token 有效期，默认 30 天（1 ~ 90 天）

# 管理后台数据导出加密盐（可选）
DATA_EXPORT_SALT="your-export-salt"

# 数据库连接池（可选）
DB_MAX_CONNS=10              # 最大连接数

# 默认头像（可选）
DEFAULT_AVATAR_URL="https://cdn.example.com/default-avatar.svg"
```

未配置 SMTP 或未设置 CAPTCHA_ENABLED 时服务会拒绝启动（注册/重置/注销验证均依赖邮件；验证码开关必须显式声明）；CAPTCHA_ENABLED=false 时跳过全部人机验证，TURNSTILE 密钥可省略。

### 编译步骤

**1. 编译前端**

```bash
cd frontend
npm ci
npm run build   # vue-tsc 类型检查 + vite build
```

构建产物输出到项目根 `dist/` 目录，含 `index.html`、`assets/`（前端编译产物）以及后端运行时依赖 `data/`、`policy/`。开发模式可运行 `npm run dev`（Vite dev server，代理 `/api`、`/oauth` 到 Go 后端）。

部署时需要把构建产物复制到 server 二进制同目录下的 `dist/`。

**2. 编译图片处理服务**

```bash
cd img-processor
zig build -Doptimize=ReleaseFast
# 产物在 zig-out/bin/img-processor
```

产物需放置到 `internal/services/img-processor-bin`，后端通过 `//go:embed` 将其嵌入编译产物。`build.ps1 -Backend` 和 CI 会在编译后端前自动完成本步骤（默认交叉编译 `x86_64-linux-gnu`，可用 `IMG_PROCESSOR_TARGET` 覆盖）。

你也可以直接运行测试：

```bash
zig build test
```

**3. 编译 / 运行后端**

```bash
# 直接运行（需要已执行前端构建，dist/ 目录存在）
go run ./cmd/server/

# 编译（可注入版本信息）
go build -trimpath -ldflags="-s -w -X auth-system/internal/version.ServerCommit=$(git rev-parse --short HEAD)" -o server ./cmd/server/

# 运行编译后的二进制（dist/ 需与二进制同目录）
./server
```

服务启动时通过 golang-migrate 执行数据库版本化迁移：首次启动会应用动态生成的版本 1（`CREATE TABLE IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS`，幂等）。已应用过迁移的数据库不会重复执行。

### 静态文件服务

服务端通过 `PreCompressedStatic` 中间件服务 `dist/` 目录下 `assets/` 中的静态资源。对于支持 Brotli 的浏览器，直接返回预压缩的 `.br` 文件（零运行时开销），`Content-Encoding: br`；不支持的浏览器自动回退到原始文件。

页面为纯 SPA：`/`、`/account/*`、`/admin`、`/policy` 等路由统一返回 `index.html`，由 vue-router 接管；未命中的 history 深层路径同样 fallback 到 `index.html`。所有页面响应均注入 CSP nonce。

## 注意事项

1. **图片处理依赖 Unix Socket**：socket 位于 Go 启动时创建的私有临时目录（权限 0700，仅运行用户可访问），Zig 端 socket 文件权限为 0600——其他本地用户无法连接。仅支持 Linux 环境部署。

2. **内存存储限制**：OAuth state 和待绑定数据存储在内存 map 中，带容量上限和 FIFO 淘汰。服务重启会丢失所有进行中的 OAuth 流程。如果是多实例部署，需要改用 Redis 等共享存储。目前适用于单实例场景。

3. **安全性**：项目中包含限流、CSRF 防护、CSP、封禁等安全机制，但作为个人项目未经过专业安全审计。在生产环境使用请自行评估风险。

4. **前端框架**：前端使用 Vue 3 + Vite 构建的纯 SPA，组件化开发。登录态通过 Cookie 中 JWT 维护，vue-router 守卫控制页面访问，服务端对 `/admin` 等敏感路径仍做鉴权与伪装（未授权返回 404）。

5. **i18n 配置**：前端文案源存储在 `frontend/src/i18n/sources/`，由 `scripts/gen-locales.mjs` 生成 `frontend/src/i18n/locales/`（不入库；`npm run dev/build/typecheck` 前自动生成）。政策 Markdown 位于 `frontend/policy/`，构建时复制进 `dist/policy/`。改动文案后需重新执行 `npm run build`。

6. **数据库迁移**：由 golang-migrate 管理，迁移 SQL 由 `getTableSchemas()` / `getIndexDefinitions()` 动态生成并作为版本 1 应用，仅执行一次。**对已有表新增列不会自动应用**（`CREATE TABLE IF NOT EXISTS` 对已存在的表是空操作）——需要手动执行 `ALTER TABLE ... ADD COLUMN`，或在迁移中追加新版本。删除列、修改类型、约束变更同样需要手动 SQL。

## License

MIT
