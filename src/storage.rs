//! 存储接缝：trait 在此定义，sqlx 实现在 `storage/sqlite.rs`（§2 依赖方向 2、3）。
//! `api/` 只依赖本模块的类型，不触及 sqlx。

pub mod sqlite;

use std::error::Error as StdError;
use std::fmt;
use std::future::Future;

use crate::model::oauth2::{AuthorizationCode, OAuthClient};
use crate::model::password::PasswordHash;
use crate::model::tokens::NewRefreshToken;
use crate::model::totp::{RecoveryCodeRecord, TotpChallenge, TotpState};
use crate::model::user::{LoginCredentials, User};
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
pub trait UserRepo: Send + Sync {
    /// 插入新用户与其口令哈希，一条 INSERT 同行落库。
    ///
    /// # Errors
    /// 用户名或邮箱已被占用返回 [`UserRepoError::Conflict`]；底层故障返回
    /// [`UserRepoError::Storage`]。
    fn insert(
        &self,
        user: &User,
        password: &PasswordHash,
    ) -> impl Future<Output = Result<(), UserRepoError>> + Send;

    /// 按邮箱查找，大小写不敏感（列 COLLATE NOCASE）。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；读取不产生 Conflict。
    fn find_by_email(
        &self,
        email: &str,
    ) -> impl Future<Output = Result<Option<User>, UserRepoError>> + Send;

    /// 按用户 id 取登录凭据（含口令哈希）；不存在返回 `None`。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]。
    fn find_by_id(
        &self,
        user_id: &crate::model::user::UserId,
    ) -> impl Future<Output = Result<Option<LoginCredentials>, UserRepoError>> + Send;

    /// 按登录标识（邮箱或用户名）取登录凭据，大小写不敏感；用户不存在或
    /// 该用户无口令哈希均返回 `None`，由调用方走时序对齐路径。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；读取不产生 Conflict。
    fn find_login_credentials(
        &self,
        identifier: &str,
    ) -> impl Future<Output = Result<Option<LoginCredentials>, UserRepoError>> + Send;

    /// 更新用户口令哈希；不触碰会话（撤销由调用方负责，红线 #1）。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]。
    fn update_password_hash(
        &self,
        user_id: &crate::model::user::UserId,
        password: &PasswordHash,
    ) -> impl Future<Output = Result<(), UserRepoError>> + Send;

    /// 按用户名查找，大小写不敏感。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；读取不产生 Conflict。
    fn find_by_username(
        &self,
        username: &str,
    ) -> impl Future<Output = Result<Option<User>, UserRepoError>> + Send;
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
    /// 底层存储故障，细节见 source。
    #[error(transparent)]
    Storage(#[from] StorageError),
}

/// refresh token 家族仓储；轮转与重放检测必须原子完成。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
pub trait RefreshTokenRepo: Send + Sync {
    /// 创建家族并落第一个 token（登录成功后调用）。
    ///
    /// # Errors
    /// 仅存储故障；家族创建不存在冲突路径。
    fn create_family(
        &self,
        token: &NewRefreshToken,
        created_at: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 原子轮转：呈递 token 须 Active 且未过期、家族须 Active；成功则旧置
    /// Rotated、同族插入新 token，并返回所属用户（供签发新 access）。
    /// 呈递已 Rotated 的 token 判定重放——整族撤销后返回 [`RotateRefreshError::Replay`]。
    ///
    /// # Errors
    /// 重放返回 [`RotateRefreshError::Replay`]；无效呈递返回
    /// [`RotateRefreshError::Invalid`]；存储故障返回 [`RotateRefreshError::Storage`]。
    fn rotate(
        &self,
        presented_hash: &str,
        new_hash: &str,
        new_expires_at: chrono::DateTime<chrono::Utc>,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<crate::model::user::UserId, RotateRefreshError>> + Send;

    /// 撤销用户全部家族；登出、改密、关闭 TOTP、注销共用同一路径（红线 #1）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn revoke_all_for_user(
        &self,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;
}

/// 邮箱验证码仓储。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
pub trait EmailVerificationRepo: Send + Sync {
    /// 为用户落一条新验证码；历史记录保留，生效的是「最新未消费」。
    ///
    /// # Errors
    /// 仅存储故障。
    fn issue(
        &self,
        entry: &NewEmailVerification,
        created_at: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 用户某用途下最新一条未消费的验证码。
    ///
    /// # Errors
    /// 仅存储故障。
    fn latest_unconsumed(
        &self,
        user_id: &crate::model::user::UserId,
        purpose: VerificationPurpose,
    ) -> impl Future<Output = Result<Option<EmailVerification>, StorageError>> + Send;

    /// 错误尝试计数 +1。
    ///
    /// # Errors
    /// 仅存储故障。
    fn record_attempt(
        &self,
        id: &uuid::Uuid,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 原子完成验证：消费该验证码并把用户置为已验证；验证码已被消费时返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    fn consume_and_mark_verified(
        &self,
        verification_id: &uuid::Uuid,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<bool, StorageError>> + Send;

    /// 消费该验证码（不附带任何用户状态变更）；已被消费时返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    fn consume(
        &self,
        verification_id: &uuid::Uuid,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<bool, StorageError>> + Send;
}

/// 账户删除仓储；删除即最终撤销。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
pub trait AccountRepo: Send + Sync {
    /// 硬删除用户及其全部从属数据（验证码、refresh 家族与 token），单事务。
    ///
    /// # Errors
    /// 仅存储故障；用户不存在不视为错误（幂等）。
    fn delete_user(
        &self,
        user_id: &crate::model::user::UserId,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;
}

/// TOTP 仓储：状态与密钥、恢复码批次、登录二段挑战。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
pub trait TotpRepo: Send + Sync {
    /// 读用户 TOTP 状态与密钥（pending/enabled 时密钥非空）。
    ///
    /// # Errors
    /// 仅存储故障；用户不存在返回 `None`。
    fn find(
        &self,
        user_id: &crate::model::user::UserId,
    ) -> impl Future<Output = Result<Option<(TotpState, Option<String>)>, StorageError>> + Send;

    /// 写入 pending 密钥（setup；重试即换新密钥）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn set_pending(
        &self,
        user_id: &crate::model::user::UserId,
        secret: &str,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 启用：pending 密钥转正为 enabled（调用方须已验证该密钥下的码）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn enable(
        &self,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 关闭：清状态与密钥并作废全部恢复码（调用方须已双因素验证）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn disable(
        &self,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 批量落恢复码哈希（启用时生成）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn add_recovery_codes(
        &self,
        user_id: &crate::model::user::UserId,
        hashes: Vec<String>,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 原子消费匹配的恢复码；未命中或已消费返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    fn consume_recovery_code(
        &self,
        user_id: &crate::model::user::UserId,
        code_hash: &str,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<bool, StorageError>> + Send;

    /// 用户全部恢复码（disable 前的余量展示等）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn recovery_codes(
        &self,
        user_id: &crate::model::user::UserId,
    ) -> impl Future<Output = Result<Vec<RecoveryCodeRecord>, StorageError>> + Send;

    /// 签发登录二段挑战（密码通过后）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn issue_challenge(
        &self,
        user_id: &crate::model::user::UserId,
        token_hash: &str,
        expires_at: chrono::DateTime<chrono::Utc>,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 按哈希取挑战记录。
    ///
    /// # Errors
    /// 仅存储故障。
    fn find_challenge(
        &self,
        token_hash: &str,
    ) -> impl Future<Output = Result<Option<TotpChallenge>, StorageError>> + Send;

    /// 挑战错误尝试 +1。
    ///
    /// # Errors
    /// 仅存储故障。
    fn record_challenge_attempt(
        &self,
        id: &uuid::Uuid,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 原子消费挑战（成功二段验证时）；已被消费返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    fn consume_challenge(
        &self,
        id: &uuid::Uuid,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<bool, StorageError>> + Send;

    /// 作废用户全部未消费挑战（disable / 改密后调用）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn invalidate_challenges(
        &self,
        user_id: &crate::model::user::UserId,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;
}

/// `OAuth2` Provider 仓储。
///
/// 返回 `+ Send` 的 future：handler 与测试在多线程 runtime 下运行。
pub trait OAuthRepo: Send + Sync {
    /// 按 `client_id` 取客户端。
    ///
    /// # Errors
    /// 仅存储故障。
    fn find_client(
        &self,
        client_id: &str,
    ) -> impl Future<Output = Result<Option<OAuthClient>, StorageError>> + Send;

    /// 取客户端密钥哈希（PHC 串）；公共客户端（未设密钥）返回 `None`。
    ///
    /// # Errors
    /// 仅存储故障。
    fn find_client_secret(
        &self,
        client_id: &str,
    ) -> impl Future<Output = Result<Option<String>, StorageError>> + Send;

    /// 落一条授权码（Fulfilled）。
    ///
    /// # Errors
    /// 仅存储故障。
    fn create_code(
        &self,
        code_hash: &str,
        code: &crate::model::oauth2::NewAuthorizationCode,
        created_at: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    /// 按哈希取授权码。
    ///
    /// # Errors
    /// 仅存储故障。
    fn find_code(
        &self,
        code_hash: &str,
    ) -> impl Future<Output = Result<Option<AuthorizationCode>, StorageError>> + Send;

    /// 原子消费授权码（Fulfilled → Exchanged）；已被消费返回 `false`。
    ///
    /// # Errors
    /// 仅存储故障。
    fn consume_code(
        &self,
        code_id: &uuid::Uuid,
        now: chrono::DateTime<chrono::Utc>,
    ) -> impl Future<Output = Result<bool, StorageError>> + Send;
}
