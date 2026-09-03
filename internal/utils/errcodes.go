// Package utils 错误码统一注册表。
//
// 约定：
//   - 所有返回给客户端的 errorCode 一律使用本文件定义的常量，禁止在 handler 中内联字符串字面量；
//   - 新增错误码：在本文件按领域分组登记 → 前端 src/api/errorCodes.ts 登记映射 →
//     src/i18n/sources/general/ 补充 error.* 文案（五语言）；纯内部失败类错误可不加文案，
//     前端未登记时回退通用文案（error.operationFailed）；
//   - models 包的哨兵错误（errors.New("XXX")）经 handlers.RespondTokenError 透传为 errorCode，
//     其字符串值即线上错误码，改动前必须同步前端映射（见 models/token.go、models/oauth_token.go 等）。
package utils

// 通用
const (
	ErrCodeInternalError      = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrCodeInvalidRequest     = "INVALID_REQUEST"
	ErrCodeMissingParameters  = "MISSING_PARAMETERS"
	ErrCodeRequestTooLarge    = "REQUEST_TOO_LARGE"
	ErrCodeRateLimit          = "RATE_LIMIT"
	ErrCodeConfigNotLoaded    = "CONFIG_NOT_LOADED"
	ErrCodeNotFound           = "NOT_FOUND"
	ErrCodeDatabaseError      = "DATABASE_ERROR"
	ErrCodeUnauthorized       = "UNAUTHORIZED"
	ErrCodeGetFailed          = "GET_FAILED"
	ErrCodeQueryFailed        = "QUERY_FAILED"
)

// 认证与会话（含 Refresh Token 轮换）
const (
	ErrCodeInvalidCredentials    = "INVALID_CREDENTIALS"
	ErrCodeRegisterFailed        = "REGISTER_FAILED"
	ErrCodeInvalidToken          = "INVALID_TOKEN"
	ErrCodeTokenExpired          = "TOKEN_EXPIRED"
	ErrCodeTokenInvalid          = "TOKEN_INVALID"
	ErrCodeInvalidRefreshToken   = "INVALID_REFRESH_TOKEN"
	ErrCodeRefreshTokenExpired   = "REFRESH_TOKEN_EXPIRED"
	ErrCodeRefreshTokenReused    = "REFRESH_TOKEN_REUSED"
	ErrCodeNoRefreshToken        = "NO_REFRESH_TOKEN"
	ErrCodeNoToken               = "NO_TOKEN"
	ErrCodeMissingToken          = "MISSING_TOKEN"
	ErrCodeCaptchaFailed         = "CAPTCHA_FAILED"
	ErrCodeTokenCreateFailed     = "TOKEN_CREATE_FAILED"
	ErrCodeTokenGenerateFailed   = "TOKEN_GENERATE_FAILED"
	ErrCodeTokenGenerationFailed = "TOKEN_GENERATION_FAILED"
	ErrCodeCSRFTokenMissing      = "CSRF_TOKEN_MISSING"
	ErrCodeCSRFTokenMismatch     = "CSRF_TOKEN_MISMATCH"
)

// 用户资料与账户操作
const (
	ErrCodeUpdateFailed   = "UPDATE_FAILED"
	ErrCodeDeleteFailed   = "DELETE_FAILED"
	ErrCodeResetFailed    = "RESET_FAILED"
	ErrCodeWrongPassword  = "WRONG_PASSWORD"
	ErrCodeSamePassword   = "SAME_PASSWORD"
	ErrCodeUserNotFound   = "USER_NOT_FOUND"
	ErrCodeUserBanned     = "USER_BANNED"
	ErrCodeInvalidUserUID = "INVALID_USER_UID"
	ErrCodeInvalidID      = "INVALID_ID"
)

// 用户名
const (
	ErrCodeUsernameAlreadyExists = "USERNAME_ALREADY_EXISTS"
)

// 管理后台（用户管理与角色）
const (
	ErrCodeInvalidRole             = "INVALID_ROLE"
	ErrCodeCannotModifySelf        = "CANNOT_MODIFY_SELF"
	ErrCodeCannotModifySuperAdmin  = "CANNOT_MODIFY_SUPER_ADMIN"
	ErrCodeCannotBanSelf           = "CANNOT_BAN_SELF"
	ErrCodeCannotBanAdmin          = "CANNOT_BAN_ADMIN"
	ErrCodeCannotPromoteBannedUser = "CANNOT_PROMOTE_BANNED_USER"
	ErrCodeCannotDeleteSelf        = "CANNOT_DELETE_SELF"
	ErrCodeCannotDeleteAdmin       = "CANNOT_DELETE_ADMIN"
	ErrCodeCannotDeleteSuperAdmin  = "CANNOT_DELETE_SUPER_ADMIN"
	ErrCodeBanFailed               = "BAN_FAILED"
	ErrCodeUnbanFailed             = "UNBAN_FAILED"
	ErrCodeReasonRequired          = "REASON_REQUIRED"
	ErrCodeInvalidReason           = "INVALID_REASON"
)

// 邮箱与注册白名单
const (
	ErrCodeEmailAlreadyExists          = "EMAIL_ALREADY_EXISTS"
	ErrCodeEmailDomainNotAllowed       = "EMAIL_DOMAIN_NOT_ALLOWED"
	ErrCodeEmailWhitelistNotConfigured = "EMAIL_WHITELIST_NOT_CONFIGURED"
	ErrCodeWhitelistCheckFailed        = "WHITELIST_CHECK_FAILED"
	ErrCodeDomainExists                = "DOMAIN_EXISTS"
	ErrCodeInvalidDomain               = "INVALID_DOMAIN"
	ErrCodeMissingDomain               = "MISSING_DOMAIN"
	ErrCodeInvalidSignupURL            = "INVALID_SIGNUP_URL"
	ErrCodeMissingSignupURL            = "MISSING_SIGNUP_URL"
)

// OAuth 绑定（Microsoft / Google 登录态关联）
const (
	ErrCodeLinkFailed         = "LINK_FAILED"
	ErrCodeUnlinkFailed       = "UNLINK_FAILED"
	ErrCodeNotLinked          = "NOT_LINKED"
	ErrCodeOAuthNotConfigured = "OAUTH_NOT_CONFIGURED"
	ErrCodeInvalidLinkToken   = "INVALID_LINK_TOKEN"
	ErrCodeLinkTokenExpired   = "LINK_TOKEN_EXPIRED"
)

// OAuth Provider（第三方应用接入）
const (
	ErrCodeInvalidRedirectURI = "INVALID_REDIRECT_URI"
	ErrCodeInvalidClientID    = "INVALID_CLIENT_ID"
	ErrCodeClientNotFound     = "CLIENT_NOT_FOUND"
	ErrCodeGrantNotFound      = "GRANT_NOT_FOUND"
	ErrCodeGrantLookupFailed  = "GRANT_LOOKUP_FAILED"
	ErrCodeRevokeFailed       = "REVOKE_FAILED"
	ErrCodeRegenerateFailed   = "REGENERATE_FAILED"
	ErrCodeToggleFailed       = "TOGGLE_FAILED"
	ErrCodeInvalidLogoURL     = "INVALID_LOGO_URL"
)

// 政策同意
const (
	ErrCodeInvalidPolicyType    = "INVALID_POLICY_TYPE"
	ErrCodeInvalidPolicyVersion = "INVALID_POLICY_VERSION"
	ErrCodeConsentLogFailed     = "CONSENT_LOG_FAILED"
)

// 数据导出与导入
const (
	ErrCodeExportFailed            = "EXPORT_FAILED"
	ErrCodeExportSaltNotConfigured = "EXPORT_SALT_NOT_CONFIGURED"
	ErrCodeImportFailed            = "IMPORT_FAILED"
	ErrCodeFileRequired            = "FILE_REQUIRED"
	ErrCodeFileReadError           = "FILE_READ_ERROR"
	ErrCodeInvalidFileFormat       = "INVALID_FILE_FORMAT"
	ErrCodeFileTokenNotFound       = "FILE_TOKEN_NOT_FOUND"
	ErrCodeEncryptionFailed        = "ENCRYPTION_FAILED"
	ErrCodeDecryptionFailed        = "DECRYPTION_FAILED"
	ErrCodeManifestNotFound        = "MANIFEST_NOT_FOUND"
)
