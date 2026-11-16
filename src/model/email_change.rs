//! 邮箱变更待确认请求：新邮箱、验证码哈希与尝试状态。

use chrono::{DateTime, Utc};

use crate::model::user::UserId;

/// 库中读出的一条待确认邮箱变更请求；每用户至多一条。
#[derive(Debug, Clone)]
pub struct EmailChangeRequest {
    /// 所属用户。
    pub user_id: UserId,
    /// 待启用的新邮箱，已小写化。
    pub new_email: String,
    /// 六位验证码的 HMAC 摘要。
    pub code_hash: String,
    /// 已发生的错误尝试次数。
    pub attempts: u32,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
    /// 请求时刻。
    pub created_at: DateTime<Utc>,
}
