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
`NEBULA_JWT__PUBLIC_KEY_PATH`、`NEBULA_JWT__ACCESS_TTL_SECS`、`NEBULA_JWT__REFRESH_TTL_SECS`。

## 单实例声明（AGENTS.md §9.4）

本服务**只支持单实例部署**（数据库为嵌入式 SQLite，ADR-0003）。进程内状态清单：

| 状态 | 位置 | 重启后果 |
|---|---|---|
| 登录/注册限流桶（按对端 IP 的 GCRA） | `tower_governor` 内存 | 限额窗口重置，攻击者最多获得一个突发额度 |
| 登录时序对齐用的哑 Argon2 哈希 | 进程静态量 | 无影响（常量） |

其余全部状态（用户、口令哈希、refresh 家族、审计等）均在 SQLite 中，重启不丢。
横向扩展需求出现时须重开 ADR-0003 与本声明。
