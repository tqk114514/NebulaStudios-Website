package services

import (
	"context"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/models"

	"github.com/gin-gonic/gin"
)

// SessionManager Session 服务接口
type SessionManager interface {
	GenerateTokens(uid string, banned bool) (accessToken string, refreshToken string, err error)
	RefreshTokens(refreshToken string) (newAccessToken string, newRefreshToken string, err error)
	RevokeUserTokens(uid string) error
	RevokeTokenFamily(uid string, familyID string) error
	VerifyToken(tokenString string) (*Claims, error)
}

// TokenManager Token 服务接口
type TokenManager interface {
	CreateToken(ctx context.Context, email, tokenType string) (string, int64, error)
	ValidateAndUseToken(ctx context.Context, tokenStr string) (*TokenResult, error)
	VerifyCode(ctx context.Context, codeStr, email, expectedType string) (*CodeResult, error)
	IsCodeVerified(ctx context.Context, codeStr, email string) (bool, error)
	UseCode(ctx context.Context, codeStr, email string) error
	InvalidateCodeByEmail(ctx context.Context, email string, tokenType *string) error
	GetCodeExpiry(ctx context.Context, codeStr, email string) (int64, error)
	GetCodeExpiryByEmail(ctx context.Context, email string) (bool, int64, error)
	CleanupExpired(ctx context.Context)
	GetTokenExpiry() time.Duration
}

// CaptchaVerifier 验证码服务接口
type CaptchaVerifier interface {
	Verify(token, captchaType, remoteIP string) error
	VerifyWithContext(ctx context.Context, token, captchaType, remoteIP string) error
	IsEnabled() bool
	GetConfig() []CaptchaConfig
	GetProviderCount() int
	HasProvider(captchaType string) bool
}

// EmailSender 邮件发送服务接口
type EmailSender interface {
	VerifyConnection() error
	SendVerificationEmailAsync(to, emailType, language, verifyURL, logContext string)
	SendVerificationEmail(to, emailType, language, verifyURL string) error
	IsConfigured() bool
	Close()
}

// OAuthClientManager OAuth 客户端管理接口
type OAuthClientManager interface {
	CreateClient(ctx context.Context, name, description, redirectURI string) (*models.OAuthClient, string, error)
	ValidateClient(ctx context.Context, clientID, clientSecret string) (*models.OAuthClient, error)
	ValidateClientID(ctx context.Context, clientID string) (*models.OAuthClient, error)
	ValidateRedirectURI(client *models.OAuthClient, redirectURI string) bool
	RegenerateSecret(ctx context.Context, id int64) (string, error)
	GetClient(ctx context.Context, id int64) (*models.OAuthClient, error)
	GetClientByClientID(ctx context.Context, clientID string) (*models.OAuthClient, error)
	GetClients(ctx context.Context, page, pageSize int, search string) ([]*models.OAuthClient, int64, error)
	UpdateClient(ctx context.Context, id int64, name string, description *string, redirectURI string) error
	ToggleClient(ctx context.Context, id int64, enabled bool) error
	DeleteClient(ctx context.Context, id int64) error
	CreateAuthorizationCode(ctx context.Context, clientID string, userUID string, redirectURI, scope, codeChallenge, codeChallengeMethod string) (string, error)
	ExchangeAuthorizationCode(ctx context.Context, code, clientID, redirectURI, codeVerifier string) (*OAuthTokenResponse, string, error)
	RefreshAccessToken(ctx context.Context, refreshToken, clientID string) (*OAuthTokenResponse, string, error)
	ValidateAccessToken(ctx context.Context, accessToken string) (*models.OAuthAccessToken, error)
	RevokeToken(ctx context.Context, token string) error
	RevokeUserClientTokens(ctx context.Context, userUID string, clientID string) error
	RevokeUserTokens(ctx context.Context, userUID string) error
	GetUserGrants(ctx context.Context, userUID string) ([]*models.OAuthGrantWithClient, error)
	RevokeClientTokens(ctx context.Context, clientID string) error
}

// WebSocketManager WebSocket 服务接口
type WebSocketManager interface {
	HandleQRLogin(c *gin.Context)
	NotifyStatusChange(token, status string, data map[string]string)
	GetConnectionCount() int
	IsShutdown() bool
	Shutdown(ctx context.Context)
	GetStats() map[string]any
}

// ImageProcessor 图像处理服务接口
type ImageProcessor interface {
	ToWebP(imageData []byte) ([]byte, error)
	IsAvailable() bool
	Shutdown(ctx context.Context)
}

// StorageService 对象存储服务接口
type StorageService interface {
	UploadAvatar(ctx context.Context, userUID string, avatarData []byte) (string, error)
	DeleteAvatar(ctx context.Context, userUID string) error
	IsConfigured() bool
	GetImgProcessor() ImageProcessor
}

// ExportManager 数据导出服务接口
type ExportManager interface {
	GenerateOTAC() (requestID, code string, expiresAt time.Time)
	ValidateOTAC(requestID, code string) error
	RevokeOTAC()
	StoreFile(data []byte, filename string) string
	RetrieveFile(token string) ([]byte, string, error)
}

// ExportTokenManager 数据导出 Token 管理接口
type ExportTokenManager interface {
	Generate(userUID string) (string, error)
	ValidateAndConsume(token string) (string, bool)
	Stop()
}

// UserCacheStore 用户缓存接口
type UserCacheStore interface {
	Get(uid string) (*models.User, bool)
	Set(uid string, user *models.User)
	GetOrLoad(ctx context.Context, uid string, loader func(context.Context, string) (*models.User, error)) (*models.User, error)
	Invalidate(uid string)
	InvalidateAll()
	Stats() cache.CacheStats
	Len() int
	ResetStats()
}
