//! 政策同意记录：按 `(用户, 政策, 版本)` 保留首次同意的时刻。

use chrono::{DateTime, Utc};
use serde::Serialize;

/// 一条已落库的政策同意记录。
///
/// 同一用户对同一政策版本重复同意不产生新行，`consented_at` 保留首次时刻。
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct PolicyConsent {
    /// 政策 slug。
    pub policy: String,
    /// 政策版本。
    pub version: String,
    /// 首次同意时刻。
    pub consented_at: DateTime<Utc>,
}
