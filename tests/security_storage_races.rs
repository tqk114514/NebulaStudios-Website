//! 存储层对抗性并发测试（AGENTS §8.1、§8.3、§9）：refresh 轮转与重放、原子消费、
//! TOCTOU、唯一约束与事务边界。
//!
//! 每个用例独立临时文件库，连接路径与生产一致（`connect_pool`：WAL、
//! busy_timeout、外键；ADR-0003），不用 `:memory:`。断言表达存储层应成立的安全与
//! 一致性不变量——失败即视为缺陷。无法凭单次仓储调用判定的缺陷，按调用方
//! （`src/api/**`）的真实调用序列强制交错复现 TOCTOU，用例注释给出对应行号。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::path::Path;

use chrono::{DateTime, TimeDelta, Utc};
use nebula_server::avatar::AvatarStore;
use nebula_server::model::audit::{AuditAction, NewAuditEntry};
use nebula_server::model::oauth2::{NewAuthorizationCode, OAuthClient};
use nebula_server::model::password::PasswordHash;
use nebula_server::model::social::{SocialLoginAction, SocialLoginState, SocialProvider};
use nebula_server::model::tokens::NewRefreshFamily;
use nebula_server::model::totp::{CHALLENGE_MAX_ATTEMPTS, CHALLENGE_TTL};
use nebula_server::model::user::{Ban, User, UserId};
use nebula_server::model::verification::{
    MAX_ATTEMPTS, NewEmailVerification, VERIFICATION_TTL, VerificationPurpose,
};
use nebula_server::storage::sqlite::{
    self, SqliteAccountRepo, SqliteAdminRepo, SqliteEmailChangeRepo, SqliteEmailVerificationRepo,
    SqliteExportRepo, SqliteOAuthRepo, SqliteRefreshRepo, SqliteSocialRepo, SqliteTotpRepo,
    SqliteUserRepo,
};
use nebula_server::storage::{
    AccountRepo, AdminRepo, ClientBinding, CodeExchange, EmailChangeError, EmailChangeRepo,
    EmailVerificationRepo, ExportRepo, OAuthRepo, RefreshTokenRepo, RotateRefreshError, SocialRepo,
    TotpRepo, UserRepo, UserRepoError,
};
use sqlx::sqlite::SqlitePool;
use uuid::Uuid;

/// 用例统一时钟；全部时间判断经显式参数注入（§2 时钟注入）。
fn now() -> DateTime<Utc> {
    DateTime::parse_from_rfc3339("2026-11-24T00:00:00Z")
        .expect("固定字面量必须可解析")
        .with_timezone(&Utc)
}

async fn fresh(dir: &Path) -> SqlitePool {
    let pool = sqlite::connect_pool(&dir.join("users.db")).await.expect("测试库必须可打开");
    sqlite::ensure_schema(&pool).await.expect("schema 初始化必须成功");
    pool
}

/// 绕过口令哈希直接种用户行：并发用例不需要 argon2 成本。
async fn seed_user(
    pool: &SqlitePool,
    user_id: UserId,
    username: &str,
    email: &str,
    verified: bool,
) {
    sqlx::query(
        "INSERT INTO users (id, username, email, created_at, email_verified)
         VALUES (?1, ?2, ?3, ?4, ?5)",
    )
    .bind(user_id.as_uuid())
    .bind(username)
    .bind(email)
    .bind(now())
    .bind(verified)
    .execute(pool)
    .await
    .expect("种子用户必须可插入");
}

/// 低参数 Argon2id PHC 串：仅用于需要 `PasswordHash` 类型、但不校验口令的用例。
fn cheap_hash() -> PasswordHash {
    PasswordHash::from_encoded(
        "$argon2id$v=19$m=8,t=1,p=1$c29tZXNhbHQ$YWJjZGVmZ2hpamtsbW5vcA".to_owned(),
    )
    .expect("测试 PHC 串必须可解析")
}

fn user_row(id: UserId, username: &str, email: &str) -> User {
    User {
        id,
        username: username.to_owned(),
        email: email.to_owned(),
        created_at: now(),
        email_verified: false,
        avatar_version: 0,
    }
}

fn family(user_id: UserId, token_hash: &str, client_id: Option<&str>) -> NewRefreshFamily {
    NewRefreshFamily {
        family_id: Uuid::now_v7(),
        user_id,
        client_id: client_id.map(ToOwned::to_owned),
        scope: client_id.map_or_else(String::new, |_| "profile".to_owned()),
        token_hash: token_hash.to_owned(),
        expires_at: now() + TimeDelta::days(30),
    }
}

fn admin_audit(actor: UserId, target: UserId, action: AuditAction) -> NewAuditEntry {
    NewAuditEntry { actor_id: actor, action, target_id: Some(target), details: None }
}

async fn family_state(pool: &SqlitePool, family_id: Uuid) -> Option<String> {
    sqlx::query_scalar::<_, String>("SELECT state FROM refresh_token_families WHERE id = ?1")
        .bind(family_id)
        .fetch_optional(pool)
        .await
        .expect("家族状态必须可读")
}

async fn active_tokens(pool: &SqlitePool, family_id: Uuid) -> i64 {
    sqlx::query_scalar::<_, i64>(
        "SELECT COUNT(*) FROM refresh_tokens WHERE family_id = ?1 AND state = 'active'",
    )
    .bind(family_id)
    .fetch_one(pool)
    .await
    .expect("token 计数必须可读")
}

async fn token_state(pool: &SqlitePool, token_hash: &str) -> Option<String> {
    sqlx::query_scalar::<_, String>("SELECT state FROM refresh_tokens WHERE token_hash = ?1")
        .bind(token_hash)
        .fetch_optional(pool)
        .await
        .expect("token 状态必须可读")
}

async fn email_of(pool: &SqlitePool, user_id: UserId) -> Option<(String, bool)> {
    sqlx::query_as::<_, (String, bool)>("SELECT email, email_verified FROM users WHERE id = ?1")
        .bind(user_id.as_uuid())
        .fetch_optional(pool)
        .await
        .expect("用户行必须可读")
}

/// 手写互斥的轮转结果分类；用于把 500（存储故障）与业务拒绝分开断言。
#[derive(Debug, Default)]
struct RotateOutcome {
    ok: usize,
    replay: usize,
    invalid: usize,
    mismatch: usize,
    storage: Vec<String>,
}

fn classify_rotate(outcome: &mut RotateOutcome, result: Result<(), RotateRefreshError>) {
    match result {
        Ok(()) => outcome.ok += 1,
        Err(RotateRefreshError::Replay) => outcome.replay += 1,
        Err(RotateRefreshError::Invalid) => outcome.invalid += 1,
        Err(RotateRefreshError::ClientMismatch) => outcome.mismatch += 1,
        Err(RotateRefreshError::Storage(err)) => outcome.storage.push(format!("{err:?}")),
    }
}

// ---------------------------------------------------------------------------
// 1. 同一 refresh token 并发轮转
// ---------------------------------------------------------------------------

/// 攻击：同一 refresh token 并发提交（合法客户端双击，或持有同一 cookie 的两个
/// 请求）。不变量：至多一个成功，家族内至多一个 active token。
/// `rotate` 的事务以 SELECT 开头（DEFERRED 事务，`src/storage/sqlite.rs:505-596`），
/// 并发写冲突会以 `SQLITE_BUSY` 呈现为 500 而非 `Replay`；该退化路径由
/// `sec_store_30` 确定性复现，本用例只断言「不会双重轮转」。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_01_refresh_same_token_concurrent_rotation_single_winner() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-rot", "race-rot@nebula.st", false).await;
    let repo = SqliteRefreshRepo::from_pool(pool.clone());
    let entry = family(user, "t0-held-by-both", None);
    let family_id = entry.family_id;
    repo.create_family(&entry, now()).await.expect("建族必须成功");

    let mut set = tokio::task::JoinSet::new();
    for index in 0..8u32 {
        let repo = repo.clone();
        set.spawn(async move {
            repo.rotate(
                "t0-held-by-both",
                &format!("replacement-{index}"),
                now() + TimeDelta::days(30),
                now(),
                ClientBinding::FirstParty,
            )
            .await
        });
    }
    let mut outcome = RotateOutcome::default();
    while let Some(result) = set.join_next().await {
        let result = result.expect("任务不得 panic");
        // 只保留成功/失败类别；成功结果本身在下面用库状态校验
        classify_rotate(&mut outcome, result.map(|_| ()));
    }

    assert!(
        outcome.ok <= 1,
        "同一 refresh token 并发轮转出现 {} 次成功（双重轮转）：{outcome:?}",
        outcome.ok
    );
    assert!(
        active_tokens(&pool, family_id).await <= 1,
        "家族内出现多个 active token：双重轮转已落库（{outcome:?}）"
    );
}

/// 攻击：持有旧 token 的攻击者在合法客户端轮转链上并发重放。
/// 不变量：重放必须被识别为 `Replay`（不能退化成 500），且整族立即撤销——
/// 撤销后链上任何 token 都不能再轮转（`src/storage/sqlite.rs:552-577`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_02_refresh_replay_revokes_family_under_concurrent_rotation() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-replay", "race-replay@nebula.st", false).await;
    let repo = SqliteRefreshRepo::from_pool(pool.clone());
    let entry = family(user, "gen-1", None);
    let family_id = entry.family_id;
    repo.create_family(&entry, now()).await.expect("建族必须成功");
    repo.rotate("gen-1", "gen-2", now() + TimeDelta::days(30), now(), ClientBinding::FirstParty)
        .await
        .expect("首次轮转必须成功");

    let attacker = repo.clone();
    let legitimate = repo.clone();
    let (replay, concurrent) = tokio::join!(
        attacker.rotate(
            "gen-1",
            "attacker-gen",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        ),
        legitimate.rotate(
            "gen-2",
            "legit-gen-3",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        ),
    );

    // 重放本身不能换到令牌；若并发写冲突使其退化成 500，则顺序重试必须检出
    let replay_note = match &replay {
        Ok(_) => panic!("重放的旧 token 换到了新令牌：{replay:?}"),
        Err(RotateRefreshError::Replay) => "首次即判定重放".to_owned(),
        Err(RotateRefreshError::Storage(err)) => {
            let retried = repo
                .rotate(
                    "gen-1",
                    "attacker-retry",
                    now() + TimeDelta::days(30),
                    now(),
                    ClientBinding::FirstParty,
                )
                .await;
            assert!(
                matches!(retried, Err(RotateRefreshError::Replay)),
                "并发下重放退化为 500，且顺序重试仍未检出：{retried:?}（首次 {err:?}）"
            );
            format!("首次退化为存储故障（{err:?}），重试检出重放")
        }
        other => panic!("重放返回非预期结果：{other:?}"),
    };
    assert_eq!(
        family_state(&pool, family_id).await.as_deref(),
        Some("revoked"),
        "判定重放后家族必须立即撤销（{replay_note}）"
    );
    assert_eq!(
        active_tokens(&pool, family_id).await,
        0,
        "撤销后不得残留 active token（含并发轮转刚插入的新代；{replay_note}）"
    );
    let after = repo
        .rotate("gen-2", "later", now() + TimeDelta::days(30), now(), ClientBinding::FirstParty)
        .await;
    assert!(
        after.is_err(),
        "重放撤销后链上 token 仍可轮转：{after:?}（并发轮转结果={concurrent:?}）"
    );
}

// ---------------------------------------------------------------------------
// 2. 验证码 / TOTP 挑战的尝试上限
// ---------------------------------------------------------------------------

/// 复刻 `src/api/auth.rs:130-158` 的校验序列：读 attempts → 判定上限 → 计一次错误。
async fn admit_verification_guess(repo: &SqliteEmailVerificationRepo, user: &UserId) -> bool {
    let Some(verification) =
        repo.latest_unconsumed(user, VerificationPurpose::VerifyEmail).await.expect("读取不得失败")
    else {
        return false;
    };
    repo.reserve_attempt(&verification.id, MAX_ATTEMPTS).await.expect("预占不得失败")
}

/// 攻击：并发提交错误验证码。不变量：同一验证码最多放行 `MAX_ATTEMPTS` 次猜码。
/// 现状是「读 attempts → 判定 → 无条件 +1」（`src/storage/sqlite.rs:746-752`），
/// 检查与计数不在同一语句，并发读会同时看到低计数。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_03_verification_attempt_limit_survives_concurrency() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-attempts", "race-attempts@nebula.st", false).await;
    let repo = SqliteEmailVerificationRepo::from_pool(pool.clone());
    repo.issue(
        &NewEmailVerification {
            user_id: user,
            code_hash: "hashed-code".to_owned(),
            expires_at: now() + VERIFICATION_TTL,
            purpose: VerificationPurpose::VerifyEmail,
            sent_to: "race-attempts@nebula.st".to_owned(),
        },
        now(),
    )
    .await
    .expect("验证码必须可签发");
    let verification_id = repo
        .latest_unconsumed(&user, VerificationPurpose::VerifyEmail)
        .await
        .expect("读取不得失败")
        .expect("刚签发的验证码必须存在")
        .id;

    // 阶段 A：8 个并发预占；原子条件更新只允许前 5 个成功
    let writes = tokio::join!(
        repo.reserve_attempt(&verification_id, MAX_ATTEMPTS),
        repo.reserve_attempt(&verification_id, MAX_ATTEMPTS),
        repo.reserve_attempt(&verification_id, MAX_ATTEMPTS),
        repo.reserve_attempt(&verification_id, MAX_ATTEMPTS),
        repo.reserve_attempt(&verification_id, MAX_ATTEMPTS),
        repo.reserve_attempt(&verification_id, MAX_ATTEMPTS),
        repo.reserve_attempt(&verification_id, MAX_ATTEMPTS),
        repo.reserve_attempt(&verification_id, MAX_ATTEMPTS),
    );
    let forced_admitted =
        [writes.0, writes.1, writes.2, writes.3, writes.4, writes.5, writes.6, writes.7]
            .into_iter()
            .filter(|result| *result.as_ref().expect("预占不得失败"))
            .count();

    // 阶段 B：另发一张码，24 个并发请求各自执行 handler 的完整读-判-写序列
    repo.consume(&verification_id, now()).await.expect("关闭旧码不得报存储故障");
    repo.issue(
        &NewEmailVerification {
            user_id: user,
            code_hash: "second-code".to_owned(),
            expires_at: now() + VERIFICATION_TTL,
            purpose: VerificationPurpose::VerifyEmail,
            sent_to: "race-attempts@nebula.st".to_owned(),
        },
        now(),
    )
    .await
    .expect("第二张验证码必须可签发");
    let mut set = tokio::task::JoinSet::new();
    for _ in 0..24 {
        let repo = repo.clone();
        set.spawn(async move { admit_verification_guess(&repo, &user).await });
    }
    let mut stress_admitted = 0;
    while let Some(result) = set.join_next().await {
        if result.expect("任务不得 panic") {
            stress_admitted += 1;
        }
    }

    assert!(
        stress_admitted <= MAX_ATTEMPTS as usize && forced_admitted <= MAX_ATTEMPTS as usize,
        "验证码尝试上限可被并发绕过：固定交错放行 {forced_admitted} 次、并发压力放行 \
         {stress_admitted} 次，上限 {MAX_ATTEMPTS}；检查与计数非原子"
    );
}

/// 复刻 `src/api/auth/login_totp.rs:49-101` 的挑战尝试序列。
async fn admit_challenge_guess(repo: &SqliteTotpRepo, token_hash: &str) -> bool {
    let Some(challenge) = repo.find_challenge(token_hash).await.expect("读取不得失败") else {
        return false;
    };
    if challenge.expires_at <= now() {
        return false;
    }
    repo.reserve_challenge_attempt(&challenge.id, CHALLENGE_MAX_ATTEMPTS)
        .await
        .expect("预占不得失败")
}

/// 攻击：同一登录挑战上并发爆破 6 位 TOTP。不变量：挑战最多放行
/// `CHALLENGE_MAX_ATTEMPTS` 次猜码。1e6 码空间内，上限被放大即直接提高命中率。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_04_totp_challenge_attempt_limit_survives_concurrency() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-totp", "race-totp@nebula.st", true).await;
    let repo = SqliteTotpRepo::from_pool(pool.clone());
    repo.issue_challenge(&user, "challenge-hash", now() + CHALLENGE_TTL, now())
        .await
        .expect("挑战必须可签发");
    let challenge_id = repo
        .find_challenge("challenge-hash")
        .await
        .expect("读取不得失败")
        .expect("刚签发的挑战必须存在")
        .id;

    let writes = tokio::join!(
        repo.reserve_challenge_attempt(&challenge_id, CHALLENGE_MAX_ATTEMPTS),
        repo.reserve_challenge_attempt(&challenge_id, CHALLENGE_MAX_ATTEMPTS),
        repo.reserve_challenge_attempt(&challenge_id, CHALLENGE_MAX_ATTEMPTS),
        repo.reserve_challenge_attempt(&challenge_id, CHALLENGE_MAX_ATTEMPTS),
        repo.reserve_challenge_attempt(&challenge_id, CHALLENGE_MAX_ATTEMPTS),
        repo.reserve_challenge_attempt(&challenge_id, CHALLENGE_MAX_ATTEMPTS),
        repo.reserve_challenge_attempt(&challenge_id, CHALLENGE_MAX_ATTEMPTS),
        repo.reserve_challenge_attempt(&challenge_id, CHALLENGE_MAX_ATTEMPTS),
    );
    let forced_admitted =
        [writes.0, writes.1, writes.2, writes.3, writes.4, writes.5, writes.6, writes.7]
            .into_iter()
            .filter(|result| *result.as_ref().expect("预占不得失败"))
            .count();

    // 另发一个挑战，24 个并发请求各自执行完整读-判-写序列
    repo.consume_challenge(&challenge_id, now()).await.expect("关闭旧挑战不得报存储故障");
    repo.issue_challenge(&user, "challenge-second", now() + CHALLENGE_TTL, now())
        .await
        .expect("第二个挑战必须可签发");
    let mut set = tokio::task::JoinSet::new();
    for _ in 0..24 {
        let repo = repo.clone();
        set.spawn(async move { admit_challenge_guess(&repo, "challenge-second").await });
    }
    let mut stress_admitted = 0;
    while let Some(result) = set.join_next().await {
        if result.expect("任务不得 panic") {
            stress_admitted += 1;
        }
    }

    assert!(
        stress_admitted <= CHALLENGE_MAX_ATTEMPTS as usize
            && forced_admitted <= CHALLENGE_MAX_ATTEMPTS as usize,
        "TOTP 挑战尝试上限可被并发绕过：固定交错放行 {forced_admitted} 次、并发压力放行 \
         {stress_admitted} 次，上限 {CHALLENGE_MAX_ATTEMPTS}"
    );
}

// ---------------------------------------------------------------------------
// 3. 一次性凭据的并发消费
// ---------------------------------------------------------------------------

/// 攻击：同一恢复码并发提交两次。不变量：恰好一次成功
/// （`src/storage/sqlite.rs:921-937` 的条件 UPDATE）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_05_recovery_code_consumed_exactly_once() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-recovery", "race-recovery@nebula.st", true).await;
    let repo = SqliteTotpRepo::from_pool(pool.clone());
    repo.add_recovery_codes(&user, vec!["recovery-hash".to_owned()], now())
        .await
        .expect("恢复码必须可落库");

    let mut set = tokio::task::JoinSet::new();
    for _ in 0..8 {
        let repo = repo.clone();
        set.spawn(async move { repo.consume_recovery_code(&user, "recovery-hash", now()).await });
    }
    let mut successes = 0;
    while let Some(result) = set.join_next().await {
        if result.expect("任务不得 panic").expect("消费不得报存储故障") {
            successes += 1;
        }
    }
    assert_eq!(successes, 1, "同一恢复码并发消费出现 {successes} 次成功");
    assert!(
        !repo
            .consume_recovery_code(&user, "recovery-hash", now())
            .await
            .expect("重复消费不得报存储故障"),
        "已消费的恢复码再次提交必须失败"
    );
    let consumed: i64 = sqlx::query_scalar::<_, i64>(
        "SELECT COUNT(*) FROM totp_recovery_codes WHERE consumed_at IS NOT NULL",
    )
    .fetch_one(&pool)
    .await
    .expect("计数必须可读");
    assert_eq!(consumed, 1);
}

/// 攻击：同一授权码并发兑换。不变量：恰好一次消费成功，且只产生一个 refresh
/// 家族（`src/api/oauth/token.rs:107-118` 的消费 → 建族组合）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_06_oauth_code_exchange_single_winner_and_family() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-oauth", "race-oauth@nebula.st", true).await;
    let oauth = SqliteOAuthRepo::from_pool(pool.clone());
    oauth
        .upsert_client(
            &OAuthClient {
                id: "race-client".to_owned(),
                name: "Race".to_owned(),
                redirect_uri: "https://app.example/cb".to_owned(),
                enabled: true,
            },
            "secret-hash",
            now(),
        )
        .await
        .expect("客户端必须可登记");
    oauth
        .create_code(
            "code-hash",
            &NewAuthorizationCode {
                client_id: "race-client".to_owned(),
                user_id: user,
                redirect_uri: "https://app.example/cb".to_owned(),
                scope: "profile".to_owned(),
                code_challenge: "challenge".to_owned(),
                expires_at: now() + TimeDelta::minutes(10),
                session_version: 0,
            },
            now(),
        )
        .await
        .expect("授权码必须可落库");
    let code_id = oauth
        .find_code("code-hash")
        .await
        .expect("读取不得失败")
        .expect("刚落库的授权码必须存在")
        .id;

    let mut set = tokio::task::JoinSet::new();
    for index in 0..8u32 {
        let oauth = oauth.clone();
        let family = family(user, &format!("client-token-{index}"), Some("race-client"));
        set.spawn(async move {
            matches!(
                oauth.exchange_code(&code_id, now(), &family).await.expect("兑换不得报存储故障"),
                CodeExchange::Exchanged
            )
        });
    }
    let mut successes = 0;
    while let Some(result) = set.join_next().await {
        if result.expect("任务不得 panic") {
            successes += 1;
        }
    }
    assert_eq!(successes, 1, "同一授权码并发兑换出现 {successes} 次成功");
    let families: i64 = sqlx::query_scalar::<_, i64>(
        "SELECT COUNT(*) FROM refresh_token_families WHERE user_id = ?1",
    )
    .bind(user.as_uuid())
    .fetch_one(&pool)
    .await
    .expect("计数必须可读");
    assert_eq!(families, 1, "一次授权码兑换只允许产生一个 refresh 家族");
}

/// 攻击：授权码过期后仍提交兑换。不变量：过期码不得完成 Fulfilled → Exchanged。
/// 现状 `consume_code` 只判 `consumed_at`（`src/storage/sqlite.rs:1113-1123`），
/// 时效只在 handler 里先读后判（`src/api/oauth/token.rs:97-113`），两者之间是 TOCTOU。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_07_oauth_code_expiry_is_enforced_at_consume() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "expired-code", "expired-code@nebula.st", true).await;
    let oauth = SqliteOAuthRepo::from_pool(pool.clone());
    oauth
        .upsert_client(
            &OAuthClient {
                id: "expired-client".to_owned(),
                name: "Expired".to_owned(),
                redirect_uri: "https://app.example/cb".to_owned(),
                enabled: true,
            },
            "secret-hash",
            now(),
        )
        .await
        .expect("客户端必须可登记");

    let mut consumed_expired = Vec::new();
    for (tag, expires_at) in
        [("already-expired", now() - TimeDelta::seconds(1)), ("boundary-now", now())]
    {
        oauth
            .create_code(
                tag,
                &NewAuthorizationCode {
                    client_id: "expired-client".to_owned(),
                    user_id: user,
                    redirect_uri: "https://app.example/cb".to_owned(),
                    scope: "profile".to_owned(),
                    code_challenge: "challenge".to_owned(),
                    expires_at,
                    session_version: 0,
                },
                now() - TimeDelta::minutes(10),
            )
            .await
            .expect("授权码必须可落库");
        let id = oauth.find_code(tag).await.expect("读取不得失败").expect("授权码必须存在").id;
        let family = family(user, &format!("expired-token-{tag}"), Some("expired-client"));
        if matches!(
            oauth.exchange_code(&id, now(), &family).await.expect("兑换不得报存储故障"),
            CodeExchange::Exchanged
        ) {
            consumed_expired.push(tag);
        }
    }
    assert!(
        consumed_expired.is_empty(),
        "过期授权码仍完成消费：{consumed_expired:?}——到期判定必须与状态转移同语句/同事务"
    );
}

/// 攻击：并发申请导出链接。不变量：每用户同时最多一个未消费令牌
/// （`src/storage.rs:666` 的文档承诺）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_08_export_issue_keeps_single_live_token() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-export", "race-export@nebula.st", true).await;
    let repo = SqliteExportRepo::from_pool(pool.clone());

    let mut set = tokio::task::JoinSet::new();
    for index in 0..8u32 {
        let repo = repo.clone();
        set.spawn(async move {
            repo.issue(
                &user,
                &format!("export-token-{index}"),
                now() + TimeDelta::minutes(10),
                now(),
            )
            .await
        });
    }
    while let Some(result) = set.join_next().await {
        result.expect("任务不得 panic").expect("签发不得报存储故障");
    }

    let live: i64 = sqlx::query_scalar::<_, i64>(
        "SELECT COUNT(*) FROM data_export_tokens WHERE user_id = ?1 AND consumed_at IS NULL",
    )
    .bind(user.as_uuid())
    .fetch_one(&pool)
    .await
    .expect("计数必须可读");
    assert_eq!(live, 1, "并发申请后留下 {live} 个未消费导出令牌，承诺上限为 1");

    // 只有一个令牌可消费：把库中活令牌取出逐个试，成功数必须为 1
    let hashes: Vec<String> = sqlx::query_scalar::<_, String>(
        "SELECT token_hash FROM data_export_tokens WHERE user_id = ?1 AND consumed_at IS NULL",
    )
    .bind(user.as_uuid())
    .fetch_all(&pool)
    .await
    .expect("令牌必须可读");
    let mut consumable = 0;
    for hash in hashes {
        if repo.consume(&hash, now()).await.expect("消费不得报存储故障").is_some() {
            consumable += 1;
        }
    }
    assert_eq!(consumable, 1);
}

/// 攻击：同一下载链接并发请求两次。不变量：恰好一次下载成功。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_09_export_consume_single_winner() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-export2", "race-export2@nebula.st", true).await;
    let repo = SqliteExportRepo::from_pool(pool.clone());
    repo.issue(&user, "one-shot", now() + TimeDelta::minutes(10), now())
        .await
        .expect("签发必须成功");

    let mut set = tokio::task::JoinSet::new();
    for _ in 0..8 {
        let repo = repo.clone();
        set.spawn(async move { repo.consume("one-shot", now()).await });
    }
    let mut successes = 0;
    while let Some(result) = set.join_next().await {
        if result.expect("任务不得 panic").expect("消费不得报存储故障").is_some() {
            successes += 1;
        }
    }
    assert_eq!(successes, 1, "同一导出令牌并发消费出现 {successes} 次成功");
}

// ---------------------------------------------------------------------------
// 4. 唯一约束：并发注册 / 改名
// ---------------------------------------------------------------------------

/// 攻击：同邮箱（大小写变体）并发注册。不变量：恰好一个成功，另一个得到
/// `Conflict`，且大小写不敏感的唯一性由列 COLLATE NOCASE 保证
/// （`src/storage/sqlite/schema.rs:24`；错误映射 `src/storage/sqlite.rs:326-333`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_10_register_same_email_concurrent_single_winner() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let repo = SqliteUserRepo::from_pool(pool.clone());
    let hash = cheap_hash();
    let first = user_row(UserId::new(), "race-a", "RACE@nebula.st");
    let second = user_row(UserId::new(), "race-b", "race@nebula.st");

    let (left, right) = tokio::join!(repo.insert(&first, &hash), repo.insert(&second, &hash));
    let results = [left, right];
    let successes = results.iter().filter(|result| result.is_ok()).count();
    let conflicts =
        results.iter().filter(|result| matches!(result, Err(UserRepoError::Conflict))).count();
    assert_eq!(successes, 1, "同邮箱并发注册出现 {successes} 次成功：{results:?}");
    assert_eq!(conflicts, 1, "落败方必须是 Conflict（唯一约束映射）：{results:?}");
    let count: i64 =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM users WHERE email = 'race@nebula.st'")
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
    assert_eq!(count, 1);
}

/// 攻击：同用户名并发注册（用户名大小写敏感，ADR-0015）。不变量：恰好一个成功。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_11_register_same_username_concurrent_single_winner() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let repo = SqliteUserRepo::from_pool(pool.clone());
    let hash = cheap_hash();
    let first = user_row(UserId::new(), "same-name", "same-a@nebula.st");
    let second = user_row(UserId::new(), "same-name", "same-b@nebula.st");

    let (left, right) = tokio::join!(repo.insert(&first, &hash), repo.insert(&second, &hash));
    let results = [left, right];
    assert_eq!(
        results.iter().filter(|result| result.is_ok()).count(),
        1,
        "同用户名并发注册出现多次成功：{results:?}"
    );
    assert_eq!(
        results.iter().filter(|result| matches!(result, Err(UserRepoError::Conflict))).count(),
        1,
        "落败方必须是 Conflict：{results:?}"
    );
}

/// 攻击：两个用户并发改成同一用户名。不变量：恰好一个成功；唯一冲突映射为
/// `Conflict` 而不是 500（`src/storage/sqlite.rs:365-382`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_12_username_update_conflict_is_mapped_not_storage_error() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let repo = SqliteUserRepo::from_pool(pool.clone());
    let hash = cheap_hash();
    let first = user_row(UserId::new(), "rename-a", "rename-a@nebula.st");
    let second = user_row(UserId::new(), "rename-b", "rename-b@nebula.st");
    repo.insert(&first, &hash).await.expect("首个用户必须可插入");
    repo.insert(&second, &hash).await.expect("第二个用户必须可插入");

    let (left, right) = tokio::join!(
        repo.update_username(&first.id, "contended"),
        repo.update_username(&second.id, "contended"),
    );
    let results = [left, right];
    assert_eq!(
        results.iter().filter(|result| matches!(result, Ok(true))).count(),
        1,
        "并发改名出现多次成功：{results:?}"
    );
    assert_eq!(
        results.iter().filter(|result| matches!(result, Err(UserRepoError::Conflict))).count(),
        1,
        "落败方必须映射为 Conflict，不得是 Storage(500)：{results:?}"
    );
    let holders: i64 =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM users WHERE username = 'contended'")
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
    assert_eq!(holders, 1);
}

/// 攻击：两个用户并发把邮箱改成同一个地址。不变量：唯一约束只允许一个成功，
/// 落败方得到 `Conflict` 且其待确认请求与用户行都回滚
/// （`src/storage/sqlite.rs:1659-1690`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_13_email_change_two_users_same_email_single_winner() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let first = UserId::new();
    let second = UserId::new();
    seed_user(&pool, first, "change-a", "change-a@nebula.st", false).await;
    seed_user(&pool, second, "change-b", "change-b@nebula.st", false).await;
    let repo = SqliteEmailChangeRepo::from_pool(pool.clone());
    repo.upsert(&first, "shared@nebula.st", "hash-a", now() + TimeDelta::minutes(10), now())
        .await
        .expect("待确认请求必须可写入");
    repo.upsert(&second, "shared@nebula.st", "hash-b", now() + TimeDelta::minutes(10), now())
        .await
        .expect("待确认请求必须可写入");

    let (left, right) = tokio::join!(
        repo.apply(&first, "shared@nebula.st", "hash-a", now()),
        repo.apply(&second, "shared@nebula.st", "hash-b", now())
    );
    let results = [left, right];
    assert_eq!(
        results.iter().filter(|result| result.is_ok()).count(),
        1,
        "两个用户并发改成同一邮箱出现多次成功：{results:?}"
    );
    assert_eq!(
        results.iter().filter(|result| matches!(result, Err(EmailChangeError::Conflict))).count(),
        1,
        "落败方必须是 Conflict：{results:?}"
    );
    let holders: i64 =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM users WHERE email = 'shared@nebula.st'")
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
    assert_eq!(holders, 1);
    // 落败方：邮箱未变、待确认请求仍在（可在解决占用后重试）
    let loser = if results[0].is_ok() { second } else { first };
    let loser_email = email_of(&pool, loser).await.expect("用户行必须存在");
    assert!(
        loser_email.0.ends_with("@nebula.st") && loser_email.0.starts_with("change-"),
        "落败方邮箱被改写：{}",
        loser_email.0
    );
    assert!(
        repo.find(&loser).await.expect("读取不得失败").is_some(),
        "冲突回滚后落败方的待确认请求必须保留"
    );
}

/// 攻击：同一用户的待确认请求并发 confirm 两次。不变量：恰好一次应用成功，
/// 第二次返回 `NotFound`（`src/storage/sqlite.rs:1679-1687` 的删除计数）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_14_email_change_concurrent_apply_single_winner() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "apply-once", "apply-once@nebula.st", false).await;
    let repo = SqliteEmailChangeRepo::from_pool(pool.clone());
    repo.upsert(&user, "new-home@nebula.st", "hash", now() + TimeDelta::minutes(10), now())
        .await
        .expect("待确认请求必须可写入");

    let (left, right) = tokio::join!(
        repo.apply(&user, "new-home@nebula.st", "hash", now()),
        repo.apply(&user, "new-home@nebula.st", "hash", now()),
    );
    let results = [left, right];
    assert_eq!(
        results.iter().filter(|result| result.is_ok()).count(),
        1,
        "同一待确认请求并发应用出现多次成功：{results:?}"
    );
    assert_eq!(
        results.iter().filter(|result| matches!(result, Err(EmailChangeError::NotFound))).count(),
        1,
        "落败方必须是 NotFound：{results:?}"
    );
    let row = email_of(&pool, user).await.expect("用户行必须存在");
    assert_eq!(row.0, "new-home@nebula.st");
    assert!(row.1, "应用成功后必须置为已验证");
    assert!(repo.find(&user).await.expect("读取不得失败").is_none());
}

/// 攻击：过期后仍应用待确认变更。不变量：过期请求不得改写邮箱。
/// 现状 `apply` 不看 `expires_at`（`src/storage/sqlite.rs:1659-1690`），
/// 时效只在 handler 先读后判（`src/api/user/email_change.rs:124-151`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_15_email_change_apply_rejects_expired_request() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "apply-expired", "apply-expired@nebula.st", false).await;
    let repo = SqliteEmailChangeRepo::from_pool(pool.clone());
    repo.upsert(
        &user,
        "expired-home@nebula.st",
        "hash",
        now() - TimeDelta::seconds(1),
        now() - TimeDelta::minutes(20),
    )
    .await
    .expect("待确认请求必须可写入");

    let applied = repo.apply(&user, "expired-home@nebula.st", "hash", now()).await;
    assert!(applied.is_err(), "过期待确认请求仍被应用：{applied:?}——到期判定必须进入状态转移本身");
    let row = email_of(&pool, user).await.expect("用户行必须存在");
    assert_eq!(row.0, "apply-expired@nebula.st", "过期请求不得改写邮箱");
}

/// 攻击：重发同一新邮箱后，用旧验证码完成的确认仍然生效。
/// 现状 `apply` 只按 `new_email` 匹配、不看 `code_hash`/`created_at`
/// （`src/storage/sqlite.rs:1679-1687`），handler 的「读请求 → 验码 → apply」
/// 之间被重发插入时，旧码仍能完成变更（`src/api/user/email_change.rs:124-151`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_16_email_change_apply_is_bound_to_verified_code() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "stale-code", "stale-code@nebula.st", false).await;
    let repo = SqliteEmailChangeRepo::from_pool(pool.clone());

    // handler 读到待确认请求并用旧码通过比对
    repo.upsert(&user, "target@nebula.st", "hash-old", now() + TimeDelta::minutes(10), now())
        .await
        .expect("首次请求必须可写入");
    let verified = repo.find(&user).await.expect("读取不得失败").expect("待确认请求必须存在");
    assert_eq!(verified.code_hash, "hash-old");

    // 并发重发（同一新邮箱）：code_hash 换成 hash-new，旧码就此作废
    repo.upsert(
        &user,
        "target@nebula.st",
        "hash-new",
        now() + TimeDelta::minutes(10),
        now() + TimeDelta::seconds(1),
    )
    .await
    .expect("重发必须覆盖请求");

    // 旧码的确认继续执行 apply：只按 new_email 匹配 → 成功
    let applied = repo.apply(&user, &verified.new_email, &verified.code_hash, now()).await;
    assert!(
        matches!(applied, Err(EmailChangeError::NotFound)),
        "重发后旧码仍完成邮箱变更：{applied:?}——apply 必须绑定被验证的那条请求（code_hash/created_at）"
    );
}

// ---------------------------------------------------------------------------
// 5. 封禁边界
// ---------------------------------------------------------------------------

/// 攻击：在「登录检查封禁」与「创建 refresh 家族」之间插入封禁。
/// 不变量：封禁完成后不得再生产可轮转的会话。
/// 现状 `create_family` 不查封禁（`src/storage/sqlite.rs:473-503`），
/// 登录的封禁判定与建族不在同一事务（`src/api/auth/login.rs:95-121`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_17_ban_after_access_check_must_not_leave_live_session() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    let admin = UserId::new();
    seed_user(&pool, user, "ban-race", "ban-race@nebula.st", false).await;
    let users = SqliteUserRepo::from_pool(pool.clone());
    let admin_repo = SqliteAdminRepo::from_pool(pool.clone());
    let refreshes = SqliteRefreshRepo::from_pool(pool.clone());

    // 登录的封禁检查：此刻未封禁
    let access =
        users.find_access_state(&user, now()).await.expect("状态必须可读").expect("用户必须存在");
    assert!(access.ban.is_none());

    // 管理员在检查之后、建族之前完成封禁
    admin_repo
        .ban_user(
            &user,
            &Ban::Permanent,
            "abuse",
            &admin_audit(admin, user, AuditAction::AdminUserBan),
            now(),
        )
        .await
        .expect("封禁必须成功");
    let banned = users.find_access_state(&user, now()).await.expect("状态必须可读");
    assert!(banned.expect("用户必须存在").ban.is_some());

    // 登录继续：建族必须被同一事务内的封禁判定拒绝
    let created = refreshes.create_family(&family(user, "post-ban-login", None), now()).await;
    assert!(created.is_err(), "封禁完成后仍创建了 refresh 家族：{created:?}");
    let rotated = refreshes
        .rotate(
            "post-ban-login",
            "post-ban-next",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        )
        .await;
    assert!(rotated.is_err(), "被永久封禁的用户仍获得可轮转的 refresh 家族：{rotated:?}");
}

/// 攻击：封禁前签发的授权码在封禁后兑换。不变量：封禁后不得为被封禁用户建立
/// 新的第三方会话。现状 `consume_code` 与 `create_family` 都不查封禁，OAuth
/// token 端点也不查（`src/api/oauth/token.rs:81-119`、`220-240`）；码 TTL 10 分钟
/// （`src/model/oauth2.rs:12`）给了充足的预置窗口，家族可继续轮转 30 天。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_18_banned_user_cannot_exchange_code_into_live_grant() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    let admin = UserId::new();
    seed_user(&pool, user, "ban-oauth", "ban-oauth@nebula.st", true).await;
    let oauth = SqliteOAuthRepo::from_pool(pool.clone());
    let admin_repo = SqliteAdminRepo::from_pool(pool.clone());
    oauth
        .upsert_client(
            &OAuthClient {
                id: "ban-client".to_owned(),
                name: "Ban Client".to_owned(),
                redirect_uri: "https://app.example/cb".to_owned(),
                enabled: true,
            },
            "secret-hash",
            now(),
        )
        .await
        .expect("客户端必须可登记");

    // 封禁前：同意授权，落一条 10 分钟有效的授权码
    oauth
        .create_code(
            "pre-ban-code",
            &NewAuthorizationCode {
                client_id: "ban-client".to_owned(),
                user_id: user,
                redirect_uri: "https://app.example/cb".to_owned(),
                scope: "profile".to_owned(),
                code_challenge: "challenge".to_owned(),
                expires_at: now() + TimeDelta::minutes(10),
                session_version: 0,
            },
            now(),
        )
        .await
        .expect("授权码必须可落库");
    admin_repo
        .ban_user(
            &user,
            &Ban::Permanent,
            "abuse",
            &admin_audit(admin, user, AuditAction::AdminUserBan),
            now(),
        )
        .await
        .expect("封禁必须成功");

    // 封禁会删除未兑换授权码；若仍存在，兑换也必须被拒
    if let Some(code) = oauth.find_code("pre-ban-code").await.expect("读取不得失败") {
        let exchange = oauth
            .exchange_code(&code.id, now(), &family(user, "banned-grant", Some("ban-client")))
            .await
            .expect("兑换不得报存储故障");
        assert_eq!(
            exchange,
            CodeExchange::Invalid,
            "封禁后授权码仍可兑换出可轮转的第三方家族：{exchange:?}"
        );
    }
    let families: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM refresh_token_families WHERE user_id = ?1 AND client_id = 'ban-client'",
    )
    .bind(user.as_uuid())
    .fetch_one(&pool)
    .await
    .expect("计数必须可读");
    assert_eq!(families, 0, "封禁后不得存在第三方家族");
}

/// 攻击：管理员封禁与目标用户并发 refresh 轮转。不变量：封禁成功后家族撤销、
/// 无 active token 残留。`ban_user` 的事务同样以 SELECT 开头
/// （`src/storage/sqlite.rs:1398-1441`），并发写冲突会让封禁返回 500——本用例
/// 允许该退化，但要求顺序重试成功且仍撤销并发轮转刚产生的 token。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_19_ban_concurrent_with_rotation_has_no_live_token() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    let admin = UserId::new();
    seed_user(&pool, user, "ban-rotate", "ban-rotate@nebula.st", false).await;
    let admin_repo = SqliteAdminRepo::from_pool(pool.clone());
    let refreshes = SqliteRefreshRepo::from_pool(pool.clone());
    let entry = family(user, "ban-gen-1", None);
    let family_id = entry.family_id;
    refreshes.create_family(&entry, now()).await.expect("建族必须成功");

    let audit = admin_audit(admin, user, AuditAction::AdminUserBan);
    let (banned, rotated) = tokio::join!(
        admin_repo.ban_user(&user, &Ban::Permanent, "abuse", &audit, now()),
        refreshes.rotate(
            "ban-gen-1",
            "ban-gen-2",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        ),
    );
    // 并发写冲突（SQLITE_BUSY）使封禁变成 500：管理员重试必须成功，
    // 且重试要撤销并发轮转刚插入的新代
    let banned = match banned {
        Ok(hit) => hit,
        Err(err) => {
            let retried = admin_repo
                .ban_user(
                    &user,
                    &Ban::Permanent,
                    "abuse",
                    &admin_audit(admin, user, AuditAction::AdminUserBan),
                    now(),
                )
                .await
                .expect("重试封禁必须成功");
            assert!(retried, "封禁重试必须命中用户（首次存储故障：{err:?}）");
            true
        }
    };
    assert!(banned, "封禁必须命中用户");
    assert_eq!(
        family_state(&pool, family_id).await.as_deref(),
        Some("revoked"),
        "封禁后家族必须撤销（rotate 结果={rotated:?}）"
    );
    assert_eq!(active_tokens(&pool, family_id).await, 0, "封禁后不得残留 active token");
    let after = refreshes
        .rotate(
            "ban-gen-2",
            "ban-gen-3",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        )
        .await;
    assert!(after.is_err(), "封禁后链上 token 仍可轮转：{after:?}");
}

// ---------------------------------------------------------------------------
// 6. 撤销路径与删除
// ---------------------------------------------------------------------------

/// 攻击：登出（整族撤销）与 refresh 并发。不变量：撤销后没有任何可轮转的 token，
/// 且轮转返回的 `token_version` 不超过撤销后的库值（ADR-0010）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_20_revoke_all_concurrent_with_rotation_leaves_nothing_usable() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "revoke-race", "revoke-race@nebula.st", false).await;
    let refreshes = SqliteRefreshRepo::from_pool(pool.clone());
    let entry = family(user, "revoke-gen-1", None);
    let family_id = entry.family_id;
    refreshes.create_family(&entry, now()).await.expect("建族必须成功");

    let (revoked, rotated) = tokio::join!(
        refreshes.revoke_all_sessions(&user, now()),
        refreshes.rotate(
            "revoke-gen-1",
            "revoke-gen-2",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        ),
    );
    revoked.expect("撤销不得报存储故障");
    assert_eq!(
        family_state(&pool, family_id).await.as_deref(),
        Some("revoked"),
        "撤销后家族必须 revoked（rotate 结果={rotated:?}）"
    );
    assert_eq!(active_tokens(&pool, family_id).await, 0, "撤销后不得残留 active token");
    let version: i64 =
        sqlx::query_scalar::<_, i64>("SELECT token_version FROM users WHERE id = ?1")
            .bind(user.as_uuid())
            .fetch_one(&pool)
            .await
            .expect("版本必须可读");
    assert!(version >= 1, "撤销必须递增 token_version");
    if let Ok(session) = rotated {
        assert!(
            session.token_version <= version,
            "轮转返回的版本 {} 超过库中当前版本 {version}",
            session.token_version
        );
    }
    let after = refreshes
        .rotate(
            "revoke-gen-2",
            "revoke-gen-3",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        )
        .await;
    assert!(after.is_err(), "撤销后仍可轮转：{after:?}");
}

/// 攻击：账户删除与 refresh 并发。不变量：删除返回后，该用户不存在任何可轮转
/// 家族（`ON DELETE CASCADE`，ADR-0007）。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_21_delete_user_concurrent_with_rotation_leaves_no_family() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "delete-race", "delete-race@nebula.st", false).await;
    let accounts = SqliteAccountRepo::from_pool(pool.clone());
    let refreshes = SqliteRefreshRepo::from_pool(pool.clone());
    refreshes
        .create_family(&family(user, "delete-gen-1", None), now())
        .await
        .expect("建族必须成功");

    let (deleted, rotated) = tokio::join!(
        accounts.delete_user(&user),
        refreshes.rotate(
            "delete-gen-1",
            "delete-gen-2",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        ),
    );
    deleted.expect("删除不得报存储故障");
    let families: i64 = sqlx::query_scalar::<_, i64>(
        "SELECT COUNT(*) FROM refresh_token_families WHERE user_id = ?1",
    )
    .bind(user.as_uuid())
    .fetch_one(&pool)
    .await
    .expect("计数必须可读");
    assert_eq!(families, 0, "删除后不得残留家族（rotate 结果={rotated:?}）");
    for token in ["delete-gen-1", "delete-gen-2"] {
        let after = refreshes
            .rotate(
                token,
                "delete-gen-3",
                now() + TimeDelta::days(30),
                now(),
                ClientBinding::FirstParty,
            )
            .await;
        assert!(after.is_err(), "已删除用户的 token 仍可轮转：{after:?}");
    }
}

/// 攻击：撤销第三方授权（`revoke_client`）后家族状态与 token 状态不一致。
/// 不变量：revoked 家族内的 token 不得仍为 active——`revoke_client` 只改家族行
/// （`src/storage/sqlite.rs:1171-1188`），而 `revoke_family_in_tx` 两者都改。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_22_revoke_client_leaves_no_active_token() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "grant-revoke", "grant-revoke@nebula.st", true).await;
    let oauth = SqliteOAuthRepo::from_pool(pool.clone());
    let refreshes = SqliteRefreshRepo::from_pool(pool.clone());
    oauth
        .upsert_client(
            &OAuthClient {
                id: "grant-client".to_owned(),
                name: "Grant Client".to_owned(),
                redirect_uri: "https://app.example/cb".to_owned(),
                enabled: true,
            },
            "secret-hash",
            now(),
        )
        .await
        .expect("客户端必须可登记");
    let entry = family(user, "grant-token", Some("grant-client"));
    let family_id = entry.family_id;
    refreshes.create_family(&entry, now()).await.expect("建族必须成功");

    assert!(
        oauth.revoke_client(&user, "grant-client", now()).await.expect("撤销不得报存储故障"),
        "撤销必须命中 active 家族"
    );
    assert_eq!(family_state(&pool, family_id).await.as_deref(), Some("revoked"));
    let active = active_tokens(&pool, family_id).await;
    let rotated = refreshes
        .rotate(
            "grant-token",
            "grant-token-next",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::Client("grant-client"),
        )
        .await;
    assert!(rotated.is_err(), "撤销后 token 仍可轮转：{rotated:?}");
    assert_eq!(active, 0, "撤销家族内残留 {active} 个 active token：状态机不变量被破坏");
}

// ---------------------------------------------------------------------------
// 7. 社交中转态与导出/验证码的跨主体绑定
// ---------------------------------------------------------------------------

/// 攻击：同一 OAuth 中转态并发回调两次。不变量：`state` 恰好被取走一次
/// （`src/storage/sqlite.rs:1819-1853` 的取出即删除）。并发写冲突不得以 500
/// 呈现——回调是浏览器导航，500 会打断登录。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_23_social_state_taken_exactly_once() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "state-race", "state-race@nebula.st", true).await;
    let repo = SqliteSocialRepo::from_pool(pool.clone());
    repo.put_state(
        &SocialLoginState {
            state: "nonce-race".to_owned(),
            provider: SocialProvider::Google,
            code_verifier: "verifier".to_owned(),
            action: SocialLoginAction::Login,
            user_id: None,
            expires_at: now() + TimeDelta::minutes(10),
        },
        now(),
    )
    .await
    .expect("中转态必须可落库");

    let mut set = tokio::task::JoinSet::new();
    for _ in 0..8 {
        let repo = repo.clone();
        set.spawn(async move { repo.take_state("nonce-race").await });
    }
    let mut taken = 0;
    let mut storage_errors = Vec::new();
    while let Some(result) = set.join_next().await {
        match result.expect("任务不得 panic") {
            Ok(Some(_)) => taken += 1,
            Ok(None) => {}
            Err(err) => storage_errors.push(format!("{err:?}")),
        }
    }
    assert_eq!(taken, 1, "同一中转态被取走 {taken} 次（存储故障 {storage_errors:?}）");
    let remaining: i64 = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM social_login_states")
        .fetch_one(&pool)
        .await
        .expect("计数必须可读");
    assert_eq!(remaining, 0, "取出后不得残留中转态");
    // 并发写冲突会让部分回调得到 500（`database is locked`）；根因与
    // `sec_store_30` 相同（DEFERRED 快照升级），此处只断言单次消费不变量
}

/// 攻击：用 A 的验证码把 B 置为已验证。不变量：验证码的消费必须绑定其所属用户。
/// 现状 `consume_and_mark_verified(verification_id, user_id)` 不校验两者关系
/// （`src/storage/sqlite.rs:754-778`）：消费 A 的码，却把 B 写为已验证。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_24_consume_and_mark_verified_is_bound_to_code_owner() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let owner = UserId::new();
    let victim = UserId::new();
    seed_user(&pool, owner, "code-owner", "code-owner@nebula.st", false).await;
    seed_user(&pool, victim, "code-victim", "code-victim@nebula.st", false).await;
    let repo = SqliteEmailVerificationRepo::from_pool(pool.clone());
    repo.issue(
        &NewEmailVerification {
            user_id: owner,
            code_hash: "owners-code".to_owned(),
            expires_at: now() + VERIFICATION_TTL,
            purpose: VerificationPurpose::VerifyEmail,
            sent_to: "code-owner@nebula.st".to_owned(),
        },
        now(),
    )
    .await
    .expect("验证码必须可签发");
    let verification_id = repo
        .latest_unconsumed(&owner, VerificationPurpose::VerifyEmail)
        .await
        .expect("读取不得失败")
        .expect("验证码必须存在")
        .id;

    let consumed = repo
        .consume_and_mark_verified(&verification_id, &victim, now())
        .await
        .expect("消费不得报存储故障");
    let victim_row = email_of(&pool, victim).await.expect("用户行必须存在");
    assert!(
        !consumed && !victim_row.1,
        "A 的验证码把 B 置为已验证：consumed={consumed}, B.email_verified={}",
        victim_row.1
    );
}

// ---------------------------------------------------------------------------
// 8. 头像版本与文件内容
// ---------------------------------------------------------------------------

/// 攻击：并发上传两张头像。不变量：资料端点给出的最高版本 `?v=N` 必须对应该次
/// 上传的内容。现状版本递增（`src/storage/sqlite.rs:353-363`）与文件写入
/// （`src/api/user/avatar_upload.rs:64-72`）分离，而文件路径只有 `{user}.webp`
/// 一份（`src/avatar.rs:145-147`）：渲染较慢的请求后写盘，会覆盖较高版本的内容。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_25_avatar_highest_version_serves_its_own_upload() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "avatar-race", "avatar-race@nebula.st", true).await;
    let users = SqliteUserRepo::from_pool(pool.clone());
    let store = AvatarStore::new(dir.path().join("avatars"));

    // 请求 1 与请求 2 各自先递增版本
    let first_version =
        users.bump_avatar_version(&user).await.expect("递增必须成功").expect("用户必须存在");
    let second_version =
        users.bump_avatar_version(&user).await.expect("递增必须成功").expect("用户必须存在");
    assert!(second_version > first_version, "版本必须单调：{first_version} → {second_version}");

    // 请求 2 先写盘，请求 1（渲染较慢）后写盘：版本化文件名互不覆盖
    store.store(&user, second_version, b"content-from-request-2").await.expect("写盘必须成功");
    store.store(&user, first_version, b"content-from-request-1").await.expect("写盘必须成功");

    let served =
        store.load(&user, second_version).await.expect("读取必须成功").expect("文件必须存在");
    assert_eq!(
        served.as_slice(),
        b"content-from-request-2",
        "资料端点会给出 ?v={second_version}，但该地址返回的是请求 1 的内容——缓存键与内容错配"
    );
}

// ---------------------------------------------------------------------------
// 9. 唯一约束语义、注入与审计顺序
// ---------------------------------------------------------------------------

/// 攻击：大小写变体绕过唯一约束 / 登录标识歧义。不变量：邮箱大小写不敏感唯一，
/// 用户名大小写敏感唯一（ADR-0015、`src/storage/sqlite/schema.rs:21-34`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_26_case_semantics_of_unique_columns() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let repo = SqliteUserRepo::from_pool(pool.clone());
    let hash = cheap_hash();
    let mixed = user_row(UserId::new(), "Nova", "Nova@Nebula.ST");
    repo.insert(&mixed, &hash).await.expect("首个用户必须可插入");

    // 邮箱 NOCASE：大小写变体必须冲突
    let case_variant = user_row(UserId::new(), "other", "nova@nebula.st");
    assert!(
        matches!(repo.insert(&case_variant, &hash).await, Err(UserRepoError::Conflict)),
        "邮箱大小写变体绕过了唯一约束"
    );
    // 用户名 BINARY：大小写不同可共存
    let lower = user_row(UserId::new(), "nova", "third@nebula.st");
    repo.insert(&lower, &hash).await.expect("大小写不同的用户名必须可共存");

    assert_eq!(
        repo.find_by_email("NOVA@NEBULA.ST").await.expect("读取不得失败").expect("必须命中").id,
        mixed.id,
        "邮箱查找必须大小写不敏感"
    );
    assert!(
        repo.find_by_username("NOVA").await.expect("读取不得失败").is_none(),
        "用户名查找必须大小写精确"
    );
    assert_eq!(
        repo.find_login_credentials("NOVA@NEBULA.ST")
            .await
            .expect("读取不得失败")
            .expect("按邮箱登录必须命中")
            .user
            .id,
        mixed.id
    );
    assert_eq!(
        repo.find_login_credentials("nova")
            .await
            .expect("读取不得失败")
            .expect("按用户名登录必须命中")
            .user
            .id,
        lower.id,
        "用户名大小写敏感：'nova' 不得命中 'Nova'"
    );

    // 自身大小写改名允许；改成他人已占用的精确用户名必须 Conflict
    assert!(
        repo.update_username(&mixed.id, "NOVA").await.expect("自身改名不得报存储故障"),
        "仅大小写变化的自身改名必须成功"
    );
    assert!(
        matches!(repo.update_username(&mixed.id, "nova").await, Err(UserRepoError::Conflict)),
        "改成他人占用的用户名必须 Conflict"
    );
}

/// 攻击：把 SQL 片段塞进邮箱/搜索词。不变量：一律按字面值处理；用户名非法字符
/// 由 schema 触发器拒绝（`src/storage/sqlite/schema.rs:190-204`），不得静默落到
/// `Conflict`。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_27_sql_metacharacters_are_literal() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let users = SqliteUserRepo::from_pool(pool.clone());
    let admin = SqliteAdminRepo::from_pool(pool.clone());
    let hash = cheap_hash();
    let payload = "x' OR '1'='1@nebula.st";

    users
        .insert(&user_row(UserId::new(), "inject", payload), &hash)
        .await
        .expect("含 SQL 元字符的邮箱必须按字面值插入");
    assert!(
        users.find_by_email(payload).await.expect("读取不得失败").is_some(),
        "字面邮箱必须可精确命中"
    );
    assert!(
        users.find_by_email("' OR '1'='1").await.expect("读取不得失败").is_none(),
        "注入串不得命中任意行"
    );
    let hits = admin
        .list_users(Some("'; DROP TABLE users; --"), 10, 0, now())
        .await
        .expect("搜索不得报存储故障");
    assert!(hits.is_empty(), "注入串不得扩大搜索结果：{hits:?}");
    let count: i64 = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM users")
        .fetch_one(&pool)
        .await
        .expect("users 表必须仍可读");
    assert_eq!(count, 1, "users 表被改写/删除");

    // 触发器拒绝用户名非法字符；映射为 Storage 而非 Conflict（不是唯一约束）
    let bad_username = user_row(UserId::new(), "bad name'", "bad-name@nebula.st");
    let result = users.insert(&bad_username, &hash).await;
    assert!(
        matches!(result, Err(UserRepoError::Storage(_))),
        "非法用户名必须由触发器拒绝并映射为 Storage：{result:?}"
    );
}

/// 攻击：封禁与解封并发，审计写入顺序与实际状态不一致。不变量：审计尾条必须
/// 描述当前生效的封禁状态，否则「谁最后封/解」无法从审计还原（ADR-0008）。
/// 现状状态转移与审计写入是两次独立事务（`src/api/admin/ban.rs:100-134`、
/// `src/api/admin/unban.rs:22-46`）。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_28_audit_tail_matches_final_ban_state() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    let admin = UserId::new();
    seed_user(&pool, user, "audit-race", "audit-race@nebula.st", true).await;
    let repo = SqliteAdminRepo::from_pool(pool.clone());
    let users = SqliteUserRepo::from_pool(pool.clone());

    // 并发封禁/解封：状态与审计在同一事务内提交，审计尾条必须匹配最终状态
    let ban_audit = admin_audit(admin, user, AuditAction::AdminUserBan);
    let unban_audit = admin_audit(admin, user, AuditAction::AdminUserUnban);
    let (_banned, _unbanned) = tokio::join!(
        repo.ban_user(&user, &Ban::Permanent, "first", &ban_audit, now()),
        repo.unban_user(&user, &unban_audit, now()),
    );

    let entries = repo.list_audit_for_user(&user).await.expect("审计必须可读");
    let tail = entries.last().expect("至少一条审计");
    let still_banned = users
        .find_access_state(&user, now())
        .await
        .expect("状态必须可读")
        .expect("用户必须存在")
        .ban
        .is_some();
    let expected = if still_banned {
        AuditAction::AdminUserBan.as_str()
    } else {
        AuditAction::AdminUserUnban.as_str()
    };
    assert_eq!(
        tail.action, expected,
        "实际状态 banned={still_banned}，但审计尾条是 {}（审计与状态转移不同事务）",
        tail.action
    );
}

/// 攻击：轮转中途因唯一约束失败，是否会留下半状态。不变量：事务回滚后旧 token
/// 仍 active、不产生新行（`src/storage/sqlite.rs:564-590`；重复 `token_hash`
/// 触发 UNIQUE，属可预期的失败路径，例如运维或未来代码复用哈希）。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_store_29_failed_rotation_rolls_back_token_state() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "rollback-race", "rollback-race@nebula.st", false).await;
    let repo = SqliteRefreshRepo::from_pool(pool.clone());
    let first = family(user, "rollback-a", None);
    let first_family = first.family_id;
    let second = family(user, "rollback-b", None);
    repo.create_family(&first, now()).await.expect("首个家族必须可建");
    repo.create_family(&second, now()).await.expect("第二个家族必须可建");

    // 新哈希与另一个家族已有 token 冲突 → INSERT 失败
    let failed = repo
        .rotate(
            "rollback-a",
            "rollback-b",
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        )
        .await;
    assert!(
        matches!(failed, Err(RotateRefreshError::Storage(_))),
        "唯一冲突应按存储故障返回：{failed:?}"
    );
    assert_eq!(
        token_state(&pool, "rollback-a").await.as_deref(),
        Some("active"),
        "轮转失败必须回滚旧 token 的 rotated 标记"
    );
    let rows: i64 =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM refresh_tokens WHERE family_id = ?1")
            .bind(first_family)
            .fetch_one(&pool)
            .await
            .expect("计数必须可读");
    assert_eq!(rows, 1, "失败轮转不得插入新 token 行");
    let active = active_tokens(&pool, first_family).await;
    assert_eq!(active, 1, "回滚后旧 token 必须仍可轮转（active={active}）");
}

/// 攻击：并发请求让「先读后写」的 DEFERRED 事务在快照升级时撞锁。
/// 不变量：`rotate` 内部重试必须吸收 `SQLITE_BUSY`/`BUSY_SNAPSHOT`，不得把锁错误
/// 暴露给调用方；否则 `/api/auth/refresh` 会以 500 结束，客户端重试又被判重放。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_30_concurrent_rotation_absorbs_busy() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "busy-shape", "busy-shape@nebula.st", false).await;
    let repo = SqliteRefreshRepo::from_pool(pool.clone());
    repo.create_family(&family(user, "busy-token", None), now()).await.expect("建族必须成功");

    let mut set = tokio::task::JoinSet::new();
    for index in 0..8u32 {
        let repo = repo.clone();
        set.spawn(async move {
            repo.rotate(
                "busy-token",
                &format!("busy-next-{index}"),
                now() + TimeDelta::days(30),
                now(),
                ClientBinding::FirstParty,
            )
            .await
        });
    }
    let mut outcome = RotateOutcome::default();
    while let Some(result) = set.join_next().await {
        classify_rotate(&mut outcome, result.expect("任务不得 panic").map(|_| ()));
    }
    assert!(
        outcome.storage.is_empty(),
        "并发轮转把 SQLite 锁错误暴露给调用方：{:?}",
        outcome.storage
    );
    assert!(outcome.ok <= 1, "并发轮转出现双重成功：{outcome:?}");
}

/// 攻击：攻击者并发洪泛错误验证码，合法用户同时提交正确码。
/// 不变量：正确码最多完成一次验证；错误洪泛要么耗尽尝试额度（用户须重发），
/// 要么被正确消费抢先——不会出现「验证成功两次」或「错误码完成验证」。
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn sec_store_31_correct_and_wrong_guesses_race_single_verification() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let pool = fresh(dir.path()).await;
    let user = UserId::new();
    seed_user(&pool, user, "race-mixed", "race-mixed@nebula.st", false).await;
    let repo = SqliteEmailVerificationRepo::from_pool(pool.clone());
    repo.issue(
        &NewEmailVerification {
            user_id: user,
            code_hash: "correct-hash".to_owned(),
            expires_at: now() + VERIFICATION_TTL,
            purpose: VerificationPurpose::VerifyEmail,
            sent_to: "race-mixed@nebula.st".to_owned(),
        },
        now(),
    )
    .await
    .expect("验证码必须可签发");

    let mut set = tokio::task::JoinSet::new();
    for index in 0..8u32 {
        let repo = repo.clone();
        set.spawn(async move {
            let Some(seen) = repo
                .latest_unconsumed(&user, VerificationPurpose::VerifyEmail)
                .await
                .expect("读取不得失败")
            else {
                return (false, false);
            };
            if seen.attempts >= MAX_ATTEMPTS {
                return (false, index == 0);
            }
            if index == 0 {
                // 合法用户：正确码 → 原子消费并置已验证
                let consumed = repo
                    .consume_and_mark_verified(&seen.id, &user, now())
                    .await
                    .expect("消费不得报存储故障");
                (consumed, false)
            } else {
                // 攻击者：错误码只计次
                repo.record_attempt(&seen.id).await.expect("计数不得失败");
                (false, false)
            }
        });
    }
    let (mut verified, mut refused_by_attempts) = (0u32, 0u32);
    while let Some(result) = set.join_next().await {
        let (consumed, refused) = result.expect("任务不得 panic");
        if consumed {
            verified += 1;
        }
        if refused {
            refused_by_attempts += 1;
        }
    }
    assert!(verified <= 1, "正确码被完成验证 {verified} 次");
    assert!(
        verified == 1 || refused_by_attempts == 1,
        "正确码既未成功也未被尝试额度拒绝：verified={verified}, refused={refused_by_attempts}"
    );
    if verified == 1 {
        let row = email_of(&pool, user).await.expect("用户行必须存在");
        assert!(row.1, "验证成功后必须置为已验证");
        assert!(
            repo.latest_unconsumed(&user, VerificationPurpose::VerifyEmail)
                .await
                .expect("读取不得失败")
                .is_none(),
            "成功后不得留下未消费验证码"
        );
    }
}
