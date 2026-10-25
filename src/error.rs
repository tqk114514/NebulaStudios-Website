//! HTTP 层错误：统一枚举与响应形状。响应契约见 ADR-0004：2xx 返回裸数据本体，
//! 非 2xx 一律用本信封；`code` 恒配同一 status 的映射只在本文件的
//! `IntoResponse` 中定义，handler 不得自选状态码。

use axum::Json;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde::Serialize;
use serde_json::Value as JsonValue;

/// 错误响应体；所有非 2xx 响应共用此形状。`details` 是唯一扩展位，
/// 携带结构化附加数据（如字段级校验错误）。
#[derive(Debug, Serialize)]
pub struct ErrorBody {
    /// 机器可读错误码，跨版本稳定（如 `internal_error`）。
    pub code: &'static str,
    /// 概要信息，供日志与排查；不承诺可直接展示给终端用户。
    pub message: String,
    /// 结构化附加数据；无则为 `None`，序列化时省略。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<JsonValue>,
}

/// 单个字段校验失败；`code` 机器可读（如 `password_too_short`），
/// 前端按 `field` + `code` 取本地化文案。
#[derive(Debug, Clone, Serialize)]
pub struct FieldInvalid {
    /// 字段名，与请求载荷的键一致。
    pub field: &'static str,
    /// 该字段的机器可读错误码。
    pub code: String,
}

/// API 统一错误类型；handler 一律返回它，经 `IntoResponse` 统一序列化。
#[derive(Debug)]
pub enum ApiError {
    /// 服务端内部错误；细节只进日志，不外泄给调用方。
    Internal(anyhow::Error),
    /// 请求体不是合法 JSON；400。
    MalformedJson(String),
    /// 请求字段未通过校验；422，字段级错误在 `details.fields`。
    Validation(Vec<FieldInvalid>),
    /// 邮箱已被注册；409。
    EmailTaken,
    /// 用户名已被注册；409。
    UsernameTaken,
    /// 唯一约束冲突但无法归因到具体字段（并发竞态下的兜底）；409。
    UserExists,
}

impl From<anyhow::Error> for ApiError {
    fn from(err: anyhow::Error) -> Self {
        Self::Internal(err)
    }
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        let (status, code, message, details): (
            StatusCode,
            &'static str,
            String,
            Option<JsonValue>,
        ) = match self {
            Self::Internal(err) => {
                tracing::error!(error = ?err, "internal error");
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "internal_error",
                    "internal server error".to_owned(),
                    None,
                )
            }
            Self::MalformedJson(reason) => {
                (StatusCode::BAD_REQUEST, "malformed_json", reason, None)
            }
            Self::Validation(fields) => (
                StatusCode::UNPROCESSABLE_ENTITY,
                "validation_failed",
                "请求字段未通过校验".to_owned(),
                Some(serde_json::json!({ "fields": fields })),
            ),
            Self::EmailTaken => {
                (StatusCode::CONFLICT, "email_taken", "邮箱已被注册".to_owned(), None)
            }
            Self::UsernameTaken => {
                (StatusCode::CONFLICT, "username_taken", "用户名已被注册".to_owned(), None)
            }
            Self::UserExists => {
                (StatusCode::CONFLICT, "user_exists", "用户名或邮箱已被注册".to_owned(), None)
            }
        };

        let body = ErrorBody { code, message, details };
        (status, Json(body)).into_response()
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
    #![allow(clippy::expect_used, clippy::unwrap_used)]

    use super::{ApiError, FieldInvalid};
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

    #[tokio::test]
    async fn validation_maps_to_422_with_field_details() {
        let response = ApiError::Validation(vec![FieldInvalid {
            field: "password",
            code: "password_too_short".to_owned(),
        }])
        .into_response();

        assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
        let body = response.into_body().collect().await.unwrap().to_bytes();
        assert_eq!(
            String::from_utf8_lossy(&body),
            // details 经 json!（serde_json::Value）构建，对象键按字典序输出
            r#"{"code":"validation_failed","message":"请求字段未通过校验","details":{"fields":[{"code":"password_too_short","field":"password"}]}}"#
        );
    }
}
