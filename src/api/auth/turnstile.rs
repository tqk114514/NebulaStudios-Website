//! `GET /api/auth/turnstile`：公开 Turnstile 站点配置。

use axum::Json;
use axum::extract::State;
use serde::Serialize;

use crate::api::AppState;

/// 前端渲染 Turnstile 所需的站点配置；未启用时只返回 `enabled: false`。
#[derive(Debug, Serialize)]
pub struct TurnstileSettingsResponse {
    /// 是否启用服务端校验。
    pub enabled: bool,
    /// 公开 site key；未启用时省略。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub site_key: Option<String>,
}

/// 返回 Turnstile 启用状态与 site key；不含 secret。
pub async fn settings(State(state): State<AppState>) -> Json<TurnstileSettingsResponse> {
    Json(TurnstileSettingsResponse {
        enabled: state.turnstile.is_some(),
        site_key: state.turnstile_site_key.clone(),
    })
}
