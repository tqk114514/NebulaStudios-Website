# Nebula v4

Nebula Studios 的身份认证服务：Rust（axum + sqlx/SQLite）后端与 Nuxt 4 前端。
工程约束见 `AGENTS.md`，决策记录见 `docs/adr/`。

## 运行

```bash
# 首次启动自动创建 nebula.db 并初始化 schema；没有迁移步骤
openssl ecparam -name prime256v1 -genkey -noout -out ec.pem      # ES256 密钥
openssl pkcs8 -topk8 -nocrypt -in ec.pem -out jwt-private.pem
openssl pkey -in jwt-private.pem -pubout -out jwt-public.pem
openssl rand -hex 32     # NEBULA_CRYPTO__TOTP_SECRET_KEY（TOTP 密钥加密）
openssl rand -hex 32     # NEBULA_CRYPTO__CODE_PEPPER（验证码 HMAC pepper）
cargo run --release
```

数据库没有真实业务数据；schema 由启动时幂等创建，变更 schema 直接删除数据库文件后重启。

环境变量（`NEBULA_` 前缀，双下划线分层级）：`NEBULA_BIND`（默认 `127.0.0.1:8080`）、
`NEBULA_DATABASE__PATH`（默认 `nebula.db`）、`NEBULA_JWT__PRIVATE_KEY_PATH`、
`NEBULA_JWT__PUBLIC_KEY_PATH`、`NEBULA_JWT__ACCESS_TTL_SECS`、`NEBULA_JWT__REFRESH_TTL_SECS`、
`NEBULA_JOBS__CLEANUP_INTERVAL_SECS`（默认 300）、`NEBULA_CORS__ALLOWED_ORIGIN`
（默认空，同源部署；本地前后端分离时设为前端来源，如 `http://localhost:3000`）、
`NEBULA_ADMIN__EMAILS`（逗号分隔的管理员邮箱，默认空；由配置决定角色，ADR-0008）、
`NEBULA_CRYPTO__TOTP_SECRET_KEY`（必填，64 个十六进制字符；缺失或格式非法则启动失败）、
`NEBULA_CRYPTO__CODE_PEPPER`（必填，64 个十六进制字符；验证码/恢复码 HMAC pepper）、
`NEBULA_MAIL__SMTP_HOST`（空则日志模式）、`NEBULA_MAIL__SMTP_PORT`（默认 587）、
`NEBULA_MAIL__SMTP_TLS`（`starttls` 默认 / `implicit` 用于 465 / `none`）、
`NEBULA_OAUTH__CLIENTS_FILE`（OAuth Provider 客户端 TOML，空则无客户端）、
`NEBULA_AVATAR__DIR`（头像文件目录，默认 `avatars`）、
`NEBULA_POLICY__MANIFEST_FILE`（政策 manifest TOML，空则不启用政策中心）、
`NEBULA_REGISTRATION__ALLOWED_EMAIL_DOMAINS`（逗号分隔的注册邮箱域名，精确匹配；
空则不限制，只作用于新注册）、
`NEBULA_TURNSTILE__ENABLED`（默认 false）、`NEBULA_TURNSTILE__SITE_KEY`、
`NEBULA_TURNSTILE__SECRET_KEY`、`NEBULA_TURNSTILE__VERIFY_URL`（默认 Cloudflare
siteverify）、
`NEBULA_SOCIAL__*`（社交登录，ADR-0012）。

## OAuth Provider 客户端

`NEBULA_OAUTH__CLIENTS_FILE` 指向一个 TOML 文件；启动时按 `client_id` upsert，
`secret` 以 Argon2 哈希入库，明文不落库。

```toml
[[clients]]
client_id = "example"
name = "Example App"
redirect_uri = "https://app.example.com/callback"
secret = "change-me"
enabled = true
```

## 头像

`POST /api/user/avatar` 接收 multipart 字段 `avatar`：原始文件上限 5 MiB、单边不超过
4096 像素，按内容解码后居中裁剪缩放为 512×512 WebP，写入 `NEBULA_AVATAR__DIR`。
`GET /api/users/{user_id}/avatar?v=N` 公开读取，`v` 只作缓存键；资料端点在
`avatar_url` 返回该地址。注销账户时头像文件一并删除。

## 资料修改

- `PATCH /api/user/username`：`{username, current_password}`；用户名大小写敏感，
  3–15 个 ASCII 字母/数字/`_`/`-`，精确唯一。UID 与会话不变。
- `POST /api/user/email/change/request`：`{new_email, current_password}`，向新邮箱
  发六位验证码；新邮箱须过域名白名单且未被占用。
- `POST /api/user/email/change/confirm`：`{code}`，更新邮箱并置已验证，向旧邮箱发
  安全通知；现有会话保留。

修改均写审计。用户名 collation 已改为大小写敏感，旧数据库需删除重建。

## 数据导出

`POST /api/user/export` 生成 10 分钟有效、单次使用的下载链接；再次申请会作废此前
未消费的链接。`GET /api/user/export/{token}` 返回 JSON（资料、头像地址、社交绑定、
TOTP 状态与恢复码余量、政策同意记录、第三方授权、审计记录），响应
`Cache-Control: no-store`。令牌明文只在申请响应中出现一次，库中只存 SHA-256。

## 第三方授权

用户可在设置页查看并撤销已授权的 OAuth 客户端：

- `GET /api/user/oauth/grants`：按客户端聚合，scope 取并集，`authorized_at` 取最早授权时间。
- `DELETE /api/user/oauth/grants/{client_id}`：撤销该客户端的全部有效 refresh 家族；
  已签发的 access token 在剩余 TTL 内仍有效，到期自然失效。撤销写入审计。

## 政策中心

`NEBULA_POLICY__MANIFEST_FILE` 指向部署侧 manifest TOML；启动时校验并排序，修改后需
重启。每个政策版本必须同时提供 `zh-CN` 与 `en` 文档，`path` 为站点内绝对路径或
https 地址；文档内容不进仓库。

```toml
[[policies]]
slug = "privacy"
version = "2026-11-01"
effective_at = "2026-11-01T00:00:00Z"

[[policies.documents]]
locale = "zh-CN"
title = "隐私政策"
path = "/policies/privacy/2026-11-01/zh-CN"

[[policies.documents]]
locale = "en"
title = "Privacy Policy"
path = "/policies/privacy/2026-11-01/en"
```

- `GET /api/policies`：公开 manifest，`current` 由服务端时钟判定当前生效版本。
- `GET /api/user/consents`：已同意记录与当前待同意版本。
- `POST /api/user/consents`：提交 `{"policy":"privacy","version":"2026-11-01"}`；
  只接受当前生效版本，重复同意幂等，返回 204。

## 人机验证

默认关闭。启用后注册、重发验证码、重置发码必须携带 `turnstile_token`；`GET
/api/auth/turnstile` 返回 `{ enabled, site_key }` 供前端渲染。Turnstile 各模式均
不呈现图像/文字谜题：managed 最多显示复选框，non-interactive 无需交互，invisible
完全隐藏，因此不需要替代路径。校验服务不可用时相关端点返回 503。配置：

```bash
NEBULA_TURNSTILE__ENABLED=true
NEBULA_TURNSTILE__SITE_KEY=...
NEBULA_TURNSTILE__SECRET_KEY=...
```

登录、改密、注销与 TOTP 开关不挂 Turnstile：前者已有 Argon2 与限流，后者已要求
当前口令或邮件验证码。

## 邮件

验证码与安全通知按请求 `Accept-Language` 的**首个受支持语言**渲染：`zh-CN` 或 `en`；
无法匹配时回落简体中文。邮件为纯文本，模板在 `src/mailer/template.rs`。

## 形式化测试

纯逻辑用 `proptest` 做 property-based 测试；refresh 家族与 OAuth 授权码另有
TLA+ 模型（`docs/tla/`），由 `bash scripts/check-tla.sh` 运行 TLC。模型检查设计层不变量，
不替代 SQLite 集成测试。

## 运维端点与后台任务

- `GET /healthz`：存活探针。
- `GET /metrics`：Prometheus 文本格式；由 `main` 安装 recorder，测试装配返回 404。
- 清理任务每 `NEBULA_JOBS__CLEANUP_INTERVAL_SECS` 秒运行一次：删除过期 refresh token、
  过期/已消费验证码、TOTP 挑战、OAuth 授权码、已消费恢复码、过期或已消费导出令牌、
  过期邮箱变更请求与已到期临时封禁；已消费记录保留 1 天，无 token 的 refresh 家族
  保留 30 天。
- 审计日志不自动清理，留存策略待单独 ADR。

## 管理 API（ADR-0008）

管理员由 `NEBULA_ADMIN__EMAILS` 配置，角色不落库，修改后需重启；邮箱必须已验证，
避免未验证邮箱被抢注后取得管理权限。管理端点均要求第一方会话且通过配置化管理员判定：

- `GET /api/admin/stats`
- `GET /api/admin/users?limit=&offset=&query=`
- `POST /api/admin/users/{user_id}/ban`（`kind` 为 `temporary` 或 `permanent`）
- `DELETE /api/admin/users/{user_id}/ban`
- `GET /api/admin/audit?limit=&offset=`

封禁在 `FirstPartyUser` 认证接缝实时生效，并在同一事务内撤销该用户全部 refresh
家族、递增 access token 版本；封禁与解封写入审计。登出、改密、密码重置、关闭 TOTP
与封禁都会递增 `token_version`，已签发的 access token 立即失效（ADR-0010）。
TOTP 密钥以 AES-256-GCM 密文存储，密钥由 `NEBULA_CRYPTO__TOTP_SECRET_KEY` 提供。

## 单实例声明（AGENTS.md §9.4）

本服务**只支持单实例部署**（数据库为嵌入式 SQLite，ADR-0003）。进程内状态清单：

| 状态 | 位置 | 重启后果 |
|---|---|---|
| 登录/注册限流桶（按对端 IP 的 GCRA） | `tower_governor` 内存 | 限额窗口重置，攻击者最多获得一个突发额度 |
| 登录时序对齐用的哑 Argon2 哈希 | 进程静态量 | 无影响（常量） |
| Prometheus 指标（请求、限流、重放、清理计数） | `metrics-exporter-prometheus` 内存 | 指标窗口重置；不影响业务数据 |
| 配置化管理员邮箱集合 | 启动时解析的内存 `HashSet` | 需重启加载新配置 |

其余全部状态（用户、口令哈希、refresh 家族、审计等）均在 SQLite 中，重启不丢。
横向扩展需求出现时须重开 ADR-0003 与本声明。
