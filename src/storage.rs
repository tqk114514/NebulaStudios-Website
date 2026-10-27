//! 存储接缝：trait 在此定义，sqlx 实现在 `storage/sqlite.rs`（§2 依赖方向 2、3）。
//! `api/` 只依赖本模块的类型，不触及 sqlx。

pub mod sqlite;

use std::error::Error as StdError;
use std::fmt;

use async_trait::async_trait;

use crate::model::audit::{AuditEntry, NewAuditEntry};
use crate::model::oauth2::{AuthorizationCode, OAuthClient};
use crate::model::password::PasswordHash;
use crate::model::tokens::NewRefreshFamily;
use crate::model::totp::{RecoveryCodeRecord, TotpChallenge, TotpState};
use crate::model::user::{Ban, LoginCredentials, User, UserAccess, UserId};
use crate::model::verification::{EmailVerification, NewEmailVerification, VerificationPurpose};

/// 不透明存储错误：实现侧（sqlx 等）的具体错误装箱封存，
/// 接缝之上只见「存储故障」，不见驱动类型。
#[derive(Debug)]
pub struct StorageError(Box<dyn StdError + Send + Sync>);

impl fmt::Display for StorageError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "存储故障: {}", self.0)
    }
}

impl StorageError {
    /// 封装任意底层错误（非 sqlx 驱动的实现侧错误，如坏哈希解析）。
    pub fn wrap<E>(source: E) -> Self
    where
        E: StdError + Send + Sync + 'static,
    {
        Self(Box::new(source))
    }
}

impl StdError for StorageError {
    fn source(&self) -> Option<&(dyn StdError + 'static)> {
        Some(self.0.as_ref())
    }
}

/// 用户仓储操作错误。
#[derive(Debug, thiserror::Error)]
pub enum UserRepoError {
    /// 用户名或邮箱被唯一约束拒绝。
    #[error("用户名或邮箱已被占用")]
    Conflict,
    /// 底层存储故障，细节见 source。
    #[error(transparent)]
    Storage(#[from] StorageError),
}

/// 用户仓储；读写以 `&self` 暴露（内部连接池自有可变性）。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
#[async_trait]
pub trait UserRepo: Send + Sync {
    /// 插入新用户与其口令哈希，一条 INSERT 同行落库。
    ///
    /// # Errors
    /// 用户名或邮箱已被占用返回 [`UserRepoError::Conflict`]；底层故障返回
    /// [`UserRepoError::Storage`]。
    async fn insert(&self, user: &User, password: &PasswordHash) -> Result<(), UserRepoError>;

    /// 按邮箱查找，大小写不敏感（列 COLLATE NOCASE）。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；读取不产生 Conflict。
    async fn find_by_email(&self, email: &str) -> Result<Option<User>, UserRepoError>;

    /// 按用户 id 取登录凭据（含口令哈希）；不存在返回 `None`。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]。
    async fn find_by_id(
        &self,
        user_id: &crate::model::user::UserId,
    ) -> Result<Option<LoginCredentials>, UserRepoError>;

    /// 读取认证接缝所需的实时状态；用户不存在返回 `None`，已到期的临时封禁归为
    /// `UserAccess::ban = None`。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；封禁行数据非法也按存储故障返回。
    async fn find_access_state(
        &self,
        user_id: &UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<Option<UserAccess>, UserRepoError>;

    /// 按登录标识（邮箱或用户名）取登录凭据，大小写不敏感；用户不存在或
    /// 该用户无口令哈希均返回 `None`，由调用方走时序对齐路径。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；读取不产生 Conflict。
    async fn find_login_credentials(
        &self,
        identifier: &str,
    ) -> Result<Option<LoginCredentials>, UserRepoError>;

    /// 更新用户口令哈希；不触碰会话（撤销由调用方负责，红线 #1）。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]。
    async fn update_password_hash(
        &self,
        user_id: &crate::model::user::UserId,
        password: &PasswordHash,
    ) -> Result<(), UserRepoError>;

    /// 按用户名查找，大小写不敏感。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；读取不产生 Conflict。
    async fn find_by_username(&self, username: &str) -> Result<Option<User>, UserRepoError>;
}

/// 刷新轮转时期望的客户端绑定；第一方会话与第三方客户端互不通用（ADR-0006）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ClientBinding<'a> {
    /// 第一方用户会话：家族不得带 `client_id`。
    FirstParty,
    /// 第三方客户端：家族 `client_id` 必须与给定值相等。
    Client(&'a str),
}

/// 一次成功轮转后的会话元数据；新 refresh 明文由调用方持有。
#[derive(Debug, Clone)]
pub struct RotatedSession {
    /// 会话所属用户。
    pub user_id: UserId,
    /// 家族创建时固定的授权范围。
    pub scope: String,
}

/// refresh token 轮转错误。
#[derive(Debug, thiserror::Error)]
pub enum RotateRefreshError {
    /// 复用已轮转 token：判定为重放，家族已整族撤销（§10 保留 v3 姿态）。
    #[error("refresh token 已被轮转，家族整族撤销")]
    Replay,
    /// token 不存在、已撤销、已过期或所属家族已撤销。
    #[error("refresh token 无效")]
    Invalid,
    /// token 属于另一个客户端（或第一方），不得在该绑定下兑换。
    #[error("refresh token 不属于当前客户端")]
    ClientMismatch,
    /// 底层存储故障，细节见 source。
    #[error(transparent)]
    Storage(#[from] StorageError),
}

/// refresh token 家族仓储；轮转、绑定校验与重放检测必须原子完成。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
#[async_trait]
pub trait RefreshTokenRepo: Send + Sync {
    /// 创建家族并落第一个 token（登录或授权码兑换时调用）。
    ///
    /// # Errors
    /// 仅存储故障；家族创建不存在冲突路径。
    async fn create_family(
        &self,
        family: &NewRefreshFamily,
        created_at: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 原子轮转：呈递 token 须 Active 且未过期、家族须 Active、且家族客户端
    /// 与 `binding` 相符；成功则旧置 Rotated、同族插入新 token，并返回家族固定的
    /// `user_id` 与 `scope`，供签发新 access。
    ///
    /// 呈递已 Rotated 的 token 判定重放——整族撤销后返回
    /// [`RotateRefreshError::Replay`]。绑定不符返回
    /// [`RotateRefreshError::ClientMismatch`]，且不触碰家族状态。
    ///
    /// # Errors
    /// 重放返回 [`RotateRefreshError::Replay`]；无效呈递返回
    /// [`RotateRefreshError::Invalid`]；绑定不符返回
    /// [`RotateRefreshError::ClientMismatch`]；存储故障返回
    /// [`RotateRefreshError::Storage`]。
    async fn rotate(
        &self,
        presented_hash: &str,
        new_hash: &str,
        new_expires_at: chrono::DateTime<chrono::Utc>,
        now: chrono::DateTime<chrono::Utc>,
        binding: ClientBinding<'_>,
    ) -> Result<RotatedSession, RotateRefreshError>;

    /// 撤销用户全部家族；登出、改密、关闭 TOTP、注销共用同一路径（红线 #1）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn revoke_all_for_user(
        &self,
        user_id: &UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;
}

/// 邮箱验证码仓储。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
#[async_trait]
pub trait EmailVerificationRepo: Send + Sync {
    /// 为用户落一条新验证码；历史记录保留，生效的是「最新未消费」。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn issue(
        &self,
        entry: &NewEmailVerification,
        created_at: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 用户某用途下最新一条未消费的验证码。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn latest_unconsumed(
        &self,
        user_id: &crate::model::user::UserId,
        purpose: VerificationPurpose,
    ) -> Result<Option<EmailVerification>, StorageError>;

    /// 错误尝试计数 +1。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn record_attempt(&self, id: &uuid::Uuid) -> Result<(), StorageError>;

    /// 原子完成验证：消费该验证码并把用户置为已验证；验证码已被消费时返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn consume_and_mark_verified(
        &self,
        verification_id: &uuid::Uuid,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<bool, StorageError>;

    /// 消费该验证码（不附带任何用户状态变更）；已被消费时返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn consume(
        &self,
        verification_id: &uuid::Uuid,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<bool, StorageError>;
}

/// 账户删除仓储；删除即最终撤销。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
#[async_trait]
pub trait AccountRepo: Send + Sync {
    /// 硬删除用户；从属数据由 `ON DELETE CASCADE` 在同一事务内删除（ADR-0007）。
    ///
    /// # Errors
    /// 仅存储故障；用户不存在不视为错误（幂等）。
    async fn delete_user(&self, user_id: &UserId) -> Result<(), StorageError>;
}

/// 过期数据清理结果；各字段为本次删除的行数。
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct CleanupReport {
    /// 删除的 refresh token 行数。
    pub refresh_tokens: u64,
    /// 删除的无 token 家族行数。
    pub refresh_families: u64,
    /// 删除的邮箱验证码行数。
    pub email_verifications: u64,
    /// 删除的已消费恢复码行数。
    pub totp_recovery_codes: u64,
    /// 删除的 TOTP 登录挑战行数。
    pub totp_login_challenges: u64,
    /// 删除的 OAuth 授权码行数。
    pub oauth_authorization_codes: u64,
    /// 删除的已到期临时封禁行数。
    pub expired_bans: u64,
}

/// 维护仓储：过期数据清理；删除在同一事务内完成。
#[async_trait]
pub trait MaintenanceRepo: Send + Sync {
    /// 删除过期或超过保留期的认证数据，返回各类删除行数。
    ///
    /// # Errors
    /// 仅存储故障；部分删除不会留下半完成状态（单事务）。
    async fn purge_expired(
        &self,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<CleanupReport, StorageError>;
}

/// 管理后台统计。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AdminStats {
    /// 用户总数。
    pub total_users: i64,
    /// 当前生效的封禁数。
    pub banned_users: i64,
    /// `since` 之后注册的用户数。
    pub new_users_today: i64,
}

/// 管理后台用户列表行。
#[derive(Debug, Clone)]
pub struct AdminUserRow {
    /// 用户标识。
    pub id: UserId,
    /// 展示用户名。
    pub username: String,
    /// 邮箱。
    pub email: String,
    /// 邮箱是否已验证。
    pub email_verified: bool,
    /// 注册时刻。
    pub created_at: chrono::DateTime<chrono::Utc>,
    /// 当前生效的封禁；已到期的临时封禁归为 `None`。
    pub ban: Option<Ban>,
}

/// 管理后台仓储：统计、用户列表、封禁/解封与审计。
///
/// 封禁与撤销 refresh 会话在同一事务内完成；审计写入是独立追加操作。
#[async_trait]
pub trait AdminRepo: Send + Sync {
    /// 取用户总数、当前封禁数与 `since` 之后的新增数。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn stats(
        &self,
        now: chrono::DateTime<chrono::Utc>,
        since: chrono::DateTime<chrono::Utc>,
    ) -> Result<AdminStats, StorageError>;

    /// 分页列出用户，`search` 同时匹配邮箱与用户名（大小写不敏感的 SQL LIKE）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn list_users(
        &self,
        search: Option<&str>,
        limit: i64,
        offset: i64,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<Vec<AdminUserRow>, StorageError>;

    /// 与 [`Self::list_users`] 同筛选条件的总数。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn count_users(&self, search: Option<&str>) -> Result<i64, StorageError>;

    /// 写入/覆盖封禁，并在同一事务内撤销该用户全部 refresh 家族；用户不存在返回
    /// `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn ban_user(
        &self,
        user_id: &UserId,
        ban: &Ban,
        reason: &str,
        actor_id: &UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<bool, StorageError>;

    /// 删除封禁记录；原本不存在返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn unban_user(&self, user_id: &UserId) -> Result<bool, StorageError>;

    /// 追加一条审计记录。
    ///
    /// # Errors
    /// 仅存储故障；`details` 无法序列化也按存储故障返回。
    async fn record_audit(
        &self,
        entry: &NewAuditEntry,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 按时间倒序分页读取审计记录。
    ///
    /// # Errors
    /// 仅存储故障；存量记录的 UUID/JSON 损坏也按存储故障返回。
    async fn list_audit(&self, limit: i64, offset: i64) -> Result<Vec<AuditEntry>, StorageError>;

    /// 审计记录总数。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn count_audit(&self) -> Result<i64, StorageError>;
}

/// TOTP 仓储：状态与密钥、恢复码批次、登录二段挑战。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
#[async_trait]
pub trait TotpRepo: Send + Sync {
    /// 读用户 TOTP 状态与密钥（pending/enabled 时密钥非空）。
    ///
    /// # Errors
    /// 仅存储故障；用户不存在返回 `None`。
    async fn find(
        &self,
        user_id: &crate::model::user::UserId,
    ) -> Result<Option<(TotpState, Option<String>)>, StorageError>;

    /// 写入 pending 密钥（setup；重试即换新密钥）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn set_pending(
        &self,
        user_id: &crate::model::user::UserId,
        secret: &str,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 启用：pending 密钥转正为 enabled（调用方须已验证该密钥下的码）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn enable(
        &self,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 关闭：清状态与密钥并作废全部恢复码（调用方须已双因素验证）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn disable(
        &self,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 批量落恢复码哈希（启用时生成）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn add_recovery_codes(
        &self,
        user_id: &crate::model::user::UserId,
        hashes: Vec<String>,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 原子消费匹配的恢复码；未命中或已消费返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn consume_recovery_code(
        &self,
        user_id: &crate::model::user::UserId,
        code_hash: &str,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<bool, StorageError>;

    /// 用户全部恢复码（disable 前的余量展示等）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn recovery_codes(
        &self,
        user_id: &crate::model::user::UserId,
    ) -> Result<Vec<RecoveryCodeRecord>, StorageError>;

    /// 签发登录二段挑战（密码通过后）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn issue_challenge(
        &self,
        user_id: &crate::model::user::UserId,
        token_hash: &str,
        expires_at: chrono::DateTime<chrono::Utc>,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 按哈希取挑战记录。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn find_challenge(&self, token_hash: &str)
    -> Result<Option<TotpChallenge>, StorageError>;

    /// 挑战错误尝试 +1。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn record_challenge_attempt(&self, id: &uuid::Uuid) -> Result<(), StorageError>;

    /// 原子消费挑战（成功二段验证时）；已被消费返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn consume_challenge(
        &self,
        id: &uuid::Uuid,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<bool, StorageError>;

    /// 作废用户全部未消费挑战（disable / 改密后调用）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn invalidate_challenges(
        &self,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;
}

/// `OAuth2` Provider 仓储。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
#[async_trait]
pub trait OAuthRepo: Send + Sync {
    /// 按 `client_id` 取客户端。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn find_client(&self, client_id: &str) -> Result<Option<OAuthClient>, StorageError>;

    /// 取客户端密钥哈希（PHC 串）；公共客户端（未设密钥）返回 `None`。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn find_client_secret(&self, client_id: &str) -> Result<Option<String>, StorageError>;

    /// 落一条授权码（Fulfilled）。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn create_code(
        &self,
        code_hash: &str,
        code: &crate::model::oauth2::NewAuthorizationCode,
        created_at: chrono::DateTime<chrono::Utc>,
    ) -> Result<(), StorageError>;

    /// 按哈希取授权码。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn find_code(&self, code_hash: &str) -> Result<Option<AuthorizationCode>, StorageError>;

    /// 原子消费授权码（Fulfilled → Exchanged）；已被消费返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    async fn consume_code(
        &self,
        code_id: &uuid::Uuid,
        now: chrono::DateTime<chrono::Utc>,
    ) -> Result<bool, StorageError>;
}
