//! 管理后台、政策中心与数据导出的 HTTP 边界测试：分页/查询校验、权限与 CSRF、
//! 封禁到期边界、审计分页与形状、政策生效边界与 pending、导出令牌一次性与响应头。
//!
//! 只补现有 `tests/admin.rs`、`tests/policies.rs`、`tests/export.rs` 未覆盖的边界；
//! 全部经 `tower::ServiceExt::oneshot` 打 `Router`，不绑端口。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::collections::HashSet;
use std::sync::Arc;

use axum::Router;
use axum::http::StatusCode;
use axum::http::header::{CACHE_CONTROL, CONTENT_DISPOSITION, SET_COOKIE};
use axum::response::Response;
use chrono::{DateTime, TimeDelta, Utc};
use common::{
    OneshotPost, Session, body_text, cookie_header, login_as, login_session, register_user,
    register_verified_user,
};
use nebula_server::api;
use nebula_server::context::{Clock, ManualClock};
use nebula_server::model::export::EXPORT_TOKEN_TTL;
use nebula_server::model::policy::{Policy, PolicyCatalog, PolicyDocument, PolicyLocale};
use nebula_server::model::user::UserId;
use serde_json::Value;
use tempfile::TempDir;
use uuid::Uuid;

const PASSWORD: &str = "Nebula!Passw0rd-2026";
const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const ADMIN_EMAIL: &str = "admin@nebula.st";

fn at(value: &str) -> DateTime<Utc> {
    DateTime::parse_from_rfc3339(value).expect("固定字面量必须可解析").with_timezone(&Utc)
}

async fn json_body(response: Response) -> Value {
    serde_json::from_str(&body_text(response).await).expect("响应体必须是 JSON")
}

/// 断言状态码与稳定错误码，返回解析后的错误信封供进一步断言 `details`。
async fn assert_error(response: Response, status: StatusCode, code: &str) -> Value {
    assert_eq!(response.status(), status, "状态码必须与契约一致");
    let body = json_body(response).await;
    assert_eq!(body["code"].as_str(), Some(code), "错误码必须稳定: {body}");
    body
}

struct AdminFixture {
    app: Router,
    clock: Arc<ManualClock>,
    admin: Session,
    admin_id: UserId,
    target: UserId,
    _dir: TempDir,
}

impl AdminFixture {
    fn cookie(&self) -> String {
        cookie_header(Some(&self.admin.access), None, Some(&self.admin.csrf))
    }

    async fn get(&self, uri: &str, query: &str) -> Response {
        self.app.clone().oneshot_get_with_cookie(uri, query, &self.cookie()).await
    }

    async fn ban(&self, raw_target: &str, body: &str) -> Response {
        self.app
            .clone()
            .oneshot_post_with(
                &format!("/api/admin/users/{raw_target}/ban"),
                body,
                &self.cookie(),
                Some(&self.admin.csrf),
            )
            .await
    }

    async fn unban(&self, raw_target: &str) -> Response {
        self.app
            .clone()
            .oneshot_delete_with_cookie(
                &format!("/api/admin/users/{raw_target}/ban"),
                &self.cookie(),
                Some(&self.admin.csrf),
            )
            .await
    }
}

/// 管理员在固定时钟起点注册并登录；推进一分钟后再注册普通用户，
/// 使 `created_at` 有确定先后，分页切片可断言具体行。
async fn admin_fixture() -> AdminFixture {
    let (mut state, clock, mailbox, dir) = common::app_state().await;
    state.admin_emails = Arc::new(HashSet::from([ADMIN_EMAIL.to_owned()]));
    let app = api::router(state.clone());
    register_verified_user(&app, ADMIN_EMAIL, "admin", PASSWORD, &mailbox).await;
    let admin = login_as(&app, ADMIN_EMAIL, PASSWORD).await;
    let admin_id = state.user_repo.find_by_email(ADMIN_EMAIL).await.unwrap().unwrap().id;
    clock.advance(TimeDelta::minutes(1));
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let target = state.user_repo.find_by_email(EMAIL).await.unwrap().unwrap().id;
    AdminFixture { app, clock, admin, admin_id, target, _dir: dir }
}

#[tokio::test]
async fn admin_users_rejects_invalid_pagination_values() {
    let fixture = admin_fixture().await;

    for query in ["limit=0", "limit=-1", "limit=101"] {
        let body = assert_error(
            fixture.get("/api/admin/users", query).await,
            StatusCode::UNPROCESSABLE_ENTITY,
            "validation_failed",
        )
        .await;
        assert_eq!(body["details"]["fields"][0]["field"], "limit");
        assert_eq!(body["details"]["fields"][0]["code"], "limit_out_of_range");
    }

    let body = assert_error(
        fixture.get("/api/admin/users", "offset=-1").await,
        StatusCode::UNPROCESSABLE_ENTITY,
        "validation_failed",
    )
    .await;
    assert_eq!(body["details"]["fields"][0]["field"], "offset");
    assert_eq!(body["details"]["fields"][0]["code"], "offset_negative");

    // 非整数无法反序列化，走查询串错误而不是字段校验
    assert_error(
        fixture.get("/api/admin/users", "limit=abc").await,
        StatusCode::BAD_REQUEST,
        "malformed_query",
    )
    .await;

    // 上界 100 本值合法
    let accepted = fixture.get("/api/admin/users", "limit=100&offset=0").await;
    assert_eq!(accepted.status(), StatusCode::OK);
    let body = json_body(accepted).await;
    assert_eq!(body["total"], 2);
    assert_eq!(body["users"].as_array().expect("users 必须是数组").len(), 2);
}

#[tokio::test]
async fn admin_users_query_length_and_trim_boundaries() {
    let fixture = admin_fixture().await;

    let at_limit = format!("query={}", "a".repeat(100));
    let accepted = fixture.get("/api/admin/users", &at_limit).await;
    assert_eq!(accepted.status(), StatusCode::OK);
    assert_eq!(json_body(accepted).await["total"], 0);

    let over_limit = format!("query={}", "a".repeat(101));
    let body = assert_error(
        fixture.get("/api/admin/users", &over_limit).await,
        StatusCode::UNPROCESSABLE_ENTITY,
        "validation_failed",
    )
    .await;
    assert_eq!(body["details"]["fields"][0]["field"], "query");
    assert_eq!(body["details"]["fields"][0]["code"], "query_too_long");

    // 只有空白时 trim 后为空，等同不筛选
    let blank = fixture.get("/api/admin/users", "query=%20%20").await;
    assert_eq!(blank.status(), StatusCode::OK);
    assert_eq!(json_body(blank).await["total"], 2);
}

#[tokio::test]
async fn admin_users_pagination_slices_by_creation_time() {
    let fixture = admin_fixture().await;

    let first = fixture.get("/api/admin/users", "limit=1&offset=0").await;
    assert_eq!(first.status(), StatusCode::OK);
    let first = json_body(first).await;
    assert_eq!(first["total"], 2);
    assert_eq!(first["users"][0]["email"], EMAIL);
    assert_eq!(first["users"].as_array().expect("users 必须是数组").len(), 1);

    let second = fixture.get("/api/admin/users", "limit=1&offset=1").await;
    assert_eq!(second.status(), StatusCode::OK);
    let second = json_body(second).await;
    assert_eq!(second["total"], 2);
    assert_eq!(second["users"][0]["email"], ADMIN_EMAIL);
    assert_eq!(second["users"][0]["is_admin"], true);

    let beyond = fixture.get("/api/admin/users", "limit=1&offset=2").await;
    assert_eq!(beyond.status(), StatusCode::OK);
    let beyond = json_body(beyond).await;
    assert_eq!(beyond["total"], 2);
    assert_eq!(beyond["users"].as_array().expect("users 必须是数组").len(), 0);
}

#[tokio::test]
async fn non_admin_and_anonymous_are_rejected_on_admin_endpoints() {
    let fixture = admin_fixture().await;
    let normal = login_as(&fixture.app, EMAIL, PASSWORD).await;
    let normal_cookie = cookie_header(Some(&normal.access), None, Some(&normal.csrf));
    let ban_body = serde_json::json!({ "kind": "permanent", "reason": "nope" }).to_string();

    for uri in ["/api/admin/users", "/api/admin/audit"] {
        let response = fixture.app.clone().oneshot_get_with_cookie(uri, "", &normal_cookie).await;
        assert_error(response, StatusCode::FORBIDDEN, "forbidden").await;
    }

    let ban = fixture
        .app
        .clone()
        .oneshot_post_with(
            &format!("/api/admin/users/{}/ban", fixture.target),
            &ban_body,
            &normal_cookie,
            Some(&normal.csrf),
        )
        .await;
    assert_error(ban, StatusCode::FORBIDDEN, "forbidden").await;

    let unban = fixture
        .app
        .clone()
        .oneshot_delete_with_cookie(
            &format!("/api/admin/users/{}/ban", fixture.target),
            &normal_cookie,
            Some(&normal.csrf),
        )
        .await;
    assert_error(unban, StatusCode::FORBIDDEN, "forbidden").await;

    // 未认证在 AdminUser 判定之前就被 FirstPartyUser 拒绝
    assert_error(
        fixture.app.clone().oneshot_get_with_cookie("/api/admin/users", "", "").await,
        StatusCode::UNAUTHORIZED,
        "unauthorized",
    )
    .await;
    assert_error(
        fixture
            .app
            .clone()
            .oneshot_post_with(
                &format!("/api/admin/users/{}/ban", fixture.target),
                &ban_body,
                "",
                None,
            )
            .await,
        StatusCode::UNAUTHORIZED,
        "unauthorized",
    )
    .await;
    assert_error(
        fixture
            .app
            .clone()
            .oneshot_delete_with_cookie(
                &format!("/api/admin/users/{}/ban", fixture.target),
                "",
                None,
            )
            .await,
        StatusCode::UNAUTHORIZED,
        "unauthorized",
    )
    .await;
}

#[tokio::test]
async fn admin_write_endpoints_require_csrf() {
    let fixture = admin_fixture().await;
    let ban_body = serde_json::json!({ "kind": "permanent", "reason": "abuse" }).to_string();

    // cookie 鉴权的写端点缺 CSRF 头必须 403，且不得产生状态变化
    let without_csrf = fixture
        .app
        .clone()
        .oneshot_post_with(
            &format!("/api/admin/users/{}/ban", fixture.target),
            &ban_body,
            &fixture.cookie(),
            None,
        )
        .await;
    assert_error(without_csrf, StatusCode::FORBIDDEN, "csrf_invalid").await;
    let after_rejected = fixture.get("/api/admin/users", "query=nova").await;
    assert!(json_body(after_rejected).await["users"][0]["ban"].is_null());

    let target = fixture.target.to_string();
    let banned = fixture.ban(&target, &ban_body).await;
    assert_eq!(banned.status(), StatusCode::NO_CONTENT);

    let unban_without_csrf = fixture
        .app
        .clone()
        .oneshot_delete_with_cookie(
            &format!("/api/admin/users/{}/ban", fixture.target),
            &fixture.cookie(),
            None,
        )
        .await;
    assert_error(unban_without_csrf, StatusCode::FORBIDDEN, "csrf_invalid").await;

    let still_banned = fixture.get("/api/admin/users", "query=nova").await;
    let still_banned = json_body(still_banned).await;
    assert_eq!(still_banned["users"][0]["ban"]["kind"], "permanent");
}

#[tokio::test]
async fn ban_target_checks_precede_body_parsing_and_report_not_found() {
    let fixture = admin_fixture().await;
    let valid = serde_json::json!({ "kind": "permanent", "reason": "abuse" }).to_string();

    // 自身判定先于请求体解析：畸形 JSON 仍得到 409 而非 400
    let admin_id = fixture.admin_id.to_string();
    let self_ban = fixture.ban(&admin_id, "{").await;
    assert_error(self_ban, StatusCode::CONFLICT, "cannot_ban_self").await;

    let invalid_uuid = fixture.ban("not-a-uuid", &valid).await;
    assert_error(invalid_uuid, StatusCode::NOT_FOUND, "user_not_found").await;

    let unknown = Uuid::now_v7().to_string();
    let unknown_uuid = fixture.ban(&unknown, &valid).await;
    assert_error(unknown_uuid, StatusCode::NOT_FOUND, "user_not_found").await;
}

#[tokio::test]
async fn unban_distinguishes_unknown_user_from_missing_ban() {
    let fixture = admin_fixture().await;

    // 合法 UUID 但用户不存在：解封路径按“没有封禁记录”处理
    let unknown = Uuid::now_v7().to_string();
    let unknown_user = fixture.unban(&unknown).await;
    assert_error(unknown_user, StatusCode::NOT_FOUND, "ban_not_found").await;

    // 非法 UUID 在路径解析阶段就失败，与不存在的用户同样 404 但错误码不同
    let invalid_uuid = fixture.unban("not-a-uuid").await;
    assert_error(invalid_uuid, StatusCode::NOT_FOUND, "user_not_found").await;
}

#[tokio::test]
async fn temporary_ban_expires_at_exact_boundary() {
    let fixture = admin_fixture().await;
    let now = fixture.clock.now();

    // 到期时刻必须严格晚于当前时刻
    let equal_to_now = serde_json::json!({
        "kind": "temporary",
        "expires_at": now.to_rfc3339(),
        "reason": "edge",
    })
    .to_string();
    let target = fixture.target.to_string();
    let rejected = fixture.ban(&target, &equal_to_now).await;
    let body = assert_error(rejected, StatusCode::UNPROCESSABLE_ENTITY, "validation_failed").await;
    assert_eq!(body["details"]["fields"][0]["field"], "expires_at");
    assert_eq!(body["details"]["fields"][0]["code"], "expiry_in_past");

    let expires_at = now + TimeDelta::seconds(1);
    let accepted_body = serde_json::json!({
        "kind": "temporary",
        "expires_at": expires_at.to_rfc3339(),
        "reason": "edge",
    })
    .to_string();
    let accepted = fixture.ban(&target, &accepted_body).await;
    assert_eq!(accepted.status(), StatusCode::NO_CONTENT);

    let listed = json_body(fixture.get("/api/admin/users", "query=nova").await).await;
    assert_eq!(listed["users"][0]["ban"]["kind"], "temporary");
    let listed_expiry =
        listed["users"][0]["ban"]["expires_at"].as_str().expect("临时封禁必须带到期时刻");
    assert_eq!(at(listed_expiry), expires_at);

    // 精确到期时刻即视为失效：登录放行、列表不再展示封禁、统计不再计数
    fixture.clock.advance(TimeDelta::seconds(1));
    let fresh = login_as(&fixture.app, EMAIL, PASSWORD).await;
    assert!(!fresh.access.is_empty(), "到期后登录必须成功并签发 access token");
    let after = json_body(fixture.get("/api/admin/users", "query=nova").await).await;
    assert!(after["users"][0]["ban"].is_null());

    let stats = json_body(fixture.get("/api/admin/stats", "").await).await;
    assert_eq!(stats["banned_users"], 0);
}

#[tokio::test]
async fn audit_pagination_and_ban_entry_shape() {
    let (mut state, clock, mailbox, _dir) = common::app_state().await;
    state.admin_emails = Arc::new(HashSet::from([ADMIN_EMAIL.to_owned()]));
    let app = api::router(state.clone());
    register_verified_user(&app, ADMIN_EMAIL, "admin", PASSWORD, &mailbox).await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let admin = login_as(&app, ADMIN_EMAIL, PASSWORD).await;
    let admin_id = state.user_repo.find_by_email(ADMIN_EMAIL).await.unwrap().unwrap().id;
    let target = state.user_repo.find_by_email(EMAIL).await.unwrap().unwrap().id;
    let cookie = cookie_header(Some(&admin.access), None, Some(&admin.csrf));

    // 推进后封禁，使 ban 审计记录成为最新一条，排序断言不依赖同刻并列
    clock.advance(TimeDelta::minutes(5));
    let ban_at = clock.now();
    let ban_body =
        serde_json::json!({ "kind": "permanent", "reason": "  edge reason  " }).to_string();
    let banned = app
        .clone()
        .oneshot_post_with(
            &format!("/api/admin/users/{target}/ban"),
            &ban_body,
            &cookie,
            Some(&admin.csrf),
        )
        .await;
    assert_eq!(banned.status(), StatusCode::NO_CONTENT);

    let first =
        app.clone().oneshot_get_with_cookie("/api/admin/audit", "limit=1&offset=0", &cookie).await;
    assert_eq!(first.status(), StatusCode::OK);
    let first = json_body(first).await;
    let total = first["total"].as_i64().expect("total 必须是整数");
    assert!(total >= 5, "注册、验证、登录与封禁都应留痕，实际 total={total}");
    let entries = first["entries"].as_array().expect("entries 必须是数组");
    assert_eq!(entries.len(), 1);
    let entry = &entries[0];
    assert_eq!(entry["action"], "admin.user.ban");
    let actor = admin_id.to_string();
    let subject = target.to_string();
    assert_eq!(entry["actor_id"].as_str(), Some(actor.as_str()));
    assert_eq!(entry["target_id"].as_str(), Some(subject.as_str()));
    Uuid::parse_str(entry["id"].as_str().expect("id 必须是字符串")).expect("id 必须是 UUID");
    assert_eq!(at(entry["created_at"].as_str().expect("created_at 必须是字符串")), ban_at);
    assert_eq!(entry["details"]["kind"], "permanent");
    assert!(entry["details"]["expires_at"].is_null());
    assert_eq!(entry["details"]["reason"], "edge reason");

    let second =
        app.clone().oneshot_get_with_cookie("/api/admin/audit", "limit=1&offset=1", &cookie).await;
    assert_eq!(second.status(), StatusCode::OK);
    let second = json_body(second).await;
    assert_eq!(second["total"], total);
    let second = second["entries"].as_array().expect("entries 必须是数组");
    assert_eq!(second.len(), 1);
    assert_ne!(second[0]["action"], "admin.user.ban");

    let beyond = app
        .clone()
        .oneshot_get_with_cookie("/api/admin/audit", "limit=2&offset=1000", &cookie)
        .await;
    assert_eq!(beyond.status(), StatusCode::OK);
    let beyond = json_body(beyond).await;
    assert_eq!(beyond["total"], total);
    assert_eq!(beyond["entries"].as_array().expect("entries 必须是数组").len(), 0);

    let full = app
        .clone()
        .oneshot_get_with_cookie("/api/admin/audit", "limit=100&offset=0", &cookie)
        .await;
    assert_eq!(full.status(), StatusCode::OK);
    let full = json_body(full).await;
    assert_eq!(full["total"], total);
    let total_count = usize::try_from(total).expect("total 必须非负");
    assert_eq!(full["entries"].as_array().expect("entries 必须是数组").len(), total_count);

    assert_error(
        app.clone().oneshot_get_with_cookie("/api/admin/audit", "limit=0", &cookie).await,
        StatusCode::UNPROCESSABLE_ENTITY,
        "validation_failed",
    )
    .await;
    assert_error(
        app.clone().oneshot_get_with_cookie("/api/admin/audit", "offset=-1", &cookie).await,
        StatusCode::UNPROCESSABLE_ENTITY,
        "validation_failed",
    )
    .await;
}

fn policy(slug: &str, version: &str, effective_at: DateTime<Utc>) -> Policy {
    Policy {
        slug: slug.to_owned(),
        version: version.to_owned(),
        effective_at,
        documents: vec![
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
        ],
    }
}

async fn with_catalog(
    catalog: PolicyCatalog,
) -> (Router, Arc<ManualClock>, common::Mailbox, TempDir) {
    let (mut state, clock, mailbox, dir) = common::app_state().await;
    state.policy_catalog = Arc::new(catalog);
    (api::router(state), clock, mailbox, dir)
}

async fn record_consent(app: &Router, session: &Session, body: &str) -> Response {
    app.clone()
        .oneshot_post_with(
            "/api/user/consents",
            body,
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

async fn consents_of(app: &Router, session: &Session) -> Value {
    let response = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/user/consents",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
        )
        .await;
    assert_eq!(response.status(), StatusCode::OK);
    json_body(response).await
}

#[tokio::test]
async fn policy_effective_at_equal_now_is_current() {
    let catalog = PolicyCatalog::new(vec![
        policy("privacy", "v1", at("2026-01-01T00:00:00Z")),
        policy("privacy", "v2", at(common::FIX_NOW)),
    ])
    .expect("目录必须合法");
    let (app, _clock, _mailbox, _dir) = with_catalog(catalog).await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let manifest = app.clone().oneshot_get_with_cookie("/api/policies", "", "").await;
    assert_eq!(manifest.status(), StatusCode::OK);
    let manifest = json_body(manifest).await;
    assert_eq!(manifest["policies"][0]["version"], "v2");
    assert_eq!(manifest["policies"][0]["current"], true);
    assert_eq!(manifest["policies"][1]["version"], "v1");
    assert_eq!(manifest["policies"][1]["current"], false);

    // 旧版本已生效但不是当前版本；生效时刻恰为 now 的新版本可被接受
    let stale = record_consent(&app, &session, r#"{"policy":"privacy","version":"v1"}"#).await;
    let body = assert_error(stale, StatusCode::UNPROCESSABLE_ENTITY, "validation_failed").await;
    assert_eq!(body["details"]["fields"][0]["code"], "version_not_current");

    let current = record_consent(&app, &session, r#"{"policy":"privacy","version":"v2"}"#).await;
    assert_eq!(current.status(), StatusCode::NO_CONTENT);

    let after = consents_of(&app, &session).await;
    assert_eq!(after["consents"].as_array().expect("consents 必须是数组").len(), 1);
    assert_eq!(after["consents"][0]["version"], "v2");
    assert_eq!(after["pending"].as_array().expect("pending 必须是数组").len(), 0);
}

#[tokio::test]
async fn consents_pending_spans_slugs_and_duplicate_is_idempotent() {
    let catalog = PolicyCatalog::new(vec![
        policy("privacy", "v1", at("2026-01-01T00:00:00Z")),
        policy("terms", "v1", at("2026-01-01T00:00:00Z")),
    ])
    .expect("目录必须合法");
    let (app, clock, _mailbox, _dir) = with_catalog(catalog).await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let before = consents_of(&app, &session).await;
    assert_eq!(before["pending"].as_array().expect("pending 必须是数组").len(), 2);
    assert_eq!(before["pending"][0]["slug"], "privacy");
    assert_eq!(before["pending"][1]["slug"], "terms");

    // 前后空白被裁剪后仍命中 manifest，落库的是规范化值
    let first = record_consent(&app, &session, r#"{"policy":" privacy ","version":" v1 "}"#).await;
    assert_eq!(first.status(), StatusCode::NO_CONTENT);
    let after_first = consents_of(&app, &session).await;
    assert_eq!(after_first["consents"].as_array().expect("consents 必须是数组").len(), 1);
    assert_eq!(after_first["consents"][0]["policy"], "privacy");
    assert_eq!(after_first["consents"][0]["version"], "v1");
    assert_eq!(after_first["pending"].as_array().expect("pending 必须是数组").len(), 1);
    assert_eq!(after_first["pending"][0]["slug"], "terms");
    let recorded_at = after_first["consents"][0]["consented_at"].clone();

    // 时间推进后重复同意仍幂等：不新增行，也不改写原同意时刻
    clock.advance(TimeDelta::minutes(1));
    let repeated = record_consent(&app, &session, r#"{"policy":"privacy","version":"v1"}"#).await;
    assert_eq!(repeated.status(), StatusCode::NO_CONTENT);
    let after_repeat = consents_of(&app, &session).await;
    assert_eq!(after_repeat["consents"].as_array().expect("consents 必须是数组").len(), 1);
    assert_eq!(after_repeat["consents"][0]["consented_at"], recorded_at);

    let terms = record_consent(&app, &session, r#"{"policy":"terms","version":"v1"}"#).await;
    assert_eq!(terms.status(), StatusCode::NO_CONTENT);
    let complete = consents_of(&app, &session).await;
    assert_eq!(complete["consents"].as_array().expect("consents 必须是数组").len(), 2);
    assert_eq!(complete["consents"][0]["policy"], "privacy");
    assert_eq!(complete["consents"][1]["policy"], "terms");
    assert_eq!(complete["pending"].as_array().expect("pending 必须是数组").len(), 0);
}

#[tokio::test]
async fn future_only_catalog_yields_no_current_or_pending() {
    let catalog = PolicyCatalog::new(vec![policy("privacy", "v1", at("2027-01-01T00:00:00Z"))])
        .expect("目录必须合法");
    let (app, _clock, _mailbox, _dir) = with_catalog(catalog).await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let manifest = app.clone().oneshot_get_with_cookie("/api/policies", "", "").await;
    assert_eq!(manifest.status(), StatusCode::OK);
    let manifest = json_body(manifest).await;
    assert_eq!(manifest["policies"].as_array().expect("policies 必须是数组").len(), 1);
    assert_eq!(manifest["policies"][0]["current"], false);

    let state = consents_of(&app, &session).await;
    assert_eq!(state["consents"].as_array().expect("consents 必须是数组").len(), 0);
    assert_eq!(state["pending"].as_array().expect("pending 必须是数组").len(), 0);

    let rejected = record_consent(&app, &session, r#"{"policy":"privacy","version":"v1"}"#).await;
    let body = assert_error(rejected, StatusCode::UNPROCESSABLE_ENTITY, "validation_failed").await;
    assert_eq!(body["details"]["fields"][0]["code"], "version_not_effective");
}

#[tokio::test]
async fn anonymous_consent_read_is_unauthorized_and_empty_manifest_is_empty() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    // 部署未配置政策中心：公开 manifest 是合法空列表
    let manifest = app.clone().oneshot_get_with_cookie("/api/policies", "", "").await;
    assert_eq!(manifest.status(), StatusCode::OK);
    let manifest = json_body(manifest).await;
    assert_eq!(manifest["policies"].as_array().expect("policies 必须是数组").len(), 0);

    // 读端点同样要求认证；匿名 GET 不得返回空 consents
    let response = app.clone().oneshot_get_with_cookie("/api/user/consents", "", "").await;
    assert_error(response, StatusCode::UNAUTHORIZED, "unauthorized").await;
}

async fn request_export(app: &Router, session: &Session) -> Value {
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/user/export",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(response.status(), StatusCode::OK, "申请导出必须成功");
    json_body(response).await
}

#[tokio::test]
async fn export_download_rejects_wrong_token_lengths() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    for token in ["a".repeat(63), "a".repeat(65), "z".repeat(64)] {
        let response =
            app.clone().oneshot_get_with_cookie(&format!("/api/user/export/{token}"), "", "").await;
        assert_error(response, StatusCode::NOT_FOUND, "export_token_invalid").await;
    }
}

#[tokio::test]
async fn export_download_rejects_token_exactly_at_expiry() {
    let (app, _state, clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let issued = request_export(&app, &session).await;
    let expires_at = issued["expires_at"].as_str().expect("必须返回到期时刻");
    assert_eq!(at(expires_at), clock.now() + EXPORT_TOKEN_TTL);

    // 消费条件是 expires_at > now；恰等到期即失效（现有用例只覆盖到期后一秒）
    clock.advance(EXPORT_TOKEN_TTL);
    let download_url = issued["download_url"].as_str().expect("必须返回下载地址");
    let response = app.clone().oneshot_get_with_cookie(download_url, "", "").await;
    assert_error(response, StatusCode::NOT_FOUND, "export_token_invalid").await;
}

#[tokio::test]
async fn concurrent_export_download_consumes_token_once() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let issued = request_export(&app, &session).await;
    let download_url = issued["download_url"].as_str().expect("必须返回下载地址");

    let first = app.clone().oneshot_get_with_cookie(download_url, "", "");
    let second = app.clone().oneshot_get_with_cookie(download_url, "", "");
    let (first, second) = tokio::join!(first, second);

    let statuses = [first.status(), second.status()];
    assert_eq!(statuses.iter().filter(|status| **status == StatusCode::OK).count(), 1);
    assert_eq!(statuses.iter().filter(|status| **status == StatusCode::NOT_FOUND).count(), 1);

    let rejected = if first.status() == StatusCode::NOT_FOUND { first } else { second };
    assert_error(rejected, StatusCode::NOT_FOUND, "export_token_invalid").await;
}

#[tokio::test]
async fn export_download_headers_bind_filename_to_user() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;
    let user_id = state.user_repo.find_by_email(EMAIL).await.unwrap().unwrap().id;

    let issued = request_export(&app, &session).await;
    let download_url = issued["download_url"].as_str().expect("必须返回下载地址");

    // 下载只凭令牌，不带 cookie；响应不得回写任何会话 cookie
    let response = app.clone().oneshot_get_with_cookie(download_url, "", "").await;
    assert_eq!(response.status(), StatusCode::OK);
    let expected_disposition = format!("attachment; filename=\"nebula-export-{user_id}.json\"");
    assert_eq!(
        response.headers().get(CONTENT_DISPOSITION).and_then(|value| value.to_str().ok()),
        Some(expected_disposition.as_str())
    );
    assert_eq!(
        response.headers().get(CACHE_CONTROL).and_then(|value| value.to_str().ok()),
        Some("no-store")
    );
    assert!(response.headers().get(SET_COOKIE).is_none());
}
