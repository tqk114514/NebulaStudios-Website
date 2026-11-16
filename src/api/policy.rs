//! `GET /api/policies`：公开政策 manifest。

use axum::Json;
use axum::extract::State;
use serde::Serialize;

use crate::api::AppState;
use crate::model::policy::Policy;

/// 政策版本视图；`current` 表示它是否为该 slug 当前生效版本。
#[derive(Debug, Serialize)]
pub struct PolicyView {
    /// 原始政策条目。
    #[serde(flatten)]
    pub policy: Policy,
    /// 是否为当前生效版本；未生效或已被新版本替代为 false。
    pub current: bool,
}

/// 政策列表响应。
#[derive(Debug, Serialize)]
pub struct PoliciesResponse {
    /// 全部政策版本，按 slug 升序、生效时刻降序。
    pub policies: Vec<PolicyView>,
}

/// 返回全部政策版本及当前生效标记；文档内容由部署侧按 `documents[].path` 提供。
pub async fn policies(State(state): State<AppState>) -> Json<PoliciesResponse> {
    let now = state.clock.now();
    let policies = state
        .policy_catalog
        .policies()
        .iter()
        .map(|policy| PolicyView {
            current: state
                .policy_catalog
                .current(&policy.slug, now)
                .map(|current| current.version.as_str())
                == Some(policy.version.as_str()),
            policy: policy.clone(),
        })
        .collect();
    Json(PoliciesResponse { policies })
}
