//! 装配与监听，无业务逻辑（AGENTS.md §2）。

use std::sync::Arc;

use anyhow::Context as _;
use nebula_server::api;
use nebula_server::config::Config;
use nebula_server::context::SystemClock;
use nebula_server::storage::sqlite::SqliteUserRepo;
use nebula_server::telemetry;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    telemetry::init();
    let config = Config::load().context("读取配置")?;
    let user_repo = SqliteUserRepo::open(&config.database.path)
        .await
        .with_context(|| format!("打开数据库 {}", config.database.path.display()))?;
    let state = api::AppState { user_repo, clock: Arc::new(SystemClock) };
    let listener = tokio::net::TcpListener::bind(config.bind)
        .await
        .with_context(|| format!("监听 {}", config.bind))?;
    tracing::info!(bind = %config.bind, "listening");
    axum::serve(listener, api::router(state))
        .with_graceful_shutdown(shutdown_signal())
        .await
        .context("服务退出")?;
    Ok(())
}

/// 等待 Ctrl-C，收到后进入优雅停机。
async fn shutdown_signal() {
    // Err 意味着该平台无法注册信号处理器，等待本身已无意义
    let _ = tokio::signal::ctrl_c().await;
}
