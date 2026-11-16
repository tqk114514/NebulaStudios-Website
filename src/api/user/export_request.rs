//! `POST /api/user/export`：申请一次性数据导出下载链接。

use axum::Json;
use axum::extract::State;
use chrono::{DateTime, Utc};
use serde::Serialize;

use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::FirstPartyUser;
use crate::model::export::{EXPORT_TOKEN_TTL, ExportToken};

/// 导出申请响应。
#[derive(Debug, Serialize)]
pub struct ExportRequestResponse {
    /// 一次性下载地址；令牌明文只在此响应中出现一次。
    pub download_url: String,
    /// 链接失效时刻。
    pub expires_at: DateTime<Utc>,
}

/// 生成下载令牌并入库存哈希；签发新令牌会作废该用户此前未消费的令牌。
///
/// # Errors
/// 未认证 401；CSRF 失败 403；存储故障 500。
pub async fn request_export(
    State(state): State<AppState>,
    user: FirstPartyUser,
) -> Result<Json<ExportRequestResponse>, ApiError> {
    let now = state.clock.now();
    let token = ExportToken::generate();
    let expires_at = now + EXPORT_TOKEN_TTL;
    state
        .export_repo
        .issue(&user.user_id, &token.to_hash_hex(), expires_at, now)
        .await
        .map_err(anyhow::Error::new)?;
    Ok(Json(ExportRequestResponse {
        download_url: format!("/api/user/export/{}", token.as_str()),
        expires_at,
    }))
}
