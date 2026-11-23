//! Google/Microsoft 客户端的本地契约测试：授权 URL 参数、Worker 背书成功/失败路径。
//! Microsoft 的 token/Graph 端点是常量，故只测 `authorize_url`；Google 的代理地址可注入，
//! 用本地 mock 验证 `fetch_identity` 的完整解析链。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::sync::Arc;

use axum::extract::State;
use axum::http::StatusCode;
use axum::routing::post;
use axum::{Json, Router};
use base64::Engine as _;
use base64::engine::general_purpose::{STANDARD, URL_SAFE_NO_PAD};
use chrono::{DateTime, Utc};
use ed25519_dalek::pkcs8::EncodePublicKey as _;
use ed25519_dalek::{Signer as _, SigningKey};
use nebula_server::context::{Clock, ManualClock};
use nebula_server::social::SocialProviderClient;
use nebula_server::social::google::{GoogleClient, GoogleConfig};
use nebula_server::social::microsoft::{MicrosoftClient, MicrosoftConfig};
use serde_json::{Value, json};
use tokio::net::TcpListener;

fn fixed_now() -> DateTime<Utc> {
    DateTime::parse_from_rfc3339("2026-10-26T08:00:00Z")
        .expect("固定字面量必须可解析")
        .with_timezone(&Utc)
}

fn public_key_pem(signing: &SigningKey) -> String {
    let der = signing.verifying_key().to_public_key_der().expect("公钥 DER 必须可编码");
    let encoded = STANDARD.encode(der.as_bytes());
    let mut body = String::new();
    for chunk in encoded.as_bytes().chunks(64) {
        body.push_str(std::str::from_utf8(chunk).expect("base64 是 ASCII"));
        body.push('\n');
    }
    format!("-----BEGIN PUBLIC KEY-----\n{body}-----END PUBLIC KEY-----\n")
}

fn fake_id_token(claims: &Value) -> String {
    let payload = URL_SAFE_NO_PAD.encode(serde_json::to_vec(claims).expect("序列化必须成功"));
    format!("header.{payload}.signature")
}

fn signed_envelope(signing: &SigningKey, data: &str, timestamp: i64) -> Value {
    let message = format!("{timestamp}\n{data}");
    let signature = STANDARD.encode(signing.sign(message.as_bytes()).to_bytes());
    json!({ "data": data, "timestamp": timestamp, "signature": signature })
}

async fn token_responder(
    State((status, body)): State<(StatusCode, String)>,
) -> (StatusCode, Json<Value>) {
    (status, Json(serde_json::from_str(&body).expect("mock 响应必须是 JSON")))
}

async fn raw_token_responder(
    State((status, body)): State<(StatusCode, String)>,
) -> (StatusCode, [(axum::http::header::HeaderName, &'static str); 1], String) {
    (status, [(axum::http::header::CONTENT_TYPE, "application/json")], body)
}

async fn mock_token_server(status: StatusCode, body: Value) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("mock 端口必须可监听");
    let addr = listener.local_addr().expect("本地地址必须可读");
    let app =
        Router::new().route("/token", post(token_responder)).with_state((status, body.to_string()));
    tokio::spawn(async move {
        axum::serve(listener, app).await.expect("mock 服务不得退出");
    });
    format!("http://{addr}")
}

async fn mock_raw_token_server(status: StatusCode, body: &'static str) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("mock 端口必须可监听");
    let addr = listener.local_addr().expect("本地地址必须可读");
    let app = Router::new()
        .route("/token", post(raw_token_responder))
        .with_state((status, body.to_owned()));
    tokio::spawn(async move {
        axum::serve(listener, app).await.expect("mock 服务不得退出");
    });
    format!("http://{addr}")
}

fn google_config(proxy_url: String, pem: String) -> GoogleConfig {
    GoogleConfig {
        client_id: "google-client".to_owned(),
        client_secret: "google-secret".to_owned(),
        redirect_uri: "https://accounts.example/callback".to_owned(),
        proxy_url,
        proxy_access_client_id: "cf-id".to_owned(),
        proxy_access_client_secret: "cf-secret".to_owned(),
        worker_signing_public_key: pem,
    }
}

#[tokio::test]
async fn google_fetch_identity_parses_worker_envelope() {
    let signing = SigningKey::from_bytes(&[7u8; 32]);
    let now = fixed_now();
    let data = json!({ "id_token": fake_id_token(&json!({"sub":"google-sub","name":"Nova"})) })
        .to_string();
    let envelope = signed_envelope(&signing, &data, now.timestamp());
    let proxy = mock_token_server(StatusCode::OK, envelope).await;
    let clock: Arc<ManualClock> = Arc::new(ManualClock::new(now));
    let client = GoogleClient::new(google_config(proxy, public_key_pem(&signing)), clock)
        .expect("Google 客户端必须可构建");

    let identity = client.fetch_identity("code", "verifier").await.expect("取身份必须成功");

    assert_eq!(identity.provider_user_id, "google-sub");
    assert_eq!(identity.display_name.as_deref(), Some("Nova"));
}

#[tokio::test]
async fn google_fetch_identity_rejects_bad_worker_status_and_payload() {
    let signing = SigningKey::from_bytes(&[7u8; 32]);
    let now = fixed_now();
    let clock: Arc<ManualClock> = Arc::new(ManualClock::new(now));

    let proxy = mock_token_server(StatusCode::INTERNAL_SERVER_ERROR, json!({})).await;
    let client = GoogleClient::new(google_config(proxy, public_key_pem(&signing)), clock.clone())
        .expect("Google 客户端必须可构建");
    assert!(matches!(
        client.fetch_identity("code", "verifier").await,
        Err(nebula_server::social::SocialError::Provider(_))
    ));

    let envelope = signed_envelope(&signing, "not-json", now.timestamp());
    let proxy = mock_token_server(StatusCode::OK, envelope).await;
    let client = GoogleClient::new(google_config(proxy, public_key_pem(&signing)), clock)
        .expect("Google 客户端必须可构建");
    assert!(matches!(
        client.fetch_identity("code", "verifier").await,
        Err(nebula_server::social::SocialError::Malformed(_))
    ));
}

#[tokio::test]
async fn google_non_json_envelope_is_malformed() {
    let signing = SigningKey::from_bytes(&[7u8; 32]);
    let now = fixed_now();
    let proxy = mock_raw_token_server(StatusCode::OK, "not-an-envelope").await;
    let clock: Arc<ManualClock> = Arc::new(ManualClock::new(now));
    let client = GoogleClient::new(google_config(proxy, public_key_pem(&signing)), clock)
        .expect("Google 客户端必须可构建");

    assert!(matches!(
        client.fetch_identity("code", "verifier").await,
        Err(nebula_server::social::SocialError::Malformed(_))
    ));
}

#[tokio::test]
async fn google_fetch_identity_rejects_id_token_without_sub() {
    let signing = SigningKey::from_bytes(&[7u8; 32]);
    let now = fixed_now();
    let data = json!({ "id_token": fake_id_token(&json!({"name":"Nova"})) }).to_string();
    let envelope = signed_envelope(&signing, &data, now.timestamp());
    let proxy = mock_token_server(StatusCode::OK, envelope).await;
    let clock: Arc<ManualClock> = Arc::new(ManualClock::new(now));
    let client = GoogleClient::new(google_config(proxy, public_key_pem(&signing)), clock)
        .expect("Google 客户端必须可构建");

    assert!(matches!(
        client.fetch_identity("code", "verifier").await,
        Err(nebula_server::social::SocialError::Malformed(_))
    ));
}

#[tokio::test]
async fn authorize_urls_carry_required_parameters() {
    let clock: Arc<dyn Clock> = Arc::new(ManualClock::new(fixed_now()));
    let google = GoogleClient::new(
        google_config("https://proxy.example".to_owned(), "pem".to_owned()),
        clock,
    )
    .expect("Google 客户端必须可构建");
    let google_url = reqwest::Url::parse(&google.authorize_url("state-1", "challenge-1"))
        .expect("Google 授权 URL 必须合法");
    let google_pairs: std::collections::HashMap<_, _> =
        google_url.query_pairs().into_owned().collect();
    assert_eq!(google_url.host_str(), Some("accounts.google.com"));
    assert_eq!(google_pairs.get("client_id").map(String::as_str), Some("google-client"));
    assert_eq!(
        google_pairs.get("redirect_uri").map(String::as_str),
        Some("https://accounts.example/callback")
    );
    assert_eq!(google_pairs.get("state").map(String::as_str), Some("state-1"));
    assert_eq!(google_pairs.get("code_challenge").map(String::as_str), Some("challenge-1"));
    assert_eq!(google_pairs.get("code_challenge_method").map(String::as_str), Some("S256"));
    assert_eq!(google_pairs.get("scope").map(String::as_str), Some("openid profile"));

    let microsoft = MicrosoftClient::new(MicrosoftConfig {
        client_id: "ms-client".to_owned(),
        client_secret: "ms-secret".to_owned(),
        redirect_uri: "https://accounts.example/callback".to_owned(),
    })
    .expect("Microsoft 客户端必须可构建");
    let ms_url = reqwest::Url::parse(&microsoft.authorize_url("state-2", "challenge-2"))
        .expect("Microsoft 授权 URL 必须合法");
    let ms_pairs: std::collections::HashMap<_, _> = ms_url.query_pairs().into_owned().collect();
    assert_eq!(ms_url.host_str(), Some("login.microsoftonline.com"));
    assert_eq!(ms_pairs.get("client_id").map(String::as_str), Some("ms-client"));
    assert_eq!(ms_pairs.get("state").map(String::as_str), Some("state-2"));
    assert_eq!(ms_pairs.get("code_challenge").map(String::as_str), Some("challenge-2"));
    assert_eq!(ms_pairs.get("scope").map(String::as_str), Some("openid profile User.Read"));
}

#[tokio::test]
async fn google_token_server_outage_is_transport_error() {
    let signing = SigningKey::from_bytes(&[7u8; 32]);
    let now = fixed_now();
    let clock: Arc<ManualClock> = Arc::new(ManualClock::new(now));
    let client = GoogleClient::new(
        google_config("http://127.0.0.1:1".to_owned(), public_key_pem(&signing)),
        clock,
    )
    .expect("Google 客户端必须可构建");

    assert!(matches!(
        client.fetch_identity("code", "verifier").await,
        Err(nebula_server::social::SocialError::Transport(_))
    ));
}
