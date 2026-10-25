//! 用户领域类型。

use chrono::{DateTime, Utc};
use uuid::Uuid;

/// 用户标识，uuid v7；创建时生成，此后不变。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, serde::Serialize)]
pub struct UserId(Uuid);

impl UserId {
    /// 生成新标识。
    ///
    /// ```
    /// use nebula_server::model::user::UserId;
    ///
    /// let id = UserId::new();
    /// assert_eq!(id.to_string().len(), 36);
    /// ```
    #[must_use]
    pub fn new() -> Self {
        Self(Uuid::now_v7())
    }

    /// 借用底层 uuid（绑定 sqlx 参数等零拷贝场景）。
    #[must_use]
    pub fn as_uuid(&self) -> &Uuid {
        &self.0
    }

    /// 从已有 uuid 构建（存储层读出时使用）。
    #[must_use]
    pub fn from_uuid(id: Uuid) -> Self {
        Self(id)
    }
}

impl Default for UserId {
    fn default() -> Self {
        Self::new()
    }
}

impl std::fmt::Display for UserId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.0.fmt(f)
    }
}

/// 注册用户快照。登录标识为邮箱或用户名（ADR-0002）；
/// 字段合法形状（长度、字符集）在注册端点经 `validator` 校验，领域层不重复编码规则。
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize)]
pub struct User {
    /// 永久标识。
    pub id: UserId,
    /// 展示用用户名，全库唯一（大小写不敏感）。
    pub username: String,
    /// 邮箱地址，全库唯一（大小写不敏感），入库前小写化。
    pub email: String,
    /// 注册时刻；RFC3339 存储于数据库（ADR-0003）。
    pub created_at: DateTime<Utc>,
    /// 邮箱是否已验证；注册时为 false，验证端点置 true。
    pub email_verified: bool,
}
