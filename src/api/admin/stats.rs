//! GET /api/admin/stats：用户总数、当前封禁数与近 24 小时新增。

use axum::Json;
use axum::extract::State;
use chrono::TimeDelta;
use serde::Serialize;

use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::AdminUser;

/// 统计响应（ADR-0004 裸数据）。
#[derive(Debug, Serialize)]
pub struct StatsResponse {
    /// 用户总数。
    pub total_users: i64,
    /// 当前生效的封禁数。
    pub banned_users: i64,
    /// 近 24 小时新增用户数。
    pub new_users_today: i64,
}

/// 读取统计；仅管理员可访问。
///
/// # Errors
/// 未认证 401；非管理员 403；存储故障 500。
pub async fn stats(
    State(state): State<AppState>,
    _admin: AdminUser,
) -> Result<Json<StatsResponse>, ApiError> {
    let now = state.clock.now();
    let snapshot = state
        .admin_repo
        .stats(now, now - TimeDelta::days(1))
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    Ok(Json(StatsResponse {
        total_users: snapshot.total_users,
        banned_users: snapshot.banned_users,
        new_users_today: snapshot.new_users_today,
    }))
}
