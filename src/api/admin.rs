//! 管理后台端点（AGENTS §0 最小集；ADR-0008）。

pub mod audit;
pub mod ban;
pub mod stats;
pub mod unban;
pub mod users;

use uuid::Uuid;

use crate::error::{ApiError, FieldInvalid};
use crate::model::user::UserId;

/// 管理端点分页参数。
pub(crate) struct Pagination {
    /// 单页条数。
    pub limit: i64,
    /// 偏移量。
    pub offset: i64,
}

/// 规范化分页：`limit` 上限 100，`offset` 不得为负；非法值返回字段级 422。
pub(crate) fn pagination(
    limit: Option<i64>,
    offset: Option<i64>,
    default_limit: i64,
) -> Result<Pagination, ApiError> {
    let limit = limit.unwrap_or(default_limit);
    if !(1..=100).contains(&limit) {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "limit",
            code: "limit_out_of_range".to_owned(),
        }]));
    }
    let offset = offset.unwrap_or(0);
    if offset < 0 {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "offset",
            code: "offset_negative".to_owned(),
        }]));
    }
    Ok(Pagination { limit, offset })
}

/// 解析路径中的用户 UUID；非法 UUID 与不存在同体返回 404，避免泄露解析细节。
pub(crate) fn target_id(raw: &str) -> Result<UserId, ApiError> {
    Uuid::parse_str(raw).map(UserId::from_uuid).map_err(|_| ApiError::UserNotFound)
}

/// 构造单字段校验错误。
pub(crate) fn invalid(field: &'static str, code: &'static str) -> ApiError {
    ApiError::Validation(vec![FieldInvalid { field, code: code.to_owned() }])
}

/// 配置化管理员判定：邮箱在名单内且已验证。
///
/// 注册不要求验证，若只看邮箱，攻击者可用未验证账号抢占管理员身份并阻止封禁。
pub(crate) fn is_admin_email(
    admin_emails: &std::collections::HashSet<String>,
    email: &str,
    email_verified: bool,
) -> bool {
    email_verified && admin_emails.contains(email)
}
