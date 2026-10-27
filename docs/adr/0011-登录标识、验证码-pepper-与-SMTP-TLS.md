# 0011 · 登录标识、验证码 pepper 与 SMTP TLS

**Status:** accepted (2026-10-27)

## Context

1. 登录查询是 `WHERE email = ?1 OR username = ?1`。用户名只校验长度，允许 `@`/`.`，因此一个用户名可以等于另一个用户的邮箱；查询可能命中两行，`fetch_optional` 返回哪一行不确定。
2. 六位邮箱验证码只有约 35 bit 熵，入库使用裸 SHA-256；数据库泄露后可离线穷举未过期码。
3. `AsyncSmtpTransport::relay` 是隐式 TLS（默认 465），但配置默认端口 587；在 587 上直接 TLS 握手会失败，与注释声称的 STARTTLS 不一致。

## Decision

1. **用户名与登录查询。** 用户名限定 3–15 位 ASCII 字母/数字/`_`/`-`，禁止 `@` 等邮箱字符；应用层 `validator` 校验，SQLite `BEFORE INSERT/UPDATE` 触发器兜底。`find_login_credentials` 的 `ORDER BY CASE WHEN email = ?1 THEN 0 ELSE 1 END` 让历史数据中 email 匹配优先，`LIMIT 1` 保证确定性。
2. **验证码与恢复码 pepper。** 新增 `CodeHasher` 端口与 HMAC-SHA256 实现，pepper 由 `NEBULA_CRYPTO__CODE_PEPPER`（64 hex）提供；`VerificationCode` 与 `RecoveryCode` 不再暴露裸哈希方法。迁移 0014/0015 分别清空旧 SHA-256 验证码与恢复码行，旧码自然失效。
3. **SMTP TLS 模式。** `NEBULA_MAIL__SMTP_TLS` 取 `starttls`（默认，587）、`implicit`（465）或 `none`；启动时校验枚举。`SmtpMailer` 按模式选择 `starttls_relay` / `relay` / `builder_dangerous`，不再让 587 走隐式 TLS。

## Consequences

**得：** 登录标识在 email/username 两列之间不再可构造歧义；验证码哈希在数据库泄露后不能脱离 pepper 穷举；SMTP 的 587/465 部署各自使用正确的 TLS 握手。

**失：** 用户名范围收窄为 ASCII，历史非 ASCII 用户名需要迁移或拒绝；验证码哈希格式变化使旧验证码失效，用户需重新发码；SMTP 配置增加一个必知项，错误模式会在启动时报错而不是首次发信时才失败。

## Sources

- [RFC 2104 · HMAC](https://www.rfc-editor.org/rfc/rfc2104)
- [SQLite GLOB](https://www.sqlite.org/lang_expr.html#glob)
- [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
- [lettre](https://docs.rs/lettre)
