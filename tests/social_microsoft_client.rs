//! Microsoft 客户端的本地契约测试：axum mock 覆盖 token → Graph `/me` 全链路、
//! 错误分支，以及注入端点下 `authorize_url` 的参数；不触外网。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use axum::Router;
use axum::extract::{Form, State};
use axum::http::{HeaderMap, StatusCode, header};
use axum::routing::{get, post};
use nebula_server::social::SocialError;
use nebula_server::social::SocialProviderClient;
use nebula_server::social::microsoft::{MicrosoftClient, MicrosoftConfig, MicrosoftEndpoints};
use reqwest::Url;
use tokio::net::TcpListener;

const CLIENT_ID: &str = "ms-client";
const CLIENT_SECRET: &str = "ms-secret";
const REDIRECT_URI: &str = "https://accounts.example/callback";
const CODE: &str = "the-code";
const VERIFIER: &str = "the-verifier";
const ACCESS_TOKEN: &str = "graph-token";

/// mock 的响应配置；默认是成功路径。
#[derive(Clone)]
struct Replies {
    token_status: StatusCode,
    token_body: String,
    me_status: StatusCode,
    me_body: String,
}

impl Default for Replies {
    fn default() -> Self {
        Self {
            token_status: StatusCode::OK,
            token_body: format!(r#"{{"access_token":"{ACCESS_TOKEN}"}}"#),
            me_status: StatusCode::OK,
            me_body: r#"{"id":"ms-1","displayName":"Nova"}"#.to_owned(),
        }
    }
}

/// mock 捕获的请求内容，供用例主体断言。
#[derive(Default)]
struct Captured {
    token_form: Mutex<Option<HashMap<String, String>>>,
    me_authorization: Mutex<Option<String>>,
}

#[derive(Clone)]
struct MockState {
    replies: Replies,
    captured: Arc<Captured>,
}

async fn token_handler(
    State(MockState { replies, captured }): State<MockState>,
    Form(form): Form<HashMap<String, String>>,
) -> (StatusCode, String) {
    *captured.token_form.lock().expect("捕获锁不得中毒") = Some(form);
    (replies.token_status, replies.token_body)
}

async fn me_handler(
    State(MockState { replies, captured }): State<MockState>,
    headers: HeaderMap,
) -> (StatusCode, String) {
    let authorization =
        headers.get(header::AUTHORIZATION).and_then(|value| value.to_str().ok()).map(str::to_owned);
    *captured.me_authorization.lock().expect("捕获锁不得中毒") = authorization;
    (replies.me_status, replies.me_body)
}

/// 起独立 mock，返回基地址与捕获容器；端口交由内核分配。
async fn spawn_mock(replies: Replies) -> (String, Arc<Captured>) {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("mock 端口必须可监听");
    let addr = listener.local_addr().expect("本地地址必须可读");
    let captured = Arc::new(Captured::default());
    let app = Router::new()
        .route("/token", post(token_handler))
        .route("/me", get(me_handler))
        .with_state(MockState { replies, captured: Arc::clone(&captured) });
    tokio::spawn(async move {
        axum::serve(listener, app).await.expect("mock 服务不得退出");
    });
    (format!("http://{addr}"), captured)
}

fn config() -> MicrosoftConfig {
    MicrosoftConfig {
        client_id: CLIENT_ID.to_owned(),
        client_secret: CLIENT_SECRET.to_owned(),
        redirect_uri: REDIRECT_URI.to_owned(),
    }
}

fn client_for(base: &str) -> MicrosoftClient {
    MicrosoftClient::with_endpoints(
        config(),
        MicrosoftEndpoints {
            authorize: Url::parse("http://127.0.0.1:1/authorize").expect("常量 URL 必须合法"),
            token: Url::parse(&format!("{base}/token")).expect("mock token URL 必须合法"),
            graph_me: Url::parse(&format!("{base}/me")).expect("mock /me URL 必须合法"),
        },
    )
    .expect("Microsoft 客户端必须可构建")
}

fn captured_form(captured: &Captured) -> HashMap<String, String> {
    captured.token_form.lock().expect("捕获锁不得中毒").clone().expect("token 端点必须被调用")
}

fn captured_authorization(captured: &Captured) -> Option<String> {
    captured.me_authorization.lock().expect("捕获锁不得中毒").clone()
}

#[tokio::test]
async fn fetch_identity_exchanges_code_then_reads_graph_me() {
    let (base, captured) = spawn_mock(Replies::default()).await;
    let client = client_for(&base);

    let identity = client.fetch_identity(CODE, VERIFIER).await.expect("取身份必须成功");

    assert_eq!(identity.provider_user_id, "ms-1");
    assert_eq!(identity.display_name.as_deref(), Some("Nova"));

    let form = captured_form(&captured);
    assert_eq!(form.len(), 6, "token 表单只应有约定的六个字段");
    assert_eq!(form.get("client_id").map(String::as_str), Some(CLIENT_ID));
    assert_eq!(form.get("client_secret").map(String::as_str), Some(CLIENT_SECRET));
    assert_eq!(form.get("code").map(String::as_str), Some(CODE));
    assert_eq!(form.get("redirect_uri").map(String::as_str), Some(REDIRECT_URI));
    assert_eq!(form.get("grant_type").map(String::as_str), Some("authorization_code"));
    assert_eq!(form.get("code_verifier").map(String::as_str), Some(VERIFIER));

    let expected = format!("Bearer {ACCESS_TOKEN}");
    assert_eq!(captured_authorization(&captured).as_deref(), Some(expected.as_str()));
}

#[tokio::test]
async fn token_non_2xx_is_provider_error() {
    let replies = Replies {
        token_status: StatusCode::BAD_REQUEST,
        token_body: r#"{"error":"invalid_grant"}"#.to_owned(),
        ..Replies::default()
    };
    let (base, captured) = spawn_mock(replies).await;
    let client = client_for(&base);

    assert!(matches!(client.fetch_identity(CODE, VERIFIER).await, Err(SocialError::Provider(_))));
    assert_eq!(captured_authorization(&captured), None, "token 失败后不得请求 /me");
}

#[tokio::test]
async fn graph_me_non_2xx_is_provider_error() {
    let replies = Replies {
        me_status: StatusCode::UNAUTHORIZED,
        me_body: String::new(),
        ..Replies::default()
    };
    let (base, captured) = spawn_mock(replies).await;
    let client = client_for(&base);

    assert!(matches!(client.fetch_identity(CODE, VERIFIER).await, Err(SocialError::Provider(_))));
    assert_eq!(
        captured_form(&captured).get("grant_type").map(String::as_str),
        Some("authorization_code"),
        "报错前 token 端点已被请求"
    );
}

#[tokio::test]
async fn graph_me_empty_id_is_malformed() {
    let replies =
        Replies { me_body: r#"{"id":"","displayName":"Nova"}"#.to_owned(), ..Replies::default() };
    let (base, _captured) = spawn_mock(replies).await;
    let client = client_for(&base);

    assert!(matches!(client.fetch_identity(CODE, VERIFIER).await, Err(SocialError::Malformed(_))));
}

// `{}` 在 serde 反序列化阶段失败；解码失败归为 provider 违约，不是传输故障。
#[tokio::test]
async fn graph_me_missing_id_field_is_malformed() {
    let replies = Replies { me_body: "{}".to_owned(), ..Replies::default() };
    let (base, _captured) = spawn_mock(replies).await;
    let client = client_for(&base);

    assert!(matches!(client.fetch_identity(CODE, VERIFIER).await, Err(SocialError::Malformed(_))));
}

#[tokio::test]
async fn token_non_json_body_is_malformed() {
    let replies = Replies { token_body: "not-json".to_owned(), ..Replies::default() };
    let (base, captured) = spawn_mock(replies).await;
    let client = client_for(&base);

    assert!(matches!(client.fetch_identity(CODE, VERIFIER).await, Err(SocialError::Malformed(_))));
    assert_eq!(captured_authorization(&captured), None, "解析失败后不得请求 /me");
}

#[tokio::test]
async fn token_endpoint_unreachable_is_transport_error() {
    let client = MicrosoftClient::with_endpoints(
        config(),
        MicrosoftEndpoints {
            authorize: Url::parse("http://127.0.0.1:1/authorize").expect("常量 URL 必须合法"),
            token: Url::parse("http://127.0.0.1:1/token").expect("常量 URL 必须合法"),
            graph_me: Url::parse("http://127.0.0.1:1/me").expect("常量 URL 必须合法"),
        },
    )
    .expect("Microsoft 客户端必须可构建");

    assert!(matches!(client.fetch_identity(CODE, VERIFIER).await, Err(SocialError::Transport(_))));
}

#[tokio::test]
async fn authorize_url_uses_injected_endpoint_and_required_parameters() {
    let client = client_for("http://127.0.0.1:1");

    let url =
        Url::parse(&client.authorize_url("state-1", "challenge-1")).expect("授权 URL 必须合法");

    assert_eq!(url.scheme(), "http");
    assert_eq!(url.host_str(), Some("127.0.0.1"));
    assert_eq!(url.port(), Some(1));
    assert_eq!(url.path(), "/authorize");

    let pairs: HashMap<_, _> = url.query_pairs().into_owned().collect();
    assert_eq!(pairs.len(), 9, "授权 URL 参数个数应固定");
    assert_eq!(pairs.get("client_id").map(String::as_str), Some(CLIENT_ID));
    assert_eq!(pairs.get("redirect_uri").map(String::as_str), Some(REDIRECT_URI));
    assert_eq!(pairs.get("response_type").map(String::as_str), Some("code"));
    assert_eq!(pairs.get("scope").map(String::as_str), Some("openid profile User.Read"));
    assert_eq!(pairs.get("response_mode").map(String::as_str), Some("query"));
    assert_eq!(pairs.get("state").map(String::as_str), Some("state-1"));
    assert_eq!(pairs.get("code_challenge").map(String::as_str), Some("challenge-1"));
    assert_eq!(pairs.get("code_challenge_method").map(String::as_str), Some("S256"));
    assert_eq!(pairs.get("prompt").map(String::as_str), Some("select_account"));
}

#[tokio::test]
async fn graph_me_without_display_name_yields_none() {
    let replies = Replies { me_body: r#"{"id":"ms-1"}"#.to_owned(), ..Replies::default() };
    let (base, _captured) = spawn_mock(replies).await;
    let client = client_for(&base);

    let identity = client.fetch_identity(CODE, VERIFIER).await.expect("取身份必须成功");

    assert_eq!(identity.provider_user_id, "ms-1");
    assert_eq!(identity.display_name, None);
}
