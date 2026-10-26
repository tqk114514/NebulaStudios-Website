//! `OAuth2 Provider` 端点（ADR-0002；ADR-0004 §6：本目录错误响应遵循 RFC 6749，
//! 不套统一信封——消费者是第三方客户端而非我们的前端）。

pub mod authorize;
pub mod token;
