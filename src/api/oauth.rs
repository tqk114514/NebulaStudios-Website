//! `OAuth2 Provider` 端点（ADR-0002）。标准 `authorize` GET 与 `token` 端点面向
//! 第三方客户端，错误遵循 RFC 6749、不套统一信封（ADR-0004 §5）；
//! `authorize/consent` 是第一方 SPA 的 JSON 端点，仍沿用 `ApiError` 统一信封。

pub mod authorize;
pub mod token;

use axum::Json;
use axum::http::{StatusCode, header};
use axum::response::{IntoResponse, Response};

/// RFC 6749 错误响应（`{"error": "..."}`，no-store）；authorize 与 token 共用。
pub(super) fn rfc_error(status: StatusCode, error: &'static str) -> Response {
    (
        status,
        [(header::CACHE_CONTROL, "no-store"), (header::PRAGMA, "no-cache")],
        Json(serde_json::json!({ "error": error })),
    )
        .into_response()
}

/// form/查询参数值的最小百分号编码（RFC 3986 保留字符；state 回传用）。
pub(super) fn form_urlencode(value: &str) -> String {
    const HEX: &[u8; 16] = b"0123456789ABCDEF";
    let mut out = String::with_capacity(value.len());
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(byte as char);
            }
            _ => {
                out.push('%');
                out.push(HEX[usize::from(byte >> 4)] as char);
                out.push(HEX[usize::from(byte & 0x0f)] as char);
            }
        }
    }
    out
}
