//! GET /api/user：读取当前登录用户的资料。

use axum::Json;
use axum::extract::State;

use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::FirstPartyUser;
use crate::model::user::User;

/// 返回当前用户；不包含口令哈希等凭据。
///
/// # Errors
/// 未认证 401；用户已不存在 401；存储故障 500。
pub async fn profile(
    State(state): State<AppState>,
    user: FirstPartyUser,
) -> Result<Json<User>, ApiError> {
    let Some(credentials) =
        state.user_repo.find_by_id(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        // 提取器刚确认过用户存在；这里缺失只可能是并发删除，按未认证处理
        return Err(ApiError::Unauthorized);
    };
    Ok(Json(credentials.user))
}
