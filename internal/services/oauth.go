/**
 * internal/services/oauth.go
 * OAuth Provider 服务
 *
 * 功能：
 * - 客户端注册和管理
 * - 客户端验证（client_id + client_secret）
 * - redirect_uri 精确匹配验证
 * - Authorization Code 生成和验证
 * - Access Token / Refresh Token 生成和验证
 * - Token 撤销
 * - 用户授权管理
 *
 * Token 有效期：
 * - Authorization Code: 10 分钟
 * - Access Token: 1 小时
 * - Refresh Token: 30 天
 *
 * 安全设计：
 * - client_secret 使用 Argon2id 哈希存储（通过 utils.HashPassword）
 * - Access Token 和 Refresh Token 使用 SHA-256 哈希存储
 * - Authorization Code 单次使用
 * - redirect_uri 必须精确匹配
 *
 * 依赖：
 * - internal/models: OAuth 模型
 * - internal/utils: 安全工具函数（HashPassword, VerifyPassword）
 */

package services

import (
	"auth-system/internal/models"
	"auth-system/internal/utils"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ====================  错误定义 ====================

var (
	// 客户端错误
	ErrOAuthInvalidClient   = errors.New("OAUTH_INVALID_CLIENT")
	ErrOAuthInvalidSecret   = errors.New("OAUTH_INVALID_SECRET")
	ErrOAuthClientDisabled  = errors.New("OAUTH_CLIENT_DISABLED")
	ErrOAuthInvalidRedirect = errors.New("OAUTH_INVALID_REDIRECT_URI")

	// Token 错误
	ErrOAuthCodeNotFound     = errors.New("OAUTH_CODE_NOT_FOUND")
	ErrOAuthCodeExpired      = errors.New("OAUTH_CODE_EXPIRED")
	ErrOAuthCodeUsed         = errors.New("OAUTH_CODE_USED")
	ErrOAuthTokenNotFound    = errors.New("OAUTH_TOKEN_NOT_FOUND")
	ErrOAuthTokenExpired     = errors.New("OAUTH_TOKEN_EXPIRED")
	ErrOAuthInvalidGrant     = errors.New("OAUTH_INVALID_GRANT")
	ErrOAuthRedirectMismatch = errors.New("OAUTH_REDIRECT_MISMATCH")
)

// ====================  常量定义 ====================

const (
	// 长度常量
	oauthClientIDLength     = 16 // 32 字符 hex
	oauthClientSecretLength = 32 // 64 字符 hex
	oauthAuthCodeLength     = 16 // 32 字符 hex
	oauthAccessTokenLength  = 32 // 64 字符 hex
	oauthRefreshTokenLength = 32 // 64 字符 hex

	// 有效期常量
	oauthAuthCodeExpiry     = 10 * time.Minute
	oauthAccessTokenExpiry  = 1 * time.Hour
	oauthRefreshTokenExpiry = 30 * 24 * time.Hour
)

// ====================  数据结构 ====================

// OAuthService OAuth 服务
type OAuthService struct {
	clientRepo       *models.OAuthClientRepository
	authCodeRepo     *models.OAuthAuthCodeRepository
	accessTokenRepo  *models.OAuthAccessTokenRepository
	refreshTokenRepo *models.OAuthRefreshTokenRepository
	grantRepo        *models.OAuthGrantRepository
}

// OAuthTokenResponse Token 响应
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

// ====================  构造函数 ====================

// NewOAuthService 创建 OAuth 服务
func NewOAuthService() *OAuthService {
	return &OAuthService{
		clientRepo:       models.NewOAuthClientRepository(),
		authCodeRepo:     models.NewOAuthAuthCodeRepository(),
		accessTokenRepo:  models.NewOAuthAccessTokenRepository(),
		refreshTokenRepo: models.NewOAuthRefreshTokenRepository(),
		grantRepo:        models.NewOAuthGrantRepository(),
	}
}

// ====================  客户端管理方法 ====================

// CreateClient 创建客户端
// 返回：客户端对象、明文 client_secret（仅此次返回）、错误
func (s *OAuthService) CreateClient(ctx context.Context, name, description, redirectURI string) (*models.OAuthClient, string, error) {
	clientID, err := s.generateRandomHex(oauthClientIDLength)
	if err != nil {
		utils.LogError("OAUTH", "CreateClient", err, "Failed to generate client_id")
		return nil, "", err
	}

	clientSecret, err := s.generateRandomHex(oauthClientSecretLength)
	if err != nil {
		utils.LogError("OAUTH", "CreateClient", err, "Failed to generate client_secret")
		return nil, "", err
	}

	secretHash, err := utils.HashPassword(clientSecret)
	if err != nil {
		utils.LogError("OAUTH", "CreateClient", err, "Failed to hash client_secret")
		return nil, "", err
	}

	client := &models.OAuthClient{
		ClientID:         clientID,
		ClientSecretHash: string(secretHash),
		Name:             name,
		Description:      description,
		RedirectURI:      redirectURI,
		IsEnabled:        true,
	}

	if err := s.clientRepo.Create(ctx, client); err != nil {
		return nil, "", err
	}

	utils.LogInfo("OAUTH", fmt.Sprintf("Client created: id=%d, name=%s", client.ID, name))
	return client, clientSecret, nil
}

// ValidateClient 验证客户端（client_id + client_secret）
func (s *OAuthService) ValidateClient(ctx context.Context, clientID, clientSecret string) (*models.OAuthClient, error) {
	client, err := s.clientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		if errors.Is(err, models.ErrOAuthClientNotFound) {
			return nil, ErrOAuthInvalidClient
		}
		return nil, err
	}

	if !client.IsEnabled {
		return nil, ErrOAuthClientDisabled
	}

	if ok, err := utils.VerifyPassword(clientSecret, client.ClientSecretHash); err != nil || !ok {
		return nil, ErrOAuthInvalidSecret
	}

	return client, nil
}

// ValidateClientID 仅验证 client_id（用于授权端点）
func (s *OAuthService) ValidateClientID(ctx context.Context, clientID string) (*models.OAuthClient, error) {
	client, err := s.clientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		if errors.Is(err, models.ErrOAuthClientNotFound) {
			return nil, ErrOAuthInvalidClient
		}
		return nil, err
	}

	if !client.IsEnabled {
		return nil, ErrOAuthClientDisabled
	}

	return client, nil
}

// ValidateRedirectURI 验证回调地址（精确匹配）
func (s *OAuthService) ValidateRedirectURI(client *models.OAuthClient, redirectURI string) bool {
	return client != nil && redirectURI != "" && client.RedirectURI == redirectURI
}

// RegenerateSecret 重新生成客户端密钥
func (s *OAuthService) RegenerateSecret(ctx context.Context, id int64) (string, error) {
	newSecret, err := s.generateRandomHex(oauthClientSecretLength)
	if err != nil {
		return "", err
	}

	secretHash, err := utils.HashPassword(newSecret)
	if err != nil {
		return "", err
	}

	if err := s.clientRepo.Update(ctx, id, map[string]any{
		"client_secret_hash": string(secretHash),
	}); err != nil {
		return "", err
	}

	utils.LogInfo("OAUTH", fmt.Sprintf("Secret regenerated: id=%d", id))
	return newSecret, nil
}

// GetClient 获取客户端详情
func (s *OAuthService) GetClient(ctx context.Context, id int64) (*models.OAuthClient, error) {
	return s.clientRepo.FindByID(ctx, id)
}

// GetClientByClientID 根据 client_id 获取客户端
func (s *OAuthService) GetClientByClientID(ctx context.Context, clientID string) (*models.OAuthClient, error) {
	return s.clientRepo.FindByClientID(ctx, clientID)
}

// GetClients 获取客户端列表
func (s *OAuthService) GetClients(ctx context.Context, page, pageSize int, search string) ([]*models.OAuthClient, int64, error) {
	return s.clientRepo.FindAll(ctx, page, pageSize, search)
}

// UpdateClient 更新客户端
func (s *OAuthService) UpdateClient(ctx context.Context, id int64, name, description, redirectURI string) error {
	updates := map[string]any{}
	if name != "" {
		updates["name"] = name
	}
	if description != "" {
		updates["description"] = description
	}
	if redirectURI != "" {
		updates["redirect_uri"] = redirectURI
	}
	if len(updates) == 0 {
		return nil
	}
	return s.clientRepo.Update(ctx, id, updates)
}

// ToggleClient 启用/禁用客户端
func (s *OAuthService) ToggleClient(ctx context.Context, id int64, enabled bool) error {
	// 如果是禁用操作，先获取 client_id 用于撤销 Token
	if !enabled {
		client, err := s.clientRepo.FindByID(ctx, id)
		if err != nil {
			return err
		}
		// 撤销该客户端的所有 Token
		_ = s.RevokeClientTokens(ctx, client.ClientID)
	}

	err := s.clientRepo.Update(ctx, id, map[string]any{"is_enabled": enabled})
	if err == nil {
		status := "enabled"
		if !enabled {
			status = "disabled"
		}
		utils.LogInfo("OAUTH", fmt.Sprintf("Client %s: id=%d", status, id))
	}
	return err
}

// DeleteClient 删除客户端
func (s *OAuthService) DeleteClient(ctx context.Context, id int64) error {
	// 先获取 client_id 用于撤销 Token
	client, err := s.clientRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	// 撤销该客户端的所有 Token
	_ = s.RevokeClientTokens(ctx, client.ClientID)

	// 删除客户端
	return s.clientRepo.Delete(ctx, id)
}

// ====================  Authorization Code 方法 ====================

// CreateAuthorizationCode 创建授权码
func (s *OAuthService) CreateAuthorizationCode(ctx context.Context, clientID string, userUID string, redirectURI, scope, codeChallenge, codeChallengeMethod string) (string, error) {
	// 强制要求 PKCE
	if codeChallenge == "" {
		return "", ErrOAuthInvalidGrant
	}
	if !utils.ValidateCodeChallenge(codeChallenge, codeChallengeMethod) {
		return "", ErrOAuthInvalidGrant
	}

	code, err := s.generateRandomHex(oauthAuthCodeLength)
	if err != nil {
		utils.LogError("OAUTH", "CreateAuthCode", err, "Failed to generate auth code")
		return "", err
	}

	authCode := &models.OAuthAuthCode{
		Code:                code,
		ClientID:            clientID,
		UserUID:             userUID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           time.Now().Add(oauthAuthCodeExpiry),
		Used:                false,
	}

	if err := s.authCodeRepo.Create(ctx, authCode); err != nil {
		return "", err
	}

	// 创建或更新用户授权记录
	grant := &models.OAuthGrant{UserUID: userUID, ClientID: clientID, Scope: scope}
	_ = s.grantRepo.CreateOrUpdate(ctx, grant)

	utils.LogInfo("OAUTH", fmt.Sprintf("Auth code created: client_id=%s, user_uid=%s", clientID, userUID))
	return code, nil
}

// ExchangeAuthorizationCode 用授权码换取 Token
func (s *OAuthService) ExchangeAuthorizationCode(ctx context.Context, code, clientID, redirectURI, codeVerifier string) (*OAuthTokenResponse, string, error) {
	authCode, err := s.authCodeRepo.FindByCode(ctx, code)
	if err != nil {
		if errors.Is(err, models.ErrOAuthCodeNotFound) {
			return nil, "", ErrOAuthCodeNotFound
		}
		return nil, "", err
	}

	if authCode.Used {
		return nil, "", ErrOAuthCodeUsed
	}

	if authCode.IsExpired() {
		return nil, "", ErrOAuthCodeExpired
	}

	if authCode.ClientID != clientID {
		return nil, "", ErrOAuthInvalidGrant
	}

	if authCode.RedirectURI != redirectURI {
		return nil, "", ErrOAuthRedirectMismatch
	}

	// 强制要求 PKCE 验证
	if codeVerifier == "" {
		return nil, "", ErrOAuthInvalidGrant
	}
	if !utils.ValidateCodeVerifier(codeVerifier) {
		return nil, "", ErrOAuthInvalidGrant
	}
	if !utils.VerifyPKCE(codeVerifier, authCode.CodeChallenge, authCode.CodeChallengeMethod) {
		return nil, "", ErrOAuthInvalidGrant
	}

	// 标记为已使用
	if err := s.authCodeRepo.MarkUsed(ctx, authCode.ID); err != nil {
		return nil, "", err
	}

	// 生成 Token
	tokenResp, err := s.createTokenPair(ctx, authCode.ClientID, authCode.UserUID, authCode.Scope)
	if err != nil {
		return nil, "", err
	}

	utils.LogInfo("OAUTH", fmt.Sprintf("Auth code exchanged: client_id=%s, user_uid=%s", clientID, authCode.UserUID))
	return tokenResp, authCode.UserUID, nil
}

// ====================  Token 方法 ====================

// RefreshAccessToken 刷新 Access Token
func (s *OAuthService) RefreshAccessToken(ctx context.Context, refreshToken, clientID string) (*OAuthTokenResponse, string, error) {
	tokenHash := s.hashToken(refreshToken)

	token, err := s.refreshTokenRepo.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, models.ErrOAuthTokenNotFound) {
			return nil, "", ErrOAuthTokenNotFound
		}
		return nil, "", err
	}

	if token.IsExpired() {
		return nil, "", ErrOAuthTokenExpired
	}

	if token.ClientID != clientID {
		return nil, "", ErrOAuthInvalidGrant
	}

	// 删除旧 Token
	_ = s.refreshTokenRepo.Delete(ctx, token.ID)
	if token.AccessTokenID > 0 {
		_ = s.accessTokenRepo.Delete(ctx, token.AccessTokenID)
	}

	// 生成新 Token
	tokenResp, err := s.createTokenPair(ctx, token.ClientID, token.UserUID, token.Scope)
	if err != nil {
		return nil, "", err
	}

	utils.LogInfo("OAUTH", fmt.Sprintf("Token refreshed: client_id=%s, user_uid=%s", clientID, token.UserUID))
	return tokenResp, token.UserUID, nil
}

// ValidateAccessToken 验证 Access Token
func (s *OAuthService) ValidateAccessToken(ctx context.Context, accessToken string) (*models.OAuthAccessToken, error) {
	tokenHash := s.hashToken(accessToken)

	token, err := s.accessTokenRepo.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, models.ErrOAuthTokenNotFound) {
			return nil, ErrOAuthTokenNotFound
		}
		return nil, err
	}

	if token.IsExpired() {
		return nil, ErrOAuthTokenExpired
	}

	return token, nil
}

// RevokeToken 撤销 Token（始终返回成功，防止探测）
func (s *OAuthService) RevokeToken(ctx context.Context, token string) error {
	tokenHash := s.hashToken(token)
	_ = s.accessTokenRepo.DeleteByTokenHash(ctx, tokenHash)
	_ = s.refreshTokenRepo.DeleteByTokenHash(ctx, tokenHash)
	return nil
}

// RevokeUserClientTokens 撤销用户对某客户端的所有授权
func (s *OAuthService) RevokeUserClientTokens(ctx context.Context, userUID string, clientID string) error {
	_, _ = s.accessTokenRepo.DeleteByUserAndClient(ctx, userUID, clientID)
	_, _ = s.refreshTokenRepo.DeleteByUserAndClient(ctx, userUID, clientID)
	_ = s.grantRepo.Delete(ctx, userUID, clientID)
	utils.LogInfo("OAUTH", fmt.Sprintf("User-client tokens revoked: user_uid=%s, client_id=%s", userUID, clientID))
	return nil
}

// RevokeUserTokens 撤销用户的所有 OAuth Token（用于封禁）
func (s *OAuthService) RevokeUserTokens(ctx context.Context, userUID string) error {
	_, _ = s.accessTokenRepo.DeleteByUser(ctx, userUID)
	_, _ = s.refreshTokenRepo.DeleteByUser(ctx, userUID)
	_, _ = s.grantRepo.DeleteByUser(ctx, userUID)
	utils.LogInfo("OAUTH", fmt.Sprintf("All user tokens revoked: user_uid=%s", userUID))
	return nil
}

// GetUserGrants 获取用户的所有授权记录
func (s *OAuthService) GetUserGrants(ctx context.Context, userUID string) ([]*models.OAuthGrantWithClient, error) {
	return s.grantRepo.FindByUserUID(ctx, userUID)
}

// RevokeClientTokens 撤销某客户端的所有 Token（用于禁用/删除客户端）
func (s *OAuthService) RevokeClientTokens(ctx context.Context, clientID string) error {
	_, _ = s.accessTokenRepo.DeleteByClient(ctx, clientID)
	_, _ = s.refreshTokenRepo.DeleteByClient(ctx, clientID)
	_, _ = s.grantRepo.DeleteByClient(ctx, clientID)
	utils.LogInfo("OAUTH", fmt.Sprintf("All client tokens revoked: client_id=%s", clientID))
	return nil
}

// ====================  私有方法 ====================

// createTokenPair 创建 Access Token 和 Refresh Token 对
func (s *OAuthService) createTokenPair(ctx context.Context, clientID string, userUID string, scope string) (*OAuthTokenResponse, error) {
	accessToken, err := s.generateRandomHex(oauthAccessTokenLength)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.generateRandomHex(oauthRefreshTokenLength)
	if err != nil {
		return nil, err
	}

	// 保存 Access Token
	accessTokenModel := &models.OAuthAccessToken{
		TokenHash: s.hashToken(accessToken),
		ClientID:  clientID,
		UserUID:   userUID,
		Scope:     scope,
		ExpiresAt: time.Now().Add(oauthAccessTokenExpiry),
	}
	if err := s.accessTokenRepo.Create(ctx, accessTokenModel); err != nil {
		return nil, err
	}

	// 保存 Refresh Token
	refreshTokenModel := &models.OAuthRefreshToken{
		TokenHash:     s.hashToken(refreshToken),
		ClientID:      clientID,
		UserUID:       userUID,
		Scope:         scope,
		ExpiresAt:     time.Now().Add(oauthRefreshTokenExpiry),
		AccessTokenID: accessTokenModel.ID,
	}
	if err := s.refreshTokenRepo.Create(ctx, refreshTokenModel); err != nil {
		_ = s.accessTokenRepo.Delete(ctx, accessTokenModel.ID)
		return nil, err
	}

	return &OAuthTokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(oauthAccessTokenExpiry.Seconds()),
		RefreshToken: refreshToken,
		Scope:        scope,
	}, nil
}

// generateRandomHex 生成随机 hex 字符串
func (s *OAuthService) generateRandomHex(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// hashToken 计算 Token 的 SHA-256 哈希
func (s *OAuthService) hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
