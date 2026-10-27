# 0010 · access token 版本撤销与 TOTP 密钥加密

**Status:** accepted (2026-10-27)

## Context

v4 的 access token 是无状态 JWT：登出、改密、密码重置、关闭 TOTP 只撤销 refresh 家族，已签发的 access token 仍可用到过期（默认 1 小时）。封禁通过 `FirstPartyUser` 实时查库拦截，但解封后旧 access token 会重新变为可用；这不符合「撤销后不可复活」的会话语义。

TOTP secret 以明文 `users.totp_secret` 入库。数据库泄露即可为所有用户生成二次验证码，绕过密码与 TOTP 两道因素。v4 尚未上线，没有需要保留的既有 TOTP 密钥。

## Decision

1. **用户版本号。** `users` 增加 `token_version`；第一方 access JWT 增加 `ver` claim，`AccessClaims::user_session` 必须显式传入版本。`FirstPartyUser` 比对 claim 与库中当前值，不一致按 401 处理。
2. **撤销即递增。** `revoke_all_sessions` 在同一个事务内递增 `token_version` 并撤销全部 refresh 家族；登出、改密、密码重置、关闭 TOTP、封禁共用该路径。登录/二段/刷新签发时读取当前版本，刷新不再让旧代 token 复活。
3. **TOTP 密钥加密。** 新增 `SecretCipher` 端口与 AES-256-GCM 实现；密钥由 `NEBULA_CRYPTO__TOTP_SECRET_KEY`（64 个 hex 字符）提供，启动时缺失或非法直接失败。密文格式为 `v1:<nonce hex>:<ciphertext+tag hex>`，`users.totp_secret_enc` 存密文。
4. **不迁移旧明文。** 迁移 0012 增加 `totp_secret_enc`、清空 TOTP 状态与恢复码/挑战、删除 `totp_secret` 列；用户需要重新启用 TOTP。v4 未上线，这一一次性重置优于保留无法验证来源的明文。
5. **密钥轮换不在本 ADR 实现。** 格式版本前缀为将来轮换留位；轮换需要离线重加密工具与双密钥窗口，另行设计。

## Consequences

**得：** 会话撤销对所有已签发 access token 立即生效，不再依赖 1 小时 TTL；封禁后解封也不会让旧 token 复活；数据库泄露不再直接暴露可用的 TOTP 密钥；加密与解密都集中在端口内，handler 不接触算法细节。

**失：** 每次第一方认证请求仍要多一次 SQLite 读取（封禁/版本共用一次查询）；`token_version` 递增会使所有设备上的 access token 同时失效，客户端必须依赖 refresh 或重新登录；TOTP 加密密钥丢失会导致所有 TOTP 密钥不可恢复，用户只能重置 TOTP；密钥轮换需要额外运维工具。

## Sources

- [RFC 7519 · JWT](https://www.rfc-editor.org/rfc/rfc7519)
- [RFC 6238 · TOTP](https://www.rfc-editor.org/rfc/rfc6238)
- [NIST SP 800-38D · GCM](https://csrc.nist.gov/pubs/sp/800/38/d/final)
- [OWASP Secrets Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
