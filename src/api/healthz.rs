//! 存活探针（AGENTS.md §9.8）。

use axum::http::StatusCode;

/// 存活检查；只证明进程能应答，不探测数据库等依赖（尚未引入）。
pub async fn healthz() -> StatusCode {
    StatusCode::OK
}
