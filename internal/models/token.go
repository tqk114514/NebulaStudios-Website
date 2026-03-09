/**
 * internal/models/token.go
 * Token 和验证码模型及数据访问层
 *
 * 功能：
 * - Token 数据结构和操作
 * - 验证码 (Code) 数据结构和操作
 * - 过期数据清理
 *
 * 依赖：
 * - PostgreSQL 数据库连接池
 */

package models

import (
	"auth-system/internal/utils"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ====================  错误定义 ====================

var (
	// ErrInvalidToken Token 无效
	ErrInvalidToken = errors.New("INVALID_TOKEN")
	// ErrTokenExpired Token 已过期
	ErrTokenExpired = errors.New("TOKEN_EXPIRED")
	// ErrTokenUsed Token 已使用
	ErrTokenUsed = errors.New("TOKEN_USED")
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
)

// ====================  常量定义 ====================

const (
	// maxCodeAttempts 验证码最大尝试次数
	maxCodeAttempts = 5

	// tokenUsed Token 已使用标记
	tokenUsed = 1

	// codeVerified 验证码已验证标记
	codeVerified = 1
)

// ====================  数据结构 ====================

// Token 验证 Token
type Token struct {
	ID         int64     `json:"id"`
	Token      string    `json:"-"` // 不序列化
	Email      string    `json:"email"`
	Type       string    `json:"type"`
	Code       *string   `json:"-"` // 关联的验证码
	CreatedAt  int64     `json:"created_at"`
	ExpireTime int64     `json:"expire_time"`
	Used       int       `json:"used"`
}

// Code 验证码
type Code struct {
	ID         int64   `json:"id"`
	Code       string  `json:"-"` // 不序列化
	Email      string  `json:"email"`
	Type       string  `json:"type"`
	CreatedAt  int64   `json:"created_at"`
	ExpireTime int64   `json:"expire_time"`
	Attempts   int     `json:"attempts"`
	Verified   int     `json:"verified"`
	VerifiedAt *int64  `json:"verified_at,omitempty"`
}

// ====================  Repository 结构 ====================

// TokenRepository Token 仓库
type TokenRepository struct{}

// CodeRepository 验证码仓库
type CodeRepository struct{}

// ====================  构造函数 ====================

// NewTokenRepository 创建 Token 仓库
func NewTokenRepository() *TokenRepository {
	return &TokenRepository{}
}

// NewCodeRepository 创建验证码仓库
func NewCodeRepository() *CodeRepository {
	return &CodeRepository{}
}

// ====================  Token 方法 ====================

// IsExpired 检查 Token 是否已过期
func (t *Token) IsExpired() bool {
	return t != nil && time.Now().UnixMilli() > t.ExpireTime
}

// IsUsed 检查 Token 是否已使用
func (t *Token) IsUsed() bool {
	return t != nil && t.Used == tokenUsed
}

// ====================  Code 方法 ====================

// IsExpired 检查验证码是否已过期
func (c *Code) IsExpired() bool {
	return c != nil && time.Now().UnixMilli() > c.ExpireTime
}

// IsVerified 检查验证码是否已验证
func (c *Code) IsVerified() bool {
	return c != nil && c.Verified == codeVerified
}

// ====================  TokenRepository 方法 ====================

// Create 创建 Token
// 参数：
//   - ctx: 上下文
//   - token: Token 对象
//
// 返回：
//   - error: 错误信息
func (r *TokenRepository) Create(ctx context.Context, token *Token) error {
	if token == nil {
		return errors.New("token object is nil")
	}
	if token.Email == "" {
		return errors.New("email is empty")
	}
	if token.Token == "" {
		return errors.New("token is empty")
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO tokens (token, email, type, created_at, expire_time, used)
		VALUES ($1, $2, $3, $4, $5, 0)
	`, token.Token, token.Email, token.Type, token.CreatedAt, token.ExpireTime)

	if err != nil {
		return utils.LogError("TOKEN", "Create", err,
			fmt.Sprintf("email=%s, type=%s", token.Email, token.Type))
	}

	utils.LogInfo("TOKEN", fmt.Sprintf("Token created: email=%s, type=%s", token.Email, token.Type))
	return nil
}

// FindByToken 根据 Token 字符串查找
// 参数：
//   - ctx: 上下文
//   - tokenStr: Token 字符串
//
// 返回：
//   - *Token: Token 对象
//   - error: 错误信息
func (r *TokenRepository) FindByToken(ctx context.Context, tokenStr string) (*Token, error) {
	if tokenStr == "" {
		return nil, ErrInvalidToken
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	token := &Token{}
	err := pool.QueryRow(ctx, `
		SELECT email, type, code, expire_time, used FROM tokens WHERE token = $1
	`, strings.TrimSpace(tokenStr)).Scan(&token.Email, &token.Type, &token.Code, &token.ExpireTime, &token.Used)

	if err != nil {
		return nil, utils.HandleDatabaseError("TOKEN", "FindByToken", err, tokenStr)
	}

	token.Token = tokenStr
	return token, nil
}

// UpdateCode 更新 Token 的验证码
// 参数：
//   - ctx: 上下文
//   - tokenStr: Token 字符串
//   - code: 验证码
//
// 返回：
//   - error: 错误信息
func (r *TokenRepository) UpdateCode(ctx context.Context, tokenStr, code string) error {
	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, "UPDATE tokens SET code = $1 WHERE token = $2", code, tokenStr)
	if err != nil {
		utils.LogWarn("TOKEN", "Failed to update token code", err)
	}
	return err
}

// MarkUsed 标记 Token 为已使用
// 参数：
//   - ctx: 上下文
//   - tokenStr: Token 字符串
//
// 返回：
//   - error: 错误信息
func (r *TokenRepository) MarkUsed(ctx context.Context, tokenStr string) error {
	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, "UPDATE tokens SET used = 1 WHERE token = $1", tokenStr)
	if err != nil {
		utils.LogWarn("TOKEN", "Failed to mark token as used", err)
	}
	return err
}

// DeleteExpired 删除过期的 Token
// 参数：
//   - ctx: 上下文
//   - now: 当前时间（毫秒时间戳）
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *TokenRepository) DeleteExpired(ctx context.Context, now int64) (int64, error) {
	if pool == nil {
		return 0, errors.New("database not ready")
	}

	result, err := pool.Exec(ctx, "DELETE FROM tokens WHERE expire_time < $1", now)
	if err != nil {
		return 0, utils.LogError("TOKEN", "DeleteExpired", err)
	}

	return result.RowsAffected(), nil
}

// DeleteByToken 删除指定 Token
// 参数：
//   - ctx: 上下文
//   - tokenStr: Token 字符串
//
// 返回：
//   - error: 错误信息
func (r *TokenRepository) DeleteByToken(ctx context.Context, tokenStr string) error {
	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, "DELETE FROM tokens WHERE token = $1", tokenStr)
	if err != nil {
		utils.LogWarn("TOKEN", "Failed to delete token", err)
	}
	return err
}

// ====================  CodeRepository 方法 ====================

// Create 创建验证码
// 参数：
//   - ctx: 上下文
//   - code: 验证码对象
//
// 返回：
//   - error: 错误信息
func (r *CodeRepository) Create(ctx context.Context, code *Code) error {
	if code == nil {
		return errors.New("code object is nil")
	}
	if code.Email == "" {
		return errors.New("email is empty")
	}
	if code.Code == "" {
		return errors.New("code is empty")
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO codes (code, email, type, created_at, expire_time, attempts, verified)
		VALUES ($1, $2, $3, $4, $5, 0, 0)
	`, code.Code, code.Email, code.Type, code.CreatedAt, code.ExpireTime)

	if err != nil {
		utils.LogWarn("TOKEN", "Failed to create code record", err)
	}
	return err
}

// FindByCode 根据验证码查找
// 参数：
//   - ctx: 上下文
//   - codeStr: 验证码字符串
//
// 返回：
//   - *Code: 验证码对象
//   - error: 错误信息
func (r *CodeRepository) FindByCode(ctx context.Context, codeStr string) (*Code, error) {
	if codeStr == "" {
		return nil, ErrInvalidCode
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	code := &Code{}
	err := pool.QueryRow(ctx, `
		SELECT email, type, expire_time, attempts, verified FROM codes WHERE code = $1
	`, strings.TrimSpace(codeStr)).Scan(&code.Email, &code.Type, &code.ExpireTime, &code.Attempts, &code.Verified)

	if err != nil {
		return nil, utils.HandleDatabaseError("TOKEN", "FindByCode", err, codeStr)
	}

	code.Code = codeStr
	return code, nil
}

// UpdateVerification 更新验证状态
// 参数：
//   - ctx: 上下文
//   - codeStr: 验证码字符串
//   - attempts: 新的尝试次数
//   - verifiedAt: 验证时间（毫秒时间戳）
//
// 返回：
//   - error: 错误信息
func (r *CodeRepository) UpdateVerification(ctx context.Context, codeStr string, attempts int, verifiedAt int64) error {
	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, `
		UPDATE codes SET attempts = $1, verified = 1, verified_at = $2 WHERE code = $3
	`, attempts, verifiedAt, codeStr)

	if err != nil {
		utils.LogWarn("TOKEN", "Failed to update code verification status", err)
	}
	return err
}

// DeleteByCode 删除指定验证码
// 参数：
//   - ctx: 上下文
//   - codeStr: 验证码字符串
//
// 返回：
//   - error: 错误信息
func (r *CodeRepository) DeleteByCode(ctx context.Context, codeStr string) error {
	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, "DELETE FROM codes WHERE code = $1", codeStr)
	if err != nil {
		utils.LogWarn("TOKEN", "Failed to delete code", err)
	}
	return err
}

// DeleteByEmail 删除指定邮箱的验证码
// 参数：
//   - ctx: 上下文
//   - email: 邮箱地址
//   - tokenType: Token 类型（可为空表示所有类型）
//
// 返回：
//   - error: 错误信息
func (r *CodeRepository) DeleteByEmail(ctx context.Context, email string, tokenType *string) error {
	if email == "" {
		return nil
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	var err error
	if tokenType != nil && *tokenType != "" {
		_, err = pool.Exec(ctx, "DELETE FROM codes WHERE email = $1 AND type = $2", email, *tokenType)
	} else {
		_, err = pool.Exec(ctx, "DELETE FROM codes WHERE email = $1", email)
	}

	if err != nil {
		return utils.LogError("TOKEN", "DeleteByEmail", err, fmt.Sprintf("email=%s", email))
	}

	utils.LogInfo("TOKEN", fmt.Sprintf("Codes deleted: email=%s", email))
	return nil
}

// GetLatestExpiryByEmail 获取指定邮箱最新验证码的过期时间
// 参数：
//   - ctx: 上下文
//   - email: 邮箱地址
//   - now: 当前时间（毫秒时间戳）
//
// 返回：
//   - int64: 过期时间（毫秒时间戳），0 表示没有有效验证码
//   - error: 错误信息
func (r *CodeRepository) GetLatestExpiryByEmail(ctx context.Context, email string, now int64) (int64, error) {
	if email == "" {
		return 0, errors.New("email is empty")
	}

	if pool == nil {
		return 0, errors.New("database not ready")
	}

	var expireTime int64
	err := pool.QueryRow(ctx, `
		SELECT expire_time FROM codes 
		WHERE email = $1 AND expire_time > $2 
		ORDER BY expire_time DESC LIMIT 1
	`, email, now).Scan(&expireTime)

	if err != nil {
		// 没有找到有效验证码
		return 0, nil
	}

	return expireTime, nil
}

// DeleteExpired 删除过期的验证码
// 参数：
//   - ctx: 上下文
//   - now: 当前时间（毫秒时间戳）
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *CodeRepository) DeleteExpired(ctx context.Context, now int64) (int64, error) {
	if pool == nil {
		return 0, errors.New("database not ready")
	}

	result, err := pool.Exec(ctx, "DELETE FROM codes WHERE expire_time < $1", now)
	if err != nil {
		return 0, utils.LogError("TOKEN", "DeleteExpired", err)
	}

	return result.RowsAffected(), nil
}
