package models

import (
	"auth-system/internal/utils"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidToken    = errors.New("INVALID_TOKEN")
	ErrTokenExpired    = errors.New("TOKEN_EXPIRED")
	ErrTokenUsed       = errors.New("TOKEN_USED")
	ErrInvalidCode     = errors.New("INVALID_CODE")
	ErrCodeExpired     = errors.New("CODE_EXPIRED")
	ErrEmailMismatch   = errors.New("EMAIL_MISMATCH")
	ErrTypeMismatch    = errors.New("TYPE_MISMATCH")
	ErrTooManyAttempts = errors.New("TOO_MANY_ATTEMPTS")
	ErrCodeNotVerified = errors.New("CODE_NOT_VERIFIED")
)

const (
	maxCodeAttempts = 5
	tokenUsed       = 1
	codeVerified    = 1
)

// Token 验证 Token
type Token struct {
	ID         int64   `json:"id"`
	Token      string  `json:"-"` // 不序列化
	Email      string  `json:"email"`
	Type       string  `json:"type"`
	Code       *string `json:"-"` // 关联的验证码
	CreatedAt  int64   `json:"created_at"`
	ExpireTime int64   `json:"expire_time"`
	Used       int     `json:"used"`
}

// Code 验证码
type Code struct {
	ID         int64  `json:"id"`
	Code       string `json:"-"` // 不序列化
	Email      string `json:"email"`
	Type       string `json:"type"`
	CreatedAt  int64  `json:"created_at"`
	ExpireTime int64  `json:"expire_time"`
	Attempts   int    `json:"attempts"`
	Verified   int    `json:"verified"`
	VerifiedAt *int64 `json:"verified_at,omitempty"`
}

// TokenRepository Token 仓库
type TokenRepository struct {
	pool *pgxpool.Pool
}

// CodeRepository 验证码仓库
type CodeRepository struct {
	pool *pgxpool.Pool
}

// NewTokenRepository 创建 Token 仓库
func NewTokenRepository(pool *pgxpool.Pool) *TokenRepository {
	return &TokenRepository{pool: pool}
}

// NewCodeRepository 创建验证码仓库
func NewCodeRepository(pool *pgxpool.Pool) *CodeRepository {
	return &CodeRepository{pool: pool}
}

// IsExpired 检查 Token 是否已过期
func (t *Token) IsExpired() bool {
	return t != nil && time.Now().UnixMilli() > t.ExpireTime
}

// IsUsed 检查 Token 是否已使用
func (t *Token) IsUsed() bool {
	return t != nil && t.Used == tokenUsed
}

// IsExpired 检查验证码是否已过期
func (c *Code) IsExpired() bool {
	return c != nil && time.Now().UnixMilli() > c.ExpireTime
}

// IsVerified 检查验证码是否已验证
func (c *Code) IsVerified() bool {
	return c != nil && c.Verified == codeVerified
}

// Create 创建 Token
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

	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, `
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
func (r *TokenRepository) FindByToken(ctx context.Context, tokenStr string) (*Token, error) {
	if tokenStr == "" {
		return nil, ErrInvalidToken
	}

	if r.pool == nil {
		return nil, errors.New("database not ready")
	}

	token := &Token{}
	err := r.pool.QueryRow(ctx, `
		SELECT email, type, code, expire_time, used FROM tokens WHERE token = $1
	`, strings.TrimSpace(tokenStr)).Scan(&token.Email, &token.Type, &token.Code, &token.ExpireTime, &token.Used)

	if err != nil {
		return nil, utils.HandleDatabaseError("TOKEN", "FindByToken", err, tokenStr)
	}

	token.Token = tokenStr
	return token, nil
}

// UpdateCode 更新 Token 的验证码
func (r *TokenRepository) UpdateCode(ctx context.Context, tokenStr, code string) error {
	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, "UPDATE tokens SET code = $1 WHERE token = $2", code, tokenStr)
	if err != nil {
		utils.LogWarn("TOKEN", "Failed to update token code", err)
	}
	return err
}

// MarkUsed 标记 Token 为已使用
func (r *TokenRepository) MarkUsed(ctx context.Context, tokenStr string) error {
	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, "UPDATE tokens SET used = 1 WHERE token = $1", tokenStr)
	if err != nil {
		utils.LogWarn("TOKEN", "Failed to mark token as used", err)
	}
	return err
}

// MarkUsedAndGet 原子性地标记 Token 为已使用并返回 Token 数据
// 使用单条 UPDATE ... RETURNING 消除 SELECT → 检查 → UPDATE 之间的竞态条件
// 仅在 used=0 且未过期时成功，失败返回 nil 表示 Token 已被使用或已过期
func (r *TokenRepository) MarkUsedAndGet(ctx context.Context, tokenStr string, now int64) (*Token, error) {
	if tokenStr == "" {
		return nil, ErrInvalidToken
	}

	if r.pool == nil {
		return nil, errors.New("database not ready")
	}

	token := &Token{Token: tokenStr}
	err := r.pool.QueryRow(ctx, `
		UPDATE tokens SET used = 1
		WHERE token = $1 AND used = 0 AND expire_time > $2
		RETURNING email, type, code, expire_time
	`, tokenStr, now).Scan(&token.Email, &token.Type, &token.Code, &token.ExpireTime)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, utils.HandleDatabaseError("TOKEN", "MarkUsedAndGet", err, tokenStr)
	}

	return token, nil
}

// DeleteExpired 删除过期的 Token
func (r *TokenRepository) DeleteExpired(ctx context.Context, now int64) (int64, error) {
	if r.pool == nil {
		return 0, errors.New("database not ready")
	}

	result, err := r.pool.Exec(ctx, "DELETE FROM tokens WHERE expire_time < $1", now)
	if err != nil {
		return 0, utils.LogError("TOKEN", "DeleteExpired", err)
	}

	return result.RowsAffected(), nil
}

// DeleteByToken 删除指定 Token
func (r *TokenRepository) DeleteByToken(ctx context.Context, tokenStr string) error {
	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, "DELETE FROM tokens WHERE token = $1", tokenStr)
	if err != nil {
		utils.LogWarn("TOKEN", "Failed to delete token", err)
	}
	return err
}

// Create 创建验证码
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

	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO codes (code, email, type, created_at, expire_time, attempts, verified)
		VALUES ($1, $2, $3, $4, $5, 0, 0)
	`, code.Code, code.Email, code.Type, code.CreatedAt, code.ExpireTime)

	if err != nil {
		utils.LogWarn("TOKEN", "Failed to create code record", err)
	}
	return err
}

// FindByCode 根据验证码查找
func (r *CodeRepository) FindByCode(ctx context.Context, codeStr string) (*Code, error) {
	if codeStr == "" {
		return nil, ErrInvalidCode
	}

	if r.pool == nil {
		return nil, errors.New("database not ready")
	}

	code := &Code{}
	err := r.pool.QueryRow(ctx, `
		SELECT email, type, expire_time, attempts, verified FROM codes WHERE code = $1
	`, strings.TrimSpace(codeStr)).Scan(&code.Email, &code.Type, &code.ExpireTime, &code.Attempts, &code.Verified)

	if err != nil {
		return nil, utils.HandleDatabaseError("TOKEN", "FindByCode", err, codeStr)
	}

	code.Code = codeStr
	return code, nil
}

// UpdateVerification 更新验证状态
func (r *CodeRepository) UpdateVerification(ctx context.Context, codeStr string, attempts int, verifiedAt int64) error {
	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE codes SET attempts = $1, verified = 1, verified_at = $2 WHERE code = $3
	`, attempts, verifiedAt, codeStr)

	if err != nil {
		utils.LogWarn("TOKEN", "Failed to update code verification status", err)
	}
	return err
}

// DeleteByCode 删除指定验证码
func (r *CodeRepository) DeleteByCode(ctx context.Context, codeStr string) error {
	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, "DELETE FROM codes WHERE code = $1", codeStr)
	if err != nil {
		utils.LogWarn("TOKEN", "Failed to delete code", err)
	}
	return err
}

// DeleteByEmail 删除指定邮箱的验证码
func (r *CodeRepository) DeleteByEmail(ctx context.Context, email string, tokenType *string) error {
	if email == "" {
		return nil
	}

	if r.pool == nil {
		return errors.New("database not ready")
	}

	var err error
	if tokenType != nil && *tokenType != "" {
		_, err = r.pool.Exec(ctx, "DELETE FROM codes WHERE email = $1 AND type = $2", email, *tokenType)
	} else {
		_, err = r.pool.Exec(ctx, "DELETE FROM codes WHERE email = $1", email)
	}

	if err != nil {
		return utils.LogError("TOKEN", "DeleteByEmail", err, fmt.Sprintf("email=%s", email))
	}

	utils.LogInfo("TOKEN", fmt.Sprintf("Codes deleted: email=%s", email))
	return nil
}

// GetLatestExpiryByEmail 获取指定邮箱最新验证码的过期时间
func (r *CodeRepository) GetLatestExpiryByEmail(ctx context.Context, email string, now int64) (int64, error) {
	if email == "" {
		return 0, errors.New("email is empty")
	}

	if r.pool == nil {
		return 0, errors.New("database not ready")
	}

	var expireTime int64
	err := r.pool.QueryRow(ctx, `
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
func (r *CodeRepository) DeleteExpired(ctx context.Context, now int64) (int64, error) {
	if r.pool == nil {
		return 0, errors.New("database not ready")
	}

	result, err := r.pool.Exec(ctx, "DELETE FROM codes WHERE expire_time < $1", now)
	if err != nil {
		return 0, utils.LogError("TOKEN", "DeleteExpired", err)
	}

	return result.RowsAffected(), nil
}
