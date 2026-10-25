# 0003 · 数据库改用 SQLite

**Status:** accepted (2026-10-25)

## Context

v3 用独立 PostgreSQL 服务（pgx 驱动）。v4 改用 SQLite：目标是去掉独立数据库进程与网络往返，部署面收敛为「单二进制 + 单数据文件」。Rust 侧候选：

- `sqlx`（SQLite 后端；驱动底层是 [libsqlite3-sys](https://github.com/rusqlite/rusqlite) bundled C，与 rusqlite 同一引擎）
- `rusqlite`（同一 C 引擎的同步直连包装，无 `query!` 编译期校验）
- [tursodatabase/turso](https://github.com/tursodatabase/turso)（原 limbo，纯 Rust 重写，异步 IO、SQLite 格式兼容；官方称多家生产使用，第三方仍按 beta 对待，API 不兼容 sqlx/rusqlite）

Rust 链接 C 无 CGo 式摩擦，bundled 编译即得单二进制，故「纯 Rust」不构成必要条件。

## Decision

1. 驱动选 `sqlx` + SQLite（bundled C）：保住 §1 的 sqlx 三项强制（`query!` 编译期校验、offline、migrate），门禁体系不动。rusqlite 丢 `query!`；Turso 成熟度有分歧且不兼容，不为单人认证服务引入。
2. 连接配置（[SQLite 文档](https://www.sqlite.org/wal.html)）：`journal_mode=WAL`（单写多读）、`busy_timeout`、`foreign_keys=ON`（SQLite 默认不启用外键）。
3. 方言映射：时间戳存 RFC3339 TEXT，统一经 `context.rs` 时钟抽象；`JSONB` → TEXT + JSON1 函数；uuid v7 主键存 TEXT。
4. 测试：`storage` 实现每用例建临时文件库，不用 `:memory:`——sqlx 连接池下每个连接各得一份内存库；`testcontainers` 退出栈。
5. 运维：热备份 `VACUUM INTO`；单实例假设与 §9.4 一致，README 维持单实例声明。

## Consequences

**得：** 部署从「应用 + Postgres」收敛为单二进制加数据文件；storage 测试不再拉容器，更快且无 Docker 依赖；备份即拷文件。

**失：** 单写者模型，并发写受 WAL 串行化约束（认证服务写负载可承受）；横向扩展即重开本决策，届时向 Postgres 迁移需付方言差异成本；失去数据库级多连接并发原语（advisory lock 等）。
