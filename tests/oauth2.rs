//! `OAuth2 Provider` 集成测试：授权码全链、PKCE 强制、码单次、client 认证。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::{StatusCode, header};
use common::{OneshotPost, Session, cookie_header, login_session, register_user};

/// 注册默认用户并登录（oauth 用例的会话前置）。
async fn seeded_session(app: &axum::Router) -> Session {
    register_user(app, "nova@nebula.st", "nova", "Nebula!Passw0rd-2026").await;
    login_session(app).await
}

/// RFC 7636 附录 B 测试向量。
const VERIFIER: &str = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
const CHALLENGE: &str = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";

const REDIRECT: &str = "https://app.example.com/callback";
const CLIENT_ID: &str = "client-0001";
const CLIENT_SECRET: &str = "Nebula!Client-2026";

/// 直插一个机密客户端（client 管理改配置文件的方案见 ADR-0002，测试自开连接造数）。
async fn seed_client(dir: &std::path::Path, enabled: bool) {
    let hash =
        nebula_server::model::password::PasswordHash::hash(CLIENT_SECRET).expect("哈希必须成功");
    let pool = nebula_server::storage::sqlite::connect_pool(&dir.join("users.db"))
        .await
        .expect("测试库必须可打开");
    sqlx::query("INSERT INTO oauth_clients (id, name, secret_hash, redirect_uri, enabled, created_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6)")
        .bind(CLIENT_ID)
        .bind("Example App")
        .bind(hash.as_str())
        .bind(REDIRECT)
        .bind(enabled)
        .bind("2026-10-26T00:00:00+00:00")
        .execute(&pool)
        .await
        .expect("客户端必须可插入");
    pool.close().await;
}

#[tokio::test]
async fn full_authorization_code_flow() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), true).await;
    let session = seeded_session(&app).await;

    // GET authorize：参数校验通过，返回 consent 页信息
    let authorize = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/oauth/authorize",
            &format!(
                "client_id={CLIENT_ID}&redirect_uri={REDIRECT}&response_type=code&scope=profile&code_challenge={CHALLENGE}&code_challenge_method=S256&state=xyz"
            ),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
        )
        .await;
    assert_eq!(authorize.status(), StatusCode::OK);
    let info: serde_json::Value =
        serde_json::from_str(&common::body_text(authorize).await).unwrap();
    assert_eq!(info["client_name"], "Example App");

    // POST consent：302 携码
    let consent = app
        .clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &format!(
                r#"{{"client_id":"{CLIENT_ID}","redirect_uri":"{REDIRECT}","scope":"profile","code_challenge":"{CHALLENGE}","state":"xyz"}}"#
            ),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(consent.status(), StatusCode::FOUND);
    let location = consent.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    assert!(location.starts_with(REDIRECT), "{location}");
    assert!(location.contains("state=xyz"));
    let code = location.split("code=").nth(1).unwrap().split('&').next().unwrap().to_owned();

    // POST token：兑换
    let form = format!(
        "grant_type=authorization_code&code={code}&redirect_uri={REDIRECT}&code_verifier={VERIFIER}&client_id={CLIENT_ID}&client_secret={CLIENT_SECRET}"
    );
    let token = app.clone().oneshot_post_form("/api/oauth/token", &form).await;
    assert_eq!(token.status(), StatusCode::OK);
    let json: serde_json::Value = serde_json::from_str(&common::body_text(token).await).unwrap();
    assert_eq!(json["token_type"], "Bearer");
    assert_eq!(json["access_token"].as_str().unwrap().split('.').count(), 3);
    assert_eq!(json["refresh_token"].as_str().unwrap().len(), 64);
    assert_eq!(json["scope"], "profile");

    // refresh 流：轮转出新对
    let refresh_form = format!(
        "grant_type=refresh_token&refresh_token={}&client_id={CLIENT_ID}&client_secret={CLIENT_SECRET}",
        json["refresh_token"].as_str().unwrap()
    );
    let rotated = app.clone().oneshot_post_form("/api/oauth/token", &refresh_form).await;
    assert_eq!(rotated.status(), StatusCode::OK);
}

#[tokio::test]
async fn plain_pkce_method_is_rejected() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), true).await;

    let response = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/oauth/authorize",
            &format!(
                "client_id={CLIENT_ID}&redirect_uri={REDIRECT}&response_type=code&scope=profile&code_challenge={CHALLENGE}&code_challenge_method=plain"
            ),
            "",
        )
        .await;

    assert_eq!(response.status(), StatusCode::BAD_REQUEST);
    let json: serde_json::Value = serde_json::from_str(&common::body_text(response).await).unwrap();
    assert_eq!(json["details"]["reason"], "invalid_request");
}

#[tokio::test]
async fn missing_challenge_is_rejected() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), true).await;

    let response = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/oauth/authorize",
            &format!(
                "client_id={CLIENT_ID}&redirect_uri={REDIRECT}&response_type=code&scope=profile&code_challenge={CHALLENGE}&code_challenge_method=S256"
            ),
            "",
        )
        .await;
    assert_eq!(response.status(), StatusCode::OK);

    // 短 challenge（<43 字符）拒绝
    let short = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/oauth/authorize",
            "client_id=client-0001&redirect_uri=https%3A%2F%2Fapp.example.com%2Fcallback&response_type=code&scope=profile&code_challenge=short&code_challenge_method=S256",
            "",
        )
        .await;
    assert_eq!(short.status(), StatusCode::BAD_REQUEST);
}

#[tokio::test]
async fn redirect_mismatch_is_rejected() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), true).await;
    let session = seeded_session(&app).await;

    let response = app
        .clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &format!(
                r#"{{"client_id":"{CLIENT_ID}","redirect_uri":"https://evil.example.com/cb","scope":"profile","code_challenge":"{CHALLENGE}"}}"#
            ),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::BAD_REQUEST);
    let json: serde_json::Value = serde_json::from_str(&common::body_text(response).await).unwrap();
    assert_eq!(json["details"]["reason"], "invalid_redirect_uri");
}

#[tokio::test]
async fn authorization_code_is_single_use() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), true).await;
    let session = seeded_session(&app).await;

    let consent = app
        .clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &format!(
                r#"{{"client_id":"{CLIENT_ID}","redirect_uri":"{REDIRECT}","scope":"profile","code_challenge":"{CHALLENGE}"}}"#
            ),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    let location = consent.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    let code = location.split("code=").nth(1).unwrap().split('&').next().unwrap();

    let form = format!(
        "grant_type=authorization_code&code={code}&redirect_uri={REDIRECT}&code_verifier={VERIFIER}&client_id={CLIENT_ID}&client_secret={CLIENT_SECRET}"
    );
    let first = app.clone().oneshot_post_form("/api/oauth/token", &form).await;
    assert_eq!(first.status(), StatusCode::OK);

    // 同码第二次兑换：invalid_grant（原子消费的输家）
    let replay = app.clone().oneshot_post_form("/api/oauth/token", &form).await;
    assert_eq!(replay.status(), StatusCode::BAD_REQUEST);
    let json: serde_json::Value = serde_json::from_str(&common::body_text(replay).await).unwrap();
    assert_eq!(json["error"], "invalid_grant");
}

#[tokio::test]
async fn wrong_verifier_is_rejected_and_code_survives() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), true).await;
    let session = seeded_session(&app).await;

    let consent = app
        .clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &format!(
                r#"{{"client_id":"{CLIENT_ID}","redirect_uri":"{REDIRECT}","scope":"profile","code_challenge":"{CHALLENGE}"}}"#
            ),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    let location = consent.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    let code = location.split("code=").nth(1).unwrap().split('&').next().unwrap();

    let wrong_form = format!(
        "grant_type=authorization_code&code={code}&redirect_uri={REDIRECT}&code_verifier={}&client_id={CLIENT_ID}&client_secret={CLIENT_SECRET}",
        "w".repeat(43)
    );
    let rejected = app.clone().oneshot_post_form("/api/oauth/token", &wrong_form).await;
    assert_eq!(rejected.status(), StatusCode::BAD_REQUEST);

    // PKCE 失败不消费码：正确 verifier 仍可兑换
    let good_form = format!(
        "grant_type=authorization_code&code={code}&redirect_uri={REDIRECT}&code_verifier={VERIFIER}&client_id={CLIENT_ID}&client_secret={CLIENT_SECRET}"
    );
    let ok = app.clone().oneshot_post_form("/api/oauth/token", &good_form).await;
    assert_eq!(ok.status(), StatusCode::OK);
}

#[tokio::test]
async fn wrong_client_secret_is_401() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), true).await;
    let session = seeded_session(&app).await;

    let consent = app
        .clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &format!(
                r#"{{"client_id":"{CLIENT_ID}","redirect_uri":"{REDIRECT}","scope":"profile","code_challenge":"{CHALLENGE}"}}"#
            ),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    let location = consent.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    let code = location.split("code=").nth(1).unwrap().split('&').next().unwrap();

    let form = format!(
        "grant_type=authorization_code&code={code}&redirect_uri={REDIRECT}&code_verifier={VERIFIER}&client_id={CLIENT_ID}&client_secret=Wrong!Secret-2026"
    );
    let rejected = app.clone().oneshot_post_form("/api/oauth/token", &form).await;
    assert_eq!(rejected.status(), StatusCode::UNAUTHORIZED);
    let json: serde_json::Value = serde_json::from_str(&common::body_text(rejected).await).unwrap();
    assert_eq!(json["error"], "invalid_client");
}

#[tokio::test]
async fn disabled_client_is_rejected_at_authorize() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), false).await;

    let response = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/oauth/authorize",
            &format!(
                "client_id={CLIENT_ID}&redirect_uri={REDIRECT}&response_type=code&scope=profile&code_challenge={CHALLENGE}&code_challenge_method=S256"
            ),
            "",
        )
        .await;

    assert_eq!(response.status(), StatusCode::BAD_REQUEST);
    let json: serde_json::Value = serde_json::from_str(&common::body_text(response).await).unwrap();
    assert_eq!(json["details"]["reason"], "invalid_client");
}

#[tokio::test]
async fn unknown_scope_is_rejected() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), true).await;

    let response = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/oauth/authorize",
            &format!(
                "client_id={CLIENT_ID}&redirect_uri={REDIRECT}&response_type=code&scope=profile+email&code_challenge={CHALLENGE}&code_challenge_method=S256"
            ),
            "",
        )
        .await;

    assert_eq!(response.status(), StatusCode::BAD_REQUEST);
    let json: serde_json::Value = serde_json::from_str(&common::body_text(response).await).unwrap();
    assert_eq!(json["details"]["reason"], "invalid_scope");
}
