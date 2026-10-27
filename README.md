# Nebula v4

Nebula Studios 的身份认证服务：Rust（axum + sqlx/SQLite）后端与 Nuxt 4 前端。
工程约束见 `AGENTS.md`，决策记录见 `docs/adr/`。

## 运行

```bash
cargo sqlx database create    # 首次建库（DATABASE_URL=sqlite://nebula.db）
cargo sqlx migrate run        # 迁移是独立部署步骤，启动不执行 DDL
openssl ecparam -name prime256v1 -genkey -noout -out ec.pem      # ES256 密钥
openssl pkcs8 -topk8 -nocrypt -in ec.pem -out jwt-private.pem
openssl pkey -in jwt-private.pem -pubout -out jwt-public.pem
cargo run --release
```

环境变量（`NEBULA_` 前缀，双下划线分层级）：`NEBULA_BIND`（默认 `127.0.0.1:8080`）、
`NEBULA_DATABASE__PATH`（默认 `nebula.db`）、`NEBULA_JWT__PRIVATE_KEY_PATH`、
`NEBULA_JWT__PUBLIC_KEY_PATH`、`NEBULA_JWT__ACCESS_TTL_SECS`、`NEBULA_JWT__REFRESH_TTL_SECS`、
`NEBULA_JOBS__CLEANUP_INTERVAL_SECS`（默认 300）、`NEBULA_CORS__ALLOWED_ORIGIN`
（默认空，同源部署；本地前后端分离时设为前端来源，如 `http://localhost:3000`）、
`NEBULA_ADMIN__EMAILS`（逗号分隔的管理员邮箱，默认空；由配置决定角色，ADR-0008）。

## 运维端点与后台任务

- `GET /healthz`：存活探针。
- `GET /metrics`：Prometheus 文本格式；由 `main` 安装 recorder，测试装配返回 404。
- 清理任务每 `NEBULA_JOBS__CLEANUP_INTERVAL_SECS` 秒运行一次：删除过期 refresh token、
  过期/已消费验证码、TOTP 挑战、OAuth 授权码、已消费恢复码与已到期临时封禁；已消费
  记录保留 1 天，无 token 的 refresh 家族保留 30 天。
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
家族；封禁与解封写入审计。已签发的 access token 在封禁期间会被 403 `banned` 拦截。

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
