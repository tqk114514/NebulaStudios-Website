//! HTTP 层错误：统一枚举与响应形状。变体随端点增加，当前只有 Internal——
//! 没有真实错误来源之前不预造分类。

use axum::Json;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde::Serialize;

/// 错误响应体；所有非 2xx 响应共用此形状。
#[derive(Debug, Serialize)]
pub struct ErrorBody {
    /// 机器可读错误码，跨版本稳定（如 `internal_error`）。
    pub code: &'static str,
    /// 概要信息，供日志与排查；不承诺可直接展示给终端用户。
    pub message: String,
}

/// API 统一错误类型；handler 一律返回它，经 `IntoResponse` 统一序列化。
#[derive(Debug)]
pub enum ApiError {
    /// 服务端内部错误；细节只进日志，不外泄给调用方。
    Internal(anyhow::Error),
}

// 允许 handler 用 `?` 把任何可转 anyhow 的错误提升为 Internal；
// `E = ApiError` 时因 anyhow 未实现 From<ApiError> 不构成自反冲突
impl<E> From<E> for ApiError
where
    anyhow::Error: From<E>,
{
    fn from(err: E) -> Self {
        Self::Internal(err.into())
    }
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        match self {
            Self::Internal(err) => {
                tracing::error!(error = ?err, "internal error");
                let body = ErrorBody {
                    code: "internal_error",
                    message: "internal server error".to_owned(),
                };
                (StatusCode::INTERNAL_SERVER_ERROR, Json(body)).into_response()
            }
        }
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
    #![allow(clippy::expect_used, clippy::unwrap_used)]

    use super::ApiError;
    use axum::http::StatusCode;
    use axum::response::IntoResponse;
    use http_body_util::BodyExt;

    #[tokio::test]
    async fn internal_maps_to_500_with_stable_body() {
        let response = ApiError::Internal(anyhow::anyhow!("boom")).into_response();

        assert_eq!(response.status(), StatusCode::INTERNAL_SERVER_ERROR);
        let body = response.into_body().collect().await.unwrap().to_bytes();
        assert_eq!(
            body.as_ref(),
            br#"{"code":"internal_error","message":"internal server error"}"#
        );
    }
}
