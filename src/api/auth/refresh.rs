//! POST /api/auth/refresh：轮转 refresh token 并续发 access；成功 204 空体
//! （ADR-0004），新凭据经 Set-Cookie 下发。

use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};

use super::{cookie_max_age, set_access_cookie, set_refresh_cookie, sign_access_for};
use crate::api::AppState;
use crate::api::{REFRESH_COOKIE, cookie_value};
use crate::error::ApiError;
use crate::model::tokens::RefreshToken;
use crate::storage::RefreshTokenRepo;
use crate::storage::RotateRefreshError;

/// 处理刷新：呈递 cookie 中的 refresh 明文，成功换新并续发 access。
/// 401 不附清 cookie 头——服务端已撤销，客户端按 401 自行丢弃本地凭据。
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
        return Err(ApiError::RefreshInvalid);
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
        Err(RotateRefreshError::Replay) => return Err(ApiError::RefreshReused),
        Err(RotateRefreshError::Invalid) => return Err(ApiError::RefreshInvalid),
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
