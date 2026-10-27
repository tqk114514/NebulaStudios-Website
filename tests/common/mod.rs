//! 集成测试共用装配：临时文件库 + 迁移 + 应用状态（§8.1：真实 SQLite）。
//! 迁移在装配中执行——启动路径不跑 DDL 约束的是生产，测试不受限。

// 不同测试二进制各用装配的不同子集，未用项不算死代码；
// 测试装配内的 expect 同样属于测试语义
#![allow(clippy::expect_used, dead_code)]

use std::collections::HashSet;
use std::path::Path;
use std::sync::Arc;

use axum::body::Body;
use axum::http::{Request, StatusCode, header::SET_COOKIE};
use chrono::TimeDelta;
use nebula_server::api::{self, AppState, JwtKeys, SessionPolicy};
use nebula_server::context::ManualClock;
use nebula_server::crypto::{AesGcmCipher, HmacCodeHasher};
pub use nebula_server::mailer::EmailMessage;
use nebula_server::mailer::{MailError, Mailer};
use nebula_server::storage::sqlite::{
    self, SqliteAccountRepo, SqliteAdminRepo, SqliteEmailVerificationRepo, SqliteMaintenanceRepo,
    SqliteOAuthRepo, SqliteRefreshRepo, SqliteTotpRepo, SqliteUserRepo,
};
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

/// 测试用捕获信箱：记录待发邮件供断言（实现 [`Mailer`] 端口）。
#[derive(Clone, Default)]
pub struct Mailbox(Arc<std::sync::Mutex<Vec<EmailMessage>>>);

impl Mailbox {
    /// 已捕获的邮件快照。
    pub fn messages(&self) -> Vec<EmailMessage> {
        self.0.lock().expect("信箱锁不应中毒").clone()
    }
}

impl Mailer for Mailbox {
    fn send(
        &self,
        message: EmailMessage,
    ) -> std::pin::Pin<Box<dyn std::future::Future<Output = Result<(), MailError>> + Send + '_>>
    {
        Box::pin(async move {
            self.0.lock().expect("信箱锁不应中毒").push(message);
            Ok(())
        })
    }
}

/// 从邮件正文提取验证码（格式见 api/auth.rs 发信助手）。
pub fn extract_code(message: &EmailMessage) -> String {
    message
        .body
        .split('：')
        .nth(1)
        .and_then(|rest| rest.split('，').next())
        .unwrap_or_default()
        .to_owned()
}

/// 等待异步发出的第一封邮件（发送在注册/重发后 spawn，须短暂轮询）。
pub async fn await_first_mail(mailbox: &Mailbox) -> EmailMessage {
    for _ in 0..200 {
        if let Some(message) = mailbox.messages().first() {
            return message.clone();
        }
        tokio::time::sleep(std::time::Duration::from_millis(5)).await;
    }
    panic!("验证邮件未在预期时间内发出");
}

/// 等待发给指定收件人的邮件；同一测试注册多个账号时避免拿到别人的码。
pub async fn await_mail_for(mailbox: &Mailbox, email: &str) -> EmailMessage {
    for _ in 0..200 {
        if let Some(message) =
            mailbox.messages().into_iter().find(|message| message.to.eq_ignore_ascii_case(email))
        {
            return message;
        }
        tokio::time::sleep(std::time::Duration::from_millis(5)).await;
    }
    panic!("发给 {email} 的验证邮件未在预期时间内发出");
}

/// 装配 `AppState`：临时库 + 固定时钟 + 测试密钥 + 捕获信箱；`TempDir` 钉住文件生命周期。
pub async fn app_state() -> (AppState, Arc<ManualClock>, Mailbox, tempfile::TempDir) {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let bootstrap = migrated_pool(dir.path()).await;
    bootstrap.close().await;
    let pool =
        sqlite::connect_pool(&dir.path().join("users.db")).await.expect("已迁移的库必须可打开");

    let start = chrono::DateTime::parse_from_rfc3339(FIX_NOW)
        .expect("固定字面量必须可解析")
        .with_timezone(&chrono::Utc);
    let manual = Arc::new(ManualClock::new(start));
    let mailbox = Mailbox::default();
    let state = AppState {
        user_repo: Arc::new(SqliteUserRepo::from_pool(pool.clone())),
        refresh_repo: Arc::new(SqliteRefreshRepo::from_pool(pool.clone())),
        clock: manual.clone(),
        jwt: test_jwt(),
        session: test_session(),
        verification_repo: Arc::new(SqliteEmailVerificationRepo::from_pool(pool.clone())),
        account_repo: Arc::new(SqliteAccountRepo::from_pool(pool.clone())),
        totp_repo: Arc::new(SqliteTotpRepo::from_pool(pool.clone())),
        oauth_repo: Arc::new(SqliteOAuthRepo::from_pool(pool.clone())),
        maintenance_repo: Arc::new(SqliteMaintenanceRepo::from_pool(pool.clone())),
        admin_repo: Arc::new(SqliteAdminRepo::from_pool(pool)),
        admin_emails: Arc::new(HashSet::new()),
        secret_cipher: Arc::new(AesGcmCipher::from_key([7u8; 32])),
        code_hasher: Arc::new(HmacCodeHasher::from_key([9u8; 32])),
        mailer: Arc::new(mailbox.clone()),
        metrics: None,
        cors_allowed_origin: None,
    };
    (state, manual, mailbox, dir)
}

/// 装配完整路由（含状态）与配套句柄。
pub async fn app() -> (axum::Router, AppState, Arc<ManualClock>, Mailbox, tempfile::TempDir) {
    let (state, clock, mailbox, dir) = app_state().await;
    (api::router(state.clone()), state, clock, mailbox, dir)
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

/// 注册并完成邮箱验证；需要管理员权限的测试必须走这里。
pub async fn register_verified_user(
    app: &axum::Router,
    email: &str,
    username: &str,
    password: &str,
    mailbox: &Mailbox,
) {
    register_user(app, email, username, password).await;
    let message = await_mail_for(mailbox, email).await;
    let code = extract_code(&message);
    let body = serde_json::json!({ "email": email, "code": code }).to_string();
    let response = app.clone().oneshot_post("/api/auth/register/verify", &body).await;
    assert_eq!(response.status(), StatusCode::NO_CONTENT, "邮箱验证助手必须成功");
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
    /// 以指定源 IP 发起（限额测试用）。
    async fn oneshot_post_from_ip(
        self,
        ip: &str,
        uri: &str,
        body: &str,
    ) -> axum::response::Response;
    /// GET with cookie。
    async fn oneshot_get_with_cookie(
        self,
        uri: &str,
        query: &str,
        cookie: &str,
    ) -> axum::response::Response;
    /// POST form-urlencoded。
    async fn oneshot_post_form(self, uri: &str, form: &str) -> axum::response::Response;
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
        inject_connect_info(&mut request);
        self.oneshot(request).await.expect("oneshot 必须成功")
    }

    async fn oneshot_post_bearer(
        self,
        uri: &str,
        body: &str,
        token: &str,
    ) -> axum::response::Response {
        use tower::ServiceExt;
        let mut request = Request::post(uri)
            .header("content-type", "application/json")
            .header("authorization", format!("Bearer {token}"))
            .body(Body::from(body.to_owned()))
            .expect("请求构造必须成功");
        inject_connect_info(&mut request);
        self.oneshot(request).await.expect("oneshot 必须成功")
    }

    async fn oneshot_post_from_ip(
        self,
        ip: &str,
        uri: &str,
        body: &str,
    ) -> axum::response::Response {
        use tower::ServiceExt;
        let mut request = Request::post(uri)
            .header("content-type", "application/json")
            .body(Body::from(body.to_owned()))
            .expect("请求构造必须成功");
        let addr: std::net::SocketAddr = format!("{ip}:44444").parse().expect("测试 IP 必须合法");
        request.extensions_mut().insert(axum::extract::ConnectInfo(addr));
        self.oneshot(request).await.expect("oneshot 必须成功")
    }

    async fn oneshot_get_with_cookie(
        self,
        uri: &str,
        query: &str,
        cookie: &str,
    ) -> axum::response::Response {
        use tower::ServiceExt;
        let mut request =
            Request::get(format!("{uri}?{query}")).body(Body::empty()).expect("请求构造必须成功");
        if !cookie.is_empty() {
            request
                .headers_mut()
                .insert(axum::http::header::COOKIE, cookie.parse().expect("cookie 头合法"));
        }
        inject_connect_info(&mut request);
        self.oneshot(request).await.expect("oneshot 必须成功")
    }

    async fn oneshot_post_form(self, uri: &str, form: &str) -> axum::response::Response {
        use tower::ServiceExt;
        let mut request = Request::post(uri)
            .header("content-type", "application/x-www-form-urlencoded")
            .body(Body::from(form.to_owned()))
            .expect("请求构造必须成功");
        inject_connect_info(&mut request);
        self.oneshot(request).await.expect("oneshot 必须成功")
    }
}

/// 注入默认对端地址（限额键提取需要）。
fn inject_connect_info(request: &mut Request<Body>) {
    let addr: std::net::SocketAddr = "127.0.0.1:40000".parse().expect("默认地址必须合法");
    request.extensions_mut().insert(axum::extract::ConnectInfo(addr));
}

/// 登录后的整套会话凭据（三 cookie 明文值）。
pub struct Session {
    /// access JWT。
    pub access: String,
    /// refresh 明文。
    pub refresh: String,
    /// csrf 双提交令牌。
    pub csrf: String,
}

/// 以默认账号登录并取回会话三件套。
pub async fn login_session(app: &axum::Router) -> Session {
    login_as(app, "nova@nebula.st", "Nebula!Passw0rd-2026").await
}

/// 以指定标识与口令登录并取回会话三件套。
pub async fn login_as(app: &axum::Router, identifier: &str, password: &str) -> Session {
    let body = serde_json::json!({ "identifier": identifier, "password": password }).to_string();
    let response = app.clone().oneshot_post("/api/auth/login", &body).await;
    assert_eq!(response.status(), axum::http::StatusCode::OK, "登录助手必须成功");
    Session {
        access: set_cookie_of(&response, "nebula_access").unwrap(),
        refresh: set_cookie_of(&response, "nebula_refresh").unwrap(),
        csrf: set_cookie_of(&response, "nebula_csrf").unwrap(),
    }
}

/// 读响应体为字符串（测试断言用）。
pub async fn body_text(response: axum::response::Response) -> String {
    let bytes = http_body_util::BodyExt::collect(response.into_body())
        .await
        .expect("读体必须成功")
        .to_bytes();
    String::from_utf8_lossy(&bytes).into_owned()
}

/// 从响应的 Set-Cookie 原始头里找指定 cookie 那一条。
pub fn raw_set_cookie(response: &axum::response::Response, name: &str) -> String {
    response
        .headers()
        .get_all(axum::http::header::SET_COOKIE)
        .iter()
        .map(|v| v.to_str().unwrap_or_default())
        .find(|v| v.starts_with(name))
        .unwrap_or_default()
        .to_owned()
}
