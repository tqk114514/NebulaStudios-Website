# Nebula Studios Website

Nebula Studios 网站的前后端源码，包含用户系统、OAuth 认证、管理后台以及一个用 Zig 编写的高性能图片处理服务。

代码开源主要为了方便备份和审查，如果你刚好需要一套基于 Go 的身份认证方案或者想看一个 Zig + Go 协作的实践，可以参考看看。不过这不是什么通用框架，很多设计都是为了满足我们自己的需求。

## 技术栈

| 层面 | 技术 |
|------|------|
| 后端语言 | Go 1.26.2 |
| Web 框架 | Gin |
| 数据库 | PostgreSQL（pgx 驱动，连接池管理） |
| 图片处理 | Zig 0.16.0（调用 libwebp + stb_image） |
| 前端语言 | TypeScript（esbuild 打包） |
| 对象存储 | Cloudflare R2（AWS S3 兼容 API） |
| 缓存 | hashicorp/golang-lru/v2（分片 LRU） |
| 日志 | go.uber.org/zap |

## 功能概览

### 用户认证

- 邮箱注册，支持邮箱白名单限制注册域名（管理员可在后台配置）
- 邮箱 + 密码登录（也支持用户名登录）
- "发送验证邮件 -> 点击链接 -> 输入验证码 -> 完成"的标准验证流程
- 密码重置、已登录状态下修改密码
- 账户注销（需邮件验证码确认）
- 会话基于 JWT（HS256），默认有效期 60 天，通过 HttpOnly Secure SameSite Cookie 存储，同时支持 Authorization Header
- 用户数据导出（打包为 JSON，需邮件验证码确认，24 小时内限导出 1 次）

### 安全机制

- **分片限流器**：基于 IP 的令牌桶限流，16 个分片降低锁竞争，LRU 淘汰策略防止内存增长。覆盖登录（5次/分钟）、注册（3次/分钟）、密码重置（3次/分钟）、OAuth Token 端点（10次/20秒）、验证码失效（2次/60秒）
- **邮件限流器**：同一邮箱 60 秒内只能发送一封邮件，16 分片 LRU
- **封禁系统**：支持临时封禁和永久封禁，BanCheckMiddleware 拦截所有需要登录的接口
- **CSRF 防护**：Double Submit Cookie 模式，状态变更请求需提供 X-CSRF-Token 头或表单字段，使用恒定时间比较防止时序攻击
- **CSP（Content Security Policy）**：所有 HTML 页面注入随机 nonce，限制脚本、样式、字体、图片、连接等来源
- **安全响应头**：X-Content-Type-Options、Referrer-Policy、Permissions-Policy
- **请求体大小限制**：全局 1MB，API 路由 64KB，上传路由 5MB
- **路径遍历防护**：静态文件服务中对所有路径做规范化检查

### OAuth 2.0

**作为 Provider（允许第三方接入）：**

- 支持标准 Authorization Code 流程，强制 PKCE（S256）
- client_secret 使用 Argon2id 哈希存储
- Access Token / Refresh Token 使用 SHA-256 哈希存储，只返回明文一次
- redirect_uri 精确匹配，不支持通配符
- 授权码单次使用，有效期 10 分钟
- Access Token 有效期 1 小时，Refresh Token 有效期 30 天
- 用户可在 Dashboard 查看和撤销已授权的第三方应用
- 管理员可在后台管理 OAuth 客户端（创建、编辑、启用/禁用、重新生成密钥、删除）
- 禁用或删除客户端时自动撤销所有关联 Token

**作为 Client（Microsoft 登录）：**

- 支持 Microsoft 账号登录和绑定
- 已有账号绑定 Microsoft 时需邮件验证确认
- 支持解绑 Microsoft 账号
- PKCE 流程保护

### 扫码登录

- PC 端生成加密二维码（AES-256-GCM），有效期 3 分钟
- 加密密钥通过环境变量 `QR_ENCRYPTION_KEY` 和 `QR_KEY_DERIVATION_SALT` 派生
- 移动端扫描后在手机端确认登录，PC 端通过 WebSocket 实时收到状态推送
- WebSocket 服务采用 8 分片设计，最大 1000 连接，Ping/Pong 心跳保活，5 分钟超时自动清理
- 支持设备信息展示（浏览器、操作系统）

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

### 验证码

支持同时配置多种验证器，前端按优先级自动选择：

- Cloudflare Turnstile
- hCaptcha

未配置任何验证器时服务仍可运行（仅记录警告），但建议至少配置一种。

### 图片处理

独立的 Zig 程序，在 Go 启动时释放并通过 Unix Socket 通信：

- 接收任意格式图片（PNG、JPG、BMP 等，通过 stb_image 解码）
- 转码为 WebP（libwebp，质量 85，压缩方法 6）
- 头像上传流程：用户上传 -> Zig 转 WebP -> 上传到 Cloudflare R2
- 二进制文件通过 `//go:embed` 嵌入 Go 编译产物，无需单独部署
- 支持自动重启（进程崩溃或 Socket 断开时）
- 最大并发数 2，图片大小限制 10MB
- Zig 端包含完整的单元测试（编解码、协议格式）

### 国际化

支持 5 种语言：简体中文、繁体中文、英语、日语、韩语。

- 前端翻译在构建时合并为 `translations.js`，按语言懒加载
- 邮件模板支持多语言，文案从 `email-texts.json` 读取
- 政策文档（隐私政策、服务条款、Cookie 政策）支持多语言多版本，前端按日期选择

### 前端

纯 TypeScript + HTML + CSS，没有前端框架。使用 esbuild 打包 TypeScript，构建系统本身也是 Go 写的（`cmd/build/`）。

构建流程：

1. 清理并创建 `dist/` 目录结构
2. 复制后端数据文件（邮件模板、文案 JSON）
3. 合并 i18n 为 `translations.js`
4. 构建 cookie-consent.js
5. esbuild 打包各模块 TypeScript 入口（home、account、admin、policy 完全独立）
6. 压缩 CSS（去空白和注释）
7. 压缩 HTML（去空白和注释）
8. 复制政策 Markdown 文件
9. 保存资源清单（asset-manifest.json）
10. 对整个 `dist/` 目录做 Brotli 预压缩（生产模式）

页面路由按模块分组：

- `/` -- 首页
- `/account/login`、`/account/register`、`/account/verify`、`/account/forgot`、`/account/dashboard`、`/account/link`、`/account/oauth`
- `/policy` -- 政策中心 SPA（hash 路由切换隐私政策/服务条款/Cookie 政策）
- `/admin` -- 管理后台 SPA（需管理员权限）

旧版路由（如 `/login`、`/register` 等）通过 301 重定向到新版路径。

### 健康检查与版本

- `GET /health`：返回服务状态（ok/degraded）、数据库连接池统计、缓存命中率、WebSocket 连接数
- `GET /api/version`：返回编译时注入的 Git commit 和 GitHub 仓库最新 commit（缓存 10 分钟）

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
│   ├── server/            # 后端服务入口（main.go、routes.go、tasks.go）
│   └── build/             # 前端构建工具（JS/CSS/HTML 压缩，Brotli 预压缩）
├── img-processor/         # Zig 图片处理服务
│   ├── src/main.zig       # 主逻辑（Socket 监听、图片编解码）
│   ├── src/stb_impl.c     # stb_image 实现
│   ├── vendor/            # libwebp + stb_image 源码
│   └── build.zig          # Zig 构建脚本
├── internal/
│   ├── cache/             # 用户 LRU 缓存
│   ├── config/            # 配置加载（环境变量、验证）
│   ├── handlers/          # HTTP Handler（auth、user、admin、oauth、qrlogin、static）
│   ├── middleware/        # Gin 中间件（auth、admin、ban、compress、cors、ratelimit、security）
│   ├── models/            # 数据库模型（CRUD、自动迁移、Schema 定义）
│   ├── paths/             # 路由路径常量
│   ├── services/          # 业务服务（token、session、captcha、email、websocket、r2、imgprocessor、oauth）
│   ├── utils/             # 工具函数（加密、验证、日志、Cookie、响应格式）
│   └── version/           # 版本信息（ldflags 注入 + GitHub API）
├── modules/               # 前端模块（home、account、admin、policy）
│   ├── */assets/          # CSS + TypeScript
│   └── */pages/           # HTML 页面
├── shared/                # 前端共享资源
│   ├── components/        # 共享 HTML 组件（header）
│   ├── css/               # 全局样式
│   ├── i18n/              # 翻译 JSON + 政策 Markdown（按语言和日期组织）
│   └── js/                # 共享 TypeScript（类型、工具函数、翻译加载器）
├── data/                  # 后端数据文件（邮件模板、文案）
├── docs/                  # 文档
├── dist/                  # 构建输出（前端编译后 + Brotli 预压缩 .br 文件）
├── go.mod / go.sum        # Go 依赖
└── tsconfig.json          # TypeScript 配置
```

## 快速开始

### 环境要求

- Go 1.26.2
- Zig 0.16.0（如果不需要图片处理可以不安装，但头像上传功能将不可用）
- PostgreSQL 14+

### 配置环境变量

项目通过环境变量配置，支持 `.env` 文件（优先读取 `/var/www/.env`，其次当前目录 `.env`）。以下是主要配置项：

**必需配置：**

```bash
DATABASE_URL="postgres://user:password@localhost:5432/dbname"  # PostgreSQL 连接字符串
JWT_SECRET="your-jwt-secret-at-least-32-chars-long"            # JWT 签名密钥（最少 32 字符）
QR_KEY_DERIVATION_SALT="your-salt"                             # 扫码登录密钥派生 Salt
```

**建议配置：**

```bash
PORT=3000                                                      # 服务端口（默认 3000）
BASE_URL="https://your-domain.com"                             # 基础 URL（用于重定向等）
CORS_ALLOW_ORIGINS="https://your-domain.com"                   # 允许的跨域来源

# SMTP 邮件（网易 163 邮箱默认使用 SSL 465 端口 + LOGIN 认证）
SMTP_HOST="smtp.163.com"
SMTP_PORT=465
SMTP_USER="your-email@163.com"
SMTP_PASSWORD="your-smtp-password"
SMTP_FROM="your-email@163.com"

# 验证码（至少配置一种，建议 Turnstile）
TURNSTILE_SITE_KEY="your-site-key"
TURNSTILE_SECRET_KEY="your-secret-key"
# 或
HCAPTCHA_SITE_KEY="your-site-key"
HCAPTCHA_SECRET_KEY="your-secret-key"

# Microsoft OAuth 登录
MICROSOFT_CLIENT_ID="your-client-id"
MICROSOFT_CLIENT_SECRET="your-client-secret"
MICROSOFT_REDIRECT_URI="https://your-domain.com/api/auth/microsoft/callback"

# 扫码登录加密
QR_ENCRYPTION_KEY="your-encryption-key"

# Cloudflare R2 对象存储（头像上传）
R2_URL="https://your-r2-url"
R2_ENDPOINT="https://your-account-id.r2.cloudflarestorage.com"
R2_ACCESS_KEY="your-access-key"
R2_SECRET_KEY="your-secret-key"
R2_BUCKET="your-bucket"

# JWT 签发配置（可选）
JWT_ISSUER="your-issuer"
JWT_AUDIENCE="your-audience"
JWT_EXPIRES_IN="1440h"      # 过期时间，支持 Go duration 格式，默认 60 天

# 数据库连接池（可选）
DB_MAX_CONNS=10              # 最大连接数

# 默认头像（可选）
DEFAULT_AVATAR_URL="https://cdn.example.com/default-avatar.svg"
```

未配置 SMTP 或验证码时服务仍会启动，但相关功能不可用，启动日志中会有相应警告。

### 编译步骤

**1. 编译前端**

```bash
go run ./cmd/build/
# 开发模式（不压缩，保留 sourcemap）：
go run ./cmd/build/ -dev
```

构建产物输出到 `dist/` 目录。生产模式下所有静态文件会额外生成 `.br`（Brotli）预压缩版本，服务端根据浏览器 `Accept-Encoding` 头决定返回压缩版还是原始文件。对于支持 Brotli 的现代浏览器，这会显著降低流量消耗，但某些老旧浏览器可能无法访问。

**2. 编译图片处理服务（可选）**

```bash
cd img-processor
zig build -Doptimize=ReleaseFast
# 产物在 zig-out/bin/img-processor
```

编译后的二进制需要在 GitHub Actions 或其他 CI 环境中放置到 `internal/services/img-processor-bin`，然后通过 `//go:embed` 嵌入 Go 服务。如果跳过这一步，头像上传功能将不可用，但其他功能不受影响。

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

# 运行编译后的二进制
./server
```

服务启动时会自动初始化数据库表结构（CREATE TABLE IF NOT EXISTS），并执行自动迁移（检测缺失的列并添加）。索引也会自动创建。

### 静态文件服务

服务端通过 `PreCompressedStatic` 中间件服务 `dist/` 目录下的静态文件。对于支持 Brotli 的浏览器，直接返回预压缩的 `.br` 文件（零运行时开销），`Content-Encoding: br`。对于不支持的浏览器，返回原始未压缩文件。

HTML 页面由 `serveHTML` 函数处理，支持 CSP nonce 注入（模板中的 `{{CSP_NONCE}}` 会在运行时替换为随机 nonce）。

## 注意事项

1. **图片处理依赖 Unix Socket**：默认路径 `/tmp/img-processor.sock`。目前仅支持 Linux 环境部署（路径硬编码），如需跨平台需修改 `img-processor/src/main.zig` 中的 `SOCKET_PATH` 和 Go 端 `imgprocessor.go` 中的 `SocketPath`。

2. **内存存储限制**：OAuth state 和待绑定数据存储在内存 map 中，带容量上限和 FIFO 淘汰。服务重启会丢失所有进行中的 OAuth 流程。如果是多实例部署，需要改用 Redis 等共享存储。目前适用于单实例场景。

3. **安全性**：项目中包含限流、CSRF 防护、CSP、封禁等安全机制，但作为个人项目未经过专业安全审计。在生产环境使用请自行评估风险。

4. **前端框架**：前端未使用 Vue/React 等框架，是纯 TypeScript + HTML + CSS 的方案。每个模块的 JS 完全独立打包，页面之间不共享运行时状态（通过 Cookie 中的 JWT 维护登录态）。

5. **i18n 构建**：前端翻译在构建时被打包进 `translations.js`（含所有语言），运行时根据用户选择的语言动态切换。这意味着翻译内容变更后需要重新执行前端构建。

6. **数据库迁移**：自动迁移只会添加缺失的列（ALTER TABLE ADD COLUMN），不会删除列、修改类型或处理约束变更。如果需要对已有列做破坏性变更，需要手动执行 SQL。

## License

MIT
