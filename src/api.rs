//! HTTP API：路由装配与请求处理依赖；一个端点一个文件（§2 vertical slice）。

pub mod auth;
pub mod healthz;

use std::fmt;
use std::sync::Arc;

use axum::Router;
use axum::routing::{get, post};

use crate::context::Clock;
use crate::storage::sqlite::SqliteUserRepo;

/// 请求处理依赖集合；克隆共享连接池与时钟。
#[derive(Clone)]
pub struct AppState {
    /// 用户仓储。
    pub user_repo: SqliteUserRepo,
    /// 时钟；所有时间取值经此（§2 时钟注入），handler 不得直接 `Utc::now()`。
    pub clock: Arc<dyn Clock>,
}

impl fmt::Debug for AppState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("AppState").field("user_repo", &self.user_repo).finish_non_exhaustive()
    }
}

/// 组装全部路由；新增端点在此登记。
pub fn router(state: AppState) -> Router {
    Router::new()
        .route("/healthz", get(healthz::healthz))
        .route("/api/auth/register", post(auth::register::register))
        .with_state(state)
}
