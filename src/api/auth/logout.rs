//! POST /api/auth/logout：撤销用户全部 refresh 家族并清除会话 cookie（红线 #1）。
//! 与改密、关闭 TOTP、注销共用 `revoke_all_sessions` 同一条撤销路径。

use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};

use crate::api::{AppState, clear_session_cookies};
use crate::error::ApiError;
use crate::extractors::FirstPartyUser;

/// 处理登出：鉴权前置由 [`FirstPartyUser`] 提取器承担——无有效 access token 时
/// handler 根本不会执行（v3 的登出路由没挂鉴权，撤销逻辑永不执行，红线 #1）；
/// cookie 来源的写请求同时过 CSRF 门禁（ADR-0005）。
///
/// # Errors
/// 未认证 401；撤销失败 500。
pub async fn logout(
    State(state): State<AppState>,
    user: FirstPartyUser,
) -> Result<Response, ApiError> {
    state
        .refresh_repo
        .revoke_all_sessions(&user.user_id, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;

    let mut headers = HeaderMap::new();
    clear_session_cookies(&mut headers);
    Ok((StatusCode::NO_CONTENT, headers).into_response())
}
