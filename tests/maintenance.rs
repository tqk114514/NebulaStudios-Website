//! 过期数据清理集成测试：真实 SQLite + 注入时钟（ADR-0003、§8.1）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use nebula_server::storage::sqlite;
use sqlx::SqlitePool;

const USER_ID: &str = "018f0000-0000-7000-8000-000000000001";
const OLD_FAMILY: &str = "018f0000-0000-7000-8000-000000000002";
const OLD_TOKEN: &str = "018f0000-0000-7000-8000-000000000003";
const NEW_FAMILY: &str = "018f0000-0000-7000-8000-000000000004";
const NEW_TOKEN: &str = "018f0000-0000-7000-8000-000000000005";
const OLD_VERIFICATION: &str = "018f0000-0000-7000-8000-000000000006";
const NEW_VERIFICATION: &str = "018f0000-0000-7000-8000-000000000007";
const OLD_CHALLENGE: &str = "018f0000-0000-7000-8000-000000000008";
const NEW_CHALLENGE: &str = "018f0000-0000-7000-8000-000000000009";
const OLD_RECOVERY: &str = "018f0000-0000-7000-8000-00000000000a";
const NEW_RECOVERY: &str = "018f0000-0000-7000-8000-00000000000b";
const OLD_CODE: &str = "018f0000-0000-7000-8000-00000000000c";
const NEW_CODE: &str = "018f0000-0000-7000-8000-00000000000d";

#[tokio::test]
async fn purge_expired_deletes_only_expired_rows() {
    let (state, _clock, _mailbox, dir) = common::app_state().await;
    let pool = sqlite::connect_pool(&dir.path().join("users.db")).await.expect("测试库必须可打开");

    seed_user_and_client(&pool).await;
    seed_refresh_data(&pool).await;
    seed_verification_data(&pool).await;
    seed_totp_data(&pool).await;
    seed_oauth_data(&pool).await;
    pool.close().await;

    let report =
        state.maintenance_repo.purge_expired(state.clock.now()).await.expect("清理必须成功");

    assert_eq!(
        report,
        nebula_server::storage::CleanupReport {
            refresh_tokens: 1,
            refresh_families: 1,
            email_verifications: 1,
            totp_recovery_codes: 1,
            totp_login_challenges: 1,
            oauth_authorization_codes: 1,
        }
    );

    let pool = sqlite::connect_pool(&dir.path().join("users.db")).await.expect("测试库必须可重开");
    assert_remaining_rows(&pool).await;
    pool.close().await;
}

async fn seed_user_and_client(pool: &SqlitePool) {
    sqlx::query(
        "INSERT INTO users (id, username, email, created_at, email_verified)
         VALUES (?1, 'cleanup-user', 'cleanup@nebula.st', '2026-09-01T00:00:00+00:00', 1)",
    )
    .bind(USER_ID)
    .execute(pool)
    .await
    .expect("用户必须可插入");
    sqlx::query(
        "INSERT INTO oauth_clients (id, name, redirect_uri, created_at)
         VALUES ('client-cleanup', 'Cleanup', 'https://app.example.com/cb', '2026-09-01T00:00:00+00:00')",
    )
    .execute(pool)
    .await
    .expect("客户端必须可插入");
}

async fn seed_refresh_data(pool: &SqlitePool) {
    for (family, created_at) in
        [(OLD_FAMILY, "2026-09-01T00:00:00+00:00"), (NEW_FAMILY, "2026-10-26T08:00:00+00:00")]
    {
        sqlx::query(
            "INSERT INTO refresh_token_families (id, user_id, state, created_at, scope)
             VALUES (?1, ?2, 'active', ?3, '')",
        )
        .bind(family)
        .bind(USER_ID)
        .bind(created_at)
        .execute(pool)
        .await
        .expect("家族必须可插入");
    }
    for (id, family, hash, expires_at) in [
        (OLD_TOKEN, OLD_FAMILY, "old-token-hash", "2026-10-26T07:00:00+00:00"),
        (NEW_TOKEN, NEW_FAMILY, "new-token-hash", "2026-10-26T09:00:00+00:00"),
    ] {
        sqlx::query(
            "INSERT INTO refresh_tokens (id, family_id, token_hash, state, created_at, expires_at)
             VALUES (?1, ?2, ?3, 'active', '2026-10-26T08:00:00+00:00', ?4)",
        )
        .bind(id)
        .bind(family)
        .bind(hash)
        .bind(expires_at)
        .execute(pool)
        .await
        .expect("token 必须可插入");
    }
}

async fn seed_verification_data(pool: &SqlitePool) {
    for (id, expires_at) in [
        (OLD_VERIFICATION, "2026-10-26T07:00:00+00:00"),
        (NEW_VERIFICATION, "2026-10-26T09:00:00+00:00"),
    ] {
        sqlx::query(
            "INSERT INTO email_verifications
             (id, user_id, code_hash, attempts, expires_at, created_at, purpose)
             VALUES (?1, ?2, 'code-hash', 0, ?3, '2026-10-26T08:00:00+00:00', 'email_verify')",
        )
        .bind(id)
        .bind(USER_ID)
        .bind(expires_at)
        .execute(pool)
        .await
        .expect("验证码必须可插入");
    }
}

async fn seed_totp_data(pool: &SqlitePool) {
    for (id, expires_at, hash) in [
        (OLD_CHALLENGE, "2026-10-26T07:00:00+00:00", "old-challenge-hash"),
        (NEW_CHALLENGE, "2026-10-26T09:00:00+00:00", "new-challenge-hash"),
    ] {
        sqlx::query(
            "INSERT INTO totp_login_challenges
             (id, user_id, token_hash, attempts, expires_at, created_at)
             VALUES (?1, ?2, ?3, 0, ?4, '2026-10-26T08:00:00+00:00')",
        )
        .bind(id)
        .bind(USER_ID)
        .bind(hash)
        .bind(expires_at)
        .execute(pool)
        .await
        .expect("挑战必须可插入");
    }
    sqlx::query(
        "INSERT INTO totp_recovery_codes (id, user_id, code_hash, created_at, consumed_at)
         VALUES (?1, ?2, 'old-recovery-hash', '2026-10-01T00:00:00+00:00', '2026-10-24T00:00:00+00:00')",
    )
    .bind(OLD_RECOVERY)
    .bind(USER_ID)
    .execute(pool)
    .await
    .expect("恢复码必须可插入");
    sqlx::query(
        "INSERT INTO totp_recovery_codes (id, user_id, code_hash, created_at)
         VALUES (?1, ?2, 'new-recovery-hash', '2026-10-26T08:00:00+00:00')",
    )
    .bind(NEW_RECOVERY)
    .bind(USER_ID)
    .execute(pool)
    .await
    .expect("恢复码必须可插入");
}

async fn seed_oauth_data(pool: &SqlitePool) {
    for (id, hash, expires_at) in [
        (OLD_CODE, "old-oauth-hash", "2026-10-26T07:00:00+00:00"),
        (NEW_CODE, "new-oauth-hash", "2026-10-26T09:00:00+00:00"),
    ] {
        sqlx::query(
            "INSERT INTO oauth_authorization_codes
             (id, code_hash, client_id, user_id, redirect_uri, scope, code_challenge, expires_at, created_at)
             VALUES (?1, ?2, 'client-cleanup', ?3, 'https://app.example.com/cb', 'profile', 'challenge', ?4, '2026-10-26T08:00:00+00:00')",
        )
        .bind(id)
        .bind(hash)
        .bind(USER_ID)
        .bind(expires_at)
        .execute(pool)
        .await
        .expect("授权码必须可插入");
    }
}

async fn assert_remaining_rows(pool: &SqlitePool) {
    for (table, sql, expected) in [
        ("refresh_tokens", "SELECT COUNT(*) FROM refresh_tokens", 1_i64),
        ("refresh_token_families", "SELECT COUNT(*) FROM refresh_token_families", 1),
        ("email_verifications", "SELECT COUNT(*) FROM email_verifications", 1),
        ("totp_recovery_codes", "SELECT COUNT(*) FROM totp_recovery_codes", 1),
        ("totp_login_challenges", "SELECT COUNT(*) FROM totp_login_challenges", 1),
        ("oauth_authorization_codes", "SELECT COUNT(*) FROM oauth_authorization_codes", 1),
    ] {
        let count: i64 = sqlx::query_scalar(sql).fetch_one(pool).await.expect("计数必须可读");
        assert_eq!(count, expected, "{table} 只应保留未过期行");
    }
    let old_family: i64 =
        sqlx::query_scalar("SELECT COUNT(*) FROM refresh_token_families WHERE id = ?1")
            .bind(OLD_FAMILY)
            .fetch_one(pool)
            .await
            .expect("计数必须可读");
    assert_eq!(old_family, 0, "无 token 的旧家族必须被删除");
}
