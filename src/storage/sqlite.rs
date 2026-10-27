//! sqlx + SQLite 实现（ADR-0003）：用户仓储与 refresh 家族仓储共享连接池。

use std::path::Path;
use std::time::Duration;

use async_trait::async_trait;
use chrono::{DateTime, TimeDelta, Utc};
use sqlx::Error as SqlxError;
use sqlx::sqlite::{SqliteConnectOptions, SqliteJournalMode, SqlitePool};
use uuid::Uuid;

use crate::model::audit::{AuditEntry, NewAuditEntry};
use crate::model::oauth2::{AuthorizationCode, NewAuthorizationCode, OAuthClient};
use crate::model::password::PasswordHash;
use crate::model::tokens::NewRefreshFamily;
use crate::model::totp::{RecoveryCodeRecord, TotpChallenge, TotpState};
use crate::model::user::{Ban, LoginCredentials, User, UserAccess, UserId};
use crate::model::verification::{EmailVerification, NewEmailVerification, VerificationPurpose};
use crate::storage::{
    AdminStats, AdminUserRow, CleanupReport, ClientBinding, RotateRefreshError, RotatedSession,
    StorageError, UserRepo, UserRepoError,
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

/// 打开既有数据库文件建立带 PRAGMA 的连接池；不创建文件、不跑迁移——迁移由
/// `sqlx migrate` 作为独立部署步骤执行，启动路径不执行 DDL（§1）。
///
/// # Errors
/// 文件不存在、权限不足或连接失败时返回 [`StorageError`]。
pub async fn connect_pool(path: &Path) -> Result<SqlitePool, StorageError> {
    let options = SqliteConnectOptions::new()
        .filename(path)
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

#[async_trait]
impl UserRepo for SqliteUserRepo {
    async fn insert(&self, user: &User, password: &PasswordHash) -> Result<(), UserRepoError> {
        let result = sqlx::query!(
            "INSERT INTO users (id, username, email, created_at, password_hash, email_verified)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            user.id.as_uuid(),
            user.username,
            user.email,
            user.created_at,
            password.as_str(),
            user.email_verified,
        )
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

    async fn find_by_email(&self, email: &str) -> Result<Option<User>, UserRepoError> {
        // SQLite TEXT 列被推断为 String，uuid/chrono 解码须经列类型覆盖声明
        let row = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid", username, email,
                      email_verified AS "email_verified: bool",
                      created_at AS "created_at: chrono::DateTime<chrono::Utc>"
               FROM users WHERE email = ?1"#,
            email,
        )
        .fetch_optional(&self.pool)
        .await?;

        Ok(row.map(|r| User {
            id: UserId::from_uuid(r.id),
            username: r.username,
            email: r.email,
            created_at: r.created_at,
            email_verified: r.email_verified,
        }))
    }

    async fn find_by_username(&self, username: &str) -> Result<Option<User>, UserRepoError> {
        let row = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid", username, email,
                      email_verified AS "email_verified: bool",
                      created_at AS "created_at: chrono::DateTime<chrono::Utc>"
               FROM users WHERE username = ?1"#,
            username,
        )
        .fetch_optional(&self.pool)
        .await?;

        Ok(row.map(|r| User {
            id: UserId::from_uuid(r.id),
            username: r.username,
            email: r.email,
            created_at: r.created_at,
            email_verified: r.email_verified,
        }))
    }

    async fn find_by_id(
        &self,
        user_id: &UserId,
    ) -> Result<Option<LoginCredentials>, UserRepoError> {
        let row = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid", username, email,
                      email_verified AS "email_verified: bool",
                      created_at AS "created_at: chrono::DateTime<chrono::Utc>",
                      password_hash
               FROM users WHERE id = ?1"#,
            user_id.as_uuid(),
        )
        .fetch_optional(&self.pool)
        .await?;

        let Some(row) = row else { return Ok(None) };
        // 存量哈希损坏是存储数据问题，须显式报错而非静默当作无口令
        let hash = row
            .password_hash
            .map(PasswordHash::from_encoded)
            .transpose()
            .map_err(|err| UserRepoError::Storage(StorageError::wrap(err)))?;

        Ok(hash.map(|password_hash| LoginCredentials {
            user: User {
                id: UserId::from_uuid(row.id),
                username: row.username,
                email: row.email,
                created_at: row.created_at,
                email_verified: row.email_verified,
            },
            password_hash,
        }))
    }

    async fn find_access_state(
        &self,
        user_id: &UserId,
        now: DateTime<Utc>,
    ) -> Result<Option<UserAccess>, UserRepoError> {
        let row = sqlx::query!(
            r#"SELECT u.email,
                      u.email_verified AS "email_verified: bool",
                      u.token_version AS "token_version: i64",
                      b.kind,
                      b.expires_at AS "expires_at: Option<chrono::DateTime<chrono::Utc>>"
               FROM users u
               LEFT JOIN user_bans b ON b.user_id = u.id
               WHERE u.id = ?1"#,
            user_id.as_uuid(),
        )
        .fetch_optional(&self.pool)
        .await?;

        let Some(row) = row else { return Ok(None) };
        let ban = decode_ban(row.kind.as_deref(), row.expires_at.flatten(), now)
            .map_err(UserRepoError::Storage)?;
        Ok(Some(UserAccess {
            email: row.email,
            email_verified: row.email_verified,
            token_version: row.token_version,
            ban,
        }))
    }

    async fn find_login_credentials(
        &self,
        identifier: &str,
    ) -> Result<Option<LoginCredentials>, UserRepoError> {
        // 哈希列可空：无口令用户按 None 返回，调用方据此走时序对齐路径
        let row = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid", username, email,
                      email_verified AS "email_verified: bool",
                      created_at AS "created_at: chrono::DateTime<chrono::Utc>",
                      password_hash
               FROM users WHERE email = ?1 OR username = ?1"#,
            identifier,
        )
        .fetch_optional(&self.pool)
        .await?;

        let Some(row) = row else { return Ok(None) };
        // 存量哈希损坏是存储数据问题，须显式报错而非静默当作无口令
        let hash = row
            .password_hash
            .map(PasswordHash::from_encoded)
            .transpose()
            .map_err(|err| UserRepoError::Storage(StorageError::wrap(err)))?;

        Ok(hash.map(|password_hash| LoginCredentials {
            user: User {
                id: UserId::from_uuid(row.id),
                username: row.username,
                email: row.email,
                created_at: row.created_at,
                email_verified: row.email_verified,
            },
            password_hash,
        }))
    }

    async fn update_password_hash(
        &self,
        user_id: &UserId,
        password: &PasswordHash,
    ) -> Result<(), UserRepoError> {
        sqlx::query!(
            "UPDATE users SET password_hash = ?2 WHERE id = ?1",
            user_id.as_uuid(),
            password.as_str(),
        )
        .execute(&self.pool)
        .await?;
        Ok(())
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
        sqlx::query!(
            "INSERT INTO refresh_token_families (id, user_id, state, created_at, client_id, scope)
             VALUES (?1, ?2, 'active', ?3, ?4, ?5)",
            family.family_id,
            family.user_id.as_uuid(),
            created_at,
            family.client_id,
            family.scope,
        )
        .execute(&mut *tx)
        .await?;
        sqlx::query!(
            "INSERT INTO refresh_tokens (id, family_id, token_hash, state, created_at, expires_at)
             VALUES (?1, ?2, ?3, 'active', ?4, ?5)",
            Uuid::now_v7(),
            family.family_id,
            family.token_hash,
            created_at,
            family.expires_at,
        )
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
        let presented = sqlx::query!(
            r#"SELECT t.id AS "id: uuid::Uuid",
                      t.family_id AS "family_id: uuid::Uuid",
                      t.state,
                      t.expires_at AS "expires_at: chrono::DateTime<chrono::Utc>",
                      f.state AS "family_state",
                      f.user_id AS "user_id: uuid::Uuid",
                      f.client_id,
                      f.scope
               FROM refresh_tokens t
               JOIN refresh_token_families f ON f.id = t.family_id
               WHERE t.token_hash = ?1"#,
            presented_hash,
        )
        .fetch_optional(&mut *tx)
        .await?;

        let Some(presented) = presented else {
            return Err(RotateRefreshError::Invalid);
        };

        // 先于状态判断：绑定不符的呈递不触碰家族状态
        let binding_matches = match binding {
            ClientBinding::FirstParty => presented.client_id.is_none(),
            ClientBinding::Client(client_id) => presented.client_id.as_deref() == Some(client_id),
        };
        if !binding_matches {
            return Err(RotateRefreshError::ClientMismatch);
        }

        match presented.state.as_str() {
            // 呈递已轮转 token = 重放：整族撤销后拒绝（事务保证撤销也落库）
            "rotated" => {
                revoke_family_in_tx(&mut tx, presented.family_id, now).await?;
                tx.commit().await?;
                Err(RotateRefreshError::Replay)
            }
            "active" => {
                if presented.expires_at <= now || presented.family_state != "active" {
                    return Err(RotateRefreshError::Invalid);
                }
                let user_id = UserId::from_uuid(presented.user_id);

                let rotated = sqlx::query!(
                    "UPDATE refresh_tokens SET state = 'rotated', rotated_at = ?2
                     WHERE id = ?1 AND state = 'active'",
                    presented.id,
                    now,
                )
                .execute(&mut *tx)
                .await?;
                // 并发下状态可能已被他者改变；0 行受影响说明此刻不再是 active，按重放处置
                if rotated.rows_affected() != 1 {
                    revoke_family_in_tx(&mut tx, presented.family_id, now).await?;
                    tx.commit().await?;
                    return Err(RotateRefreshError::Replay);
                }

                sqlx::query!(
                    "INSERT INTO refresh_tokens (id, family_id, token_hash, state, created_at, expires_at)
                     VALUES (?1, ?2, ?3, 'active', ?4, ?5)",
                    Uuid::now_v7(),
                    presented.family_id,
                    new_hash,
                    now,
                    new_expires_at,
                )
                .execute(&mut *tx)
                .await?;
                tx.commit().await?;
                Ok(RotatedSession { user_id, scope: presented.scope })
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
    sqlx::query!(
        "UPDATE users SET token_version = token_version + 1 WHERE id = ?1",
        user_id.as_uuid(),
    )
    .execute(&mut **tx)
    .await?;

    let families = sqlx::query!(
        r#"SELECT id AS "id: uuid::Uuid" FROM refresh_token_families
           WHERE user_id = ?1 AND state = 'active'"#,
        user_id.as_uuid(),
    )
    .fetch_all(&mut **tx)
    .await?;
    for family in families {
        revoke_family_in_tx(tx, family.id, now).await?;
    }
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
    sqlx::query!(
        "UPDATE refresh_token_families SET state = 'revoked', revoked_at = ?2 WHERE id = ?1",
        family_id,
        now,
    )
    .execute(&mut **tx)
    .await?;
    sqlx::query!(
        "UPDATE refresh_tokens SET state = 'revoked' WHERE family_id = ?1 AND state != 'revoked'",
        family_id,
    )
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
        sqlx::query!(
            "INSERT INTO email_verifications (id, user_id, code_hash, attempts, expires_at, created_at, purpose)
             VALUES (?1, ?2, ?3, 0, ?4, ?5, ?6)",
            Uuid::now_v7(),
            entry.user_id.as_uuid(),
            entry.code_hash,
            entry.expires_at,
            created_at,
            entry.purpose.as_str(),
        )
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn latest_unconsumed(
        &self,
        user_id: &UserId,
        purpose: VerificationPurpose,
    ) -> Result<Option<EmailVerification>, StorageError> {
        let row = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid", code_hash, attempts AS "attempts: u32",
                      expires_at AS "expires_at: chrono::DateTime<chrono::Utc>",
                      created_at AS "created_at: chrono::DateTime<chrono::Utc>"
               FROM email_verifications
               WHERE user_id = ?1 AND consumed_at IS NULL AND purpose = ?2
               ORDER BY created_at DESC LIMIT 1"#,
            user_id.as_uuid(),
            purpose.as_str(),
        )
        .fetch_optional(&self.pool)
        .await?;

        Ok(row.map(|r| EmailVerification {
            id: r.id,
            user_id: *user_id,
            code_hash: r.code_hash,
            attempts: r.attempts,
            expires_at: r.expires_at,
            created_at: r.created_at,
        }))
    }

    async fn record_attempt(&self, id: &Uuid) -> Result<(), StorageError> {
        sqlx::query!("UPDATE email_verifications SET attempts = attempts + 1 WHERE id = ?1", id,)
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
        let consumed = sqlx::query!(
            "UPDATE email_verifications SET consumed_at = ?2
             WHERE id = ?1 AND consumed_at IS NULL",
            verification_id,
            now,
        )
        .execute(&mut *tx)
        .await?;
        if consumed.rows_affected() != 1 {
            return Ok(false);
        }
        sqlx::query!("UPDATE users SET email_verified = 1 WHERE id = ?1", user_id.as_uuid(),)
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
        let consumed = sqlx::query!(
            "UPDATE email_verifications SET consumed_at = ?2
             WHERE id = ?1 AND consumed_at IS NULL",
            verification_id,
            now,
        )
        .execute(&self.pool)
        .await?;
        Ok(consumed.rows_affected() == 1)
    }
}

#[async_trait]
impl crate::storage::AccountRepo for SqliteAccountRepo {
    async fn delete_user(&self, user_id: &UserId) -> Result<(), StorageError> {
        // 用户从属表统一以 ON DELETE CASCADE 引用 users（ADR-0007）；
        // 单条 DELETE 由 SQLite 在同一事务内完成级联。
        sqlx::query!("DELETE FROM users WHERE id = ?1", user_id.as_uuid())
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
        let row = sqlx::query!(
            r#"SELECT totp_state, totp_secret_enc FROM users WHERE id = ?1"#,
            user_id.as_uuid(),
        )
        .fetch_optional(&self.pool)
        .await?;
        Ok(row.map(|r| {
            let state = TotpState::from_str_raw(&r.totp_state)
                .unwrap_or_else(|| unreachable!("库中 TOTP 状态值非法: {}", r.totp_state));
            (state, r.totp_secret_enc)
        }))
    }

    async fn set_pending(
        &self,
        user_id: &UserId,
        secret: &str,
        _now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query!(
            "UPDATE users SET totp_state = 'pending', totp_secret_enc = ?2 WHERE id = ?1",
            user_id.as_uuid(),
            secret,
        )
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn enable(&self, user_id: &UserId, _now: DateTime<Utc>) -> Result<(), StorageError> {
        sqlx::query!(
            "UPDATE users SET totp_state = 'enabled' WHERE id = ?1 AND totp_state = 'pending'",
            user_id.as_uuid(),
        )
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn disable(&self, user_id: &UserId, now: DateTime<Utc>) -> Result<(), StorageError> {
        let mut tx = self.pool.begin().await?;
        sqlx::query!(
            "UPDATE users SET totp_state = 'disabled', totp_secret_enc = NULL WHERE id = ?1",
            user_id.as_uuid(),
        )
        .execute(&mut *tx)
        .await?;
        sqlx::query!(
            "UPDATE totp_recovery_codes SET consumed_at = ?2
             WHERE user_id = ?1 AND consumed_at IS NULL",
            user_id.as_uuid(),
            now,
        )
        .execute(&mut *tx)
        .await?;
        sqlx::query!(
            "UPDATE totp_login_challenges SET consumed_at = ?2
             WHERE user_id = ?1 AND consumed_at IS NULL",
            user_id.as_uuid(),
            now,
        )
        .execute(&mut *tx)
        .await?;
        tx.commit().await?;
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
            sqlx::query!(
                "INSERT INTO totp_recovery_codes (id, user_id, code_hash, created_at)
                 VALUES (?1, ?2, ?3, ?4)",
                Uuid::now_v7(),
                user_id.as_uuid(),
                hash,
                now,
            )
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
        let consumed = sqlx::query!(
            "UPDATE totp_recovery_codes SET consumed_at = ?3
             WHERE user_id = ?1 AND code_hash = ?2 AND consumed_at IS NULL",
            user_id.as_uuid(),
            code_hash,
            now,
        )
        .execute(&self.pool)
        .await?;
        Ok(consumed.rows_affected() == 1)
    }

    async fn recovery_codes(
        &self,
        user_id: &UserId,
    ) -> Result<Vec<RecoveryCodeRecord>, StorageError> {
        let rows = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid", consumed_at AS "consumed_at: Option<chrono::DateTime<chrono::Utc>>"
               FROM totp_recovery_codes WHERE user_id = ?1"#,
            user_id.as_uuid(),
        )
        .fetch_all(&self.pool)
        .await?;
        Ok(rows
            .into_iter()
            // 可空列 + Option 覆盖 = Option<Option>；内层即语义值
            .map(|r| RecoveryCodeRecord { id: r.id, consumed_at: r.consumed_at.flatten() })
            .collect())
    }

    async fn issue_challenge(
        &self,
        user_id: &UserId,
        token_hash: &str,
        expires_at: DateTime<Utc>,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query!(
            "INSERT INTO totp_login_challenges (id, user_id, token_hash, attempts, expires_at, created_at)
             VALUES (?1, ?2, ?3, 0, ?4, ?5)",
            Uuid::now_v7(),
            user_id.as_uuid(),
            token_hash,
            expires_at,
            now,
        )
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn find_challenge(
        &self,
        token_hash: &str,
    ) -> Result<Option<TotpChallenge>, StorageError> {
        let row = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid",
                      user_id AS "user_id: uuid::Uuid",
                      attempts AS "attempts: u32",
                      expires_at AS "expires_at: chrono::DateTime<chrono::Utc>"
               FROM totp_login_challenges WHERE token_hash = ?1"#,
            token_hash,
        )
        .fetch_optional(&self.pool)
        .await?;
        Ok(row.map(|r| TotpChallenge {
            id: r.id,
            user_id: UserId::from_uuid(r.user_id),
            attempts: r.attempts,
            expires_at: r.expires_at,
        }))
    }

    async fn record_challenge_attempt(&self, id: &Uuid) -> Result<(), StorageError> {
        sqlx::query!("UPDATE totp_login_challenges SET attempts = attempts + 1 WHERE id = ?1", id,)
            .execute(&self.pool)
            .await?;
        Ok(())
    }

    async fn consume_challenge(&self, id: &Uuid, now: DateTime<Utc>) -> Result<bool, StorageError> {
        let consumed = sqlx::query!(
            "UPDATE totp_login_challenges SET consumed_at = ?2
             WHERE id = ?1 AND consumed_at IS NULL",
            id,
            now,
        )
        .execute(&self.pool)
        .await?;
        Ok(consumed.rows_affected() == 1)
    }

    async fn invalidate_challenges(
        &self,
        user_id: &UserId,
        now: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query!(
            "UPDATE totp_login_challenges SET consumed_at = ?2
             WHERE user_id = ?1 AND consumed_at IS NULL",
            user_id.as_uuid(),
            now,
        )
        .execute(&self.pool)
        .await?;
        Ok(())
    }
}

#[async_trait]
impl crate::storage::OAuthRepo for SqliteOAuthRepo {
    async fn find_client(&self, client_id: &str) -> Result<Option<OAuthClient>, StorageError> {
        let row = sqlx::query!(
            r#"SELECT id, name, redirect_uri, enabled AS "enabled: bool"
               FROM oauth_clients WHERE id = ?1"#,
            client_id,
        )
        .fetch_optional(&self.pool)
        .await?;
        Ok(row.map(|r| OAuthClient {
            id: r.id,
            name: r.name,
            redirect_uri: r.redirect_uri,
            enabled: r.enabled,
        }))
    }

    async fn find_client_secret(&self, client_id: &str) -> Result<Option<String>, StorageError> {
        let row = sqlx::query!("SELECT secret_hash FROM oauth_clients WHERE id = ?1", client_id)
            .fetch_optional(&self.pool)
            .await?;
        Ok(row.and_then(|r| r.secret_hash))
    }

    async fn create_code(
        &self,
        code_hash: &str,
        code: &NewAuthorizationCode,
        created_at: DateTime<Utc>,
    ) -> Result<(), StorageError> {
        sqlx::query!(
            "INSERT INTO oauth_authorization_codes
             (id, code_hash, client_id, user_id, redirect_uri, scope, code_challenge, expires_at, created_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)",
            Uuid::now_v7(),
            code_hash,
            code.client_id,
            code.user_id.as_uuid(),
            code.redirect_uri,
            code.scope,
            code.code_challenge,
            code.expires_at,
            created_at,
        )
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn find_code(&self, code_hash: &str) -> Result<Option<AuthorizationCode>, StorageError> {
        let row = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid", client_id, user_id AS "user_id: uuid::Uuid",
                      redirect_uri, scope, code_challenge,
                      expires_at AS "expires_at: chrono::DateTime<chrono::Utc>"
               FROM oauth_authorization_codes WHERE code_hash = ?1"#,
            code_hash,
        )
        .fetch_optional(&self.pool)
        .await?;
        Ok(row.map(|r| AuthorizationCode {
            id: r.id,
            client_id: r.client_id,
            user_id: UserId::from_uuid(r.user_id),
            redirect_uri: r.redirect_uri,
            scope: r.scope,
            code_challenge: r.code_challenge,
            expires_at: r.expires_at,
        }))
    }

    async fn consume_code(&self, code_id: &Uuid, now: DateTime<Utc>) -> Result<bool, StorageError> {
        let consumed = sqlx::query!(
            "UPDATE oauth_authorization_codes SET consumed_at = ?2
             WHERE id = ?1 AND consumed_at IS NULL",
            code_id,
            now,
        )
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

        let refresh_tokens = sqlx::query!("DELETE FROM refresh_tokens WHERE expires_at < ?1", now,)
            .execute(&mut *tx)
            .await?
            .rows_affected();

        let refresh_families = sqlx::query!(
            r#"DELETE FROM refresh_token_families
               WHERE created_at < ?1
                 AND NOT EXISTS (
                     SELECT 1 FROM refresh_tokens WHERE family_id = refresh_token_families.id
                 )"#,
            family_cutoff,
        )
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let email_verifications = sqlx::query!(
            "DELETE FROM email_verifications
             WHERE expires_at < ?1
                OR (consumed_at IS NOT NULL AND consumed_at < ?2)",
            now,
            consumed_cutoff,
        )
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let totp_recovery_codes = sqlx::query!(
            "DELETE FROM totp_recovery_codes
             WHERE consumed_at IS NOT NULL AND consumed_at < ?1",
            consumed_cutoff,
        )
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let totp_login_challenges = sqlx::query!(
            "DELETE FROM totp_login_challenges
             WHERE expires_at < ?1
                OR (consumed_at IS NOT NULL AND consumed_at < ?2)",
            now,
            consumed_cutoff,
        )
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let oauth_authorization_codes = sqlx::query!(
            "DELETE FROM oauth_authorization_codes
             WHERE expires_at < ?1
                OR (consumed_at IS NOT NULL AND consumed_at < ?2)",
            now,
            consumed_cutoff,
        )
        .execute(&mut *tx)
        .await?
        .rows_affected();

        let expired_bans = sqlx::query!(
            "DELETE FROM user_bans WHERE kind = 'temporary' AND expires_at <= ?1",
            now,
        )
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
            sqlx::query_scalar!("SELECT COUNT(*) FROM users").fetch_one(&self.pool).await?;
        let banned_users: i64 = sqlx::query_scalar!(
            "SELECT COUNT(*) FROM user_bans WHERE kind = 'permanent' OR expires_at > ?1",
            now,
        )
        .fetch_one(&self.pool)
        .await?;
        let new_users_today: i64 =
            sqlx::query_scalar!("SELECT COUNT(*) FROM users WHERE created_at >= ?1", since,)
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
        let rows = sqlx::query!(
            r#"SELECT u.id AS "id: uuid::Uuid",
                      u.username,
                      u.email,
                      u.email_verified AS "email_verified: bool",
                      u.created_at AS "created_at: chrono::DateTime<chrono::Utc>",
                      b.kind,
                      b.expires_at AS "expires_at: Option<chrono::DateTime<chrono::Utc>>"
               FROM users u
               LEFT JOIN user_bans b ON b.user_id = u.id
               WHERE ?1 = ''
                  OR u.email LIKE '%' || ?1 || '%'
                  OR u.username LIKE '%' || ?1 || '%'
               ORDER BY u.created_at DESC
               LIMIT ?2 OFFSET ?3"#,
            search.unwrap_or(""),
            limit,
            offset,
        )
        .fetch_all(&self.pool)
        .await?;

        rows.into_iter()
            .map(|row| {
                let ban = decode_ban(row.kind.as_deref(), row.expires_at.flatten(), now)?;
                Ok(AdminUserRow {
                    id: UserId::from_uuid(row.id),
                    username: row.username,
                    email: row.email,
                    email_verified: row.email_verified,
                    created_at: row.created_at,
                    ban,
                })
            })
            .collect()
    }

    async fn count_users(&self, search: Option<&str>) -> Result<i64, StorageError> {
        let total: i64 = sqlx::query_scalar!(
            r#"SELECT COUNT(*) FROM users u
               WHERE ?1 = ''
                  OR u.email LIKE '%' || ?1 || '%'
                  OR u.username LIKE '%' || ?1 || '%'"#,
            search.unwrap_or(""),
        )
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
        let exists: i64 =
            sqlx::query_scalar!("SELECT COUNT(*) FROM users WHERE id = ?1", user_id.as_uuid())
                .fetch_one(&mut *tx)
                .await?;
        if exists == 0 {
            return Ok(false);
        }

        let (kind, expires_at) = match ban {
            Ban::Permanent => ("permanent", None),
            Ban::Temporary(expires_at) => ("temporary", Some(*expires_at)),
        };
        sqlx::query!(
            "INSERT INTO user_bans (user_id, kind, expires_at, reason, created_at, created_by)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)
             ON CONFLICT(user_id) DO UPDATE SET
                 kind = excluded.kind,
                 expires_at = excluded.expires_at,
                 reason = excluded.reason,
                 created_at = excluded.created_at,
                 created_by = excluded.created_by",
            user_id.as_uuid(),
            kind,
            expires_at,
            reason,
            now,
            actor_id.as_uuid(),
        )
        .execute(&mut *tx)
        .await?;
        // 封禁与撤销会话必须同事务：避免封禁生效但旧 refresh 仍可轮转
        revoke_all_sessions_in_tx(&mut tx, user_id, now).await?;
        tx.commit().await?;
        Ok(true)
    }

    async fn unban_user(&self, user_id: &UserId) -> Result<bool, StorageError> {
        let deleted = sqlx::query!("DELETE FROM user_bans WHERE user_id = ?1", user_id.as_uuid())
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
        sqlx::query!(
            "INSERT INTO audit_logs (id, actor_id, action, target_id, details, created_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            Uuid::now_v7(),
            entry.actor_id.as_uuid(),
            entry.action,
            entry.target_id.as_ref().map(UserId::as_uuid),
            details,
            now,
        )
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn list_audit(&self, limit: i64, offset: i64) -> Result<Vec<AuditEntry>, StorageError> {
        let rows = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid",
                      actor_id AS "actor_id: uuid::Uuid",
                      action,
                      target_id AS "target_id: Option<uuid::Uuid>",
                      details,
                      created_at AS "created_at: chrono::DateTime<chrono::Utc>"
               FROM audit_logs
               ORDER BY created_at DESC
               LIMIT ?1 OFFSET ?2"#,
            limit,
            offset,
        )
        .fetch_all(&self.pool)
        .await?;

        rows.into_iter()
            .map(|row| {
                let details: Option<serde_json::Value> = row
                    .details
                    .map(|raw| serde_json::from_str(&raw))
                    .transpose()
                    .map_err(StorageError::wrap)?;
                Ok(AuditEntry {
                    id: row.id,
                    actor_id: UserId::from_uuid(row.actor_id),
                    action: row.action,
                    target_id: row.target_id.flatten().map(UserId::from_uuid),
                    details,
                    created_at: row.created_at,
                })
            })
            .collect()
    }

    async fn count_audit(&self) -> Result<i64, StorageError> {
        let total: i64 =
            sqlx::query_scalar!("SELECT COUNT(*) FROM audit_logs").fetch_one(&self.pool).await?;
        Ok(total)
    }
}
