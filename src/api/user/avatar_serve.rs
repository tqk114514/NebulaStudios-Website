//! `GET /api/users/{user_id}/avatar`：公开读取用户头像。

use axum::extract::{Path, State};
use axum::http::header::{CACHE_CONTROL, CONTENT_TYPE};
use axum::http::{HeaderMap, HeaderValue};
use axum::response::{IntoResponse, Response};
use uuid::Uuid;

use crate::api::AppState;
use crate::error::ApiError;
use crate::model::user::UserId;

/// 返回用户头像 WebP；地址中的 `v` 只是缓存键，服务端不校验其值。
///
/// 头像按用户 id 公开可读：资料页与管理员列表都要在无额外凭证的情况下展示。
/// 响应标记为一年 immutable——版本变化时资料端点给出的 URL 会同步变化。
///
/// # Errors
/// 用户 id 非法或头像文件不存在 404；文件读取失败 500。
pub async fn avatar(
    State(state): State<AppState>,
    Path(raw_id): Path<String>,
) -> Result<Response, ApiError> {
    let Ok(uuid) = Uuid::parse_str(&raw_id) else {
        return Err(ApiError::UserNotFound);
    };
    let user_id = UserId::from_uuid(uuid);
    let Some(bytes) = state.avatar_store.load(&user_id).await.map_err(anyhow::Error::new)? else {
        return Err(ApiError::UserNotFound);
    };

    let mut headers = HeaderMap::new();
    headers.insert(CONTENT_TYPE, HeaderValue::from_static("image/webp"));
    headers.insert(CACHE_CONTROL, HeaderValue::from_static("public, max-age=31536000, immutable"));
    Ok((headers, bytes).into_response())
}
