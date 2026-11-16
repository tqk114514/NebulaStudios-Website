//! 政策中心集成测试：公开 manifest、当前版本标记、同意记录与版本校验。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::sync::Arc;

use axum::http::StatusCode;
use chrono::{DateTime, Utc};
use common::{OneshotPost, Session, body_text, cookie_header, login_session, register_user};
use nebula_server::api;
use nebula_server::model::policy::{Policy, PolicyCatalog, PolicyDocument, PolicyLocale};

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

fn at(value: &str) -> DateTime<Utc> {
    DateTime::parse_from_rfc3339(value).expect("固定字面量必须可解析").with_timezone(&Utc)
}

fn documents(slug: &str, version: &str) -> Vec<PolicyDocument> {
    vec![
        PolicyDocument {
            locale: PolicyLocale::ZhCn,
            title: "隐私政策".to_owned(),
            path: format!("/policies/{slug}/{version}/zh-CN"),
        },
        PolicyDocument {
            locale: PolicyLocale::En,
            title: "Privacy Policy".to_owned(),
            path: format!("/policies/{slug}/{version}/en"),
        },
    ]
}

fn policy(version: &str, effective_at: DateTime<Utc>) -> Policy {
    Policy {
        slug: "privacy".to_owned(),
        version: version.to_owned(),
        effective_at,
        documents: documents("privacy", version),
    }
}

fn single_version_catalog() -> PolicyCatalog {
    PolicyCatalog::new(vec![policy("v1", at("2026-01-01T00:00:00Z"))]).expect("目录必须合法")
}

fn multi_version_catalog() -> PolicyCatalog {
    PolicyCatalog::new(vec![
        policy("v1", at("2026-01-01T00:00:00Z")),
        policy("v2", at("2026-10-01T00:00:00Z")),
        policy("v3", at("2027-01-01T00:00:00Z")),
    ])
    .expect("目录必须合法")
}

async fn with_catalog(
    catalog: PolicyCatalog,
) -> (axum::Router, common::Mailbox, tempfile::TempDir) {
    let (mut state, _clock, mailbox, dir) = common::app_state().await;
    state.policy_catalog = Arc::new(catalog);
    (api::router(state), mailbox, dir)
}

async fn consents(app: &axum::Router, session: &Session) -> serde_json::Value {
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    let response = app.clone().oneshot_get_with_cookie("/api/user/consents", "", &cookie).await;
    assert_eq!(response.status(), StatusCode::OK);
    serde_json::from_str(&body_text(response).await).expect("同意响应必须是 JSON")
}

async fn record_consent(
    app: &axum::Router,
    session: &Session,
    body: &str,
) -> axum::response::Response {
    app.clone()
        .oneshot_post_with(
            "/api/user/consents",
            body,
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

#[tokio::test]
async fn manifest_lists_all_versions_with_current_flag() {
    let (app, _mailbox, _dir) = with_catalog(multi_version_catalog()).await;

    let response = app.clone().oneshot_get_with_cookie("/api/policies", "", "").await;
    assert_eq!(response.status(), StatusCode::OK);
    let json: serde_json::Value =
        serde_json::from_str(&body_text(response).await).expect("manifest 必须是 JSON");
    let policies = json["policies"].as_array().expect("policies 必须是数组");
    assert_eq!(policies.len(), 3);
    // 排序：同一 slug 下生效时刻降序
    assert_eq!(policies[0]["version"], "v3");
    assert_eq!(policies[1]["version"], "v2");
    assert_eq!(policies[2]["version"], "v1");
    assert_eq!(policies[0]["current"], false);
    assert_eq!(policies[1]["current"], true);
    assert_eq!(policies[2]["current"], false);
    assert_eq!(policies[1]["documents"][0]["locale"], "zh-CN");
    assert_eq!(policies[1]["documents"][1]["locale"], "en");
}

#[tokio::test]
async fn consent_flow_records_current_version_and_clears_pending() {
    let (app, _mailbox, _dir) = with_catalog(single_version_catalog()).await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let before = consents(&app, &session).await;
    assert_eq!(before["consents"].as_array().expect("consents 必须是数组").len(), 0);
    assert_eq!(before["pending"][0]["slug"], "privacy");
    assert_eq!(before["pending"][0]["version"], "v1");
    assert_eq!(
        before["pending"][0]["documents"].as_array().expect("documents 必须是数组").len(),
        2
    );

    let recorded = record_consent(&app, &session, r#"{"policy":"privacy","version":"v1"}"#).await;
    assert_eq!(recorded.status(), StatusCode::NO_CONTENT);

    let after = consents(&app, &session).await;
    assert_eq!(after["consents"][0]["policy"], "privacy");
    assert_eq!(after["consents"][0]["version"], "v1");
    assert_eq!(after["pending"].as_array().expect("pending 必须是数组").len(), 0);

    // 幂等：重复同意不产生第二行，也不报错
    let repeated = record_consent(&app, &session, r#"{"policy":"privacy","version":"v1"}"#).await;
    assert_eq!(repeated.status(), StatusCode::NO_CONTENT);
    let again = consents(&app, &session).await;
    assert_eq!(again["consents"].as_array().expect("consents 必须是数组").len(), 1);
}

#[tokio::test]
async fn consent_rejects_unknown_stale_and_future_versions() {
    let (app, _mailbox, _dir) = with_catalog(multi_version_catalog()).await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let stale = record_consent(&app, &session, r#"{"policy":"privacy","version":"v1"}"#).await;
    assert_eq!(stale.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(stale).await.contains("version_not_current"));

    let future = record_consent(&app, &session, r#"{"policy":"privacy","version":"v3"}"#).await;
    assert_eq!(future.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(future).await.contains("version_not_effective"));

    let unknown_policy =
        record_consent(&app, &session, r#"{"policy":"ghost","version":"v1"}"#).await;
    assert_eq!(unknown_policy.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(unknown_policy).await.contains("policy_unknown"));

    let unknown_version =
        record_consent(&app, &session, r#"{"policy":"privacy","version":"v9"}"#).await;
    assert_eq!(unknown_version.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(unknown_version).await.contains("version_unknown"));

    let missing = record_consent(&app, &session, r#"{"policy":"","version":""}"#).await;
    assert_eq!(missing.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(missing).await.contains("policy_missing"));
}

#[tokio::test]
async fn consent_write_requires_authentication_and_csrf() {
    let (app, _mailbox, _dir) = with_catalog(single_version_catalog()).await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let anonymous = app
        .clone()
        .oneshot_post("/api/user/consents", r#"{"policy":"privacy","version":"v1"}"#)
        .await;
    assert_eq!(anonymous.status(), StatusCode::UNAUTHORIZED);

    let without_csrf = app
        .clone()
        .oneshot_post_with(
            "/api/user/consents",
            r#"{"policy":"privacy","version":"v1"}"#,
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            None,
        )
        .await;
    assert_eq!(without_csrf.status(), StatusCode::FORBIDDEN);
    assert!(body_text(without_csrf).await.contains("csrf_invalid"));
}
