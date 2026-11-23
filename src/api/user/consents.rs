//! `GET` / `POST /api/user/consents`：政策同意记录与待同意版本。

use std::collections::HashSet;

use axum::Json;
use axum::extract::State;
use axum::http::StatusCode;
use serde::{Deserialize, Serialize};

use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::{FirstPartyUser, JsonBody};
use crate::model::consent::PolicyConsent;
use crate::model::policy::Policy;

/// 同意记录响应。
#[derive(Debug, Serialize)]
pub struct ConsentsResponse {
    /// 已同意记录，按同意时刻升序。
    pub consents: Vec<PolicyConsent>,
    /// 当前生效但尚未同意的政策版本。
    pub pending: Vec<Policy>,
}

/// 同意请求。
#[derive(Debug, Deserialize)]
pub struct RecordConsentRequest {
    /// 政策 slug。
    pub policy: String,
    /// 政策版本。
    pub version: String,
}

/// 返回用户全部同意记录与当前待同意的政策版本。
///
/// # Errors
/// 未认证 401；存储故障 500。
pub async fn list(
    State(state): State<AppState>,
    user: FirstPartyUser,
) -> Result<Json<ConsentsResponse>, ApiError> {
    let now = state.clock.now();
    let consents =
        state.consent_repo.list_for_user(&user.user_id).await.map_err(anyhow::Error::new)?;
    let accepted: HashSet<(&str, &str)> = consents
        .iter()
        .map(|consent| (consent.policy.as_str(), consent.version.as_str()))
        .collect();
    let pending = state
        .policy_catalog
        .current_policies(now)
        .into_iter()
        .filter(|policy| !accepted.contains(&(policy.slug.as_str(), policy.version.as_str())))
        .cloned()
        .collect();
    Ok(Json(ConsentsResponse { consents, pending }))
}

/// 记录对当前生效版本的同意；重复提交幂等，返回 204。
///
/// 只接受 manifest 中存在、已生效且为该 slug 当前版本的条目；旧版本会让前端
/// 在提交前拿到 422，避免把过期条款记成有效同意。
///
/// # Errors
/// 未认证 401；CSRF 失败 403；字段缺失或政策/版本不可接受 422；存储故障 500。
pub async fn record(
    State(state): State<AppState>,
    user: FirstPartyUser,
    body: JsonBody<RecordConsentRequest>,
) -> Result<StatusCode, ApiError> {
    let request = body.0;
    let policy = request.policy.trim();
    let version = request.version.trim();
    if policy.is_empty() {
        return Err(invalid("policy", "policy_missing"));
    }
    if version.is_empty() {
        return Err(invalid("version", "version_missing"));
    }

    let Some(entry) = state.policy_catalog.find(policy, version) else {
        return if state.policy_catalog.has_slug(policy) {
            Err(invalid("version", "version_unknown"))
        } else {
            Err(invalid("policy", "policy_unknown"))
        };
    };
    let now = state.clock.now();
    if entry.effective_at > now {
        return Err(invalid("version", "version_not_effective"));
    }
    if state.policy_catalog.current(policy, now).map(|current| current.version.as_str())
        != Some(version)
    {
        return Err(invalid("version", "version_not_current"));
    }

    state
        .consent_repo
        .record(&user.user_id, policy, version, now)
        .await
        .map_err(anyhow::Error::new)?;
    Ok(StatusCode::NO_CONTENT)
}

fn invalid(field: &'static str, code: &'static str) -> ApiError {
    ApiError::Validation(vec![FieldInvalid { field, code: code.to_owned() }])
}
