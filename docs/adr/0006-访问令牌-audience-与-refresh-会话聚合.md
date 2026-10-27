# 0006 · 访问令牌 audience 与 refresh 会话聚合

**Status:** accepted (2026-10-27)

## Context

v4 的 access token 只有 `sub`、`iat`、`exp`，第一方用户会话与 OAuth 客户端授权令牌由同一把密钥、同一种 claims 签发。`AuthUser` 只验证签名与 `sub`，因此任何第三方 access token 都能调用第一方账户端点。实测：用与 `issue_pair` 相同形状的令牌调用 `/api/auth/logout` 得到 204，调用 `/api/user/totp/setup` 得到 200。`scope` 当前不进入令牌，只是响应里的装饰字段。

OAuth refresh 端点先用 `rotate` 在原家族插入 replacement，随后又调用 `issue_pair` 新建家族并把另一个 token 返回。原家族留下无人持有的 active token，返回的 token 属于新家族；重放旧 token 只撤销旧家族，不波及当前会话。`rotate` 只返回 `user_id`，不比较家族 `client_id` 与请求客户端，任何客户端都能兑换他人名下的 refresh token。

v4 尚未上线，没有真实用户，令牌格式与会话都可以直接失效，不需要兼容层。

## Decision

1. **access token 增加固定 audience。** 第一方会话签发 `aud = nebula:user-session`；第三方客户端授权签发 `aud = nebula:client-grant`，并带 `client_id` 与 `scope`。`aud` 命名令牌的信任面，不取 `client_id`；客户端身份继续由独立的 `client_id` claim 表达。
2. **验证结果是类型化的。** `verify_access` 返回 `VerifiedAccessToken::UserSession` 或 `ClientGrant`；`FirstPartyUser` 提取器只接受前者。第三方令牌在第一方端点上无法构造出该提取器，handler 不需要逐个判断 audience。
3. **claims 只能经构造函数产生。** `AccessClaims` 字段私有，公开 `user_session` 与 `client_grant` 两个构造入口；`client_grant` 在构造时校验 scope 白名单，非法 scope 不进入签名路径。
4. **refresh family 是会话聚合。** 家族创建时固定 `user_id`、`client_id`、`scope`，之后只轮转 token。`rotate` 在同一条查询里带出家族字段，接受 `ClientBinding` 并在存储层校验绑定关系；成功返回 `RotatedSession { user_id, scope }`。
5. **刷新只轮转，不建族。** token 端点的 `refresh_token` 分支下发本次 `rotate` 插入的 token，不再调用 `issue_pair`；只有授权码首次兑换才创建家族。重放撤销因此覆盖整条 token 链，跨客户端兑换在存储层失败。
6. **不做兼容层。** 不签发旧形状 token，不保留旧刷新路径；迁移补 `scope` 后撤销全部既有 refresh 家族（`0010_revoke_legacy_sessions`），旧 access token 也因 audience 变更失效，用户重新登录。部署前由旧代码创建的分叉家族不能靠新代码合并修复，只能整体撤销。

## Consequences

**得：** 第一方与第三方令牌的边界由类型与 audience 共同保证；OAuth 刷新保持家族连续，重放检测重新作用于当前会话；scope 来自家族而不是 handler 常量，授权范围在刷新前后一致；存储层承担绑定校验，handler 不再有事后比较分支。

**失：** access token 格式变更，所有现有会话失效；`refresh_token_families` 增加 `scope` 列，需要新迁移；未来若接入独立资源服务器，`aud` 需要按资源重新划分，第三方令牌的 `client_id`/`scope` 仍可复用。

## Sources

- [RFC 6749 §1.4 Access Token](https://www.rfc-editor.org/rfc/rfc6749#section-1.4)
- [RFC 9068 JWT Profile for OAuth 2.0 Access Tokens](https://www.rfc-editor.org/rfc/rfc9068)
- [RFC 6750 Bearer Token Usage](https://www.rfc-editor.org/rfc/rfc6750)
- [RFC 7636 PKCE](https://www.rfc-editor.org/rfc/rfc7636)
