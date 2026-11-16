//! sqlx + SQLite 实现（ADR-0003）：用户仓储与 refresh 家族仓储共享连接池。

mod schema;

use std::path::Path;
use std::time::Duration;

use async_trait::async_trait;
use chrono::{DateTime, TimeDelta, Utc};
use sqlx::Error as SqlxError;
use sqlx::sqlite::{SqliteConnectOptions, SqliteJournalMode, SqlitePool};
use uuid::Uuid;

pub use schema::ensure_schema;

use crate::model::audit::{AuditEntry, NewAuditEntry};
use crate::model::oauth2::{AuthorizationCode, NewAuthorizationCode, OAuthClient};
use crate::model::password::PasswordHash;
use crate::model::social::{
    NewSocialIdentity, SocialIdentity, SocialLoginAction, SocialLoginState, SocialProvider,
};
use crate::model::tokens::NewRefreshFamily;
use crate::model::totp::{RecoveryCodeRecord, TotpChallenge, TotpState};
use crate::model::user::{Ban, LoginCredentials, User, UserAccess, UserId};
use crate::model::verification::{EmailVerification, NewEmailVerification, VerificationPurpose};
use crate::storage::{
    AdminStats, AdminUserRow, CleanupReport, ClientBinding, LinkIdentityError, RotateRefreshError,
    RotatedSession, StorageError, UserRepo, UserRepoError,
};

/// SQLite 用户仓储；克隆共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteUserRepo {
    pool: SqlitePool,
}

/// SQLite refresh 家族仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteRefreshRepo {
    pool: SqlitePool,
}

/// SQLite 邮箱验证码仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteEmailVerificationRepo {
    pool: SqlitePool,
}

/// SQLite 账户删除仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteAccountRepo {
    pool: SqlitePool,
}

/// SQLite TOTP 仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteTotpRepo {
    pool: SqlitePool,
}

/// SQLite `OAuth2` 仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteOAuthRepo {
    pool: SqlitePool,
}

/// SQLite 维护仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteMaintenanceRepo {
    pool: SqlitePool,
}

/// SQLite 管理后台仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteAdminRepo {
    pool: SqlitePool,
}

/// SQLite 社交身份仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteSocialRepo {
    pool: SqlitePool,
}

/// SQLite 数据导出令牌仓储；与用户仓储共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteExportRepo {
    pool: SqlitePool,
}

/// 打开或创建数据库文件并建立带 PRAGMA 的连接池；schema 由
/// [`ensure_schema`] 幂等创建（ADR-0003 修订），没有独立迁移步骤。
///
/// # Errors
/// 文件路径非法、权限不足或连接失败时返回 [`StorageError`]。
pub async fn connect_pool(path: &Path) -> Result<SqlitePool, StorageError> {
    let options = SqliteConnectOptions::new()
        .filename(path)
        .create_if_missing(true)
        .foreign_keys(true)
        .journal_mode(SqliteJournalMode::Wal)
        .busy_timeout(Duration::from_secs(5));
    SqlitePool::connect_with(options).await.map_err(StorageError::from)
}

impl SqliteUserRepo {
    /// 等价于 [`connect_pool`] 后 [`Self::from_pool`]。
    ///
    /// # Errors
    /// 见 [`connect_pool`]。
    pub async fn open(path: &Path) -> Result<Self, StorageError> {
        Ok(Self { pool: connect_pool(path).await? })
    }

    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteRefreshRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteEmailVerificationRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteAccountRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteTotpRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteOAuthRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteMaintenanceRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteAdminRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteSocialRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl SqliteExportRepo {
    /// 从既有连接池构建。
    #[must_use]
    pub fn from_pool(pool: SqlitePool) -> Self {
        Self { pool }
    }
}

impl From<SqlxError> for StorageError {
    fn from(err: SqlxError) -> Self {
        Self(Box::new(err))
    }
}

// 两跳转换（SqlxError → StorageError → UserRepoError）拆给 `?` 用
impl From<SqlxError> for UserRepoError {
    fn from(err: SqlxError) -> Self {
        Self::Storage(err.into())
    }
}

impl From<SqlxError> for RotateRefreshError {
    fn from(err: SqlxError) -> Self {
        Self::Storage(err.into())
    }
}

// 两跳转换（SqlxError → StorageError → LinkIdentityError）拆给 `?` 用
impl From<SqlxError> for LinkIdentityError {
    fn from(err: SqlxError) -> Self {
        Self::Storage(err.into())
    }
}

type UserTuple = (Uuid, String, String, bool, DateTime<Utc>, i64);

fn user_from_tuple(
    (id, username, email, email_verified, created_at, avatar_version): UserTuple,
) -> User {
    User { id: UserId::from_uuid(id), username, email, created_at, email_verified, avatar_version }
}

type CredentialsTuple = (Uuid, String, String, bool, DateTime<Utc>, i64, Option<String>);

type AccessTuple = (String, bool, i64, Option<String>, Option<DateTime<Utc>>);
type RefreshTuple = (Uuid, Uuid, String, DateTime<Utc>, String, Uuid, Option<String>, String, i64);
type VerificationTuple = (Uuid, String, u32, DateTime<Utc>, DateTime<Utc>);
type RecoveryTuple = (Uuid, Option<DateTime<Utc>>);
type ChallengeTuple = (Uuid, Uuid, u32, DateTime<Utc>);
type ClientTuple = (String, String, String, bool);
type CodeTuple = (Uuid, String, Uuid, String, String, String, DateTime<Utc>);
type AdminUserTuple =
    (Uuid, String, String, bool, DateTime<Utc>, i64, Option<String>, Option<DateTime<Utc>>);
type AuditTuple = (Uuid, Uuid, String, Option<Uuid>, Option<String>, DateTime<Utc>);
type SocialIdentityTuple = (Uuid, Uuid, String, String, Option<String>, DateTime<Utc>);
type SocialStateTuple = (String, String, String, String, Option<Uuid>, DateTime<Utc>);

fn audit_from_tuple(
    (id, actor_id, action, target_id, raw_details, created_at): AuditTuple,
) -> Result<AuditEntry, StorageError> {
    let details: Option<serde_json::Value> = raw_details
        .map(|raw| serde_json::from_str(&raw))
        .transpose()
        .map_err(StorageError::wrap)?;
    Ok(AuditEntry {
        id,
        actor_id: UserId::from_uuid(actor_id),
        action,
        target_id: target_id.map(UserId::from_uuid),
        details,
        created_at,
    })
}

fn credentials_from_tuple(
    (id, username, email, email_verified, created_at, avatar_version, encoded): CredentialsTuple,
) -> Result<Option<LoginCredentials>, UserRepoError> {
    // 存量哈希损坏是存储数据问题，须显式报错而非静默当作无口令
    let Some(encoded) = encoded else { return Ok(None) };
    let password_hash = PasswordHash::from_encoded(encoded)
        .map_err(|err| UserRepoError::Storage(StorageError::wrap(err)))?;
    Ok(Some(LoginCredentials {
        user: user_from_tuple((id, username, email, email_verified, created_at, avatar_version)),
        password_hash,
    }))
}

#[async_trait]
impl UserRepo for SqliteUserRepo {
    async fn insert(&self, user: &User, password: &PasswordHash) -> Result<(), UserRepoError> {
        let result = sqlx::query(
            "INSERT INTO users (id, username, email, created_at, password_hash, email_verified)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
        )
        .bind(user.id.as_uuid())
        .bind(&user.username)
        .bind(&user.email)
        .bind(user.created_at)
        .bind(password.as_str())
        .bind(user.email_verified)
        .execute(&self.pool)
        .await;

        match result {
            Ok(_) => Ok(()),
            // SQLite 唯一约束错误码 2067/1555，is_unique_violation 均覆盖
            Err(SqlxError::Database(db)) if db.is_unique_violation() => {
                Err(UserRepoError::Conflict)
            }
            Err(err) => Err(UserRepoError::from(err)),
        }
    }

    async fn upgrade_password_hash(
        &self,
        user_id: &UserId,
        expected: &PasswordHash,
        replacement: &PasswordHash,
    ) -> Result<bool, UserRepoError> {
        // 比较并交换：并发改密已写入新哈希时，旧口令的重算结果不得覆盖它
        let updated =
            sqlx::query("UPDATE users SET password_hash = ?3 WHERE id = ?1 AND password_hash = ?2")
                .bind(user_id.as_uuid())
                .bind(expected.as_str())
                .bind(replacement.as_str())
                .execute(&self.pool)
                .await?;
        Ok(updated.rows_affected() == 1)
    }

    async fn bump_avatar_version(&self, user_id: &UserId) -> Result<Option<i64>, UserRepoError> {
        let version: Option<i64> = sqlx::query_scalar(
            "UPDATE users SET avatar_version = avatar_version + 1
             WHERE id = ?1
             RETURNING avatar_version",
        )
        .bind(user_id.as_uuid())
        .fetch_optional(&self.pool)
        .await?;
        Ok(version)
    }

    async fn find_by_email(&self, email: &str) -> Result<Option<User>, UserRepoError> {
        let row: Option<UserTuple> = sqlx::query_as(
            "SELECT id, username, email, email_verified, created_at, avatar_version
             FROM users WHERE email = ?1",
        )
        .bind(email)
        .fetch_optional(&self.pool)
        .await?;

        Ok(row.map(user_from_tuple))
    }

    async fn find_by_username(&self, username: &str) -> Result<Option<User>, UserRepoError> {
        let row: Option<UserTuple> = sqlx::query_as(
            "SELECT id, username, email, email_verified, created_at, avatar_version
             FROM users WHERE username = ?1",
        )
        .bind(username)
        .fetch_optional(&self.pool)
        .await?;

        Ok(row.map(user_from_tuple))
    }

    async fn find_by_id(
        &self,
        user_id: &UserId,
    ) -> Result<Option<LoginCredentials>, UserRepoError> {
        let row: Option<CredentialsTuple> = sqlx::query_as(
            "SELECT id, username, email, email_verified, created_at, avatar_version, password_hash
             FROM users WHERE id = ?1",
        )
        .bind(user_id.as_uuid())
        .fetch_optional(&self.pool)
        .await?;

        match row {
            Some(tuple) => credentials_from_tuple(tuple),
            None => Ok(None),
        }
    }

    async fn find_access_state(
        &self,
        user_id: &UserId,
        now: DateTime<Utc>,
    ) -> Result<Option<UserAccess>, UserRepoError> {
        let row: Option<AccessTuple> = sqlx::query_as(
            "SELECT u.email, u.email_verified, u.token_version, b.kind, b.expires_at
                 FROM users u
                 LEFT JOIN user_bans b ON b.user_id = u.id
                 WHERE u.id = ?1",
        )
        .bind(user_id.as_uuid())
        .fetch_optional(&self.pool)
        .await?;

        let Some((email, email_verified, token_version, kind, expires_at)) = row else {
            return Ok(None);
        };
        let ban = decode_ban(kind.as_deref(), expires_at, now).map_err(UserRepoError::Storage)?;
        Ok(Some(UserAccess { email, email_verified, token_version, ban }))
    }

    async fn find_login_credentials(
        &self,
        identifier: &str,
    ) -> Result<Option<LoginCredentials>, UserRepoError> {
        // 哈希列可空：无口令用户按 None 返回，调用方据此走时序对齐路径
        let row: Option<CredentialsTuple> = sqlx::query_as(
            "SELECT id, username, email, email_verified, created_at, avatar_version, password_hash
             FROM users WHERE email = ?1 OR username = ?1
             -- 兼容历史数据：若 username 恰好等于他人 email，email 匹配优先
             ORDER BY CASE WHEN email = ?1 THEN 0 ELSE 1 END, created_at ASC
             LIMIT 1",
        )
        .bind(identifier)
        .fetch_optional(&self.pool)
        .await?;

        match row {
            Some(tuple) => credentials_from_tuple(tuple),
            None => Ok(None),
        }
    }
}

#[async_trait]
impl crate::storage::RefreshTokenRepo for SqliteRefreshRepo {
    async fn create_family(
        &self,
        family: &NewRefreshFamily,
        created_at: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        let mut tx = self.pool.begin().await?;
        sqlx::query(
            "INSERT INTO refresh_token_families (id, user_id, state, created_at, client_id, scope)
             VALUES (?1, ?2, 'active', ?3, ?4, ?5)",
        )
        .bind(family.family_id)
        .bind(family.user_id.as_uuid())
        .bind(created_at)
        .bind(family.client_id.as_deref())
        .bind(family.scope.as_str())
        .execute(&mut *tx)
        .await?;
        sqlx::query(
            "INSERT INTO refresh_tokens (id, family_id, token_hash, state, created_at, expires_at)
             VALUES (?1, ?2, ?3, 'active', ?4, ?5)",
        )
        .bind(Uuid::now_v7())
        .bind(family.family_id)
        .bind(family.token_hash.as_str())
        .bind(created_at)
        .bind(family.expires_at)
        .execute(&mut *tx)
        .await?;
        tx.commit().await?;
        Ok(())
    }

    async fn rotate(
        &self,
        presented_hash: &str,
        new_hash: &str,
        new_expires_at: DateTime<Utc>,
        now: DateTime<Utc>,
        binding: ClientBinding<'_>,
    ) -> Result<RotatedSession, RotateRefreshError> {
        let mut tx = self.pool.begin().await?;

        // 家族字段与 token 一次带出：绑定校验与轮转在同一个事务内完成
        let presented: Option<RefreshTuple> = sqlx::query_as(
            "SELECT t.id, t.family_id, t.state, t.expires_at, f.state, f.user_id,
                    f.client_id, f.scope, u.token_version
             FROM refresh_tokens t
             JOIN refresh_token_families f ON f.id = t.family_id
             JOIN users u ON u.id = f.user_id
             WHERE t.token_hash = ?1",
        )
        .bind(presented_hash)
        .fetch_optional(&mut *tx)
        .await?;

        let Some((
            id,
            family_id,
            state,
            expires_at,
            family_state,
            user_id,
            client_id,
            scope,
            token_version,
        )) = presented
        else {
            return Err(RotateRefreshError::Invalid);
        };

        // 先于状态判断：绑定不符的呈递不触碰家族状态
        let binding_matches = match binding {
            ClientBinding::FirstParty => client_id.is_none(),
            ClientBinding::Client(expected) => client_id.as_deref() == Some(expected),
        };
        if !binding_matches {
            return Err(RotateRefreshError::ClientMismatch);
        }

        match state.as_str() {
            // 呈递已轮转 token = 重放：整族撤销后拒绝（事务保证撤销也落库）
            "rotated" => {
                revoke_family_in_tx(&mut tx, family_id, now).await?;
                tx.commit().await?;
                Err(RotateRefreshError::Replay)
            }
            "active" => {
                if expires_at <= now || family_state != "active" {
                    return Err(RotateRefreshError::Invalid);
                }

                let rotated = sqlx::query(
                    "UPDATE refresh_tokens SET state = 'rotated', rotated_at = ?2
                     WHERE id = ?1 AND state = 'active'",
                )
                .bind(id)
                .bind(now)
                .execute(&mut *tx)
                .await?;
                // 并发下状态可能已被他者改变；0 行受影响说明此刻不再是 active，按重放处置
                if rotated.rows_affected() != 1 {
                    revoke_family_in_tx(&mut tx, family_id, now).await?;
                    tx.commit().await?;
                    return Err(RotateRefreshError::Replay);
                }

                sqlx::query(
                    "INSERT INTO refresh_tokens (id, family_id, token_hash, state, created_at, expires_at)
                     VALUES (?1, ?2, ?3, 'active', ?4, ?5)",
                )
                .bind(Uuid::now_v7())
                .bind(family_id)
                .bind(new_hash)
                .bind(now)
                .bind(new_expires_at)
                .execute(&mut *tx)
                .await?;
                tx.commit().await?;
                Ok(RotatedSession { user_id: UserId::from_uuid(user_id), scope, token_version })
            }
            // revoked 或未知状态值（未知值属数据损坏，按无效拒绝）
            _ => Err(RotateRefreshError::Invalid),
        }
    }

    async fn revoke_all_sessions(
        &self,
        user_id: &UserId,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        let mut tx = self.pool.begin().await?;
        revoke_all_sessions_in_tx(&mut tx, user_id, now).await?;
        tx.commit().await?;
        Ok(())
    }
}

/// 在事务内递增 access token 版本并撤销用户全部 active 家族；
/// 封禁、登出、改密、重置、关闭 TOTP 共用。
async fn revoke_all_sessions_in_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Sqlite>,
    user_id: &UserId,
    now: DateTime<Utc>,
) -> Result<(), StorageError> {
    sqlx::query("UPDATE users SET token_version = token_version + 1 WHERE id = ?1")
        .bind(user_id.as_uuid())
        .execute(&mut **tx)
        .await?;

    let families: Vec<Uuid> = sqlx::query_scalar(
        "SELECT id FROM refresh_token_families
         WHERE user_id = ?1 AND state = 'active'",
    )
    .bind(user_id.as_uuid())
    .fetch_all(&mut **tx)
    .await?;
    for family in families {
        revoke_family_in_tx(tx, family, now).await?;
    }
    Ok(())
}

/// 在事务内作废用户全部未消费的 TOTP 登录挑战；改密/重置与关闭 TOTP 共用。
async fn invalidate_challenges_in_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Sqlite>,
    user_id: &UserId,
    now: DateTime<Utc>,
) -> Result<(), StorageError> {
    sqlx::query(
        "UPDATE totp_login_challenges SET consumed_at = ?2
         WHERE user_id = ?1 AND consumed_at IS NULL",
    )
    .bind(user_id.as_uuid())
    .bind(now)
    .execute(&mut **tx)
    .await?;
    Ok(())
}

/// 把 `user_bans` 的 (`kind`, `expires_at`) 解码为领域状态；到期临时封禁归为 `None`。
fn decode_ban(
    kind: Option<&str>,
    expires_at: Option<DateTime<Utc>>,
    now: DateTime<Utc>,
) -> Result<Option<Ban>, StorageError> {
    match kind {
        None => Ok(None),
        Some("permanent") => Ok(Some(Ban::Permanent)),
        Some("temporary") => match expires_at {
            Some(expires_at) if expires_at > now => Ok(Some(Ban::Temporary(expires_at))),
            Some(_) => Ok(None),
            None => Err(StorageError::wrap(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                "temporary ban missing expires_at",
            ))),
        },
        Some(other) => Err(StorageError::wrap(std::io::Error::new(
            std::io::ErrorKind::InvalidData,
            format!("unknown ban kind: {other}"),
        ))),
    }
}

/// 在事务内撤销一个家族：家族置 revoked，族内未撤销 token 全部置 revoked。
async fn revoke_family_in_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Sqlite>,
    family_id: Uuid,
    now: DateTime<Utc>,
) -> Result<(), StorageError> {
    sqlx::query(
        "UPDATE refresh_token_families SET state = 'revoked', revoked_at = ?2 WHERE id = ?1",
    )
    .bind(family_id)
    .bind(now)
    .execute(&mut **tx)
    .await?;
    sqlx::query(
        "UPDATE refresh_tokens SET state = 'revoked' WHERE family_id = ?1 AND state != 'revoked'",
    )
    .bind(family_id)
    .execute(&mut **tx)
    .await?;
    Ok(())
}

#[async_trait]
impl crate::storage::EmailVerificationRepo for SqliteEmailVerificationRepo {
    async fn issue(
        &self,
        entry: &NewEmailVerification,
        created_at: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query(
            "INSERT INTO email_verifications (id, user_id, code_hash, attempts, expires_at, created_at, purpose)
             VALUES (?1, ?2, ?3, 0, ?4, ?5, ?6)",
        )
        .bind(Uuid::now_v7())
        .bind(entry.user_id.as_uuid())
        .bind(entry.code_hash.as_str())
        .bind(entry.expires_at)
        .bind(created_at)
        .bind(entry.purpose.as_str())
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn latest_unconsumed(
        &self,
        user_id: &UserId,
        purpose: VerificationPurpose,
    ) -> Result<Option<EmailVerification>, StorageError> {
        let row: Option<VerificationTuple> = sqlx::query_as(
            "SELECT id, code_hash, attempts, expires_at, created_at
             FROM email_verifications
             WHERE user_id = ?1 AND consumed_at IS NULL AND purpose = ?2
             ORDER BY created_at DESC LIMIT 1",
        )
        .bind(user_id.as_uuid())
        .bind(purpose.as_str())
        .fetch_optional(&self.pool)
        .await?;

        Ok(row.map(|(id, code_hash, attempts, expires_at, created_at)| EmailVerification {
            id,
            user_id: *user_id,
            code_hash,
            attempts,
            expires_at,
            created_at,
        }))
    }

    async fn record_attempt(&self, id: &Uuid) -> Result<(), StorageError> {
        sqlx::query("UPDATE email_verifications SET attempts = attempts + 1 WHERE id = ?1")
            .bind(id)
            .execute(&self.pool)
            .await?;
        Ok(())
    }

    async fn consume_and_mark_verified(
        &self,
        verification_id: &Uuid,
        user_id: &UserId,
        now: DateTime<Utc>,
    ) -> Result<bool, StorageError> {
        let mut tx = self.pool.begin().await?;
        let consumed = sqlx::query(
            "UPDATE email_verifications SET consumed_at = ?2
             WHERE id = ?1 AND consumed_at IS NULL",
        )
        .bind(verification_id)
        .bind(now)
        .execute(&mut *tx)
        .await?;
        if consumed.rows_affected() != 1 {
            return Ok(false);
        }
        sqlx::query("UPDATE users SET email_verified = 1 WHERE id = ?1")
            .bind(user_id.as_uuid())
            .execute(&mut *tx)
            .await?;
        tx.commit().await?;
        Ok(true)
    }

    async fn consume(
        &self,
        verification_id: &Uuid,
        now: DateTime<Utc>,
    ) -> Result<bool, StorageError> {
        let consumed = sqlx::query(
            "UPDATE email_verifications SET consumed_at = ?2
             WHERE id = ?1 AND consumed_at IS NULL",
        )
        .bind(verification_id)
        .bind(now)
        .execute(&self.pool)
        .await?;
        Ok(consumed.rows_affected() == 1)
    }
}

#[async_trait]
impl crate::storage::AccountRepo for SqliteAccountRepo {
    async fn apply_password_change(
        &self,
        user_id: &UserId,
        new_hash: &PasswordHash,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        let mut tx = self.pool.begin().await?;
        sqlx::query("UPDATE users SET password_hash = ?2 WHERE id = ?1")
            .bind(user_id.as_uuid())
            .bind(new_hash.as_str())
            .execute(&mut *tx)
            .await?;
        // 撤销在事务内一并递增 token_version 并整族撤销（红线 #1、ADR-0010）
        revoke_all_sessions_in_tx(&mut tx, user_id, now).await?;
        invalidate_challenges_in_tx(&mut tx, user_id, now).await?;
        tx.commit().await?;
        Ok(())
    }

    async fn apply_totp_disable(
        &self,
        user_id: &UserId,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        let mut tx = self.pool.begin().await?;
        sqlx::query(
            "UPDATE users SET totp_state = 'disabled', totp_secret_enc = NULL WHERE id = ?1",
        )
        .bind(user_id.as_uuid())
        .execute(&mut *tx)
        .await?;
        sqlx::query(
            "UPDATE totp_recovery_codes SET consumed_at = ?2
             WHERE user_id = ?1 AND consumed_at IS NULL",
        )
        .bind(user_id.as_uuid())
        .bind(now)
        .execute(&mut *tx)
        .await?;
        invalidate_challenges_in_tx(&mut tx, user_id, now).await?;
        // 关闭即下线（红线 #1）：降级与会话撤销同事务
        revoke_all_sessions_in_tx(&mut tx, user_id, now).await?;
        tx.commit().await?;
        Ok(())
    }

    async fn delete_user(&self, user_id: &UserId) -> Result<(), StorageError> {
        // 用户从属表统一以 ON DELETE CASCADE 引用 users（ADR-0007）；
        // 单条 DELETE 由 SQLite 在同一事务内完成级联。
        sqlx::query("DELETE FROM users WHERE id = ?1")
            .bind(user_id.as_uuid())
            .execute(&self.pool)
            .await?;
        Ok(())
    }
}

#[async_trait]
impl crate::storage::TotpRepo for SqliteTotpRepo {
    async fn find(
        &self,
        user_id: &UserId,
    ) -> Result<Option<(TotpState, Option<String>)>, StorageError> {
        let row: Option<(String, Option<String>)> =
            sqlx::query_as("SELECT totp_state, totp_secret_enc FROM users WHERE id = ?1")
                .bind(user_id.as_uuid())
                .fetch_optional(&self.pool)
                .await?;
        Ok(row.map(|(raw_state, secret)| {
            let state = TotpState::from_str_raw(&raw_state)
                .unwrap_or_else(|| unreachable!("库中 TOTP 状态值非法: {raw_state}"));
            (state, secret)
        }))
    }

    async fn set_pending(
        &self,
        user_id: &UserId,
        secret: &str,
        _now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query("UPDATE users SET totp_state = 'pending', totp_secret_enc = ?2 WHERE id = ?1")
            .bind(user_id.as_uuid())
            .bind(secret)
            .execute(&self.pool)
            .await?;
        Ok(())
    }

    async fn enable(&self, user_id: &UserId, _now: DateTime<Utc>) -> Result<(), StorageError> {
        sqlx::query(
            "UPDATE users SET totp_state = 'enabled' WHERE id = ?1 AND totp_state = 'pending'",
        )
        .bind(user_id.as_uuid())
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn add_recovery_codes(
        &self,
        user_id: &UserId,
        hashes: Vec<String>,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        let mut tx = self.pool.begin().await?;
        for hash in hashes {
            sqlx::query(
                "INSERT INTO totp_recovery_codes (id, user_id, code_hash, created_at)
                 VALUES (?1, ?2, ?3, ?4)",
            )
            .bind(Uuid::now_v7())
            .bind(user_id.as_uuid())
            .bind(hash)
            .bind(now)
            .execute(&mut *tx)
            .await?;
        }
        tx.commit().await?;
        Ok(())
    }

    async fn consume_recovery_code(
        &self,
        user_id: &UserId,
        code_hash: &str,
        now: DateTime<Utc>,
    ) -> Result<bool, StorageError> {
        let consumed = sqlx::query(
            "UPDATE totp_recovery_codes SET consumed_at = ?3
             WHERE user_id = ?1 AND code_hash = ?2 AND consumed_at IS NULL",
        )
        .bind(user_id.as_uuid())
        .bind(code_hash)
        .bind(now)
        .execute(&self.pool)
        .await?;
        Ok(consumed.rows_affected() == 1)
    }

    async fn recovery_codes(
        &self,
        user_id: &UserId,
    ) -> Result<Vec<RecoveryCodeRecord>, StorageError> {
        let rows: Vec<RecoveryTuple> =
            sqlx::query_as("SELECT id, consumed_at FROM totp_recovery_codes WHERE user_id = ?1")
                .bind(user_id.as_uuid())
                .fetch_all(&self.pool)
                .await?;
        Ok(rows
            .into_iter()
            .map(|(id, consumed_at)| RecoveryCodeRecord { id, consumed_at })
            .collect())
    }

    async fn issue_challenge(
        &self,
        user_id: &UserId,
        token_hash: &str,
        expires_at: DateTime<Utc>,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query(
            "INSERT INTO totp_login_challenges (id, user_id, token_hash, attempts, expires_at, created_at)
             VALUES (?1, ?2, ?3, 0, ?4, ?5)",
        )
        .bind(Uuid::now_v7())
        .bind(user_id.as_uuid())
        .bind(token_hash)
        .bind(expires_at)
        .bind(now)
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn find_challenge(
        &self,
        token_hash: &str,
    ) -> Result<Option<TotpChallenge>, StorageError> {
        let row: Option<ChallengeTuple> = sqlx::query_as(
            "SELECT id, user_id, attempts, expires_at
             FROM totp_login_challenges WHERE token_hash = ?1",
        )
        .bind(token_hash)
        .fetch_optional(&self.pool)
        .await?;
        Ok(row.map(|(id, user_id, attempts, expires_at)| TotpChallenge {
            id,
            user_id: UserId::from_uuid(user_id),
            attempts,
            expires_at,
        }))
    }

    async fn record_challenge_attempt(&self, id: &Uuid) -> Result<(), StorageError> {
        sqlx::query("UPDATE totp_login_challenges SET attempts = attempts + 1 WHERE id = ?1")
            .bind(id)
            .execute(&self.pool)
            .await?;
        Ok(())
    }

    async fn consume_challenge(&self, id: &Uuid, now: DateTime<Utc>) -> Result<bool, StorageError> {
        let consumed = sqlx::query(
            "UPDATE totp_login_challenges SET consumed_at = ?2
             WHERE id = ?1 AND consumed_at IS NULL",
        )
        .bind(id)
        .bind(now)
        .execute(&self.pool)
        .await?;
        Ok(consumed.rows_affected() == 1)
    }
}

#[async_trait]
impl crate::storage::OAuthRepo for SqliteOAuthRepo {
    async fn find_client(&self, client_id: &str) -> Result<Option<OAuthClient>, StorageError> {
        let row: Option<ClientTuple> = sqlx::query_as(
            "SELECT id, name, redirect_uri, enabled FROM oauth_clients WHERE id = ?1",
        )
        .bind(client_id)
        .fetch_optional(&self.pool)
        .await?;
        Ok(row.map(|(id, name, redirect_uri, enabled)| OAuthClient {
            id,
            name,
            redirect_uri,
            enabled,
        }))
    }

    async fn find_client_secret(&self, client_id: &str) -> Result<Option<String>, StorageError> {
        let row: Option<Option<String>> =
            sqlx::query_scalar("SELECT secret_hash FROM oauth_clients WHERE id = ?1")
                .bind(client_id)
                .fetch_optional(&self.pool)
                .await?;
        Ok(row.flatten())
    }

    async fn upsert_client(
        &self,
        client: &OAuthClient,
        secret_hash: &str,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query(
            "INSERT INTO oauth_clients (id, name, secret_hash, redirect_uri, enabled, created_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)
             ON CONFLICT(id) DO UPDATE SET
                 name = excluded.name,
                 secret_hash = excluded.secret_hash,
                 redirect_uri = excluded.redirect_uri,
                 enabled = excluded.enabled",
        )
        .bind(&client.id)
        .bind(&client.name)
        .bind(secret_hash)
        .bind(&client.redirect_uri)
        .bind(client.enabled)
        .bind(now)
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn create_code(
        &self,
        code_hash: &str,
        code: &NewAuthorizationCode,
        created_at: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query(
            "INSERT INTO oauth_authorization_codes
             (id, code_hash, client_id, user_id, redirect_uri, scope, code_challenge, expires_at, created_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)",
        )
        .bind(Uuid::now_v7())
        .bind(code_hash)
        .bind(&code.client_id)
        .bind(code.user_id.as_uuid())
        .bind(&code.redirect_uri)
        .bind(&code.scope)
        .bind(&code.code_challenge)
        .bind(code.expires_at)
        .bind(created_at)
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn find_code(&self, code_hash: &str) -> Result<Option<AuthorizationCode>, StorageError> {
        let row: Option<CodeTuple> = sqlx::query_as(
            "SELECT id, client_id, user_id, redirect_uri, scope, code_challenge, expires_at
                 FROM oauth_authorization_codes WHERE code_hash = ?1",
        )
        .bind(code_hash)
        .fetch_optional(&self.pool)
        .await?;
        Ok(row.map(|(id, client_id, user_id, redirect_uri, scope, code_challenge, expires_at)| {
            AuthorizationCode {
                id,
                client_id,
                user_id: UserId::from_uuid(user_id),
                redirect_uri,
                scope,
                code_challenge,
                expires_at,
            }
        }))
    }

    async fn consume_code(&self, code_id: &Uuid, now: DateTime<Utc>) -> Result<bool, StorageError> {
        let consumed = sqlx::query(
            "UPDATE oauth_authorization_codes SET consumed_at = ?2
             WHERE id = ?1 AND consumed_at IS NULL",
        )
        .bind(code_id)
        .bind(now)
        .execute(&self.pool)
        .await?;
        Ok(consumed.rows_affected() == 1)
    }
}

/// 已消费记录保留期：保留一天便于排查，之后清理。
const CONSUMED_RETENTION: TimeDelta = TimeDelta::days(1);
/// 空 refresh 家族保留期：留出观察窗口，不因一次清理立即抹掉。
const EMPTY_FAMILY_RETENTION: TimeDelta = TimeDelta::days(30);

#[async_trait]
impl crate::storage::MaintenanceRepo for SqliteMaintenanceRepo {
    async fn purge_expired(&self, now: DateTime<Utc>) -> Result<CleanupReport, StorageError> {
        let consumed_cutoff = now - CONSUMED_RETENTION;
        let family_cutoff = now - EMPTY_FAMILY_RETENTION;
        let mut tx = self.pool.begin().await?;

        let refresh_tokens = sqlx::query("DELETE FROM refresh_tokens WHERE expires_at < ?1")
            .bind(now)
            .execute(&mut *tx)
            .await?
            .rows_affected();

        let refresh_families = sqlx::query(
            "DELETE FROM refresh_token_families
             WHERE created_at < ?1
               AND NOT EXISTS (
                   SELECT 1 FROM refresh_tokens WHERE family_id = refresh_token_families.id
               )",
        )
        .bind(family_cutoff)
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let email_verifications = sqlx::query(
            "DELETE FROM email_verifications
             WHERE expires_at < ?1
                OR (consumed_at IS NOT NULL AND consumed_at < ?2)",
        )
        .bind(now)
        .bind(consumed_cutoff)
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let totp_recovery_codes = sqlx::query(
            "DELETE FROM totp_recovery_codes
             WHERE consumed_at IS NOT NULL AND consumed_at < ?1",
        )
        .bind(consumed_cutoff)
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let totp_login_challenges = sqlx::query(
            "DELETE FROM totp_login_challenges
             WHERE expires_at < ?1
                OR (consumed_at IS NOT NULL AND consumed_at < ?2)",
        )
        .bind(now)
        .bind(consumed_cutoff)
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let oauth_authorization_codes = sqlx::query(
            "DELETE FROM oauth_authorization_codes
             WHERE expires_at < ?1
                OR (consumed_at IS NOT NULL AND consumed_at < ?2)",
        )
        .bind(now)
        .bind(consumed_cutoff)
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let expired_bans =
            sqlx::query("DELETE FROM user_bans WHERE kind = 'temporary' AND expires_at <= ?1")
                .bind(now)
                .execute(&mut *tx)
                .await?
                .rows_affected();

        let social_login_states =
            sqlx::query("DELETE FROM social_login_states WHERE expires_at < ?1")
                .bind(now)
                .execute(&mut *tx)
                .await?
                .rows_affected();

        let data_export_tokens = sqlx::query(
            "DELETE FROM data_export_tokens
             WHERE expires_at < ?1
                OR (consumed_at IS NOT NULL AND consumed_at < ?2)",
        )
        .bind(now)
        .bind(consumed_cutoff)
        .execute(&mut *tx)
        .await?
        .rows_affected();

        tx.commit().await?;
        Ok(CleanupReport {
            refresh_tokens,
            refresh_families,
            email_verifications,
            totp_recovery_codes,
            totp_login_challenges,
            oauth_authorization_codes,
            expired_bans,
            social_login_states,
            data_export_tokens,
        })
    }
}

#[async_trait]
impl crate::storage::AdminRepo for SqliteAdminRepo {
    async fn stats(
        &self,
        now: DateTime<Utc>,
        since: DateTime<Utc>,
    ) -> Result<AdminStats, StorageError> {
        let total_users: i64 =
            sqlx::query_scalar("SELECT COUNT(*) FROM users").fetch_one(&self.pool).await?;
        let banned_users: i64 = sqlx::query_scalar(
            "SELECT COUNT(*) FROM user_bans WHERE kind = 'permanent' OR expires_at > ?1",
        )
        .bind(now)
        .fetch_one(&self.pool)
        .await?;
        let new_users_today: i64 =
            sqlx::query_scalar("SELECT COUNT(*) FROM users WHERE created_at >= ?1")
                .bind(since)
                .fetch_one(&self.pool)
                .await?;
        Ok(AdminStats { total_users, banned_users, new_users_today })
    }

    async fn list_users(
        &self,
        search: Option<&str>,
        limit: i64,
        offset: i64,
        now: DateTime<Utc>,
    ) -> Result<Vec<AdminUserRow>, StorageError> {
        let rows: Vec<AdminUserTuple> = sqlx::query_as(
            "SELECT u.id, u.username, u.email, u.email_verified, u.created_at,
                    u.avatar_version, b.kind, b.expires_at
             FROM users u
             LEFT JOIN user_bans b ON b.user_id = u.id
             WHERE ?1 = ''
                OR u.email LIKE '%' || ?1 || '%'
                OR u.username LIKE '%' || ?1 || '%'
             ORDER BY u.created_at DESC
             LIMIT ?2 OFFSET ?3",
        )
        .bind(search.unwrap_or(""))
        .bind(limit)
        .bind(offset)
        .fetch_all(&self.pool)
        .await?;

        rows.into_iter()
            .map(
                |(
                    id,
                    username,
                    email,
                    email_verified,
                    created_at,
                    avatar_version,
                    kind,
                    expires_at,
                )| {
                    let ban = decode_ban(kind.as_deref(), expires_at, now)?;
                    Ok(AdminUserRow {
                        id: UserId::from_uuid(id),
                        username,
                        email,
                        email_verified,
                        created_at,
                        avatar_version,
                        ban,
                    })
                },
            )
            .collect()
    }

    async fn count_users(&self, search: Option<&str>) -> Result<i64, StorageError> {
        let total: i64 = sqlx::query_scalar(
            "SELECT COUNT(*) FROM users u
             WHERE ?1 = ''
                OR u.email LIKE '%' || ?1 || '%'
                OR u.username LIKE '%' || ?1 || '%'",
        )
        .bind(search.unwrap_or(""))
        .fetch_one(&self.pool)
        .await?;
        Ok(total)
    }

    async fn ban_user(
        &self,
        user_id: &UserId,
        ban: &Ban,
        reason: &str,
        actor_id: &UserId,
        now: DateTime<Utc>,
    ) -> Result<bool, StorageError> {
        let mut tx = self.pool.begin().await?;
        let exists: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM users WHERE id = ?1")
            .bind(user_id.as_uuid())
            .fetch_one(&mut *tx)
            .await?;
        if exists == 0 {
            return Ok(false);
        }

        let (kind, expires_at) = match ban {
            Ban::Permanent => ("permanent", None),
            Ban::Temporary(expires_at) => ("temporary", Some(*expires_at)),
        };
        sqlx::query(
            "INSERT INTO user_bans (user_id, kind, expires_at, reason, created_at, created_by)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)
             ON CONFLICT(user_id) DO UPDATE SET
                 kind = excluded.kind,
                 expires_at = excluded.expires_at,
                 reason = excluded.reason,
                 created_at = excluded.created_at,
                 created_by = excluded.created_by",
        )
        .bind(user_id.as_uuid())
        .bind(kind)
        .bind(expires_at)
        .bind(reason)
        .bind(now)
        .bind(actor_id.as_uuid())
        .execute(&mut *tx)
        .await?;
        // 封禁与撤销会话必须同事务：避免封禁生效但旧 refresh 仍可轮转
        revoke_all_sessions_in_tx(&mut tx, user_id, now).await?;
        tx.commit().await?;
        Ok(true)
    }

    async fn unban_user(&self, user_id: &UserId) -> Result<bool, StorageError> {
        let deleted = sqlx::query("DELETE FROM user_bans WHERE user_id = ?1")
            .bind(user_id.as_uuid())
            .execute(&self.pool)
            .await?;
        Ok(deleted.rows_affected() == 1)
    }

    async fn record_audit(
        &self,
        entry: &NewAuditEntry,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        let details = entry
            .details
            .as_ref()
            .map(serde_json::to_string)
            .transpose()
            .map_err(StorageError::wrap)?;
        sqlx::query(
            "INSERT INTO audit_logs (id, actor_id, action, target_id, details, created_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
        )
        .bind(Uuid::now_v7())
        .bind(entry.actor_id.as_uuid())
        .bind(entry.action.as_str())
        .bind(entry.target_id.map(|id| *id.as_uuid()))
        .bind(details)
        .bind(now)
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn list_audit(&self, limit: i64, offset: i64) -> Result<Vec<AuditEntry>, StorageError> {
        let rows: Vec<AuditTuple> = sqlx::query_as(
            "SELECT id, actor_id, action, target_id, details, created_at
                 FROM audit_logs
                 ORDER BY created_at DESC
                 LIMIT ?1 OFFSET ?2",
        )
        .bind(limit)
        .bind(offset)
        .fetch_all(&self.pool)
        .await?;

        rows.into_iter().map(audit_from_tuple).collect()
    }

    async fn count_audit(&self) -> Result<i64, StorageError> {
        let total: i64 =
            sqlx::query_scalar("SELECT COUNT(*) FROM audit_logs").fetch_one(&self.pool).await?;
        Ok(total)
    }

    async fn list_audit_for_user(&self, user_id: &UserId) -> Result<Vec<AuditEntry>, StorageError> {
        let rows: Vec<AuditTuple> = sqlx::query_as(
            "SELECT id, actor_id, action, target_id, details, created_at
             FROM audit_logs
             WHERE actor_id = ?1 OR target_id = ?1
             ORDER BY created_at ASC",
        )
        .bind(user_id.as_uuid())
        .fetch_all(&self.pool)
        .await?;

        rows.into_iter().map(audit_from_tuple).collect()
    }
}

#[async_trait]
impl crate::storage::ExportRepo for SqliteExportRepo {
    async fn issue(
        &self,
        user_id: &UserId,
        token_hash: &str,
        expires_at: DateTime<Utc>,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        // 同一用户只保留最新一条未消费令牌；旧链接申请新链接后立即失效
        let mut tx = self.pool.begin().await?;
        sqlx::query("DELETE FROM data_export_tokens WHERE user_id = ?1 AND consumed_at IS NULL")
            .bind(user_id.as_uuid())
            .execute(&mut *tx)
            .await?;
        sqlx::query(
            "INSERT INTO data_export_tokens (token_hash, user_id, expires_at, created_at)
             VALUES (?1, ?2, ?3, ?4)",
        )
        .bind(token_hash)
        .bind(user_id.as_uuid())
        .bind(expires_at)
        .bind(now)
        .execute(&mut *tx)
        .await?;
        tx.commit().await?;
        Ok(())
    }

    async fn consume(
        &self,
        token_hash: &str,
        now: DateTime<Utc>,
    ) -> Result<Option<UserId>, StorageError> {
        let user_id: Option<Uuid> = sqlx::query_scalar(
            "UPDATE data_export_tokens SET consumed_at = ?2
             WHERE token_hash = ?1
               AND consumed_at IS NULL
               AND expires_at > ?2
             RETURNING user_id",
        )
        .bind(token_hash)
        .bind(now)
        .fetch_optional(&self.pool)
        .await?;
        Ok(user_id.map(UserId::from_uuid))
    }
}

#[async_trait]
impl crate::storage::SocialRepo for SqliteSocialRepo {
    async fn find_identity(
        &self,
        provider: SocialProvider,
        provider_user_id: &str,
    ) -> Result<Option<SocialIdentity>, StorageError> {
        let row: Option<SocialIdentityTuple> = sqlx::query_as(
            "SELECT id, user_id, provider, provider_user_id, display_name, created_at
                 FROM user_social_identities
                 WHERE provider = ?1 AND provider_user_id = ?2",
        )
        .bind(provider.as_str())
        .bind(provider_user_id)
        .fetch_optional(&self.pool)
        .await?;

        let Some((id, user_id, raw_provider, provider_user_id, display_name, created_at)) = row
        else {
            return Ok(None);
        };
        let Some(provider) = SocialProvider::from_str_raw(&raw_provider) else {
            return Err(unknown_social_value("provider", &raw_provider));
        };
        Ok(Some(SocialIdentity {
            id,
            user_id: UserId::from_uuid(user_id),
            provider,
            provider_user_id,
            display_name,
            created_at,
        }))
    }

    async fn link_identity(
        &self,
        identity: &NewSocialIdentity,
        now: DateTime<Utc>,
    ) -> Result<(), LinkIdentityError> {
        let result = sqlx::query(
            "INSERT INTO user_social_identities
             (id, user_id, provider, provider_user_id, display_name, created_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
        )
        .bind(Uuid::now_v7())
        .bind(identity.user_id.as_uuid())
        .bind(identity.provider.as_str())
        .bind(identity.provider_user_id.as_str())
        .bind(identity.display_name.as_deref())
        .bind(now)
        .execute(&self.pool)
        .await;

        match result {
            Ok(_) => Ok(()),
            Err(SqlxError::Database(db)) if db.is_unique_violation() => {
                Err(LinkIdentityError::Conflict)
            }
            Err(err) => Err(LinkIdentityError::from(err)),
        }
    }

    async fn unlink(
        &self,
        user_id: &UserId,
        provider: SocialProvider,
    ) -> Result<bool, StorageError> {
        let deleted =
            sqlx::query("DELETE FROM user_social_identities WHERE user_id = ?1 AND provider = ?2")
                .bind(user_id.as_uuid())
                .bind(provider.as_str())
                .execute(&self.pool)
                .await?;
        Ok(deleted.rows_affected() == 1)
    }

    async fn list_for_user(&self, user_id: &UserId) -> Result<Vec<SocialIdentity>, StorageError> {
        let rows: Vec<SocialIdentityTuple> = sqlx::query_as(
            "SELECT id, user_id, provider, provider_user_id, display_name, created_at
                 FROM user_social_identities
                 WHERE user_id = ?1
                 ORDER BY created_at ASC",
        )
        .bind(user_id.as_uuid())
        .fetch_all(&self.pool)
        .await?;

        rows.into_iter()
            .map(|(id, user_id, raw_provider, provider_user_id, display_name, created_at)| {
                let Some(provider) = SocialProvider::from_str_raw(&raw_provider) else {
                    return Err(unknown_social_value("provider", &raw_provider));
                };
                Ok(SocialIdentity {
                    id,
                    user_id: UserId::from_uuid(user_id),
                    provider,
                    provider_user_id,
                    display_name,
                    created_at,
                })
            })
            .collect()
    }

    async fn put_state(
        &self,
        state: &SocialLoginState,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query(
            "INSERT INTO social_login_states
             (state, provider, code_verifier, action, user_id, expires_at, created_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)",
        )
        .bind(state.state.as_str())
        .bind(state.provider.as_str())
        .bind(state.code_verifier.as_str())
        .bind(state.action.as_str())
        .bind(state.user_id.map(|id| *id.as_uuid()))
        .bind(state.expires_at)
        .bind(now)
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn take_state(&self, state: &str) -> Result<Option<SocialLoginState>, StorageError> {
        let mut tx = self.pool.begin().await?;
        let row: Option<SocialStateTuple> = sqlx::query_as(
            "SELECT state, provider, code_verifier, action, user_id, expires_at
                 FROM social_login_states WHERE state = ?1",
        )
        .bind(state)
        .fetch_optional(&mut *tx)
        .await?;
        let Some((state, raw_provider, code_verifier, raw_action, user_id, expires_at)) = row
        else {
            return Ok(None);
        };
        // 取出即删除：单次消费，重放同一 state 落到上面的 None 分支
        sqlx::query("DELETE FROM social_login_states WHERE state = ?1")
            .bind(state.as_str())
            .execute(&mut *tx)
            .await?;
        tx.commit().await?;

        let Some(provider) = SocialProvider::from_str_raw(&raw_provider) else {
            return Err(unknown_social_value("provider", &raw_provider));
        };
        let Some(action) = SocialLoginAction::from_str_raw(&raw_action) else {
            return Err(unknown_social_value("action", &raw_action));
        };
        Ok(Some(SocialLoginState {
            state,
            provider,
            code_verifier,
            action,
            user_id: user_id.map(UserId::from_uuid),
            expires_at,
        }))
    }
}

/// 库中枚举列出现未知值时构造存储故障；未知值属数据损坏，不得当作「无记录」。
fn unknown_social_value(column: &str, value: &str) -> StorageError {
    StorageError::wrap(std::io::Error::new(
        std::io::ErrorKind::InvalidData,
        format!("unknown social {column}: {value}"),
    ))
}
