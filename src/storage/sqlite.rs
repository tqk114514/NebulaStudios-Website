//! 用户仓储的 sqlx + SQLite 实现（ADR-0003）。

use std::path::Path;
use std::time::Duration;

use sqlx::Error as SqlxError;
use sqlx::sqlite::{SqliteConnectOptions, SqliteJournalMode, SqlitePool};

use crate::model::user::{User, UserId};
use crate::storage::{StorageError, UserRepo, UserRepoError};

/// SQLite 用户仓储；克隆共享同一连接池。
#[derive(Debug, Clone)]
pub struct SqliteUserRepo {
    pool: SqlitePool,
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

impl SqliteUserRepo {
    /// 打开既有数据库文件建立连接池；不创建文件、不跑迁移——迁移由
    /// `sqlx migrate` 作为独立部署步骤执行，启动路径不执行 DDL（§1）。
    ///
    /// # Errors
    /// 文件不存在、权限不足或连接失败时返回 [`StorageError`]。
    pub async fn open(path: &Path) -> Result<Self, StorageError> {
        let options = SqliteConnectOptions::new()
            .filename(path)
            .foreign_keys(true)
            .journal_mode(SqliteJournalMode::Wal)
            .busy_timeout(Duration::from_secs(5));
        let pool = SqlitePool::connect_with(options).await?;
        Ok(Self { pool })
    }
}

impl UserRepo for SqliteUserRepo {
    async fn insert(&self, user: &User) -> Result<(), UserRepoError> {
        let result = sqlx::query!(
            "INSERT INTO users (id, username, email, created_at) VALUES (?1, ?2, ?3, ?4)",
            user.id.as_uuid(),
            user.username,
            user.email,
            user.created_at,
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
        }))
    }

    async fn find_by_username(&self, username: &str) -> Result<Option<User>, UserRepoError> {
        let row = sqlx::query!(
            r#"SELECT id AS "id: uuid::Uuid", username, email,
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
        }))
    }
}
