//! 纯领域类型：零 I/O，不依赖 axum / sqlx / lettre / 文件系统（§2 依赖方向 1）。

pub mod password;
pub mod tokens;
pub mod user;
