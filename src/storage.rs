//! 存储接缝：trait 在此定义，sqlx 实现在 `storage/sqlite.rs`（§2 依赖方向 2、3）。
//! `api/` 只依赖本模块的类型，不触及 sqlx。

pub mod sqlite;

use std::error::Error as StdError;
use std::fmt;
use std::future::Future;

use crate::model::user::User;

/// 不透明存储错误：实现侧（sqlx 等）的具体错误装箱封存，
/// 接缝之上只见「存储故障」，不见驱动类型。
#[derive(Debug)]
pub struct StorageError(Box<dyn StdError + Send + Sync>);

impl fmt::Display for StorageError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "存储故障: {}", self.0)
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
    /// 插入新用户。
    ///
    /// # Errors
    /// 用户名或邮箱已被占用返回 [`UserRepoError::Conflict`]；底层故障返回
    /// [`UserRepoError::Storage`]。
    fn insert(&self, user: &User) -> impl Future<Output = Result<(), UserRepoError>> + Send;

    /// 按邮箱查找，大小写不敏感（列 COLLATE NOCASE）。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；读取不产生 Conflict。
    fn find_by_email(
        &self,
        email: &str,
    ) -> impl Future<Output = Result<Option<User>, UserRepoError>> + Send;

    /// 按用户名查找，大小写不敏感。
    ///
    /// # Errors
    /// 仅 [`UserRepoError::Storage`]；读取不产生 Conflict。
    fn find_by_username(
        &self,
        username: &str,
    ) -> impl Future<Output = Result<Option<User>, UserRepoError>> + Send;
}
