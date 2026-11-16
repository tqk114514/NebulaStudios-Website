//! 政策中心：部署侧 manifest 的领域类型与目录查询（ADR-0002、ADR-0013）。
//!
//! 政策文档本身不进仓库；manifest 只描述 slug、版本、生效时刻与各语言文档路径。

use std::collections::HashSet;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// 支持的政策语言；与站点 2 语言决议一致（AGENTS §0）。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum PolicyLocale {
    /// 简体中文。
    #[serde(rename = "zh-CN")]
    ZhCn,
    /// 英文。
    #[serde(rename = "en")]
    En,
}

impl PolicyLocale {
    /// 全部受支持的语言；manifest 中每个政策版本必须两种齐全。
    pub const ALL: [Self; 2] = [Self::ZhCn, Self::En];

    /// 语言标签。
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::ZhCn => "zh-CN",
            Self::En => "en",
        }
    }
}

/// 某政策版本的单个语言文档。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PolicyDocument {
    /// 文档语言。
    pub locale: PolicyLocale,
    /// 展示标题。
    pub title: String,
    /// 部署侧提供的文档地址：站点内绝对路径或 https 地址。
    pub path: String,
}

/// 一个政策版本。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Policy {
    /// URL 安全的政策标识，如 `privacy`。
    pub slug: String,
    /// 版本标识，如 `2026-11-01`。
    pub version: String,
    /// 生效时刻；未生效的版本不可被接受。
    pub effective_at: DateTime<Utc>,
    /// 各语言文档；必须恰好覆盖 [`PolicyLocale::ALL`]。
    pub documents: Vec<PolicyDocument>,
}

/// manifest 校验失败。
#[derive(Debug, thiserror::Error)]
#[error("政策 manifest 非法: {0}")]
pub struct PolicyCatalogError(String);

impl PolicyCatalogError {
    fn new(message: impl Into<String>) -> Self {
        Self(message.into())
    }
}

/// 启动时加载的政策目录；按 slug 升序、生效时刻降序排列。
///
/// 构造时完成全部校验，之后 [`Self::current`] 依赖排序不变量直接取每个 slug
/// 第一个已生效版本。
#[derive(Debug, Clone, Default)]
pub struct PolicyCatalog {
    policies: Vec<Policy>,
}

impl PolicyCatalog {
    /// 校验并排序政策列表；空列表是合法目录（部署未配置政策中心）。
    ///
    /// ```
    /// use chrono::Utc;
    /// use nebula_server::model::policy::{Policy, PolicyCatalog, PolicyDocument, PolicyLocale};
    ///
    /// let catalog = PolicyCatalog::new(vec![Policy {
    ///     slug: "privacy".to_owned(),
    ///     version: "2026-11-01".to_owned(),
    ///     effective_at: Utc::now(),
    ///     documents: vec![
    ///         PolicyDocument {
    ///             locale: PolicyLocale::ZhCn,
    ///             title: "隐私政策".to_owned(),
    ///             path: "/policies/privacy/2026-11-01/zh-CN".to_owned(),
    ///         },
    ///         PolicyDocument {
    ///             locale: PolicyLocale::En,
    ///             title: "Privacy Policy".to_owned(),
    ///             path: "/policies/privacy/2026-11-01/en".to_owned(),
    ///         },
    ///     ],
    /// }])?;
    /// assert!(catalog.has_slug("privacy"));
    /// # Ok::<(), Box<dyn std::error::Error>>(())
    /// ```
    ///
    /// # Errors
    /// slug/version 字符非法、文档语言缺失或重复、标题为空、文档地址非法、
    /// `(slug, version)` 或同一 slug 的生效时刻重复时返回错误。
    pub fn new(mut policies: Vec<Policy>) -> Result<Self, PolicyCatalogError> {
        let mut versions = HashSet::new();
        let mut effective_at = HashSet::new();
        for policy in &policies {
            validate_slug(&policy.slug)?;
            validate_version(&policy.version)?;
            if !versions.insert((policy.slug.clone(), policy.version.clone())) {
                return Err(PolicyCatalogError::new(format!(
                    "slug/version 重复: {}/{}",
                    policy.slug, policy.version
                )));
            }
            if !effective_at.insert((policy.slug.clone(), policy.effective_at.timestamp_micros())) {
                return Err(PolicyCatalogError::new(format!(
                    "同一政策的生效时刻重复: {} @ {}",
                    policy.slug, policy.effective_at
                )));
            }
            if policy.documents.is_empty() {
                return Err(PolicyCatalogError::new(format!(
                    "政策缺少文档: {}/{}",
                    policy.slug, policy.version
                )));
            }
            let mut locales = HashSet::new();
            for document in &policy.documents {
                if !locales.insert(document.locale) {
                    return Err(PolicyCatalogError::new(format!(
                        "政策语言重复: {}/{} {}",
                        policy.slug,
                        policy.version,
                        document.locale.as_str()
                    )));
                }
                if document.title.trim().is_empty() {
                    return Err(PolicyCatalogError::new(format!(
                        "政策标题为空: {}/{} {}",
                        policy.slug,
                        policy.version,
                        document.locale.as_str()
                    )));
                }
                validate_path(&document.path)?;
            }
            for locale in PolicyLocale::ALL {
                if !locales.contains(&locale) {
                    return Err(PolicyCatalogError::new(format!(
                        "政策缺少语言 {}: {}/{}",
                        locale.as_str(),
                        policy.slug,
                        policy.version
                    )));
                }
            }
        }

        policies.sort_by(|left, right| {
            left.slug.cmp(&right.slug).then(right.effective_at.cmp(&left.effective_at))
        });
        Ok(Self { policies })
    }

    /// 全部政策版本，按 slug 升序、生效时刻降序。
    #[must_use]
    pub fn policies(&self) -> &[Policy] {
        &self.policies
    }

    /// 目录是否为空（部署未配置政策中心）。
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.policies.is_empty()
    }

    /// 指定 slug 是否存在任意版本。
    #[must_use]
    pub fn has_slug(&self, slug: &str) -> bool {
        self.policies.iter().any(|policy| policy.slug == slug)
    }

    /// 精确查找 slug 与版本。
    #[must_use]
    pub fn find(&self, slug: &str, version: &str) -> Option<&Policy> {
        self.policies.iter().find(|policy| policy.slug == slug && policy.version == version)
    }

    /// 指定 slug 在 `now` 时已生效的最新版本；不存在已生效版本返回 `None`。
    ///
    /// 依赖构造时的排序：同一 slug 下第一个 `effective_at <= now` 即最新版本。
    #[must_use]
    pub fn current(&self, slug: &str, now: DateTime<Utc>) -> Option<&Policy> {
        self.policies.iter().find(|policy| policy.slug == slug && policy.effective_at <= now)
    }

    /// 每个 slug 在 `now` 时的最新已生效版本，按 slug 升序。
    #[must_use]
    pub fn current_policies(&self, now: DateTime<Utc>) -> Vec<&Policy> {
        let mut current = Vec::new();
        let mut previous: Option<&str> = None;
        for policy in &self.policies {
            if previous == Some(policy.slug.as_str()) {
                continue;
            }
            if policy.effective_at <= now {
                current.push(policy);
                previous = Some(policy.slug.as_str());
            }
        }
        current
    }
}

fn validate_slug(slug: &str) -> Result<(), PolicyCatalogError> {
    let valid = !slug.is_empty()
        && slug.len() <= 64
        && !slug.starts_with('-')
        && !slug.ends_with('-')
        && slug
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-');
    if valid { Ok(()) } else { Err(PolicyCatalogError::new(format!("slug 非法: {slug}"))) }
}

fn validate_version(version: &str) -> Result<(), PolicyCatalogError> {
    let valid = !version.is_empty()
        && version.len() <= 64
        && version
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'-' | b'_'));
    if valid { Ok(()) } else { Err(PolicyCatalogError::new(format!("version 非法: {version}"))) }
}

fn validate_path(path: &str) -> Result<(), PolicyCatalogError> {
    let valid = !path.is_empty()
        && path.len() <= 512
        && path.trim() == path
        && (path.starts_with('/') || path.starts_with("https://"))
        && !path.starts_with("//")
        && !path.contains('\\')
        && !path.split('/').any(|segment| segment == "..");
    if valid { Ok(()) } else { Err(PolicyCatalogError::new(format!("文档路径非法: {path}"))) }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
    #![allow(clippy::expect_used, clippy::unwrap_used)]

    use chrono::{DateTime, Utc};

    use super::{Policy, PolicyCatalog, PolicyDocument, PolicyLocale};

    fn at(value: &str) -> DateTime<Utc> {
        DateTime::parse_from_rfc3339(value).expect("固定字面量必须可解析").with_timezone(&Utc)
    }

    fn documents() -> Vec<PolicyDocument> {
        vec![
            PolicyDocument {
                locale: PolicyLocale::ZhCn,
                title: "隐私政策".to_owned(),
                path: "/policies/privacy/zh-CN".to_owned(),
            },
            PolicyDocument {
                locale: PolicyLocale::En,
                title: "Privacy Policy".to_owned(),
                path: "/policies/privacy/en".to_owned(),
            },
        ]
    }

    fn policy(version: &str, effective_at: DateTime<Utc>) -> Policy {
        Policy {
            slug: "privacy".to_owned(),
            version: version.to_owned(),
            effective_at,
            documents: documents(),
        }
    }

    #[test]
    fn current_selects_latest_effective_version() {
        let catalog = PolicyCatalog::new(vec![
            policy("v1", at("2026-01-01T00:00:00Z")),
            policy("v3", at("2027-01-01T00:00:00Z")),
            policy("v2", at("2026-06-01T00:00:00Z")),
        ])
        .expect("合法目录必须构建成功");

        assert_eq!(
            catalog.current("privacy", at("2026-07-01T00:00:00Z")).map(|p| p.version.as_str()),
            Some("v2")
        );
        assert_eq!(
            catalog.current("privacy", at("2027-02-01T00:00:00Z")).map(|p| p.version.as_str()),
            Some("v3")
        );
        assert_eq!(catalog.current_policies(at("2025-01-01T00:00:00Z")).len(), 0);
        assert_eq!(catalog.current_policies(at("2026-07-01T00:00:00Z")).len(), 1);
    }

    #[test]
    fn catalog_rejects_duplicate_version_and_effective_time() {
        let duplicate = PolicyCatalog::new(vec![
            policy("v1", at("2026-01-01T00:00:00Z")),
            policy("v1", at("2026-02-01T00:00:00Z")),
        ]);
        assert!(duplicate.is_err());

        let same_time = PolicyCatalog::new(vec![
            policy("v1", at("2026-01-01T00:00:00Z")),
            policy("v2", at("2026-01-01T00:00:00Z")),
        ]);
        assert!(same_time.is_err());
    }

    #[test]
    fn catalog_requires_both_languages_and_safe_paths() {
        let mut missing = policy("v1", at("2026-01-01T00:00:00Z"));
        missing.documents.pop();
        assert!(PolicyCatalog::new(vec![missing]).is_err());

        let mut unsafe_path = policy("v1", at("2026-01-01T00:00:00Z"));
        unsafe_path.documents[0].path = "javascript:alert(1)".to_owned();
        assert!(PolicyCatalog::new(vec![unsafe_path]).is_err());

        let mut traversal = policy("v1", at("2026-01-01T00:00:00Z"));
        traversal.documents[0].path = "/policies/../admin".to_owned();
        assert!(PolicyCatalog::new(vec![traversal]).is_err());

        let mut protocol_relative = policy("v1", at("2026-01-01T00:00:00Z"));
        protocol_relative.documents[0].path = "//evil.example/policy".to_owned();
        assert!(PolicyCatalog::new(vec![protocol_relative]).is_err());
    }

    #[test]
    fn catalog_rejects_invalid_slug_and_version() {
        let mut bad_slug = policy("v1", at("2026-01-01T00:00:00Z"));
        bad_slug.slug = "Privacy Policy".to_owned();
        assert!(PolicyCatalog::new(vec![bad_slug]).is_err());

        let mut bad_version = policy("v 1", at("2026-01-01T00:00:00Z"));
        bad_version.version = "v 1".to_owned();
        assert!(PolicyCatalog::new(vec![bad_version]).is_err());
    }
}
