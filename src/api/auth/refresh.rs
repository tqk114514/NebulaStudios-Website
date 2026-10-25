//! POST /api/auth/refresh：轮转 refresh token 并续发 access；成功 204 空体
//! （ADR-0004），新凭据经 Set-Cookie 下发。

use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};

use super::{
    clear_session_cookies, cookie_max_age, set_access_cookie, set_refresh_cookie, sign_access_for,
};
use crate::api::AppState;
use crate::api::{REFRESH_COOKIE, cookie_value};
use crate::error::ApiError;
use crate::model::tokens::RefreshToken;
use crate::storage::RefreshTokenRepo;
use crate::storage::RotateRefreshError;

/// 处理刷新：呈递 cookie 中的 refresh 明文，成功换新并续发 access。
/// 401 附带清 cookie 头：安全不依赖它（服务端已撤销），但不清会让浏览器揣着
/// 死 cookie 重放最长 30 天，污染 401 指标与日志（§9.8），也拖累盗用恢复体验。
///
/// # Errors
/// cookie 缺失/未知/过期 401 `refresh_invalid`；重放 401 `refresh_reused`
/// （家族已整族撤销）；内部故障 500。
pub async fn refresh(
    State(state): State<AppState>,
    headers: HeaderMap,
) -> Result<Response, ApiError> {
    // TODO(@tqk114514): CSRF 门禁（double-submit + 恒定时间比较）; 随 CSRF 切片统一落地
    let Some(presented) = cookie_value(&headers, REFRESH_COOKIE) else {
        return Ok(unauthorized_with_cleared_cookies(ApiError::RefreshInvalid));
    };

    let now = state.clock.now();
    let replacement = RefreshToken::generate();
    let rotated = state
        .refresh_repo
        .rotate(
            &RefreshToken::hash_of(&presented),
            &replacement.to_hash_hex(),
            now + state.session.refresh_ttl,
            now,
        )
        .await;

    let user_id = match rotated {
        Ok(user_id) => user_id,
        Err(RotateRefreshError::Replay) => {
            return Ok(unauthorized_with_cleared_cookies(ApiError::RefreshReused));
        }
        Err(RotateRefreshError::Invalid) => {
            return Ok(unauthorized_with_cleared_cookies(ApiError::RefreshInvalid));
        }
        Err(RotateRefreshError::Storage(err)) => {
            return Err(ApiError::Internal(err.into()));
        }
    };

    // 轮转已把 replacement 插进原家族——直接下发它，绝不另建家族（否则重放
    // 撤销原家族时伤不到新代，家族隔离失效）
    let access = sign_access_for(&state, user_id)?;
    let mut response_headers = HeaderMap::new();
    set_access_cookie(&mut response_headers, &access, cookie_max_age(state.session.access_ttl));
    set_refresh_cookie(
        &mut response_headers,
        replacement.as_str(),
        cookie_max_age(state.session.refresh_ttl),
    );
    Ok((StatusCode::NO_CONTENT, response_headers).into_response())
}

/// 组装带清 cookie 头的 401 响应；状态与信封由 [`ApiError`] 单点映射产生，
/// 此处只叠加 Set-Cookie。
fn unauthorized_with_cleared_cookies(err: ApiError) -> Response {
    let mut response = err.into_response();
    let mut clear = HeaderMap::new();
    clear_session_cookies(&mut clear);
    for (name, value) in &clear {
        response.headers_mut().append(name, value.clone());
    }
    response
}
