/**
 * internal/services/token.go
 * Token 和验证码管理服务
 *
 * 功能：
 * - 验证 Token 生成和验证
 * - 验证码生成和验证
 * - 过期数据自动清理
 * - 支持多种 Token 类型（注册、重置密码、修改密码、删除账户）
 *
 * 数据表：
 * - tokens: 存储验证 Token
 * - codes: 存储验证码
 *
 * 依赖：
 * - PostgreSQL 数据库
 * - utils.GenerateSecureToken: 安全 Token 生成
 * - utils.GenerateCode: 验证码生成
 */

package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/utils"
)

// ====================  错误定义 ====================

var (
	// ErrTokenDBNotReady 数据库未就绪
	ErrTokenDBNotReady = errors.New("database not ready")
	// ErrInvalidToken Token 无效
	ErrInvalidToken = errors.New("INVALID_TOKEN")
	// ErrTokenExpired Token 已过期
	ErrTokenExpired = errors.New("TOKEN_EXPIRED")
	// ErrTokenUsed Token 已使用
	ErrTokenUsed = errors.New("TOKEN_USED")
	// ErrTokenCreateFailed Token 创建失败
	ErrTokenCreateFailed = errors.New("TOKEN_CREATE_FAILED")
	// ErrTokenValidationFailed Token 验证失败
	ErrTokenValidationFailed = errors.New("TOKEN_VALIDATION_FAILED")
	// ErrInvalidCode 验证码无效
	ErrInvalidCode = errors.New("INVALID_CODE")
	// ErrCodeExpired 验证码已过期
	ErrCodeExpired = errors.New("CODE_EXPIRED")
	// ErrEmailMismatch 邮箱不匹配
	ErrEmailMismatch = errors.New("EMAIL_MISMATCH")
	// ErrTypeMismatch 类型不匹配
	ErrTypeMismatch = errors.New("TYPE_MISMATCH")
	// ErrTooManyAttempts 尝试次数过多
	ErrTooManyAttempts = errors.New("TOO_MANY_ATTEMPTS")
	// ErrCodeNotVerified 验证码未验证
	ErrCodeNotVerified = errors.New("CODE_NOT_VERIFIED")
	// ErrEmptyEmail 邮箱为空
	ErrEmptyEmail = errors.New("email is empty")
)

// ====================  常量定义 ====================

const (
	// TokenTypeRegister 注册 Token 类型
	TokenTypeRegister = "register"
	// TokenTypeResetPassword 重置密码 Token 类型
	TokenTypeResetPassword = "reset_password"
	// TokenTypeChangePassword 修改密码 Token 类型
	TokenTypeChangePassword = "change_password"
	// TokenTypeDeleteAccount 删除账户 Token 类型
	TokenTypeDeleteAccount = "delete_account"

	// tokenExpiry Token 过期时间（5 分钟）
	tokenExpiry = 5 * time.Minute

	// maxCodeAttempts 验证码最大尝试次数
	maxCodeAttempts = 5

	// tokenUsed Token 已使用标记
	tokenUsed = 1

	// codeVerified 验证码已验证标记
	codeVerified = 1
)

// ====================  数据结构 ====================

// TokenResult Token 验证结果
type TokenResult struct {
	Code  string `json:"code"`
	Email string `json:"email"`
	Type  string `json:"type"`
}

// CodeResult 验证码验证结果
type CodeResult struct {
	Type            string `json:"type"`
	AlreadyVerified bool   `json:"alreadyVerified"`
}

// TokenService Token 服务
type TokenService struct{}

// ====================  构造函数 ====================

// NewTokenService 创建 Token 服务
// 返回：
//   - *TokenService: Token 服务实例
func NewTokenService() *TokenService {
	utils.LogPrintf("[TOKEN] Token service initialized")
	return &TokenService{}
}

// ====================  公开方法 ====================

// CreateToken 创建 Token
// 参数：
//   - ctx: 上下文
//   - email: 邮箱地址
//   - tokenType: Token 类型
//
// 返回：
//   - string: Token 字符串
//   - int64: 过期时间（毫秒时间戳）
//   - error: 错误信息
func (s *TokenService) CreateToken(ctx context.Context, email, tokenType string) (string, int64, error) {
	// 参数验证
	if email == "" {
		return "", 0, ErrEmptyEmail
	}

	// 检查数据库连接
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[TOKEN] ERROR: Database pool is nil")
		return "", 0, ErrTokenDBNotReady
	}

	// 规范化输入
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	normalizedType := strings.TrimSpace(tokenType)
	if normalizedType == "" {
		normalizedType = TokenTypeRegister
	}

	// 生成安全 Token
	token, err := utils.GenerateSecureToken()
	if err != nil {
		utils.LogPrintf("[TOKEN] ERROR: Failed to generate secure token: %v", err)
		return "", 0, fmt.Errorf("%w: %v", ErrTokenCreateFailed, err)
	}

	// 计算时间
	now := time.Now().UnixMilli()
	expireTime := now + int64(tokenExpiry.Milliseconds())

	// 插入数据库
	_, err = pool.Exec(ctx, `
		INSERT INTO tokens (token, email, type, created_at, expire_time, used)
		VALUES ($1, $2, $3, $4, $5, 0)
	`, token, normalizedEmail, normalizedType, now, expireTime)

	if err != nil {
		utils.LogPrintf("[TOKEN] ERROR: Failed to insert token: email=%s, type=%s, error=%v",
			normalizedEmail, normalizedType, err)
		return "", 0, fmt.Errorf("%w: %v", ErrTokenCreateFailed, err)
	}

	utils.LogPrintf("[TOKEN] Token created: email=%s, type=%s, expiry=%v",
		normalizedEmail, normalizedType, tokenExpiry)
	return token, expireTime, nil
}

// ValidateAndUseToken 验证并使用 Token
// 参数：
//   - ctx: 上下文
//   - token: Token 字符串
//
// 返回：
//   - *TokenResult: 验证结果
//   - error: 错误信息
func (s *TokenService) ValidateAndUseToken(ctx context.Context, token string) (*TokenResult, error) {
	// 参数验证
	if token == "" {
		return nil, ErrInvalidToken
	}

	// 检查数据库连接
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[TOKEN] ERROR: Database pool is nil")
		return nil, ErrTokenDBNotReady
	}

	trimmedToken := strings.TrimSpace(token)

	// 查询 Token
	var email, tokenType string
	var code *string
	var expireTime int64
	var used int

	err := pool.QueryRow(ctx, `
		SELECT email, type, code, expire_time, used FROM tokens WHERE token = $1
	`, trimmedToken).Scan(&email, &tokenType, &code, &expireTime, &used)

	if err != nil {
		utils.LogPrintf("[TOKEN] DEBUG: Token not found or query error: %v", err)
		return nil, ErrInvalidToken
	}

	now := time.Now().UnixMilli()

	// 检查过期
	if now > expireTime {
		// 删除过期 Token
		if _, err := pool.Exec(ctx, "DELETE FROM tokens WHERE token = $1", trimmedToken); err != nil {
			utils.LogPrintf("[TOKEN] WARN: Failed to delete expired token: %v", err)
		}
		return nil, ErrTokenExpired
	}

	// 检查是否已使用
	if used == tokenUsed {
		return nil, ErrTokenUsed
	}

	// 生成验证码（如果没有）
	var codeStr string
	if code == nil || *code == "" {
		var err error
		codeStr, err = utils.GenerateCode()
		if err != nil {
			utils.LogPrintf("[TOKEN] ERROR: Failed to generate verification code: %v", err)
			return nil, fmt.Errorf("failed to generate code: %w", err)
		}
		codeExpireTime := now + int64(tokenExpiry.Milliseconds())

		// 更新 Token 的验证码
		if _, err := pool.Exec(ctx, "UPDATE tokens SET code = $1 WHERE token = $2", codeStr, trimmedToken); err != nil {
			utils.LogPrintf("[TOKEN] WARN: Failed to update token code: %v", err)
		}

		// 创建验证码记录
		if _, err := pool.Exec(ctx, `
			INSERT INTO codes (code, email, type, created_at, expire_time, attempts, verified)
			VALUES ($1, $2, $3, $4, $5, 0, 0)
		`, codeStr, email, tokenType, now, codeExpireTime); err != nil {
			utils.LogPrintf("[TOKEN] WARN: Failed to create code record: %v", err)
		}
	} else {
		codeStr = *code
	}

	// 标记 Token 已使用
	if _, err := pool.Exec(ctx, "UPDATE tokens SET used = 1 WHERE token = $1", trimmedToken); err != nil {
		utils.LogPrintf("[TOKEN] WARN: Failed to mark token as used: %v", err)
	}

	utils.LogPrintf("[TOKEN] Token validated: email=%s, type=%s", email, tokenType)

	return &TokenResult{
		Code:  codeStr,
		Email: email,
		Type:  tokenType,
	}, nil
}

// VerifyCode 验证验证码
// 参数：
//   - ctx: 上下文
//   - code: 验证码
//   - email: 邮箱地址
//   - expectedType: 期望的类型（可为空）
//
// 返回：
//   - *CodeResult: 验证结果
//   - error: 错误信息
func (s *TokenService) VerifyCode(ctx context.Context, code, email, expectedType string) (*CodeResult, error) {
	// 参数验证
	if code == "" {
		return nil, ErrInvalidCode
	}
	if email == "" {
		return nil, ErrEmptyEmail
	}

	// 检查数据库连接
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[TOKEN] ERROR: Database pool is nil")
		return nil, ErrTokenDBNotReady
	}

	trimmedCode := strings.TrimSpace(code)
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	// 查询验证码
	var codeEmail, codeType string
	var expireTime int64
	var attempts, verified int

	err := pool.QueryRow(ctx, `
		SELECT email, type, expire_time, attempts, verified FROM codes WHERE code = $1
	`, trimmedCode).Scan(&codeEmail, &codeType, &expireTime, &attempts, &verified)

	if err != nil {
		utils.LogPrintf("[TOKEN] DEBUG: Code not found or query error: %v", err)
		return nil, ErrInvalidCode
	}

	// 检查邮箱
	if codeEmail != normalizedEmail {
		utils.LogPrintf("[TOKEN] WARN: Email mismatch: expected=%s, got=%s", codeEmail, normalizedEmail)
		return nil, ErrEmailMismatch
	}

	// 检查类型
	if expectedType != "" && codeType != expectedType {
		utils.LogPrintf("[TOKEN] WARN: Type mismatch: expected=%s, got=%s", expectedType, codeType)
		return nil, ErrTypeMismatch
	}

	now := time.Now().UnixMilli()

	// 检查过期
	if now > expireTime {
		// 删除过期验证码
		if _, err := pool.Exec(ctx, "DELETE FROM codes WHERE code = $1", trimmedCode); err != nil {
			utils.LogPrintf("[TOKEN] WARN: Failed to delete expired code: %v", err)
		}
		return nil, ErrCodeExpired
	}

	// 检查是否已验证
	if verified == codeVerified {
		return &CodeResult{Type: codeType, AlreadyVerified: true}, nil
	}

	// 检查尝试次数
	newAttempts := attempts + 1
	if newAttempts > maxCodeAttempts {
		// 删除超过尝试次数的验证码
		if _, err := pool.Exec(ctx, "DELETE FROM codes WHERE code = $1", trimmedCode); err != nil {
			utils.LogPrintf("[TOKEN] WARN: Failed to delete code after max attempts: %v", err)
		}
		utils.LogPrintf("[TOKEN] WARN: Too many attempts for code: email=%s", normalizedEmail)
		return nil, ErrTooManyAttempts
	}

	// 更新验证状态
	if _, err := pool.Exec(ctx, `
		UPDATE codes SET attempts = $1, verified = 1, verified_at = $2 WHERE code = $3
	`, newAttempts, now, trimmedCode); err != nil {
		utils.LogPrintf("[TOKEN] WARN: Failed to update code verification status: %v", err)
	}

	utils.LogPrintf("[TOKEN] Code verified: email=%s, type=%s, attempts=%d", normalizedEmail, codeType, newAttempts)

	return &CodeResult{Type: codeType}, nil
}

// IsCodeVerified 检查验证码是否已验证
// 参数：
//   - ctx: 上下文
//   - code: 验证码
//   - email: 邮箱地址
//
// 返回：
//   - bool: 是否已验证
//   - error: 错误信息
func (s *TokenService) IsCodeVerified(ctx context.Context, code, email string) (bool, error) {
	// 参数验证
	if code == "" {
		return false, ErrInvalidCode
	}
	if email == "" {
		return false, ErrEmptyEmail
	}

	// 检查数据库连接
	pool := models.GetPool()
	if pool == nil {
		return false, ErrTokenDBNotReady
	}

	trimmedCode := strings.TrimSpace(code)
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	// 查询验证码
	var codeEmail string
	var expireTime int64
	var verified int

	err := pool.QueryRow(ctx, `
		SELECT email, expire_time, verified FROM codes WHERE code = $1
	`, trimmedCode).Scan(&codeEmail, &expireTime, &verified)

	if err != nil {
		return false, ErrInvalidCode
	}

	// 检查邮箱
	if codeEmail != normalizedEmail {
		return false, ErrEmailMismatch
	}

	// 检查过期
	now := time.Now().UnixMilli()
	if now > expireTime {
		// 删除过期验证码
		if _, err := pool.Exec(ctx, "DELETE FROM codes WHERE code = $1", trimmedCode); err != nil {
			utils.LogPrintf("[TOKEN] WARN: Failed to delete expired code: %v", err)
		}
		return false, ErrCodeExpired
	}

	// 检查是否已验证
	if verified != codeVerified {
		return false, ErrCodeNotVerified
	}

	return true, nil
}

// UseCode 使用验证码（删除）
// 参数：
//   - ctx: 上下文
//   - code: 验证码
//   - email: 邮箱地址
//
// 返回：
//   - error: 错误信息
func (s *TokenService) UseCode(ctx context.Context, code, email string) error {
	// 参数验证
	if code == "" {
		return ErrInvalidCode
	}
	if email == "" {
		return ErrEmptyEmail
	}

	// 检查数据库连接
	pool := models.GetPool()
	if pool == nil {
		return ErrTokenDBNotReady
	}

	trimmedCode := strings.TrimSpace(code)
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	// 查询验证码
	var codeEmail string
	var verified int

	err := pool.QueryRow(ctx, `
		SELECT email, verified FROM codes WHERE code = $1
	`, trimmedCode).Scan(&codeEmail, &verified)

	if err != nil {
		return ErrInvalidCode
	}

	// 验证邮箱和状态
	if codeEmail != normalizedEmail {
		return ErrEmailMismatch
	}
	if verified != codeVerified {
		return ErrCodeNotVerified
	}

	// 删除验证码
	if _, err := pool.Exec(ctx, "DELETE FROM codes WHERE code = $1", trimmedCode); err != nil {
		utils.LogPrintf("[TOKEN] WARN: Failed to delete used code: %v", err)
	}

	utils.LogPrintf("[TOKEN] Code used and removed: email=%s", normalizedEmail)
	return nil
}

// InvalidateCodeByEmail 使指定邮箱的验证码失效
// 参数：
//   - ctx: 上下文
//   - email: 邮箱地址
//   - tokenType: Token 类型（可为 nil 表示所有类型）
//
// 返回：
//   - error: 错误信息
func (s *TokenService) InvalidateCodeByEmail(ctx context.Context, email string, tokenType *string) error {
	// 空邮箱直接返回
	if email == "" {
		return nil
	}

	// 检查数据库连接
	pool := models.GetPool()
	if pool == nil {
		return ErrTokenDBNotReady
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	var err error
	if tokenType != nil && *tokenType != "" {
		_, err = pool.Exec(ctx, "DELETE FROM codes WHERE email = $1 AND type = $2", normalizedEmail, *tokenType)
	} else {
		_, err = pool.Exec(ctx, "DELETE FROM codes WHERE email = $1", normalizedEmail)
	}

	if err != nil {
		utils.LogPrintf("[TOKEN] WARN: Failed to invalidate codes: email=%s, error=%v", normalizedEmail, err)
		return fmt.Errorf("failed to invalidate codes: %w", err)
	}

	utils.LogPrintf("[TOKEN] Codes invalidated: email=%s", normalizedEmail)
	return nil
}

// GetCodeExpiry 获取验证码过期时间
// 参数：
//   - ctx: 上下文
//   - code: 验证码
//   - email: 邮箱地址
//
// 返回：
//   - int64: 过期时间（毫秒时间戳）
//   - error: 错误信息
func (s *TokenService) GetCodeExpiry(ctx context.Context, code, email string) (int64, error) {
	// 参数验证
	if code == "" {
		return 0, ErrInvalidCode
	}
	if email == "" {
		return 0, ErrEmptyEmail
	}

	// 检查数据库连接
	pool := models.GetPool()
	if pool == nil {
		return 0, ErrTokenDBNotReady
	}

	trimmedCode := strings.TrimSpace(code)
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	// 查询验证码
	var codeEmail string
	var expireTime int64

	err := pool.QueryRow(ctx, `
		SELECT email, expire_time FROM codes WHERE code = $1
	`, trimmedCode).Scan(&codeEmail, &expireTime)

	if err != nil {
		return 0, ErrInvalidCode
	}

	// 检查邮箱
	if codeEmail != normalizedEmail {
		return 0, ErrEmailMismatch
	}

	return expireTime, nil
}

// CleanupExpired 清理过期数据
// 参数：
//   - ctx: 上下文
func (s *TokenService) CleanupExpired(ctx context.Context) {
	// 检查数据库连接
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[TOKEN] WARN: Cannot cleanup - database pool is nil")
		return
	}

	now := time.Now().UnixMilli()

	// 清理过期 Token
	tokenResult, err := pool.Exec(ctx, "DELETE FROM tokens WHERE expire_time < $1", now)
	if err != nil {
		utils.LogPrintf("[TOKEN] WARN: Failed to cleanup expired tokens: %v", err)
	}

	// 清理过期验证码
	codeResult, codeErr := pool.Exec(ctx, "DELETE FROM codes WHERE expire_time < $1", now)
	if codeErr != nil {
		utils.LogPrintf("[TOKEN] WARN: Failed to cleanup expired codes: %v", codeErr)
	}

	// 记录清理结果
	var tokenCount, codeCount int64
	if err == nil {
		tokenCount = tokenResult.RowsAffected()
	}
	if codeErr == nil {
		codeCount = codeResult.RowsAffected()
	}

	if tokenCount > 0 || codeCount > 0 {
		utils.LogPrintf("[TOKEN] Cleanup completed: %d tokens, %d codes removed", tokenCount, codeCount)
	}
}

// GetTokenExpiry 获取 Token 过期时间配置
// 返回：
//   - time.Duration: Token 过期时间
func (s *TokenService) GetTokenExpiry() time.Duration {
	return tokenExpiry
}
