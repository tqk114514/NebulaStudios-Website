//! GET /api/admin/users：分页用户列表，按邮箱/用户名模糊搜索。

use axum::Json;
use axum::extract::{RawQuery, State};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::pagination;
use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::AdminUser;
use crate::model::user::{Ban, UserId};

/// 用户列表查询参数。
#[derive(Debug, Default, Deserialize)]
pub struct ListUsersQuery {
    /// 单页条数，默认 50，最大 100。
    pub limit: Option<i64>,
    /// 偏移量。
    pub offset: Option<i64>,
    /// 同时匹配邮箱与用户名；空串视为不筛选。
    pub query: Option<String>,
}

/// 封禁视图；`kind` 与 `expires_at` 的组合与领域状态一致。
#[derive(Debug, Serialize)]
pub struct BanView {
    /// `temporary` 或 `permanent`。
    pub kind: &'static str,
    /// 临时封禁的到期时刻；永久封禁为 `None`。
    pub expires_at: Option<DateTime<Utc>>,
}

/// 用户列表行。
#[derive(Debug, Serialize)]
pub struct UserView {
    /// 用户标识。
    pub id: UserId,
    /// 展示用户名。
    pub username: String,
    /// 邮箱。
    pub email: String,
    /// 邮箱是否已验证。
    pub email_verified: bool,
    /// 注册时刻。
    pub created_at: DateTime<Utc>,
    /// 当前生效的封禁。
    pub ban: Option<BanView>,
    /// 是否为配置化管理员（ADR-0008）。
    pub is_admin: bool,
}

/// 用户列表响应。
#[derive(Debug, Serialize)]
pub struct UsersResponse {
    /// 当前页用户。
    pub users: Vec<UserView>,
    /// 同筛选条件下的总数。
    pub total: i64,
}

/// 分页列出用户；仅管理员可访问。
///
/// # Errors
/// 未认证 401；非管理员 403；查询参数非法 400/422；存储故障 500。
pub async fn users(
    State(state): State<AppState>,
    _admin: AdminUser,
    RawQuery(raw): RawQuery,
) -> Result<Json<UsersResponse>, ApiError> {
    let query: ListUsersQuery = match raw {
        Some(raw) => serde_urlencoded::from_str(&raw)
            .map_err(|err| ApiError::MalformedQuery(err.to_string()))?,
        None => ListUsersQuery::default(),
    };
    let page = pagination(query.limit, query.offset, 50)?;
    let search = query.query.as_deref().map(str::trim).filter(|value| !value.is_empty());
    if let Some(search) = search
        && search.chars().count() > 100
    {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "query",
            code: "query_too_long".to_owned(),
        }]));
    }

    let now = state.clock.now();
    let rows = state
        .admin_repo
        .list_users(search, page.limit, page.offset, now)
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    let total = state
        .admin_repo
        .count_users(search)
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;

    let users = rows
        .into_iter()
        .map(|row| UserView {
            is_admin: state.admin_emails.contains(&row.email),
            id: row.id,
            username: row.username,
            email: row.email,
            email_verified: row.email_verified,
            created_at: row.created_at,
            ban: row.ban.map(|ban| match ban {
                Ban::Temporary(expires_at) => {
                    BanView { kind: "temporary", expires_at: Some(expires_at) }
                }
                Ban::Permanent => BanView { kind: "permanent", expires_at: None },
            }),
        })
        .collect();

    Ok(Json(UsersResponse { users, total }))
}
