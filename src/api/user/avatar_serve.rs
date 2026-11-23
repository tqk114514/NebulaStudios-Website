//! `GET /api/users/{user_id}/avatar`：公开读取用户头像。

use axum::extract::{Path, Query, State};
use axum::http::header::{CACHE_CONTROL, CONTENT_TYPE};
use axum::http::{HeaderMap, HeaderValue};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use uuid::Uuid;

use crate::api::AppState;
use crate::error::ApiError;
use crate::model::user::UserId;

/// 头像读取查询参数；省略 `v` 时回落到用户当前版本。
#[derive(Debug, Deserialize)]
pub struct AvatarQuery {
    /// 上传响应给出的版本缓存键。
    pub v: Option<i64>,
}

/// 返回用户头像 WebP；`v` 指定版本，省略时取用户当前头像版本。
///
/// 头像按用户 id 公开可读：资料页与管理员列表都要在无额外凭证的情况下展示。
/// 响应标记为一年 immutable——版本变化时资料端点给出的 URL 会同步变化。
///
/// # Errors
/// 用户 id 非法、用户不存在或头像文件不存在 404；文件读取失败 500。
pub async fn avatar(
    State(state): State<AppState>,
    Path(raw_id): Path<String>,
    Query(query): Query<AvatarQuery>,
) -> Result<Response, ApiError> {
    let Ok(uuid) = Uuid::parse_str(&raw_id) else {
        return Err(ApiError::UserNotFound);
    };
    let user_id = UserId::from_uuid(uuid);
    // 用户删除后残留的孤儿文件不得继续公开可读
    let Some(credentials) =
        state.user_repo.find_by_id(&user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::UserNotFound);
    };
    let version = match query.v {
        Some(version) if version > 0 => version,
        Some(_) => return Err(ApiError::UserNotFound),
        None => credentials.user.avatar_version,
    };
    let Some(bytes) =
        state.avatar_store.load(&user_id, version).await.map_err(anyhow::Error::new)?
    else {
        return Err(ApiError::UserNotFound);
    };

    let mut headers = HeaderMap::new();
    headers.insert(CONTENT_TYPE, HeaderValue::from_static("image/webp"));
    headers.insert(CACHE_CONTROL, HeaderValue::from_static("public, max-age=31536000, immutable"));
    Ok((headers, bytes).into_response())
}
