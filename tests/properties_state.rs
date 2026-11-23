//! 状态机实现层 property 测试：真实 SQLite 上跑随机操作序列，并与参考模型对照。
//!
//! TLA+ 模型检查设计；这些用例检查 Rust/SQL 实现没有偏离同一组不变量。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use chrono::{DateTime, TimeDelta, Utc};
use nebula_server::model::tokens::NewRefreshFamily;
use nebula_server::model::user::{Ban, UserId};
use nebula_server::storage::sqlite::{
    SqliteOAuthRepo, SqliteRefreshRepo, SqliteTotpRepo, SqliteUserRepo,
};
use nebula_server::storage::{
    ClientBinding, CodeExchange, OAuthRepo, RefreshTokenRepo, RotateRefreshError, TotpRepo,
    UserRepo,
};
use proptest::prelude::*;
use sqlx::sqlite::SqlitePool;
use uuid::Uuid;

const FIXED_NOW: &str = "2026-11-01T00:00:00Z";

fn fixed_now() -> DateTime<Utc> {
    DateTime::parse_from_rfc3339(FIXED_NOW).expect("固定字面量必须可解析").with_timezone(&Utc)
}

fn runtime() -> tokio::runtime::Runtime {
    tokio::runtime::Builder::new_multi_thread()
        .worker_threads(4)
        .enable_all()
        .build()
        .expect("测试 runtime 必须可创建")
}

async fn seed_user(pool: &SqlitePool, user_id: UserId, now: DateTime<Utc>) {
    sqlx::query(
        "INSERT INTO users (id, username, email, created_at, email_verified)
         VALUES (?1, ?2, ?3, ?4, 1)",
    )
    .bind(user_id.as_uuid())
    .bind(format!("user-{user_id}"))
    .bind(format!("{user_id}@nebula.st"))
    .bind(now)
    .execute(pool)
    .await
    .expect("用户必须可插入");
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum FamilyModel {
    Active,
    Revoked,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum TokenModel {
    Active,
    Rotated,
    Revoked,
}

#[derive(Debug)]
struct Model {
    family: FamilyModel,
    tokens: Vec<(String, TokenModel)>,
    next: usize,
}

impl Model {
    fn new(initial_hash: &str) -> Self {
        Self {
            family: FamilyModel::Active,
            tokens: vec![(initial_hash.to_owned(), TokenModel::Active)],
            next: 1,
        }
    }

    fn revoke(&mut self) {
        self.family = FamilyModel::Revoked;
        for (_, state) in &mut self.tokens {
            *state = TokenModel::Revoked;
        }
    }
}

async fn seed_family(
    repo: &SqliteRefreshRepo,
    user_id: UserId,
    family_id: Uuid,
    now: DateTime<Utc>,
) {
    repo.create_family(
        &NewRefreshFamily {
            family_id,
            user_id,
            client_id: None,
            scope: String::new(),
            token_hash: "token-0".to_owned(),
            expires_at: now + TimeDelta::days(30),
        },
        now,
    )
    .await
    .expect("家族必须可创建");
}

async fn assert_db_matches(pool: &SqlitePool, family_id: Uuid, model: &Model) {
    let family_state: String =
        sqlx::query_scalar("SELECT state FROM refresh_token_families WHERE id = ?1")
            .bind(family_id)
            .fetch_one(pool)
            .await
            .expect("家族状态必须可读");
    assert_eq!(
        family_state,
        match model.family {
            FamilyModel::Active => "active",
            FamilyModel::Revoked => "revoked",
        }
    );

    let rows: Vec<(String, String)> = sqlx::query_as(
        "SELECT token_hash, state FROM refresh_tokens
         WHERE family_id = ?1 ORDER BY rowid",
    )
    .bind(family_id)
    .fetch_all(pool)
    .await
    .expect("token 行必须可读");
    assert_eq!(rows.len(), model.tokens.len(), "token 数量必须与模型一致");
    for ((db_hash, db_state), (model_hash, model_state)) in rows.iter().zip(&model.tokens) {
        assert_eq!(db_hash, model_hash, "token 顺序/哈希必须与模型一致");
        assert_eq!(
            db_state,
            match model_state {
                TokenModel::Active => "active",
                TokenModel::Rotated => "rotated",
                TokenModel::Revoked => "revoked",
            }
        );
    }

    let active: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM refresh_tokens WHERE family_id = ?1 AND state = 'active'",
    )
    .bind(family_id)
    .fetch_one(pool)
    .await
    .expect("active token 计数必须可读");
    assert!(active <= 1, "同一家族最多一个 active token");
    if model.family == FamilyModel::Revoked {
        assert_eq!(active, 0, "撤销家族不得留下 active token");
    }
}

#[derive(Debug, Clone, Copy)]
enum RefreshOp {
    Present(usize),
    Revoke,
}

fn refresh_ops() -> impl Strategy<Value = Vec<RefreshOp>> {
    prop::collection::vec(
        prop_oneof![any::<usize>().prop_map(RefreshOp::Present), Just(RefreshOp::Revoke),],
        1..=8,
    )
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(32))]

    #[test]
    fn refresh_family_matches_reference_model(ops in refresh_ops()) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = common::schema_pool(dir.path()).await;
            let user_id = UserId::new();
            let family_id = Uuid::now_v7();
            let now = fixed_now();
            seed_user(&pool, user_id, now).await;
            let repo = SqliteRefreshRepo::from_pool(pool.clone());
            seed_family(&repo, user_id, family_id, now).await;
            let mut model = Model::new("token-0");

            for op in ops {
                match op {
                    RefreshOp::Present(raw_index) => {
                        let index = raw_index % model.tokens.len();
                        let presented = model.tokens[index].0.clone();
                        let expected = match (model.family, model.tokens[index].1) {
                            (FamilyModel::Revoked, _) | (_, TokenModel::Revoked) => "invalid",
                            (_, TokenModel::Active) => "rotate",
                            (_, TokenModel::Rotated) => "replay",
                        };
                        let new_hash = format!("token-{}", model.next);
                        let result = repo
                            .rotate(
                                &presented,
                                &new_hash,
                                now + TimeDelta::days(30),
                                now,
                                ClientBinding::FirstParty,
                            )
                            .await;
                        match expected {
                            "rotate" => {
                                assert!(result.is_ok(), "active token 必须可轮转");
                                model.tokens[index].1 = TokenModel::Rotated;
                                model.tokens.push((new_hash, TokenModel::Active));
                                model.next += 1;
                            }
                            "replay" => {
                                assert!(matches!(result, Err(RotateRefreshError::Replay)), "rotated token 必须触发重放");
                                model.revoke();
                            }
                            _ => assert!(matches!(result, Err(RotateRefreshError::Invalid)), "无效呈递必须被拒绝"),
                        }
                    }
                    RefreshOp::Revoke => {
                        repo.revoke_all_sessions(&user_id, now).await.expect("撤销必须成功");
                        model.revoke();
                    }
                }
                assert_db_matches(&pool, family_id, &model).await;
            }
        });
    }
}

async fn seed_recovery_code(
    pool: &SqlitePool,
    user_id: UserId,
    code_hash: &str,
    now: DateTime<Utc>,
) {
    sqlx::query(
        "INSERT INTO totp_recovery_codes (id, user_id, code_hash, created_at)
         VALUES (?1, ?2, ?3, ?4)",
    )
    .bind(Uuid::now_v7())
    .bind(user_id.as_uuid())
    .bind(code_hash)
    .bind(now)
    .execute(pool)
    .await
    .expect("恢复码必须可插入");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn recovery_code_is_consumed_exactly_once() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = common::schema_pool(dir.path()).await;
    let user_id = UserId::new();
    let now = fixed_now();
    seed_user(&pool, user_id, now).await;
    seed_recovery_code(&pool, user_id, "recovery-hash", now).await;
    let repo = SqliteTotpRepo::from_pool(pool.clone());

    let mut set = tokio::task::JoinSet::new();
    for _ in 0..16 {
        let repo = repo.clone();
        set.spawn(async move {
            repo.consume_recovery_code(&user_id, "recovery-hash", now).await.expect("消费必须成功")
        });
    }
    let mut successes = 0;
    while let Some(result) = set.join_next().await {
        if result.expect("任务不得 panic") {
            successes += 1;
        }
    }
    assert_eq!(successes, 1, "同一恢复码只能被消费一次");
    let consumed: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM totp_recovery_codes WHERE consumed_at IS NOT NULL",
    )
    .fetch_one(&pool)
    .await
    .expect("恢复码计数必须可读");
    assert_eq!(consumed, 1);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn oauth_code_is_consumed_exactly_once() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = common::schema_pool(dir.path()).await;
    let user_id = UserId::new();
    let now = fixed_now();
    seed_user(&pool, user_id, now).await;
    sqlx::query(
        "INSERT INTO oauth_clients (id, name, redirect_uri, created_at)
         VALUES ('client-property', 'Property', 'https://app.example/cb', ?1)",
    )
    .bind(now)
    .execute(&pool)
    .await
    .expect("客户端必须可插入");
    let code_id = Uuid::now_v7();
    sqlx::query(
        "INSERT INTO oauth_authorization_codes
         (id, code_hash, client_id, user_id, redirect_uri, scope, code_challenge, expires_at, created_at)
         VALUES (?1, 'code-hash', 'client-property', ?2, 'https://app.example/cb', 'profile',
                 'challenge', ?3, ?4)",
    )
    .bind(code_id)
    .bind(user_id.as_uuid())
    .bind(now + TimeDelta::minutes(10))
    .bind(now)
    .execute(&pool)
    .await
    .expect("授权码必须可插入");
    let repo = SqliteOAuthRepo::from_pool(pool.clone());

    let mut set = tokio::task::JoinSet::new();
    for index in 0..16 {
        let repo = repo.clone();
        let family = NewRefreshFamily {
            family_id: Uuid::now_v7(),
            user_id,
            client_id: Some("client-property".to_owned()),
            scope: "profile".to_owned(),
            token_hash: format!("token-{index}"),
            expires_at: now + TimeDelta::days(30),
        };
        set.spawn(async move {
            repo.exchange_code(&code_id, now, &family).await.expect("兑换必须成功")
        });
    }
    let mut successes = 0;
    while let Some(result) = set.join_next().await {
        if matches!(result.expect("任务不得 panic"), CodeExchange::Exchanged) {
            successes += 1;
        }
    }
    assert_eq!(successes, 1, "同一授权码只能被消费一次");
    let consumed: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM oauth_authorization_codes WHERE consumed_at IS NOT NULL",
    )
    .fetch_one(&pool)
    .await
    .expect("授权码计数必须可读");
    assert_eq!(consumed, 1);
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(32))]

    #[test]
    fn temporary_ban_is_active_exactly_before_expiry(delta_secs in -600i64..=600) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = common::schema_pool(dir.path()).await;
            let user_id = UserId::new();
            let now = fixed_now();
            let expires_at = now + TimeDelta::minutes(10);
            seed_user(&pool, user_id, now).await;
            sqlx::query(
                "INSERT INTO user_bans (user_id, kind, expires_at, reason, created_at, created_by)
                 VALUES (?1, 'temporary', ?2, 'property', ?3, ?1)",
            )
            .bind(user_id.as_uuid())
            .bind(expires_at)
            .bind(now)
            .execute(&pool)
            .await
            .expect("临时封禁必须可插入");
            let repo = SqliteUserRepo::from_pool(pool.clone());
            let observed_at = expires_at + TimeDelta::seconds(delta_secs);
            let access = repo
                .find_access_state(&user_id, observed_at)
                .await
                .expect("访问状态必须可读")
                .expect("用户必须存在");
            match access.ban {
                Some(Ban::Temporary(at)) => {
                    assert_eq!(at, expires_at);
                    assert!(observed_at < expires_at);
                }
                None => assert!(observed_at >= expires_at),
                Some(Ban::Permanent) => panic!("临时封禁不得解码为永久封禁"),
            }
        });
    }
}
