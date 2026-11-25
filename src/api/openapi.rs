//! `OpenAPI` 文档：供前端生成 API client，同时暴露在 `GET /api/openapi.json`。
//!
//! 只登记已进入前端接线范围的端点；`paths` 是显式清单，新增端点时同步补上，
//! 不根据 axum 路由表反射生成（自定义 extractor 与分层路由无法可靠反射）。

use axum::Json;
use utoipa::OpenApi;

/// 当前公开端点与 schema 的 `OpenAPI` 描述。
#[derive(Debug, OpenApi)]
#[openapi(
    info(
        title = "Nebula API",
        version = env!("CARGO_PKG_VERSION"),
        description = "Nebula Studios 身份认证服务；错误响应统一为 ErrorBody，详见 ADR-0004。"
    ),
    paths(
        crate::api::auth::turnstile::settings,
        crate::api::auth::register::register,
        crate::api::auth::verify::verify,
        crate::api::auth::resend::resend,
        crate::api::auth::login::login,
        crate::api::auth::login_totp::login_totp,
        crate::api::auth::refresh::refresh,
        crate::api::auth::logout::logout,
        crate::api::auth::password_reset::reset_request,
        crate::api::auth::password_reset::reset_confirm,
        crate::api::user::profile::profile
    ),
    components(schemas(
        crate::error::ErrorBody,
        crate::error::FieldInvalid,
        crate::model::user::User,
        crate::api::auth::register::RegisterRequest,
        crate::api::auth::verify::VerifyRequest,
        crate::api::auth::resend::ResendRequest,
        crate::api::auth::login::LoginRequest,
        crate::api::auth::login_totp::LoginTotpRequest,
        crate::api::auth::password_reset::ResetRequestPayload,
        crate::api::auth::password_reset::ResetConfirmPayload,
        crate::api::auth::turnstile::TurnstileSettingsResponse,
        crate::api::user::profile::ProfileResponse
    )),
    tags(
        (name = "auth", description = "注册、登录、会话与密码重置"),
        (name = "user", description = "第一方用户资料")
    )
)]
pub struct ApiDoc;

/// 返回 `OpenAPI` JSON；无数据库、无认证，供构建期代码生成读取。
pub async fn json() -> Json<utoipa::openapi::OpenApi> {
    Json(ApiDoc::openapi())
}
