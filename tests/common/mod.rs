//! 集成测试共用装配：临时文件库 + 迁移 + 应用状态（§8.1：真实 SQLite）。
//! 迁移在装配中执行——启动路径不跑 DDL 约束的是生产，测试不受限。

// 不同测试二进制各用装配的不同子集，未用项不算死代码
#![allow(dead_code)]

use std::path::Path;
use std::sync::Arc;

use axum::body::Body;
use axum::http::{Request, StatusCode, header::SET_COOKIE};
use chrono::TimeDelta;
use nebula_server::api::{self, AppState, JwtKeys, SessionPolicy};
use nebula_server::context::ManualClock;
use nebula_server::storage::sqlite::{self, SqliteRefreshRepo, SqliteUserRepo};
use sqlx::sqlite::{SqliteConnectOptions, SqlitePool};

/// `ManualClock` 的固定起点，使 `created_at` 可精确断言。
pub const FIX_NOW: &str = "2026-10-26T08:00:00Z";

/// 仅测试用的 P-256 密钥对（PKCS#8 私钥 + 公钥），不用于任何真实环境。
pub const TEST_PRIVATE_PEM: &str = "-----BEGIN PRIVATE KEY-----\nMIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgHHnffUGVWWLk8Hz8\nT+NdLnsaBCFWKphSj4rcq30zzkShRANCAASIriVp5Ne81a6mczgeIzuAXgLonIjY\nBRQBVkauO2/OKAe/QmK5cQ8TkPMha0N9171Z6eBiUfhJUeZs9qieNjw8\n-----END PRIVATE KEY-----";
pub const TEST_PUBLIC_PEM: &str = "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEiK4laeTXvNWupnM4HiM7gF4C6JyI\n2AUUAVZGrjtvzigHv0JiuXEPE5DzIWtDfde9WengYlH4SVHmbPaonjY8PA==\n-----END PUBLIC KEY-----";

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

fn test_jwt() -> JwtKeys {
    JwtKeys::from_pem(TEST_PRIVATE_PEM.as_bytes(), TEST_PUBLIC_PEM.as_bytes())
        .expect("测试密钥必须可解析")
}

fn test_session() -> SessionPolicy {
    SessionPolicy { access_ttl: TimeDelta::minutes(10), refresh_ttl: TimeDelta::days(30) }
}

/// 装配 `AppState`：临时库 + 固定时钟 + 测试密钥；`TempDir` 钉住文件生命周期。
pub async fn app_state() -> (AppState, Arc<ManualClock>, tempfile::TempDir) {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let bootstrap = migrated_pool(dir.path()).await;
    bootstrap.close().await;
    let pool =
        sqlite::connect_pool(&dir.path().join("users.db")).await.expect("已迁移的库必须可打开");

    let start = chrono::DateTime::parse_from_rfc3339(FIX_NOW)
        .expect("固定字面量必须可解析")
        .with_timezone(&chrono::Utc);
    let manual = Arc::new(ManualClock::new(start));
    let state = AppState {
        user_repo: SqliteUserRepo::from_pool(pool.clone()),
        refresh_repo: SqliteRefreshRepo::from_pool(pool),
        clock: manual.clone(),
        jwt: test_jwt(),
        session: test_session(),
    };
    (state, manual, dir)
}

/// 装配完整路由（含状态）与配套句柄。
pub async fn app() -> (axum::Router, AppState, Arc<ManualClock>, tempfile::TempDir) {
    let (state, clock, dir) = app_state().await;
    (api::router(state.clone()), state, clock, dir)
}

/// 装配共享同一连接池的用户与 refresh 仓储（仓储层测试用）。
pub async fn repos() -> (SqliteUserRepo, SqliteRefreshRepo, tempfile::TempDir) {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let bootstrap = migrated_pool(dir.path()).await;
    bootstrap.close().await;

    let pool =
        sqlite::connect_pool(&dir.path().join("users.db")).await.expect("已迁移的库必须可打开");
    (SqliteUserRepo::from_pool(pool.clone()), SqliteRefreshRepo::from_pool(pool), dir)
}

/// 经注册端点造一个已注册用户；断言 201。
pub async fn register_user(app: &axum::Router, email: &str, username: &str, password: &str) {
    let body = format!(r#"{{"email":"{email}","username":"{username}","password":"{password}"}}"#);
    let response = app.clone().oneshot_post("/api/auth/register", &body).await;
    assert_eq!(response.status(), StatusCode::CREATED, "注册助手必须成功");
}

/// 从响应的 Set-Cookie 里取出指定 cookie 的值。
pub fn set_cookie_of(response: &axum::response::Response, name: &str) -> Option<String> {
    response.headers().get_all(SET_COOKIE).iter().find_map(|value| {
        let raw = value.to_str().ok()?;
        let pair = raw.split(';').next()?.trim();
        pair.strip_prefix(&format!("{name}=")).map(ToOwned::to_owned)
    })
}

/// 供后续请求回带的 cookie 头（含 csrf 双提交的 cookie 半边）。
pub fn cookie_header(access: Option<&str>, refresh: Option<&str>, csrf: Option<&str>) -> String {
    let mut parts = Vec::new();
    if let Some(access) = access {
        parts.push(format!("nebula_access={access}"));
    }
    if let Some(refresh) = refresh {
        parts.push(format!("nebula_refresh={refresh}"));
    }
    if let Some(csrf) = csrf {
        parts.push(format!("nebula_csrf={csrf}"));
    }
    parts.join("; ")
}

/// oneshot POST JSON 的快捷方式；csrf 为 `X-CSRF-Token` 头的值。
pub trait OneshotPost {
    async fn oneshot_post(self, uri: &str, body: &str) -> axum::response::Response;
    async fn oneshot_post_with(
        self,
        uri: &str,
        body: &str,
        cookie: &str,
        csrf: Option<&str>,
    ) -> axum::response::Response;
    async fn oneshot_post_bearer(
        self,
        uri: &str,
        body: &str,
        token: &str,
    ) -> axum::response::Response;
}

impl OneshotPost for axum::Router {
    async fn oneshot_post(self, uri: &str, body: &str) -> axum::response::Response {
        self.oneshot_post_with(uri, body, "", None).await
    }

    async fn oneshot_post_with(
        self,
        uri: &str,
        body: &str,
        cookie: &str,
        csrf: Option<&str>,
    ) -> axum::response::Response {
        use tower::ServiceExt;
        let mut request = Request::post(uri)
            .header("content-type", "application/json")
            .body(Body::from(body.to_owned()))
            .expect("请求构造必须成功");
        if !cookie.is_empty() {
            request
                .headers_mut()
                .insert(axum::http::header::COOKIE, cookie.parse().expect("cookie 头合法"));
        }
        if let Some(csrf) = csrf {
            request.headers_mut().insert("x-csrf-token", csrf.parse().expect("csrf 头合法"));
        }
        self.oneshot(request).await.expect("oneshot 必须成功")
    }

    async fn oneshot_post_bearer(
        self,
        uri: &str,
        body: &str,
        token: &str,
    ) -> axum::response::Response {
        use tower::ServiceExt;
        let request = Request::post(uri)
            .header("content-type", "application/json")
            .header("authorization", format!("Bearer {token}"))
            .body(Body::from(body.to_owned()))
            .expect("请求构造必须成功");
        self.oneshot(request).await.expect("oneshot 必须成功")
    }
}
