//! POST /api/auth/register/resend：为未验证邮箱重发验证码。
//! 未注册与已验证同样返回 204，三者对外同形——注册状态不由本端点泄露
//! （注册端点的 409 已公开邮箱占用，不构成新增泄露面）。

use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::issue_and_send_code;
use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::{JsonBody, PreferredLocale};
use crate::model::user::canonicalize_email;
use crate::model::verification::{RESEND_COOLDOWN, VerificationPurpose};

/// 重发请求。
#[derive(Debug, Deserialize, utoipa::ToSchema)]
pub struct ResendRequest {
    /// 需要重发的邮箱。
    pub email: String,
    /// Turnstile token；启用校验时必填。
    #[serde(default)]
    pub turnstile_token: Option<String>,
}

/// 处理重发：冷却检查 → 签发新码并异步发送。
///
/// # Errors
/// 字段缺失 422；内部故障 500。冷却与未注册均为 204，响应不泄露账户状态。
#[utoipa::path(
    post,
    path = "/api/auth/register/resend",
    tag = "auth",
    request_body = ResendRequest,
    responses(
        (status = 204, description = "已受理（未注册与冷却中同样返回）"),
        (status = 400, description = "请求体非法", body = crate::error::ErrorBody),
        (status = 415, description = "Content-Type 必须为 application/json", body = crate::error::ErrorBody),
        (status = 422, description = "字段校验失败", body = crate::error::ErrorBody),
        (status = 429, description = "请求过于频繁", body = crate::error::ErrorBody),
        (status = 500, description = "内部错误", body = crate::error::ErrorBody),
        (status = 503, description = "人机验证暂不可用", body = crate::error::ErrorBody),
    )
)]
pub async fn resend(
    State(state): State<AppState>,
    locale: PreferredLocale,
    body: JsonBody<ResendRequest>,
) -> Result<Response, ApiError> {
    let req = body.0;
    let email = canonicalize_email(&req.email).unwrap_or_else(|| req.email.trim().to_lowercase());
    if email.is_empty() {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "email",
            code: "email_missing".to_owned(),
        }]));
    }
    crate::api::verify_turnstile(&state, req.turnstile_token.as_deref()).await?;

    let user = state.user_repo.find_by_email(&email).await.map_err(anyhow::Error::from)?;

    if let Some(user) = user
        && !user.email_verified
    {
        if let Some(latest) = state
            .verification_repo
            .latest_unconsumed(&user.id, VerificationPurpose::VerifyEmail)
            .await
            .map_err(anyhow::Error::from)?
        {
            let elapsed = state.clock.now() - latest.created_at;
            if elapsed < RESEND_COOLDOWN {
                // 冷却中对已注册与未注册邮箱返回同一 204，避免存在性 oracle
                return Ok(StatusCode::NO_CONTENT.into_response());
            }
        }
        issue_and_send_code(&state, &user, VerificationPurpose::VerifyEmail, locale.0).await?;
    }
    Ok(StatusCode::NO_CONTENT.into_response())
}
