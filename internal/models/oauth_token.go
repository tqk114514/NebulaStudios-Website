/**
 * internal/models/oauth_token.go
 * OAuth Token 相关模型和数据访问层
 *
 * 功能：
 * - 授权码 (AuthCode) 数据结构和操作
 * - Access Token 数据结构和操作
 * - Refresh Token 数据结构和操作
 * - 用户授权记录 (Grant) 数据结构和操作
 * - 过期 Token 清理
 *
 * 依赖：
 * - PostgreSQL 数据库连接池
 */

package models

import (
	"auth-system/internal/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ====================  错误定义 ====================

var (
	// ErrOAuthCodeNotFound 授权码未找到
	ErrOAuthCodeNotFound = errors.New("OAUTH_CODE_NOT_FOUND")
	// ErrOAuthCodeExpired 授权码已过期
	ErrOAuthCodeExpired = errors.New("OAUTH_CODE_EXPIRED")
	// ErrOAuthCodeUsed 授权码已使用
	ErrOAuthCodeUsed = errors.New("OAUTH_CODE_USED")
	// ErrOAuthTokenNotFound Token 未找到
	ErrOAuthTokenNotFound = errors.New("OAUTH_TOKEN_NOT_FOUND")
	// ErrOAuthTokenExpired Token 已过期
	ErrOAuthTokenExpired = errors.New("OAUTH_TOKEN_EXPIRED")
	// ErrOAuthGrantNotFound 授权记录未找到
	ErrOAuthGrantNotFound = errors.New("OAUTH_GRANT_NOT_FOUND")
	// ErrOAuthTokenRepoDBNotReady 数据库未就绪
	ErrOAuthTokenRepoDBNotReady = errors.New("database not ready")
)

// ====================  数据结构 ====================

// OAuthAuthCode 授权码
type OAuthAuthCode struct {
	ID          int64     `json:"id"`
	Code        string    `json:"-"` // 不序列化
	ClientID    string    `json:"client_id"`
	UserUID     string    `json:"user_uid"`
	RedirectURI string    `json:"redirect_uri"`
	Scope       string    `json:"scope"`
	ExpiresAt   time.Time `json:"expires_at"`
	Used        bool      `json:"used"`
	CreatedAt   time.Time `json:"created_at"`
}

// OAuthAccessToken 访问令牌
type OAuthAccessToken struct {
	ID        int64     `json:"id"`
	TokenHash string    `json:"-"` // 不序列化
	ClientID  string    `json:"client_id"`
	UserUID   string    `json:"user_uid"`
	Scope     string    `json:"scope"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// OAuthRefreshToken 刷新令牌
type OAuthRefreshToken struct {
	ID            int64     `json:"id"`
	TokenHash     string    `json:"-"` // 不序列化
	ClientID      string    `json:"client_id"`
	UserUID       string    `json:"user_uid"`
	Scope         string    `json:"scope"`
	ExpiresAt     time.Time `json:"expires_at"`
	AccessTokenID int64     `json:"access_token_id"`
	CreatedAt     time.Time `json:"created_at"`
}

// OAuthGrant 用户授权记录（用于用户管理已授权的应用）
type OAuthGrant struct {
	ID        int64     `json:"id"`
	UserUID   string    `json:"user_uid"`
	ClientID  string    `json:"client_id"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OAuthGrantWithClient 带客户端信息的授权记录（用于用户查看已授权应用）
type OAuthGrantWithClient struct {
	OAuthGrant
	ClientName        string `json:"client_name"`
	ClientDescription string `json:"client_description"`
}

// ====================  Repository 结构 ====================

// OAuthAuthCodeRepository 授权码仓库
type OAuthAuthCodeRepository struct{}

// OAuthAccessTokenRepository Access Token 仓库
type OAuthAccessTokenRepository struct{}

// OAuthRefreshTokenRepository Refresh Token 仓库
type OAuthRefreshTokenRepository struct{}

// OAuthGrantRepository 授权记录仓库
type OAuthGrantRepository struct{}

// ====================  构造函数 ====================

// NewOAuthAuthCodeRepository 创建授权码仓库
func NewOAuthAuthCodeRepository() *OAuthAuthCodeRepository {
	return &OAuthAuthCodeRepository{}
}

// NewOAuthAccessTokenRepository 创建 Access Token 仓库
func NewOAuthAccessTokenRepository() *OAuthAccessTokenRepository {
	return &OAuthAccessTokenRepository{}
}

// NewOAuthRefreshTokenRepository 创建 Refresh Token 仓库
func NewOAuthRefreshTokenRepository() *OAuthRefreshTokenRepository {
	return &OAuthRefreshTokenRepository{}
}

// NewOAuthGrantRepository 创建授权记录仓库
func NewOAuthGrantRepository() *OAuthGrantRepository {
	return &OAuthGrantRepository{}
}

// ====================  OAuthAuthCode 方法 ====================

// IsExpired 检查授权码是否已过期
func (c *OAuthAuthCode) IsExpired() bool {
	return c != nil && time.Now().After(c.ExpiresAt)
}

// IsValid 检查授权码是否有效（未过期且未使用）
func (c *OAuthAuthCode) IsValid() bool {
	return c != nil && !c.Used && !c.IsExpired()
}

// ====================  OAuthAccessToken 方法 ====================

// IsExpired 检查 Access Token 是否已过期
func (t *OAuthAccessToken) IsExpired() bool {
	return t != nil && time.Now().After(t.ExpiresAt)
}

// ====================  OAuthRefreshToken 方法 ====================

// IsExpired 检查 Refresh Token 是否已过期
func (t *OAuthRefreshToken) IsExpired() bool {
	return t != nil && time.Now().After(t.ExpiresAt)
}

// ====================  OAuthAuthCodeRepository 方法 ====================

// Create 创建授权码
// 参数：
//   - ctx: 上下文
//   - code: 授权码对象
//
// 返回：
//   - error: 错误信息
func (r *OAuthAuthCodeRepository) Create(ctx context.Context, code *OAuthAuthCode) error {
	if code == nil {
		return fmt.Errorf("code object is nil")
	}

	if err := r.checkDB(); err != nil {
		return err
	}

	err := pool.QueryRow(ctx, `
		INSERT INTO oauth_auth_codes (code, client_id, user_uid, redirect_uri, scope, expires_at, used)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`, code.Code, code.ClientID, code.UserUID, code.RedirectURI, code.Scope, code.ExpiresAt, code.Used).Scan(
		&code.ID, &code.CreatedAt,
	)

	if err != nil {
		return utils.LogError("OAUTH_CODE", "Create", err, fmt.Sprintf("client_id=%s, user_uid=%s", code.ClientID, code.UserUID))
	}

	utils.LogInfo("OAUTH_CODE", fmt.Sprintf("Auth code created: id=%d, client_id=%s, user_uid=%s", code.ID, code.ClientID, code.UserUID))
	return nil
}

// FindByCode 根据授权码查找
// 参数：
//   - ctx: 上下文
//   - code: 授权码字符串
//
// 返回：
//   - *OAuthAuthCode: 授权码对象
//   - error: 错误信息
func (r *OAuthAuthCodeRepository) FindByCode(ctx context.Context, code string) (*OAuthAuthCode, error) {
	if code == "" {
		return nil, fmt.Errorf("code is empty")
	}

	if err := r.checkDB(); err != nil {
		return nil, err
	}

	authCode := &OAuthAuthCode{}
	err := pool.QueryRow(ctx, `
		SELECT id, code, client_id, user_uid, redirect_uri, scope, expires_at, used, created_at
		FROM oauth_auth_codes WHERE code = $1
	`, code).Scan(
		&authCode.ID, &authCode.Code, &authCode.ClientID, &authCode.UserUID,
		&authCode.RedirectURI, &authCode.Scope, &authCode.ExpiresAt, &authCode.Used, &authCode.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set" {
			return nil, ErrOAuthCodeNotFound
		}
		return nil, utils.LogError("OAUTH_CODE", "FindByCode", err, fmt.Sprintf("code=%s", code))
	}

	return authCode, nil
}

// MarkUsed 标记授权码为已使用
// 参数：
//   - ctx: 上下文
//   - id: 授权码 ID
//
// 返回：
//   - error: 错误信息
func (r *OAuthAuthCodeRepository) MarkUsed(ctx context.Context, id int64) error {
	if err := r.checkDB(); err != nil {
		return err
	}

	result, err := pool.Exec(ctx, `
		UPDATE oauth_auth_codes SET used = true WHERE id = $1
	`, id)

	if err != nil {
		return utils.LogError("OAUTH_CODE", "MarkUsed", err, fmt.Sprintf("id=%d", id))
	}

	if result.RowsAffected() == 0 {
		return ErrOAuthCodeNotFound
	}

	utils.LogInfo("OAUTH_CODE", fmt.Sprintf("Auth code marked as used: id=%d", id))
	return nil
}

// DeleteExpired 删除过期的授权码
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthAuthCodeRepository) DeleteExpired(ctx context.Context) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, `
		DELETE FROM oauth_auth_codes WHERE expires_at < NOW() OR used = true
	`)

	if err != nil {
		return 0, utils.LogError("OAUTH_CODE", "DeleteExpired", err)
	}

	count := result.RowsAffected()
	if count > 0 {
		utils.LogInfo("OAUTH_CODE", fmt.Sprintf("Deleted %d expired/used auth codes", count))
	}
	return count, nil
}

func (r *OAuthAuthCodeRepository) checkDB() error {
	if pool == nil {
		utils.LogError("OAUTH_CODE", "checkDB", ErrOAuthTokenRepoDBNotReady)
		return ErrOAuthTokenRepoDBNotReady
	}
	return nil
}

// ====================  OAuthAccessTokenRepository 方法 ====================

// Create 创建 Access Token
// 参数：
//   - ctx: 上下文
//   - token: Access Token 对象
//
// 返回：
//   - error: 错误信息
func (r *OAuthAccessTokenRepository) Create(ctx context.Context, token *OAuthAccessToken) error {
	if token == nil {
		return fmt.Errorf("token object is nil")
	}

	if err := r.checkDB(); err != nil {
		return err
	}

	err := pool.QueryRow(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, client_id, user_uid, scope, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, token.TokenHash, token.ClientID, token.UserUID, token.Scope, token.ExpiresAt).Scan(
		&token.ID, &token.CreatedAt,
	)

	if err != nil {
		return utils.LogError("OAUTH_ACCESS_TOKEN", "Create", err, fmt.Sprintf("client_id=%s, user_uid=%s", token.ClientID, token.UserUID))
	}

	utils.LogInfo("OAUTH_ACCESS_TOKEN", fmt.Sprintf("Access token created: id=%d, client_id=%s, user_uid=%s", token.ID, token.ClientID, token.UserUID))
	return nil
}

// FindByTokenHash 根据 Token Hash 查找
// 参数：
//   - ctx: 上下文
//   - tokenHash: Token 的 SHA-256 哈希
//
// 返回：
//   - *OAuthAccessToken: Access Token 对象
//   - error: 错误信息
func (r *OAuthAccessTokenRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*OAuthAccessToken, error) {
	if tokenHash == "" {
		return nil, fmt.Errorf("token hash is empty")
	}

	if err := r.checkDB(); err != nil {
		return nil, err
	}

	token := &OAuthAccessToken{}
	err := pool.QueryRow(ctx, `
		SELECT id, token_hash, client_id, user_uid, scope, expires_at, created_at
		FROM oauth_access_tokens WHERE token_hash = $1
	`, tokenHash).Scan(
		&token.ID, &token.TokenHash, &token.ClientID, &token.UserUID,
		&token.Scope, &token.ExpiresAt, &token.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set" {
			return nil, ErrOAuthTokenNotFound
		}
		return nil, utils.LogError("OAUTH_ACCESS_TOKEN", "FindByTokenHash", err)
	}

	return token, nil
}

// Delete 删除 Access Token
// 参数：
//   - ctx: 上下文
//   - id: Token ID
//
// 返回：
//   - error: 错误信息
func (r *OAuthAccessTokenRepository) Delete(ctx context.Context, id int64) error {
	if err := r.checkDB(); err != nil {
		return err
	}

	_, err := pool.Exec(ctx, "DELETE FROM oauth_access_tokens WHERE id = $1", id)
	if err != nil {
		return utils.LogError("OAUTH_ACCESS_TOKEN", "Delete", err, fmt.Sprintf("id=%d", id))
	}

	utils.LogInfo("OAUTH_ACCESS_TOKEN", fmt.Sprintf("Access token deleted: id=%d", id))
	return nil
}

// DeleteByTokenHash 根据 Token Hash 删除
// 参数：
//   - ctx: 上下文
//   - tokenHash: Token 的 SHA-256 哈希
//
// 返回：
//   - error: 错误信息
func (r *OAuthAccessTokenRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	if err := r.checkDB(); err != nil {
		return err
	}

	_, err := pool.Exec(ctx, "DELETE FROM oauth_access_tokens WHERE token_hash = $1", tokenHash)
	if err != nil {
		return utils.LogError("OAUTH_ACCESS_TOKEN", "DeleteByTokenHash", err)
	}

	return nil
}

// DeleteByUserAndClient 删除指定用户和客户端的所有 Access Token
// 参数：
//   - ctx: 上下文
//   - userUID: 用户 UID
//   - clientID: 客户端 ID
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthAccessTokenRepository) DeleteByUserAndClient(ctx context.Context, userUID string, clientID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, `
		DELETE FROM oauth_access_tokens WHERE user_uid = $1 AND client_id = $2
	`, userUID, clientID)

	if err != nil {
		return 0, utils.LogError("OAUTH_ACCESS_TOKEN", "DeleteByUserAndClient", err, fmt.Sprintf("user_uid=%s, client_id=%s", userUID, clientID))
	}

	count := result.RowsAffected()
	utils.LogInfo("OAUTH_ACCESS_TOKEN", fmt.Sprintf("Deleted %d access tokens for user_uid=%s, client_id=%s", count, userUID, clientID))
	return count, nil
}

// DeleteByUser 删除指定用户的所有 Access Token
// 参数：
//   - ctx: 上下文
//   - userUID: 用户 UID
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthAccessTokenRepository) DeleteByUser(ctx context.Context, userUID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_access_tokens WHERE user_uid = $1", userUID)
	if err != nil {
		return 0, utils.LogError("OAUTH_ACCESS_TOKEN", "DeleteByUser", err, fmt.Sprintf("user_uid=%s", userUID))
	}

	count := result.RowsAffected()
	utils.LogInfo("OAUTH_ACCESS_TOKEN", fmt.Sprintf("Deleted %d access tokens for user_uid=%s", count, userUID))
	return count, nil
}

// DeleteExpired 删除过期的 Access Token
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthAccessTokenRepository) DeleteExpired(ctx context.Context) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_access_tokens WHERE expires_at < NOW()")
	if err != nil {
		return 0, utils.LogError("OAUTH_ACCESS_TOKEN", "DeleteExpired", err)
	}

	count := result.RowsAffected()
	if count > 0 {
		utils.LogInfo("OAUTH_ACCESS_TOKEN", fmt.Sprintf("Deleted %d expired access tokens", count))
	}
	return count, nil
}

// DeleteByClient 删除指定客户端的所有 Access Token
// 参数：
//   - ctx: 上下文
//   - clientID: 客户端 ID
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthAccessTokenRepository) DeleteByClient(ctx context.Context, clientID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_access_tokens WHERE client_id = $1", clientID)
	if err != nil {
		return 0, utils.LogError("OAUTH_ACCESS_TOKEN", "DeleteByClient", err, fmt.Sprintf("client_id=%s", clientID))
	}

	count := result.RowsAffected()
	utils.LogInfo("OAUTH_ACCESS_TOKEN", fmt.Sprintf("Deleted %d access tokens for client_id=%s", count, clientID))
	return count, nil
}

func (r *OAuthAccessTokenRepository) checkDB() error {
	if pool == nil {
		utils.LogError("OAUTH_ACCESS_TOKEN", "checkDB", ErrOAuthTokenRepoDBNotReady)
		return ErrOAuthTokenRepoDBNotReady
	}
	return nil
}

// ====================  OAuthRefreshTokenRepository 方法 ====================

// Create 创建 Refresh Token
// 参数：
//   - ctx: 上下文
//   - token: Refresh Token 对象
//
// 返回：
//   - error: 错误信息
func (r *OAuthRefreshTokenRepository) Create(ctx context.Context, token *OAuthRefreshToken) error {
	if token == nil {
		return fmt.Errorf("token object is nil")
	}

	if err := r.checkDB(); err != nil {
		return err
	}

	var accessTokenID any
	if token.AccessTokenID > 0 {
		accessTokenID = token.AccessTokenID
	}

	err := pool.QueryRow(ctx, `
		INSERT INTO oauth_refresh_tokens (token_hash, client_id, user_uid, scope, expires_at, access_token_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, token.TokenHash, token.ClientID, token.UserUID, token.Scope, token.ExpiresAt, accessTokenID).Scan(
		&token.ID, &token.CreatedAt,
	)

	if err != nil {
		return utils.LogError("OAUTH_REFRESH_TOKEN", "Create", err, fmt.Sprintf("client_id=%s, user_uid=%s", token.ClientID, token.UserUID))
	}

	utils.LogInfo("OAUTH_REFRESH_TOKEN", fmt.Sprintf("Refresh token created: id=%d, client_id=%s, user_uid=%s", token.ID, token.ClientID, token.UserUID))
	return nil
}

// FindByTokenHash 根据 Token Hash 查找
// 参数：
//   - ctx: 上下文
//   - tokenHash: Token 的 SHA-256 哈希
//
// 返回：
//   - *OAuthRefreshToken: Refresh Token 对象
//   - error: 错误信息
func (r *OAuthRefreshTokenRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*OAuthRefreshToken, error) {
	if tokenHash == "" {
		return nil, fmt.Errorf("token hash is empty")
	}

	if err := r.checkDB(); err != nil {
		return nil, err
	}

	token := &OAuthRefreshToken{}
	var accessTokenID sql.NullInt64
	err := pool.QueryRow(ctx, `
		SELECT id, token_hash, client_id, user_uid, scope, expires_at, access_token_id, created_at
		FROM oauth_refresh_tokens WHERE token_hash = $1
	`, tokenHash).Scan(
		&token.ID, &token.TokenHash, &token.ClientID, &token.UserUID,
		&token.Scope, &token.ExpiresAt, &accessTokenID, &token.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set" {
			return nil, ErrOAuthTokenNotFound
		}
		return nil, utils.LogError("OAUTH_REFRESH_TOKEN", "FindByTokenHash", err)
	}

	if accessTokenID.Valid {
		token.AccessTokenID = accessTokenID.Int64
	}

	return token, nil
}

// Delete 删除 Refresh Token
// 参数：
//   - ctx: 上下文
//   - id: Token ID
//
// 返回：
//   - error: 错误信息
func (r *OAuthRefreshTokenRepository) Delete(ctx context.Context, id int64) error {
	if err := r.checkDB(); err != nil {
		return err
	}

	_, err := pool.Exec(ctx, "DELETE FROM oauth_refresh_tokens WHERE id = $1", id)
	if err != nil {
		return utils.LogError("OAUTH_REFRESH_TOKEN", "Delete", err, fmt.Sprintf("id=%d", id))
	}

	utils.LogInfo("OAUTH_REFRESH_TOKEN", fmt.Sprintf("Refresh token deleted: id=%d", id))
	return nil
}

// DeleteByTokenHash 根据 Token Hash 删除
// 参数：
//   - ctx: 上下文
//   - tokenHash: Token 的 SHA-256 哈希
//
// 返回：
//   - error: 错误信息
func (r *OAuthRefreshTokenRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	if err := r.checkDB(); err != nil {
		return err
	}

	_, err := pool.Exec(ctx, "DELETE FROM oauth_refresh_tokens WHERE token_hash = $1", tokenHash)
	if err != nil {
		return utils.LogError("OAUTH_REFRESH_TOKEN", "DeleteByTokenHash", err)
	}

	return nil
}

// DeleteByUserAndClient 删除指定用户和客户端的所有 Refresh Token
// 参数：
//   - ctx: 上下文
//   - userUID: 用户 UID
//   - clientID: 客户端 ID
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthRefreshTokenRepository) DeleteByUserAndClient(ctx context.Context, userUID string, clientID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, `
		DELETE FROM oauth_refresh_tokens WHERE user_uid = $1 AND client_id = $2
	`, userUID, clientID)

	if err != nil {
		return 0, utils.LogError("OAUTH_REFRESH_TOKEN", "DeleteByUserAndClient", err, fmt.Sprintf("user_uid=%s, client_id=%s", userUID, clientID))
	}

	count := result.RowsAffected()
	utils.LogInfo("OAUTH_REFRESH_TOKEN", fmt.Sprintf("Deleted %d refresh tokens for user_uid=%s, client_id=%s", count, userUID, clientID))
	return count, nil
}

// DeleteByUser 删除指定用户的所有 Refresh Token
// 参数：
//   - ctx: 上下文
//   - userUID: 用户 UID
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthRefreshTokenRepository) DeleteByUser(ctx context.Context, userUID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_refresh_tokens WHERE user_uid = $1", userUID)
	if err != nil {
		return 0, utils.LogError("OAUTH_REFRESH_TOKEN", "DeleteByUser", err, fmt.Sprintf("user_uid=%s", userUID))
	}

	count := result.RowsAffected()
	utils.LogInfo("OAUTH_REFRESH_TOKEN", fmt.Sprintf("Deleted %d refresh tokens for user_uid=%s", count, userUID))
	return count, nil
}

// DeleteExpired 删除过期的 Refresh Token
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthRefreshTokenRepository) DeleteExpired(ctx context.Context) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_refresh_tokens WHERE expires_at < NOW()")
	if err != nil {
		return 0, utils.LogError("OAUTH_REFRESH_TOKEN", "DeleteExpired", err)
	}

	count := result.RowsAffected()
	if count > 0 {
		utils.LogInfo("OAUTH_REFRESH_TOKEN", fmt.Sprintf("Deleted %d expired refresh tokens", count))
	}
	return count, nil
}

// DeleteByClient 删除指定客户端的所有 Refresh Token
// 参数：
//   - ctx: 上下文
//   - clientID: 客户端 ID
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthRefreshTokenRepository) DeleteByClient(ctx context.Context, clientID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_refresh_tokens WHERE client_id = $1", clientID)
	if err != nil {
		return 0, utils.LogError("OAUTH_REFRESH_TOKEN", "DeleteByClient", err, fmt.Sprintf("client_id=%s", clientID))
	}

	count := result.RowsAffected()
	utils.LogInfo("OAUTH_REFRESH_TOKEN", fmt.Sprintf("Deleted %d refresh tokens for client_id=%s", count, clientID))
	return count, nil
}

func (r *OAuthRefreshTokenRepository) checkDB() error {
	if pool == nil {
		utils.LogError("OAUTH_REFRESH_TOKEN", "checkDB", ErrOAuthTokenRepoDBNotReady)
		return ErrOAuthTokenRepoDBNotReady
	}
	return nil
}

// ====================  OAuthGrantRepository 方法 ====================

// CreateOrUpdate 创建或更新授权记录
// 参数：
//   - ctx: 上下文
//   - grant: 授权记录对象
//
// 返回：
//   - error: 错误信息
func (r *OAuthGrantRepository) CreateOrUpdate(ctx context.Context, grant *OAuthGrant) error {
	if grant == nil {
		return fmt.Errorf("grant object is nil")
	}

	if err := r.checkDB(); err != nil {
		return err
	}

	// 使用 UPSERT（INSERT ... ON CONFLICT）
	err := pool.QueryRow(ctx, `
		INSERT INTO oauth_grants (user_uid, client_id, scope)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_uid, client_id) DO UPDATE SET
			scope = EXCLUDED.scope,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, created_at, updated_at
	`, grant.UserUID, grant.ClientID, grant.Scope).Scan(
		&grant.ID, &grant.CreatedAt, &grant.UpdatedAt,
	)

	if err != nil {
		return utils.LogError("OAUTH_GRANT", "CreateOrUpdate", err, fmt.Sprintf("user_uid=%s, client_id=%s", grant.UserUID, grant.ClientID))
	}

	utils.LogInfo("OAUTH_GRANT", fmt.Sprintf("Grant created/updated: id=%d, user_uid=%s, client_id=%s", grant.ID, grant.UserUID, grant.ClientID))
	return nil
}

// FindByUserUID 查找用户的所有授权记录（带客户端信息）
// 参数：
//   - ctx: 上下文
//   - userUID: 用户 UID
//
// 返回：
//   - []*OAuthGrantWithClient: 授权记录列表
//   - error: 错误信息
func (r *OAuthGrantRepository) FindByUserUID(ctx context.Context, userUID string) ([]*OAuthGrantWithClient, error) {
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, `
		SELECT g.id, g.user_uid, g.client_id, g.scope, g.created_at, g.updated_at,
		       c.name, COALESCE(c.description, '')
		FROM oauth_grants g
		JOIN oauth_clients c ON g.client_id = c.client_id
		WHERE g.user_uid = $1
		ORDER BY g.updated_at DESC
	`, userUID)

	if err != nil {
		return nil, utils.LogError("OAUTH_GRANT", "FindByUserUID", err, fmt.Sprintf("user_uid=%s", userUID))
	}
	defer rows.Close()

	grants := make([]*OAuthGrantWithClient, 0)
	for rows.Next() {
		grant := &OAuthGrantWithClient{}
		err := rows.Scan(
			&grant.ID, &grant.UserUID, &grant.ClientID, &grant.Scope,
			&grant.CreatedAt, &grant.UpdatedAt,
			&grant.ClientName, &grant.ClientDescription,
		)
		if err != nil {
			utils.LogError("OAUTH_GRANT", "FindByUserID.Scan", err)
			continue
		}
		grants = append(grants, grant)
	}

	return grants, nil
}

// FindByUserAndClient 查找指定用户和客户端的授权记录
// 参数：
//   - ctx: 上下文
//   - userUID: 用户 UID
//   - clientID: 客户端 ID
//
// 返回：
//   - *OAuthGrant: 授权记录
//   - error: 错误信息
func (r *OAuthGrantRepository) FindByUserAndClient(ctx context.Context, userUID string, clientID string) (*OAuthGrant, error) {
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	grant := &OAuthGrant{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_uid, client_id, scope, created_at, updated_at
		FROM oauth_grants WHERE user_uid = $1 AND client_id = $2
	`, userUID, clientID).Scan(
		&grant.ID, &grant.UserUID, &grant.ClientID, &grant.Scope,
		&grant.CreatedAt, &grant.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set" {
			return nil, ErrOAuthGrantNotFound
		}
		return nil, utils.LogError("OAUTH_GRANT", "FindByUserAndClient", err, fmt.Sprintf("user_uid=%s, client_id=%s", userUID, clientID))
	}

	return grant, nil
}

// Delete 删除授权记录
// 参数：
//   - ctx: 上下文
//   - userUID: 用户 UID
//   - clientID: 客户端 ID
//
// 返回：
//   - error: 错误信息
func (r *OAuthGrantRepository) Delete(ctx context.Context, userUID string, clientID string) error {
	if err := r.checkDB(); err != nil {
		return err
	}

	result, err := pool.Exec(ctx, `
		DELETE FROM oauth_grants WHERE user_uid = $1 AND client_id = $2
	`, userUID, clientID)

	if err != nil {
		return utils.LogError("OAUTH_GRANT", "Delete", err, fmt.Sprintf("user_uid=%s, client_id=%s", userUID, clientID))
	}

	if result.RowsAffected() == 0 {
		return ErrOAuthGrantNotFound
	}

	utils.LogInfo("OAUTH_GRANT", fmt.Sprintf("Grant deleted: user_uid=%s, client_id=%s", userUID, clientID))
	return nil
}

// DeleteByUser 删除用户的所有授权记录
// 参数：
//   - ctx: 上下文
//   - userUID: 用户 UID
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthGrantRepository) DeleteByUser(ctx context.Context, userUID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_grants WHERE user_uid = $1", userUID)
	if err != nil {
		return 0, utils.LogError("OAUTH_GRANT", "DeleteByUser", err, fmt.Sprintf("user_uid=%s", userUID))
	}

	count := result.RowsAffected()
	utils.LogInfo("OAUTH_GRANT", fmt.Sprintf("Deleted %d grants for user_uid=%s", count, userUID))
	return count, nil
}

// DeleteByClient 删除客户端的所有授权记录
// 参数：
//   - ctx: 上下文
//   - clientID: 客户端 ID
//
// 返回：
//   - int64: 删除的数量
//   - error: 错误信息
func (r *OAuthGrantRepository) DeleteByClient(ctx context.Context, clientID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_grants WHERE client_id = $1", clientID)
	if err != nil {
		return 0, utils.LogError("OAUTH_GRANT", "DeleteByClient", err, fmt.Sprintf("client_id=%s", clientID))
	}

	count := result.RowsAffected()
	utils.LogInfo("OAUTH_GRANT", fmt.Sprintf("Deleted %d grants for client_id=%s", count, clientID))
	return count, nil
}

func (r *OAuthGrantRepository) checkDB() error {
	if pool == nil {
		utils.LogError("OAUTH_GRANT", "checkDB", ErrOAuthTokenRepoDBNotReady)
		return ErrOAuthTokenRepoDBNotReady
	}
	return nil
}
