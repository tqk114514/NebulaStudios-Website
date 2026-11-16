//! GET /api/user：读取当前登录用户的资料。

use axum::Json;
use axum::extract::State;
use serde::Serialize;

use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::FirstPartyUser;
use crate::model::user::User;

/// 当前用户资料；用户字段平铺，另加派生的头像地址。
#[derive(Debug, Serialize)]
pub struct ProfileResponse {
    /// 用户快照；不含口令哈希与头像版本。
    #[serde(flatten)]
    pub user: User,
    /// 头像公开地址；从未上传为 `None`。
    pub avatar_url: Option<String>,
}

/// 返回当前用户；不包含口令哈希等凭据。
///
/// # Errors
/// 未认证 401；用户已不存在 401；存储故障 500。
pub async fn profile(
    State(state): State<AppState>,
    user: FirstPartyUser,
) -> Result<Json<ProfileResponse>, ApiError> {
    let Some(credentials) =
        state.user_repo.find_by_id(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        // 提取器刚确认过用户存在；这里缺失只可能是并发删除，按未认证处理
        return Err(ApiError::Unauthorized);
    };
    let avatar_url = crate::api::avatar_url(&credentials.user.id, credentials.user.avatar_version);
    Ok(Json(ProfileResponse { user: credentials.user, avatar_url }))
}
