//! POST /api/auth/register：邮箱注册（ADR-0002 语义）。创建未验证账号，
//! 口令以 Argon2id 哈希落库；验证邮件发送随邮件切片接入。

use axum::Json;
use axum::body::Bytes;
use axum::extract::State;
use axum::http::StatusCode;
use serde::Deserialize;
use validator::{Validate, ValidationError, ValidationErrors};

use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::model::password::PasswordHash;
use crate::model::user::{User, UserId};
use crate::storage::{UserRepo, UserRepoError};

/// 注册请求；字段规则沿用 v3（ADR-0002）：邮箱非空且 ≤254、用户名 1–15 个
/// Unicode 字符、密码 16–64 且须含大小写字母、数字与标点。
#[derive(Debug, Deserialize, Validate)]
pub struct RegisterRequest {
    /// 邮箱；服务端 trim 并小写化后入库（v3 语义）。
    #[validate(length(min = 1, max = 254))]
    #[validate(email)]
    pub email: String,
    /// 展示用户名。
    #[validate(length(min = 1, max = 15))]
    pub username: String,
    /// 明文口令；只在本端点边界出现，不进日志与响应。
    #[validate(length(min = 16, max = 64))]
    #[validate(custom(function = "validate_password_strength"))]
    pub password: String,
}

impl RegisterRequest {
    /// 归一化：去首尾空白、邮箱小写化；在验证前完成，长度按归一后计。
    fn normalized(self) -> Self {
        Self {
            email: self.email.trim().to_lowercase(),
            username: self.username.trim().to_owned(),
            // 口令的空白是口令的一部分，不 trim
            password: self.password,
        }
    }
}

/// 密码规则委托共享助手 [`super::password_problem`]（与改密一致）。
fn validate_password_strength(password: &str) -> Result<(), ValidationError> {
    match super::password_problem(password) {
        Some(code) => Err(ValidationError::new(code)),
        None => Ok(()),
    }
}

/// 把 validator 错误映射为字段级 code。长度类错误按归一化后的值重算方向，
/// 不解析 validator 的参数结构；邮箱的任何失败统一 `invalid_email`（v3 语义）。
fn field_issues(req: &RegisterRequest, errors: &ValidationErrors) -> Vec<FieldInvalid> {
    errors
        .errors()
        .iter()
        // 只处理顶层字段错误；嵌套结构校验当前不存在
        .filter_map(|(field, kind)| {
            // 只处理顶层字段错误；嵌套结构校验当前不存在
            let validator::ValidationErrorsKind::Field(errs) = kind else {
                return None;
            };
            match field.as_ref() {
                "email" => Some(FieldInvalid { field: "email", code: "invalid_email".to_owned() }),
                "username" => {
                    let code = if req.username.chars().count() > 15 {
                        "username_too_long"
                    } else {
                        "username_too_short"
                    };
                    errs.first().map(|_| FieldInvalid { field: "username", code: code.to_owned() })
                }
                "password" => {
                    let code = match errs.first().map(|err| err.code.as_ref()) {
                        // length 规则失败：按实际长度定向；custom 规则直接透传其 code
                        Some("length") if req.password.chars().count() > 64 => "password_too_long",
                        Some("length") => "password_too_short",
                        Some(custom) => custom,
                        None => return None,
                    };
                    Some(FieldInvalid { field: "password", code: code.to_owned() })
                }
                _ => None,
            }
        })
        .collect()
}

/// 处理注册：归一化 → 校验 → 哈希（`spawn_blocking`）→ 落库 → 201 裸用户。
///
/// # Errors
/// JSON 非法 400、字段校验失败 422、用户名或邮箱占用 409、内部故障 500（ADR-0004）。
pub async fn register(
    State(state): State<AppState>,
    body: Bytes,
) -> Result<(StatusCode, Json<User>), ApiError> {
    // 手工反序列化而非 Json 提取器：坏 JSON 必须走统一错误信封，不用 axum 默认 400
    let req: RegisterRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let req = req.normalized();
    req.validate().map_err(|errors| ApiError::Validation(field_issues(&req, &errors)))?;

    let plain = req.password.clone();
    let hash = tokio::task::spawn_blocking(move || PasswordHash::hash(&plain))
        .await
        .map_err(anyhow::Error::from)?
        .map_err(anyhow::Error::new)?;

    let user = User {
        id: UserId::new(),
        username: req.username,
        email: req.email,
        created_at: state.clock.now(),
        email_verified: false,
    };

    match state.user_repo.insert(&user, &hash).await {
        Ok(()) => {
            // 注册即发首封验证邮件；发送失败可经重发端点补发
            super::issue_and_send_code(
                &state,
                &user,
                crate::model::verification::VerificationPurpose::VerifyEmail,
            )
            .await?;
            Ok((StatusCode::CREATED, Json(user)))
        }
        Err(UserRepoError::Conflict) => {
            Err(classify_conflict(state.user_repo.as_ref(), &user).await)
        }
        Err(err) => Err(anyhow::Error::new(err).into()),
    }
}

/// 唯一约束已触发后用只读查询归因错误码。归因查询失败时落到 `user_exists`：
/// 仍是正确的 409，只是少了字段级信息，不值得为它返回 500。
async fn classify_conflict(repo: &dyn UserRepo, candidate: &User) -> ApiError {
    let email_taken = repo.find_by_email(&candidate.email).await.is_ok_and(|user| user.is_some());
    if email_taken {
        return ApiError::EmailTaken;
    }
    let username_taken =
        repo.find_by_username(&candidate.username).await.is_ok_and(|user| user.is_some());
    if username_taken {
        return ApiError::UsernameTaken;
    }
    ApiError::UserExists
}
