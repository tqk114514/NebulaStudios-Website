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

/// 用户名最短字符数。
pub const USERNAME_MIN_CHARS: usize = 3;
/// 用户名最长字符数。
pub const USERNAME_MAX_CHARS: usize = 15;

/// 归一化邮箱：trim、local 小写、域名转 IDNA A-label。
///
/// 同一 IDNA 域的不同 Unicode 规范化形式（NFC/NFD）得到同一结果，
/// 避免唯一索引把同一邮箱当成两个账户。输入不是 `local@domain` 时返回 `None`。
///
/// ```
/// use nebula_server::model::user::canonicalize_email;
///
/// assert_eq!(
///     canonicalize_email(" User@BÜCHER.example ").as_deref(),
///     Some("user@xn--bcher-kva.example")
/// );
/// ```
#[must_use]
pub fn canonicalize_email(raw: &str) -> Option<String> {
    let (local, domain) = raw.trim().rsplit_once('@')?;
    if local.is_empty() || domain.is_empty() {
        return None;
    }
    let ascii_domain = idna::domain_to_ascii(domain).ok()?;
    if ascii_domain.is_empty() {
        return None;
    }
    Some(format!("{}@{}", local.to_lowercase(), ascii_domain.to_ascii_lowercase()))
}

/// 用户名规则：3–15 个 ASCII 字母、数字、下划线或连字符；返回首个错误码。
///
/// ```
/// use nebula_server::model::user::username_problem;
///
/// assert_eq!(username_problem("nova"), None);
/// assert_eq!(username_problem("no"), Some("username_too_short"));
/// assert_eq!(username_problem("nova!"), Some("username_invalid_characters"));
/// ```
#[must_use]
pub fn username_problem(username: &str) -> Option<&'static str> {
    let len = username.chars().count();
    if len < USERNAME_MIN_CHARS {
        Some("username_too_short")
    } else if len > USERNAME_MAX_CHARS {
        Some("username_too_long")
    } else if !username.chars().all(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '-')) {
        Some("username_invalid_characters")
    } else {
        None
    }
}

/// 注册用户快照。登录标识为邮箱或用户名（ADR-0002）；
/// 字段合法形状（长度、字符集）在注册端点经 `validator` 校验，领域层不重复编码规则。
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize)]
pub struct User {
    /// 永久标识（API 字段名 `id`），UUID v7，即稳定 UID；修改用户名或邮箱不变。
    pub id: UserId,
    /// 展示用用户名，全库唯一（大小写不敏感）。
    pub username: String,
    /// 邮箱地址，全库唯一（大小写不敏感），入库前小写化。
    pub email: String,
    /// 注册时刻；RFC3339 存储于数据库（ADR-0003）。
    pub created_at: DateTime<Utc>,
    /// 邮箱是否已验证；注册时为 false，验证端点置 true。
    pub email_verified: bool,
    /// 头像版本；0 表示从未上传，每次上传递增，用作公开头像地址的缓存键。
    /// 不进入对外用户 JSON，资料端点单独给出 `avatar_url`。
    #[serde(skip)]
    pub avatar_version: i64,
}

/// 登录凭据查询结果：用户快照与口令哈希一并返回，供登录端点一次性取得。
#[derive(Debug, Clone)]
pub struct LoginCredentials {
    /// 用户快照。
    pub user: User,
    /// 已存口令哈希；无口令的用户不会出现在本结构中（不可口令登录）。
    pub password_hash: crate::model::password::PasswordHash,
}

/// 封禁状态；`user_bans` 的两种合法形态。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Ban {
    /// 到期自动解除。
    Temporary(DateTime<Utc>),
    /// 永不过期，须管理员解封。
    Permanent,
}

/// 认证接缝需要的实时用户状态：邮箱（用于配置化管理员判定）与封禁。
#[derive(Debug, Clone)]
pub struct UserAccess {
    /// 已小写化的注册邮箱。
    pub email: String,
    /// 邮箱是否已验证；配置化管理员必须同时满足此项。
    pub email_verified: bool,
    /// access token 版本；撤销会话时递增，token 中的 `ver` 必须与此相等。
    pub token_version: i64,
    /// 当前生效的封禁；已到期的临时封禁在读取时归为 `None`。
    pub ban: Option<Ban>,
}
