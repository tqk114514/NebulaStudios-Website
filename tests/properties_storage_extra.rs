//! 存储层原子性与状态性质补充（AGENTS §8.1、§8.3）：真实临时 SQLite 上验证
//! 邮箱验证码、TOTP 登录挑战、导出令牌、政策同意、邮箱变更与社交中转态的
//! 「恰好一次」语义。
//!
//! 与 `properties_state.rs` 分工：该文件覆盖 refresh 家族、恢复码、OAuth 授权码
//! 与临时封禁；本文件覆盖其余仓储，两者不重叠。每个 case 新建临时库，连接参数
//! 与生产启动路径一致（外键、WAL、busy_timeout，ADR-0003）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::path::Path;

use chrono::{DateTime, TimeDelta, Utc};
use nebula_server::model::social::{
    SOCIAL_STATE_TTL, SocialLoginAction, SocialLoginState, SocialProvider,
};
use nebula_server::model::totp::CHALLENGE_TTL;
use nebula_server::model::user::UserId;
use nebula_server::model::verification::{
    NewEmailVerification, VERIFICATION_TTL, VerificationPurpose,
};
use nebula_server::storage::sqlite::{
    self, SqliteConsentRepo, SqliteEmailChangeRepo, SqliteEmailVerificationRepo, SqliteExportRepo,
    SqliteSocialRepo, SqliteTotpRepo,
};
use nebula_server::storage::{
    ConsentRepo, EmailChangeError, EmailChangeRepo, EmailVerificationRepo, ExportRepo, SocialRepo,
    TotpRepo,
};
use proptest::prelude::*;
use sqlx::sqlite::SqlitePool;
use uuid::Uuid;

fn fixed_now() -> DateTime<Utc> {
    DateTime::parse_from_rfc3339("2026-11-23T00:00:00Z")
        .expect("固定字面量必须可解析")
        .with_timezone(&Utc)
}

fn runtime() -> tokio::runtime::Runtime {
    tokio::runtime::Builder::new_multi_thread()
        .worker_threads(4)
        .enable_all()
        .build()
        .expect("测试 runtime 必须可创建")
}

/// 新建临时库并开启外键、WAL 与 busy timeout。
async fn fresh_pool(dir: &Path) -> SqlitePool {
    let pool = sqlite::connect_pool(&dir.join("users.db")).await.expect("测试库必须可打开");
    sqlite::ensure_schema(&pool).await.expect("schema 初始化必须成功");
    pool
}

/// 绕过口令哈希直接种用户：本文件不测口令路径，省去 argon2 开销。
async fn seed_user(
    pool: &SqlitePool,
    user_id: UserId,
    email: &str,
    email_verified: bool,
    now: DateTime<Utc>,
) {
    sqlx::query(
        "INSERT INTO users (id, username, email, created_at, email_verified)
         VALUES (?1, ?2, ?3, ?4, ?5)",
    )
    .bind(user_id.as_uuid())
    .bind(format!("user-{user_id}"))
    .bind(email)
    .bind(now)
    .bind(email_verified)
    .execute(pool)
    .await
    .expect("种子用户必须可插入");
}

/// 签发一条验证码并返回其记录 id。
async fn issue_verification(
    repo: &SqliteEmailVerificationRepo,
    user_id: UserId,
    purpose: VerificationPurpose,
    code_hash: &str,
    expires_at: DateTime<Utc>,
    now: DateTime<Utc>,
) -> Uuid {
    repo.issue(
        &NewEmailVerification { user_id, code_hash: code_hash.to_owned(), expires_at, purpose },
        now,
    )
    .await
    .expect("验证码必须可签发");
    repo.latest_unconsumed(&user_id, purpose)
        .await
        .expect("未消费验证码必须可读")
        .expect("刚签发的验证码必须存在")
        .id
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(24))]

    #[test]
    fn verification_consume_wins_exactly_once_under_concurrency(workers in 8usize..=16usize) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "verify-concurrent@nebula.st", false, now).await;
            let repo = SqliteEmailVerificationRepo::from_pool(pool.clone());
            let id = issue_verification(
                &repo,
                user_id,
                VerificationPurpose::VerifyEmail,
                "code-hash",
                now + VERIFICATION_TTL,
                now,
            )
            .await;

            let mut set = tokio::task::JoinSet::new();
            for _ in 0..workers {
                let repo = repo.clone();
                set.spawn(async move {
                    repo.consume(&id, now).await.expect("消费不得报存储故障")
                });
            }
            let mut successes = 0;
            while let Some(result) = set.join_next().await {
                if result.expect("任务不得 panic") {
                    successes += 1;
                }
            }
            assert_eq!(successes, 1, "{workers} 个并发任务争抢同一验证码，只允许一个成功");

            let consumed: i64 = sqlx::query_scalar(
                "SELECT COUNT(*) FROM email_verifications WHERE consumed_at IS NOT NULL",
            )
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
            assert_eq!(consumed, 1, "库中只允许一条消费记录");

            assert!(
                !repo.consume(&id, now).await.expect("重复消费不得报存储故障"),
                "已消费的验证码再次消费必须返回 false"
            );
            assert!(
                repo.latest_unconsumed(&user_id, VerificationPurpose::VerifyEmail)
                    .await
                    .expect("读取不得失败")
                    .is_none(),
                "已消费的验证码不得再出现在未消费集合中"
            );
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(16))]

    #[test]
    fn verification_attempts_are_counted_and_marking_verified_is_single_use(
        attempts in 1u32..=5u32,
    ) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "verify-attempts@nebula.st", false, now).await;
            let repo = SqliteEmailVerificationRepo::from_pool(pool.clone());
            let expires_at = now + VERIFICATION_TTL;
            let id = issue_verification(
                &repo,
                user_id,
                VerificationPurpose::VerifyEmail,
                "code-hash",
                expires_at,
                now,
            )
            .await;

            for _ in 0..attempts {
                repo.record_attempt(&id).await.expect("错误尝试必须可计数");
            }
            let latest = repo
                .latest_unconsumed(&user_id, VerificationPurpose::VerifyEmail)
                .await
                .expect("验证码必须可读")
                .expect("未消费验证码必须存在");
            assert_eq!(latest.attempts, attempts, "错误尝试必须逐次累加");
            assert_eq!(latest.expires_at, expires_at, "计数不得改写入库时效");

            assert!(
                repo.consume_and_mark_verified(&id, &user_id, now)
                    .await
                    .expect("消费并置位必须成功"),
                "首次验证必须成功"
            );
            assert!(
                !repo
                    .consume_and_mark_verified(&id, &user_id, now)
                    .await
                    .expect("重复验证不得报存储故障"),
                "同一验证码只能完成一次验证"
            );
            assert!(
                !repo.consume(&id, now).await.expect("重复消费不得报存储故障"),
                "已消费的验证码再次消费必须返回 false"
            );

            let verified: bool =
                sqlx::query_scalar("SELECT email_verified FROM users WHERE id = ?1")
                    .bind(user_id.as_uuid())
                    .fetch_one(&pool)
                    .await
                    .expect("用户行必须可读");
            assert!(verified, "验证成功后邮箱必须置为已验证");
            assert!(
                repo.latest_unconsumed(&user_id, VerificationPurpose::VerifyEmail)
                    .await
                    .expect("读取不得失败")
                    .is_none(),
                "消费后不得留下未消费记录"
            );
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(24))]

    #[test]
    fn totp_challenge_is_consumed_exactly_once_under_concurrency(workers in 8usize..=16usize) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "totp-challenge@nebula.st", true, now).await;
            let repo = SqliteTotpRepo::from_pool(pool.clone());

            let token_hash = "challenge-token-hash";
            let expires_at = now + CHALLENGE_TTL;
            repo.issue_challenge(&user_id, token_hash, expires_at, now)
                .await
                .expect("挑战必须可签发");
            let challenge = repo
                .find_challenge(token_hash)
                .await
                .expect("挑战必须可读")
                .expect("刚签发的挑战必须存在");
            assert_eq!(challenge.user_id, user_id);
            assert_eq!(challenge.expires_at, expires_at);
            assert_eq!(challenge.attempts, 0);

            let mut set = tokio::task::JoinSet::new();
            for _ in 0..workers {
                let repo = repo.clone();
                let id = challenge.id;
                set.spawn(async move {
                    repo.consume_challenge(&id, now).await.expect("消费不得报存储故障")
                });
            }
            let mut successes = 0;
            while let Some(result) = set.join_next().await {
                if result.expect("任务不得 panic") {
                    successes += 1;
                }
            }
            assert_eq!(successes, 1, "{workers} 个并发任务争抢同一挑战，只允许一个成功");

            let consumed: i64 = sqlx::query_scalar(
                "SELECT COUNT(*) FROM totp_login_challenges WHERE consumed_at IS NOT NULL",
            )
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
            assert_eq!(consumed, 1, "库中只允许一条消费记录");
            assert!(
                !repo
                    .consume_challenge(&challenge.id, now)
                    .await
                    .expect("重复消费不得报存储故障"),
                "已消费的挑战再次消费必须返回 false"
            );
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(16))]

    #[test]
    fn totp_challenge_attempts_are_counted(attempts in 0u32..=5u32) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "totp-attempts@nebula.st", true, now).await;
            let repo = SqliteTotpRepo::from_pool(pool.clone());

            let token_hash = "attempt-token-hash";
            repo.issue_challenge(&user_id, token_hash, now + CHALLENGE_TTL, now)
                .await
                .expect("挑战必须可签发");
            let challenge = repo
                .find_challenge(token_hash)
                .await
                .expect("挑战必须可读")
                .expect("刚签发的挑战必须存在");

            for _ in 0..attempts {
                repo.record_challenge_attempt(&challenge.id)
                    .await
                    .expect("错误尝试必须可计数");
            }
            let reread = repo
                .find_challenge(token_hash)
                .await
                .expect("挑战必须可读")
                .expect("挑战必须存在");
            assert_eq!(reread.attempts, attempts, "错误尝试必须逐次累加");
            assert_eq!(reread.expires_at, challenge.expires_at, "计数不得改写入库时效");
            assert_eq!(reread.user_id, user_id);
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(24))]

    #[test]
    fn export_reissue_invalidates_unconsumed_tokens_of_that_user(
        reissues in 1usize..=4usize,
        ttl_secs in 60i64..=3600i64,
    ) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let owner = UserId::new();
            let other = UserId::new();
            seed_user(&pool, owner, "export-owner@nebula.st", true, now).await;
            seed_user(&pool, other, "export-other@nebula.st", true, now).await;
            let repo = SqliteExportRepo::from_pool(pool.clone());

            repo.issue(&other, "other-token", now + TimeDelta::hours(1), now)
                .await
                .expect("其他用户的令牌必须可签发");

            for index in 0..reissues {
                repo.issue(
                    &owner,
                    &format!("owner-token-{index}"),
                    now + TimeDelta::seconds(ttl_secs),
                    now,
                )
                .await
                .expect("令牌必须可签发");
            }

            for index in 0..reissues - 1 {
                assert!(
                    repo.consume(&format!("owner-token-{index}"), now)
                        .await
                        .expect("消费不得报存储故障")
                        .is_none(),
                    "被新令牌作废的旧令牌必须不可消费"
                );
            }
            let rows: i64 = sqlx::query_scalar(
                "SELECT COUNT(*) FROM data_export_tokens WHERE user_id = ?1",
            )
            .bind(owner.as_uuid())
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
            assert_eq!(rows, 1, "每用户同时最多一条未消费令牌");

            let newest = format!("owner-token-{}", reissues - 1);
            assert_eq!(
                repo.consume(&newest, now).await.expect("消费不得报存储故障"),
                Some(owner),
                "最新令牌必须可消费一次"
            );
            assert!(
                repo.consume(&newest, now)
                    .await
                    .expect("重复消费不得报存储故障")
                    .is_none(),
                "同一令牌只能消费一次"
            );
            assert_eq!(
                repo.consume("other-token", now).await.expect("消费不得报存储故障"),
                Some(other),
                "作废旧令牌不得波及其他用户"
            );
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(24))]

    #[test]
    fn export_consume_wins_exactly_once_under_concurrency(
        workers in 8usize..=16usize,
        ttl_secs in 60i64..=3600i64,
    ) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let owner = UserId::new();
            seed_user(&pool, owner, "export-concurrent@nebula.st", true, now).await;
            let repo = SqliteExportRepo::from_pool(pool.clone());
            repo.issue(
                &owner,
                "export-token",
                now + TimeDelta::seconds(ttl_secs),
                now,
            )
            .await
            .expect("令牌必须可签发");

            let mut set = tokio::task::JoinSet::new();
            for _ in 0..workers {
                let repo = repo.clone();
                set.spawn(async move {
                    repo.consume("export-token", now).await.expect("消费不得报存储故障")
                });
            }
            let mut successes = 0;
            while let Some(result) = set.join_next().await {
                if let Some(user_id) = result.expect("任务不得 panic") {
                    assert_eq!(user_id, owner, "消费必须返回令牌所属用户");
                    successes += 1;
                }
            }
            assert_eq!(successes, 1, "{workers} 个并发任务争抢同一令牌，只允许一个成功");

            let consumed: i64 = sqlx::query_scalar(
                "SELECT COUNT(*) FROM data_export_tokens WHERE consumed_at IS NOT NULL",
            )
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
            assert_eq!(consumed, 1, "库中只允许一条消费记录");
            assert!(
                repo.consume("export-token", now)
                    .await
                    .expect("重复消费不得报存储故障")
                    .is_none(),
                "已消费的令牌必须不可再次消费"
            );
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(24))]

    #[test]
    fn export_consume_requires_unexpired_token(offset_secs in -600i64..=600i64) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let owner = UserId::new();
            seed_user(&pool, owner, "export-expiry@nebula.st", true, now).await;
            let repo = SqliteExportRepo::from_pool(pool.clone());
            let expires_at = now + TimeDelta::seconds(offset_secs);
            repo.issue(&owner, "boundary-token", expires_at, now)
                .await
                .expect("令牌必须可签发");

            let consumed = repo
                .consume("boundary-token", now)
                .await
                .expect("消费不得报存储故障");
            if offset_secs > 0 {
                assert_eq!(consumed, Some(owner), "未过期令牌必须可消费");
                assert!(
                    repo.consume("boundary-token", now)
                        .await
                        .expect("重复消费不得报存储故障")
                        .is_none(),
                    "未过期令牌也只能消费一次"
                );
            } else {
                assert_eq!(
                    consumed, None,
                    "过期令牌不可消费；到期瞬间（expires_at == now）同样拒绝"
                );
                let consumed_rows: i64 = sqlx::query_scalar(
                    "SELECT COUNT(*) FROM data_export_tokens WHERE consumed_at IS NOT NULL",
                )
                .fetch_one(&pool)
                .await
                .expect("计数必须可读");
                assert_eq!(consumed_rows, 0, "拒绝过期令牌不得落消费标记");
            }
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(16))]

    #[test]
    fn consent_record_is_idempotent_and_keeps_first_timestamp(repeats in 1i64..=5i64) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "consent-idempotent@nebula.st", true, now).await;
            let repo = SqliteConsentRepo::from_pool(pool.clone());

            repo.record(&user_id, "privacy", "2026-11", now)
                .await
                .expect("首次同意必须可记录");
            for index in 1..=repeats {
                repo.record(
                    &user_id,
                    "privacy",
                    "2026-11",
                    now + TimeDelta::minutes(index),
                )
                .await
                .expect("重复同意不得报存储故障");
            }

            let rows = repo.list_for_user(&user_id).await.expect("同意记录必须可读");
            assert_eq!(rows.len(), 1, "重复同意不得产生新行");
            assert_eq!(rows[0].policy, "privacy");
            assert_eq!(rows[0].version, "2026-11");
            assert_eq!(rows[0].consented_at, now, "必须保留首次同意时刻");
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(16))]

    #[test]
    fn concurrent_duplicate_consents_produce_single_row(workers in 8usize..=16usize) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "consent-concurrent@nebula.st", true, now).await;
            let repo = SqliteConsentRepo::from_pool(pool.clone());

            let mut set = tokio::task::JoinSet::new();
            for _ in 0..workers {
                let repo = repo.clone();
                set.spawn(async move {
                    repo.record(&user_id, "privacy", "2026-11", now)
                        .await
                        .expect("并发同意不得报存储故障");
                });
            }
            while let Some(result) = set.join_next().await {
                result.expect("任务不得 panic");
            }

            let rows = repo.list_for_user(&user_id).await.expect("同意记录必须可读");
            assert_eq!(rows.len(), 1, "{workers} 个并发重复同意只允许一行");
            assert_eq!(rows[0].consented_at, now);
            let count: i64 =
                sqlx::query_scalar("SELECT COUNT(*) FROM policy_consents WHERE user_id = ?1")
                    .bind(user_id.as_uuid())
                    .fetch_one(&pool)
                    .await
                    .expect("计数必须可读");
            assert_eq!(count, 1);
        });
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn consent_list_orders_by_time_then_policy_then_version() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh_pool(dir.path()).await;
    let now = fixed_now();
    let user_id = UserId::new();
    let other = UserId::new();
    seed_user(&pool, user_id, "consent-order@nebula.st", true, now).await;
    seed_user(&pool, other, "consent-order-other@nebula.st", true, now).await;
    let repo = SqliteConsentRepo::from_pool(pool.clone());

    let earlier = now - TimeDelta::hours(1);
    repo.record(&user_id, "terms", "1.0", now).await.expect("同意必须可记录");
    repo.record(&user_id, "privacy", "2026-01", now).await.expect("同意必须可记录");
    repo.record(&user_id, "privacy", "2025-12", now).await.expect("同意必须可记录");
    repo.record(&user_id, "cookies", "1.0", earlier).await.expect("同意必须可记录");
    repo.record(&other, "privacy", "2026-01", earlier).await.expect("其他用户同意必须可记录");

    let listed: Vec<(String, String)> = repo
        .list_for_user(&user_id)
        .await
        .expect("同意记录必须可读")
        .into_iter()
        .map(|row| (row.policy, row.version))
        .collect();
    assert_eq!(
        listed,
        vec![
            ("cookies".to_owned(), "1.0".to_owned()),
            ("privacy".to_owned(), "2025-12".to_owned()),
            ("privacy".to_owned(), "2026-01".to_owned()),
            ("terms".to_owned(), "1.0".to_owned()),
        ],
        "按同意时刻升序，同一时刻按政策名再按版本升序，且只含该用户的记录"
    );
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(16))]

    #[test]
    fn email_change_upsert_overwrites_and_resets_attempts(
        attempts in 0u32..=4u32,
        gap_secs in 1i64..=600i64,
    ) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "change-upsert@nebula.st", true, now).await;
            let repo = SqliteEmailChangeRepo::from_pool(pool.clone());

            let first_expires = now + TimeDelta::minutes(10);
            repo.upsert(&user_id, "first@nebula.st", "hash-first", first_expires, now)
                .await
                .expect("待确认请求必须可写入");
            for _ in 0..attempts {
                repo.record_attempt(&user_id).await.expect("错误尝试必须可计数");
            }
            let pending = repo
                .find(&user_id)
                .await
                .expect("待确认请求必须可读")
                .expect("待确认请求必须存在");
            assert_eq!(pending.new_email, "first@nebula.st");
            assert_eq!(pending.code_hash, "hash-first");
            assert_eq!(pending.attempts, attempts);
            assert_eq!(pending.expires_at, first_expires);
            assert_eq!(pending.created_at, now);

            let reissued_at = now + TimeDelta::seconds(gap_secs);
            let second_expires = reissued_at + TimeDelta::minutes(20);
            repo.upsert(
                &user_id,
                "second@nebula.st",
                "hash-second",
                second_expires,
                reissued_at,
            )
            .await
            .expect("重发必须覆盖待确认请求");
            let pending = repo
                .find(&user_id)
                .await
                .expect("待确认请求必须可读")
                .expect("待确认请求必须存在");
            assert_eq!(pending.new_email, "second@nebula.st");
            assert_eq!(pending.code_hash, "hash-second");
            assert_eq!(pending.attempts, 0, "重发必须重置尝试次数");
            assert_eq!(pending.expires_at, second_expires);
            assert_eq!(pending.created_at, reissued_at);

            let rows: i64 = sqlx::query_scalar(
                "SELECT COUNT(*) FROM email_change_requests WHERE user_id = ?1",
            )
            .bind(user_id.as_uuid())
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
            assert_eq!(rows, 1, "每用户至多一条待确认请求");
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(4))]

    #[test]
    fn email_change_apply_conflict_rolls_back_and_keeps_pending(variant in 0usize..4usize) {
        const TAKEN: [&str; 4] = [
            "taken@example.net",
            "Taken@example.net",
            "taken@Example.net",
            "TAKEN@EXAMPLE.NET",
        ];
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let owner = UserId::new();
            let holder = UserId::new();
            seed_user(&pool, owner, "change-owner@nebula.st", false, now).await;
            seed_user(&pool, holder, "taken@example.net", true, now).await;
            let repo = SqliteEmailChangeRepo::from_pool(pool.clone());

            let requested = TAKEN[variant];
            repo.upsert(&owner, requested, "hash-taken", now + TimeDelta::minutes(10), now)
                .await
                .expect("待确认请求必须可写入");
            let result = repo.apply(&owner, requested).await;
            assert!(
                matches!(&result, Err(EmailChangeError::Conflict)),
                "邮箱唯一约束冲突必须返回 Conflict，实际为 {result:?}"
            );

            let owner_row: (String, bool) =
                sqlx::query_as("SELECT email, email_verified FROM users WHERE id = ?1")
                    .bind(owner.as_uuid())
                    .fetch_one(&pool)
                    .await
                    .expect("用户行必须可读");
            assert_eq!(owner_row.0, "change-owner@nebula.st", "冲突后用户行必须回滚");
            assert!(!owner_row.1, "冲突不得把用户置为已验证");

            let pending = repo
                .find(&owner)
                .await
                .expect("待确认请求必须可读")
                .expect("冲突后待确认行必须保留");
            assert_eq!(pending.new_email, requested);
            assert_eq!(pending.code_hash, "hash-taken");

            let holder_email: String =
                sqlx::query_scalar("SELECT email FROM users WHERE id = ?1")
                    .bind(holder.as_uuid())
                    .fetch_one(&pool)
                    .await
                    .expect("占用者的用户行必须可读");
            assert_eq!(holder_email, "taken@example.net", "冲突不得改写占用者邮箱");
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(16))]

    #[test]
    fn email_change_apply_writes_new_email_and_removes_pending(
        new_email in "[a-z]{4,10}@example\\.net",
    ) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "change-success@nebula.st", false, now).await;
            let repo = SqliteEmailChangeRepo::from_pool(pool.clone());

            repo.upsert(&user_id, &new_email, "hash-new", now + TimeDelta::minutes(10), now)
                .await
                .expect("待确认请求必须可写入");
            repo.apply(&user_id, &new_email).await.expect("无冲突的变更必须成功");

            let row: (String, bool) =
                sqlx::query_as("SELECT email, email_verified FROM users WHERE id = ?1")
                    .bind(user_id.as_uuid())
                    .fetch_one(&pool)
                    .await
                    .expect("用户行必须可读");
            assert_eq!(row.0, new_email, "新邮箱必须写入用户行");
            assert!(row.1, "应用新邮箱后必须置为已验证");
            assert!(
                repo.find(&user_id)
                    .await
                    .expect("待确认请求必须可读")
                    .is_none(),
                "成功应用后必须删除待确认行"
            );
            let rows: i64 = sqlx::query_scalar(
                "SELECT COUNT(*) FROM email_change_requests WHERE user_id = ?1",
            )
            .bind(user_id.as_uuid())
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
            assert_eq!(rows, 0);
            assert!(
                matches!(
                    repo.apply(&user_id, &new_email).await,
                    Err(EmailChangeError::NotFound)
                ),
                "待确认行已删除，重复应用必须返回 NotFound"
            );
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(4))]

    #[test]
    fn email_change_apply_rejects_stale_or_missing_request_without_touching_user(
        present in any::<bool>(),
    ) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "change-stale@nebula.st", false, now).await;
            let repo = SqliteEmailChangeRepo::from_pool(pool.clone());

            if present {
                repo.upsert(
                    &user_id,
                    "pending@example.net",
                    "hash-pending",
                    now + TimeDelta::minutes(10),
                    now,
                )
                .await
                .expect("待确认请求必须可写入");
            }

            let result = repo.apply(&user_id, "different@example.net").await;
            assert!(
                matches!(&result, Err(EmailChangeError::NotFound)),
                "待确认请求缺失或与 expected 不符必须返回 NotFound，实际为 {result:?}"
            );

            let row: (String, bool) =
                sqlx::query_as("SELECT email, email_verified FROM users WHERE id = ?1")
                    .bind(user_id.as_uuid())
                    .fetch_one(&pool)
                    .await
                    .expect("用户行必须可读");
            assert_eq!(row.0, "change-stale@nebula.st", "失败应用必须回滚先行 UPDATE");
            assert!(!row.1, "失败应用不得把用户置为已验证");

            match repo.find(&user_id).await.expect("待确认请求必须可读") {
                None => assert!(!present, "原本存在的待确认行不得被失败应用删除"),
                Some(pending) => {
                    assert!(present, "原本不存在的待确认行不得被凭空创建");
                    assert_eq!(pending.new_email, "pending@example.net");
                }
            }
        });
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(16))]

    #[test]
    fn social_state_is_taken_exactly_once(
        action in prop_oneof![Just(SocialLoginAction::Login), Just(SocialLoginAction::Link)],
        provider in prop_oneof![Just(SocialProvider::Google), Just(SocialProvider::Microsoft)],
        raw_state in "[A-Za-z0-9_-]{8,24}",
    ) {
        let rt = runtime();
        rt.block_on(async {
            let dir = tempfile::tempdir().expect("临时目录必须可创建");
            let pool = fresh_pool(dir.path()).await;
            let now = fixed_now();
            let user_id = UserId::new();
            seed_user(&pool, user_id, "social-state@nebula.st", true, now).await;
            let repo = SqliteSocialRepo::from_pool(pool.clone());

            let target = match action {
                SocialLoginAction::Login => None,
                SocialLoginAction::Link => Some(user_id),
            };
            let expires_at = now + SOCIAL_STATE_TTL;
            let state = SocialLoginState {
                state: raw_state.clone(),
                provider,
                code_verifier: "pkce-verifier".to_owned(),
                action,
                user_id: target,
                expires_at,
            };
            repo.put_state(&state, now).await.expect("中转态必须可落库");

            let taken = repo
                .take_state(&raw_state)
                .await
                .expect("取出不得报存储故障")
                .expect("已落库的中转态必须可取出");
            assert_eq!(taken.state, raw_state);
            assert_eq!(taken.provider, provider);
            assert_eq!(taken.action, action);
            assert_eq!(taken.user_id, target);
            assert_eq!(taken.code_verifier, "pkce-verifier");
            assert_eq!(taken.expires_at, expires_at);

            assert!(
                repo.take_state(&raw_state)
                    .await
                    .expect("重复取出不得报存储故障")
                    .is_none(),
                "同一 state 只能消费一次"
            );
            let remaining: i64 =
                sqlx::query_scalar("SELECT COUNT(*) FROM social_login_states")
                    .fetch_one(&pool)
                    .await
                    .expect("计数必须可读");
            assert_eq!(remaining, 0, "取出即删除，库中不得残留中转态");
            assert!(
                repo.take_state("never-issued-unknown-state")
                    .await
                    .expect("未知 state 不得报存储故障")
                    .is_none(),
                "未知 state 必须返回 None"
            );
        });
    }
}
