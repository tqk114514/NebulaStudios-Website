//! GET /api/admin/audit：按时间倒序分页读取审计记录。

use axum::Json;
use axum::extract::{RawQuery, State};
use serde::{Deserialize, Serialize};

use super::pagination;
use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::AdminUser;
use crate::model::audit::AuditEntry;

/// 审计列表查询参数。
#[derive(Debug, Default, Deserialize)]
pub struct ListAuditQuery {
    /// 单页条数，默认 50，最大 100。
    pub limit: Option<i64>,
    /// 偏移量。
    pub offset: Option<i64>,
}

/// 审计列表响应。
#[derive(Debug, Serialize)]
pub struct AuditResponse {
    /// 当前页记录。
    pub entries: Vec<AuditEntry>,
    /// 记录总数。
    pub total: i64,
}

/// 读取审计记录；仅管理员可访问。
///
/// # Errors
/// 未认证 401；非管理员 403；查询参数非法 400/422；存储故障 500。
pub async fn audit(
    State(state): State<AppState>,
    _admin: AdminUser,
    RawQuery(raw): RawQuery,
) -> Result<Json<AuditResponse>, ApiError> {
    let query: ListAuditQuery = match raw {
        Some(raw) => serde_urlencoded::from_str(&raw)
            .map_err(|err| ApiError::MalformedQuery(err.to_string()))?,
        None => ListAuditQuery::default(),
    };
    let page = pagination(query.limit, query.offset, 50)?;
    let entries = state
        .admin_repo
        .list_audit(page.limit, page.offset)
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    let total = state
        .admin_repo
        .count_audit()
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    Ok(Json(AuditResponse { entries, total }))
}
