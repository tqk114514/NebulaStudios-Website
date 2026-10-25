//! 集成测试共用装配：临时文件库 + 迁移 + 应用状态（§8.1：真实 SQLite）。
//! 迁移在装配中执行——启动路径不跑 DDL 约束的是生产，测试不受限。

// 不同测试二进制各用装配的不同子集，未用项不算死代码
#![allow(dead_code)]

use std::path::Path;
use std::sync::Arc;

use nebula_server::api::{self, AppState};
use nebula_server::context::ManualClock;
use nebula_server::storage::sqlite::SqliteUserRepo;
use sqlx::sqlite::{SqliteConnectOptions, SqlitePool};

/// `ManualClock` 的固定起点，使 `created_at` 可精确断言。
pub const FIX_NOW: &str = "2026-10-26T08:00:00Z";

/// 在 `dir` 下建库并跑全部迁移，返回保持打开的连接池（供测试直接查库）。
pub async fn migrated_pool(dir: &Path) -> SqlitePool {
    let db_path = dir.join("users.db");
    let pool = SqlitePool::connect_with(
        SqliteConnectOptions::new().filename(&db_path).create_if_missing(true),
    )
    .await
    .expect("建库连接必须成功");
    sqlx::migrate!().run(&pool).await.expect("迁移必须成功");
    pool
}

/// 装配 `AppState`：临时库 + 固定时钟；`TempDir` 由调用方持有钉住生命周期。
pub async fn app_state() -> (AppState, Arc<ManualClock>, tempfile::TempDir) {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = migrated_pool(dir.path()).await;
    let db_path = dir.path().join("users.db");

    let user_repo = SqliteUserRepo::open(&db_path).await.expect("已迁移的库必须可打开");
    pool.close().await;

    let start = chrono::DateTime::parse_from_rfc3339(FIX_NOW)
        .expect("固定字面量必须可解析")
        .with_timezone(&chrono::Utc);
    let manual = Arc::new(ManualClock::new(start));
    let state = AppState { user_repo, clock: manual.clone() };
    (state, manual, dir)
}

/// 装配完整路由（含状态）与配套句柄。
pub async fn app() -> (axum::Router, AppState, Arc<ManualClock>, tempfile::TempDir) {
    let (state, clock, dir) = app_state().await;
    (api::router(state.clone()), state, clock, dir)
}
