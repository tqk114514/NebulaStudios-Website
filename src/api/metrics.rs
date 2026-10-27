//! Prometheus 指标导出（§9.8）。
//!
//! 指标录制由装配层安装的 `axum-prometheus` 层完成；本端点只渲染当前快照。
//! 测试装配不安装全局 recorder，故 `metrics` 为 `None` 时返回 404。

use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};

use crate::api::AppState;

/// 渲染 Prometheus 文本格式；未装配指标时返回 404。
pub async fn metrics(State(state): State<AppState>) -> Response {
    match &state.metrics {
        Some(handle) => handle.render().into_response(),
        None => StatusCode::NOT_FOUND.into_response(),
    }
}
