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
	"sync"

	"strings"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/utils"
)

// ====================  错误定义 ====================

// 错误定义从 models 层导入
var (
	ErrInvalidToken     = models.ErrInvalidToken
	ErrTokenExpired     = models.ErrTokenExpired
	ErrTokenUsed        = models.ErrTokenUsed
	ErrInvalidCode      = models.ErrInvalidCode
	ErrCodeExpired      = models.ErrCodeExpired
	ErrEmailMismatch    = models.ErrEmailMismatch
	ErrTypeMismatch     = models.ErrTypeMismatch
	ErrTooManyAttempts  = models.ErrTooManyAttempts
	ErrCodeNotVerified  = models.ErrCodeNotVerified
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
	utils.LogInfo("TOKEN", "Token service initialized")
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
	if email == "" {
		return "", 0, errors.New("email is empty")
	}

	// 规范化输入
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	normalizedType := strings.TrimSpace(tokenType)
	if normalizedType == "" {
		normalizedType = TokenTypeRegister
	}

	// 生成安全 Token
	tokenStr, err := utils.GenerateSecureToken()
	if err != nil {
		return "", 0, utils.LogError("TOKEN", "GenerateSecureToken", err)
	}

	// 计算时间
	now := time.Now().UnixMilli()
	expireTime := now + int64(tokenExpiry.Milliseconds())

	// 创建 Token 对象
	token := &models.Token{
		Token:      tokenStr,
		Email:      normalizedEmail,
		Type:       normalizedType,
		CreatedAt:  now,
		ExpireTime: expireTime,
	}

	// 保存到数据库
	repo := models.NewTokenRepository()
	if err := repo.Create(ctx, token); err != nil {
		return "", 0, err
	}

	return tokenStr, expireTime, nil
}

// ValidateAndUseToken 验证并使用 Token
// 参数：
//   - ctx: 上下文
//   - token: Token 字符串
//
// 返回：
//   - *TokenResult: 验证结果
//   - error: 错误信息
func (s *TokenService) ValidateAndUseToken(ctx context.Context, tokenStr string) (*TokenResult, error) {
	// 参数验证
	if tokenStr == "" {
		return nil, models.ErrInvalidToken
	}

	tokenRepo := models.NewTokenRepository()
	codeRepo := models.NewCodeRepository()

	// 查询 Token
	token, err := tokenRepo.FindByToken(ctx, tokenStr)
	if err != nil {
		if utils.IsDatabaseNotFound(err) {
			utils.LogDebug("TOKEN", "Token not found", tokenStr)
			return nil, models.ErrInvalidToken
		}
		return nil, err
	}

	// 检查过期
	if token.IsExpired() {
		tokenRepo.DeleteByToken(ctx, tokenStr)
		return nil, models.ErrTokenExpired
	}

	// 检查是否已使用
	if token.IsUsed() {
		return nil, models.ErrTokenUsed
	}

	now := time.Now().UnixMilli()

	// 生成验证码（如果没有）
	var codeStr string
	if token.Code == nil || *token.Code == "" {
		codeStr, err = utils.GenerateCode()
		if err != nil {
			return nil, utils.LogError("TOKEN", "GenerateCode", err)
		}

		// 更新 Token 的验证码
		tokenRepo.UpdateCode(ctx, tokenStr, codeStr)

		// 创建验证码记录
		code := &models.Code{
			Code:       codeStr,
			Email:      token.Email,
			Type:       token.Type,
			CreatedAt:  now,
			ExpireTime: now + int64(tokenExpiry.Milliseconds()),
		}
		codeRepo.Create(ctx, code)
	} else {
		codeStr = *token.Code
	}

	// 标记 Token 已使用
	tokenRepo.MarkUsed(ctx, tokenStr)

	utils.LogInfo("TOKEN", fmt.Sprintf("Token validated: email=%s, type=%s", token.Email, token.Type))

	return &TokenResult{
		Code:  codeStr,
		Email: token.Email,
		Type:  token.Type,
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
func (s *TokenService) VerifyCode(ctx context.Context, codeStr, email, expectedType string) (*CodeResult, error) {
	// 参数验证
	if codeStr == "" {
		return nil, models.ErrInvalidCode
	}
	if email == "" {
		return nil, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	repo := models.NewCodeRepository()

	// 查询验证码
	code, err := repo.FindByCode(ctx, codeStr)
	if err != nil {
		if utils.IsDatabaseNotFound(err) {
			utils.LogDebug("TOKEN", "Code not found", codeStr)
			return nil, models.ErrInvalidCode
		}
		return nil, err
	}

	// 检查邮箱
	if code.Email != normalizedEmail {
		utils.LogWarn("TOKEN", fmt.Sprintf("Email mismatch: expected=%s, got=%s", code.Email, normalizedEmail))
		return nil, models.ErrEmailMismatch
	}

	// 检查类型
	if expectedType != "" && code.Type != expectedType {
		utils.LogWarn("TOKEN", fmt.Sprintf("Type mismatch: expected=%s, got=%s", expectedType, code.Type))
		return nil, models.ErrTypeMismatch
	}

	// 检查过期
	if code.IsExpired() {
		repo.DeleteByCode(ctx, codeStr)
		return nil, models.ErrCodeExpired
	}

	// 检查是否已验证
	if code.IsVerified() {
		return &CodeResult{Type: code.Type, AlreadyVerified: true}, nil
	}

	// 检查尝试次数
	newAttempts := code.Attempts + 1
	if newAttempts > maxCodeAttempts {
		repo.DeleteByCode(ctx, codeStr)
		utils.LogWarn("TOKEN", fmt.Sprintf("Too many attempts for code: email=%s", normalizedEmail))
		return nil, models.ErrTooManyAttempts
	}

	// 更新验证状态
	now := time.Now().UnixMilli()
	repo.UpdateVerification(ctx, codeStr, newAttempts, now)

	utils.LogInfo("TOKEN", fmt.Sprintf("Code verified: email=%s, type=%s, attempts=%d", normalizedEmail, code.Type, newAttempts))

	return &CodeResult{Type: code.Type}, nil
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
func (s *TokenService) IsCodeVerified(ctx context.Context, codeStr, email string) (bool, error) {
	// 参数验证
	if codeStr == "" {
		return false, models.ErrInvalidCode
	}
	if email == "" {
		return false, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	repo := models.NewCodeRepository()

	// 查询验证码
	code, err := repo.FindByCode(ctx, codeStr)
	if err != nil {
		return false, models.ErrInvalidCode
	}

	// 检查邮箱
	if code.Email != normalizedEmail {
		return false, models.ErrEmailMismatch
	}

	// 检查过期
	if code.IsExpired() {
		repo.DeleteByCode(ctx, codeStr)
		return false, models.ErrCodeExpired
	}

	// 检查是否已验证
	if !code.IsVerified() {
		return false, models.ErrCodeNotVerified
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
func (s *TokenService) UseCode(ctx context.Context, codeStr, email string) error {
	// 参数验证
	if codeStr == "" {
		return models.ErrInvalidCode
	}
	if email == "" {
		return errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	repo := models.NewCodeRepository()

	// 查询验证码
	code, err := repo.FindByCode(ctx, codeStr)
	if err != nil {
		return models.ErrInvalidCode
	}

	// 验证邮箱和状态
	if code.Email != normalizedEmail {
		return models.ErrEmailMismatch
	}
	if !code.IsVerified() {
		return models.ErrCodeNotVerified
	}

	// 删除验证码
	repo.DeleteByCode(ctx, codeStr)

	utils.LogInfo("TOKEN", fmt.Sprintf("Code used and removed: email=%s", normalizedEmail))
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

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	repo := models.NewCodeRepository()

	return repo.DeleteByEmail(ctx, normalizedEmail, tokenType)
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
func (s *TokenService) GetCodeExpiry(ctx context.Context, codeStr, email string) (int64, error) {
	// 参数验证
	if codeStr == "" {
		return 0, models.ErrInvalidCode
	}
	if email == "" {
		return 0, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	repo := models.NewCodeRepository()

	// 查询验证码
	code, err := repo.FindByCode(ctx, codeStr)
	if err != nil {
		return 0, models.ErrInvalidCode
	}

	// 检查邮箱
	if code.Email != normalizedEmail {
		return 0, models.ErrEmailMismatch
	}

	return code.ExpireTime, nil
}

// GetCodeExpiryByEmail 根据邮箱获取最新验证码的过期时间
// 参数：
//   - ctx: 上下文
//   - email: 邮箱地址
//
// 返回：
//   - expired: 是否已过期或不存在
//   - expireTime: 过期时间（毫秒时间戳），仅当 expired=false 时有效
//   - error: 错误信息
func (s *TokenService) GetCodeExpiryByEmail(ctx context.Context, email string) (bool, int64, error) {
	// 参数验证
	if email == "" {
		return true, 0, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	repo := models.NewCodeRepository()

	now := time.Now().UnixMilli()
	expireTime, err := repo.GetLatestExpiryByEmail(ctx, normalizedEmail, now)
	if err != nil {
		return true, 0, err
	}

	if expireTime == 0 {
		// 没有找到有效验证码
		return true, 0, nil
	}

	return false, expireTime, nil
}

// CleanupExpired 清理过期数据
// 所有清理操作并行执行，提高效率
// 参数：
//   - ctx: 上下文
func (s *TokenService) CleanupExpired(ctx context.Context) {
	var wg sync.WaitGroup
	now := time.Now().UnixMilli()

	tokenRepo := models.NewTokenRepository()
	codeRepo := models.NewCodeRepository()

	// 清理过期 Token（异步）
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "TokenCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		count, err := tokenRepo.DeleteExpired(ctx, now)
		if err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup expired tokens", err)
			return
		}
		if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired tokens", count))
		}
	}()

	// 清理过期验证码（异步）
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "CodeCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		count, err := codeRepo.DeleteExpired(ctx, now)
		if err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup expired codes", err)
			return
		}
		if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired codes", count))
		}
	}()

	// 清理过期 OAuth 授权码（异步）
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "OAuthAuthCodeCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		repo := models.NewOAuthAuthCodeRepository()
		if count, err := repo.DeleteExpired(ctx); err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup OAuth auth codes", err)
		} else if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired OAuth auth codes", count))
		}
	}()

	// 清理过期 OAuth Access Token（异步）
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "OAuthAccessTokenCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		repo := models.NewOAuthAccessTokenRepository()
		if count, err := repo.DeleteExpired(ctx); err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup OAuth access tokens", err)
		} else if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired OAuth access tokens", count))
		}
	}()

	// 清理过期 OAuth Refresh Token（异步）
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "OAuthRefreshTokenCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		repo := models.NewOAuthRefreshTokenRepository()
		if count, err := repo.DeleteExpired(ctx); err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup OAuth refresh tokens", err)
		} else if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired OAuth refresh tokens", count))
		}
	}()

	// 等待所有清理任务完成
	wg.Wait()
}

// GetTokenExpiry 获取 Token 过期时间配置
// 返回：
//   - time.Duration: Token 过期时间
func (s *TokenService) GetTokenExpiry() time.Duration {
	return tokenExpiry
}
