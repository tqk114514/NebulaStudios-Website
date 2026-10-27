//! 审计日志领域类型；追加写，不随用户删除（ADR-0008）。

use chrono::{DateTime, Utc};
use serde_json::Value as JsonValue;
use uuid::Uuid;

use crate::model::user::UserId;

/// 库中读出的审计记录。
#[derive(Debug, Clone, serde::Serialize)]
pub struct AuditEntry {
    /// 记录标识。
    pub id: Uuid,
    /// 操作者；用户删除后仍保留其 UUID 文本。
    pub actor_id: UserId,
    /// 机器可读动作，如 `admin.user.ban`。
    pub action: String,
    /// 被操作对象；无目标时为 `None`。
    pub target_id: Option<UserId>,
    /// 结构化附加信息（JSON）。
    pub details: Option<JsonValue>,
    /// 记录时刻。
    pub created_at: DateTime<Utc>,
}

/// 待写入的审计记录；明文不在此结构之外出现。
#[derive(Debug, Clone)]
pub struct NewAuditEntry {
    /// 操作者。
    pub actor_id: UserId,
    /// 机器可读动作。
    pub action: &'static str,
    /// 被操作对象。
    pub target_id: Option<UserId>,
    /// 结构化附加信息。
    pub details: Option<JsonValue>,
}
