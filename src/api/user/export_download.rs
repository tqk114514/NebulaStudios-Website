//! `GET /api/user/export/{token}`：一次性下载用户数据 JSON 快照。

use axum::Json;
use axum::extract::{Path, State};
use axum::http::header::{CACHE_CONTROL, CONTENT_DISPOSITION};
use axum::http::{HeaderMap, HeaderValue};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Utc};
use serde::Serialize;

use crate::api::AppState;
use crate::error::ApiError;
use crate::model::audit::AuditEntry;
use crate::model::consent::PolicyConsent;
use crate::model::export::ExportToken;
use crate::model::totp::TotpState;
use crate::model::user::User;

/// 导出文档中的 TOTP 状态；不含密钥与恢复码明文。
#[derive(Debug, Serialize)]
pub struct TotpExport {
    /// 是否已启用两步验证。
    pub enabled: bool,
    /// 尚未使用的恢复码数量。
    pub recovery_codes_remaining: usize,
}

/// 导出文档中的社交身份；不含内部记录 id 与所属用户 id。
#[derive(Debug, Serialize)]
pub struct SocialIdentityExport {
    /// provider 标识，如 `google`。
    pub provider: String,
    /// provider 侧用户 id。
    pub provider_user_id: String,
    /// provider 昵称。
    pub display_name: Option<String>,
    /// 绑定时刻。
    pub created_at: DateTime<Utc>,
}

/// 完整导出文档；字段与 `format_version` 是跨版本契约。
#[derive(Debug, Serialize)]
pub struct ExportDocument {
    /// 文档格式版本；当前为 1。
    pub format_version: u8,
    /// 生成时刻。
    pub exported_at: DateTime<Utc>,
    /// 用户资料；不含口令哈希与内部头像版本。
    pub user: User,
    /// 头像公开地址；从未上传为 `None`。
    pub avatar_url: Option<String>,
    /// 两步验证状态。
    pub totp: TotpExport,
    /// 已绑定的社交身份。
    pub social_identities: Vec<SocialIdentityExport>,
    /// 政策同意记录，按同意时刻升序。
    pub policy_consents: Vec<PolicyConsent>,
    /// 该用户作为操作者或被操作对象的审计记录，按时间升序。
    pub audit_log: Vec<AuditEntry>,
}

/// 消费令牌并返回数据快照；令牌在读取数据前即被消费，后续查询失败需重新申请。
///
/// # Errors
/// 令牌非法、过期或已消费 404；存储故障 500。
pub async fn download_export(
    State(state): State<AppState>,
    Path(raw_token): Path<String>,
) -> Result<Response, ApiError> {
    // 令牌固定 64 个十六进制字符；长度不符直接拒绝，不做无谓的哈希与查询
    if raw_token.len() != 64 {
        return Err(ApiError::ExportTokenInvalid);
    }
    let now = state.clock.now();
    let Some(user_id) = state
        .export_repo
        .consume(&ExportToken::hash_of(&raw_token), now)
        .await
        .map_err(anyhow::Error::new)?
    else {
        return Err(ApiError::ExportTokenInvalid);
    };

    // 令牌已消费；以下任一查询失败都要求用户重新申请链接
    let Some(credentials) =
        state.user_repo.find_by_id(&user_id).await.map_err(anyhow::Error::new)?
    else {
        return Err(ApiError::ExportTokenInvalid);
    };
    let social_identities =
        state.social_repo.list_for_user(&user_id).await.map_err(anyhow::Error::new)?;
    let totp = state.totp_repo.find(&user_id).await.map_err(anyhow::Error::new)?;
    let recovery_codes =
        state.totp_repo.recovery_codes(&user_id).await.map_err(anyhow::Error::new)?;
    let policy_consents =
        state.consent_repo.list_for_user(&user_id).await.map_err(anyhow::Error::new)?;
    let audit_log =
        state.admin_repo.list_audit_for_user(&user_id).await.map_err(anyhow::Error::new)?;

    let avatar_url = crate::api::avatar_url(&credentials.user.id, credentials.user.avatar_version);
    let document = ExportDocument {
        format_version: 1,
        exported_at: now,
        user: credentials.user,
        avatar_url,
        totp: TotpExport {
            enabled: matches!(totp, Some((TotpState::Enabled, _))),
            recovery_codes_remaining: recovery_codes
                .iter()
                .filter(|code| code.consumed_at.is_none())
                .count(),
        },
        social_identities: social_identities
            .into_iter()
            .map(|identity| SocialIdentityExport {
                provider: identity.provider.as_str().to_owned(),
                provider_user_id: identity.provider_user_id,
                display_name: identity.display_name,
                created_at: identity.created_at,
            })
            .collect(),
        policy_consents,
        audit_log,
    };

    let mut headers = HeaderMap::new();
    headers.insert(CACHE_CONTROL, HeaderValue::from_static("no-store"));
    let disposition = format!("attachment; filename=\"nebula-export-{user_id}.json\"");
    headers.insert(
        CONTENT_DISPOSITION,
        HeaderValue::from_str(&disposition).map_err(anyhow::Error::new)?,
    );
    Ok((headers, Json(document)).into_response())
}
