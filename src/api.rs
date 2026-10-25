//! HTTP API：路由装配；一个端点一个文件，挂在 `api/` 子模块里。

pub mod healthz;

use axum::Router;
use axum::routing::get;

/// 组装全部路由；新增端点在此登记，不带状态与中间件（后续切片引入）。
pub fn router() -> Router {
    Router::new().route("/healthz", get(healthz::healthz))
}
