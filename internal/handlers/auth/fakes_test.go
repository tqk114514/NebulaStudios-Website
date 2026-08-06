package auth

// 本文件为 AuthHandler 测试提供 fake 依赖实现。
// 接口已按使用方拆分（UserReadWriter 等小接口），fake 只需实现被调用方法 + no-op 补齐。

import (
	"context"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ---------- fakeUserRepo: models.UserReadWriter ----------

type fakeUserRepo struct {
	emails         map[string]*models.User
	usernames      map[string]*models.User
	uids           map[string]*models.User
	createdUsers   []*models.User
	createErr      error
	findByEmailErr error
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		emails:    make(map[string]*models.User),
		usernames: make(map[string]*models.User),
		uids:      make(map[string]*models.User),
	}
}

func (f *fakeUserRepo) seed(user *models.User) {
	f.emails[user.Email] = user
	f.usernames[user.Username] = user
	f.uids[user.UID] = user
}

func (f *fakeUserRepo) FindByUID(_ context.Context, uid string) (*models.User, error) {
	return f.uids[uid], nil
}
func (f *fakeUserRepo) FindByEmail(_ context.Context, email string) (*models.User, error) {
	if f.findByEmailErr != nil {
		return nil, f.findByEmailErr
	}
	return f.emails[email], nil
}
func (f *fakeUserRepo) FindByEmailOrUsername(_ context.Context, identifier string) (*models.User, error) {
	if u := f.emails[identifier]; u != nil {
		return u, nil
	}
	if u := f.usernames[identifier]; u != nil {
		return u, nil
	}
	return nil, &utils.DatabaseError{Operation: "FindByEmailOrUsername", NotFound: true}
}
func (f *fakeUserRepo) FindByUsername(_ context.Context, username string) (*models.User, error) {
	return f.usernames[username], nil
}
func (f *fakeUserRepo) FindByMicrosoftID(context.Context, string) (*models.User, error) {
	return nil, nil
}
func (f *fakeUserRepo) FindByGoogleID(context.Context, string) (*models.User, error) { return nil, nil }
func (f *fakeUserRepo) Create(_ context.Context, user *models.User) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createdUsers = append(f.createdUsers, user)
	f.seed(user)
	return nil
}
func (f *fakeUserRepo) Update(context.Context, string, map[string]any) error { return nil }
func (f *fakeUserRepo) UpdatePassword(context.Context, string, string) error { return nil }
func (f *fakeUserRepo) Delete(context.Context, string) error                 { return nil }

// ---------- fakeTokenManager: services.TokenManager ----------

type fakeTokenManager struct {
	verifyCodeErr error
	invalidated   []string
}

func (f *fakeTokenManager) CreateToken(context.Context, string, string) (string, int64, error) {
	return "token", time.Now().Add(time.Hour).UnixMilli(), nil
}
func (f *fakeTokenManager) ValidateAndUseToken(context.Context, string) (*services.TokenResult, error) {
	return nil, nil
}
func (f *fakeTokenManager) VerifyCode(_ context.Context, code, _, _ string) (*services.CodeResult, error) {
	// 成功与否仅由 verifyCodeErr 开关控制，其余参数不参与判定
	if f.verifyCodeErr != nil {
		return nil, f.verifyCodeErr
	}
	return &services.CodeResult{Type: services.TokenTypeRegister, AlreadyVerified: false}, nil
}
func (f *fakeTokenManager) IsCodeVerified(context.Context, string, string) (bool, error) {
	return true, nil
}
func (f *fakeTokenManager) UseCode(context.Context, string, string) error { return nil }
func (f *fakeTokenManager) InvalidateCodeByEmail(_ context.Context, email string, _ *string) error {
	f.invalidated = append(f.invalidated, email)
	return nil
}
func (f *fakeTokenManager) GetCodeExpiry(context.Context, string, string) (int64, error) {
	return time.Now().Add(time.Hour).UnixMilli(), nil
}
func (f *fakeTokenManager) GetCodeExpiryByEmail(context.Context, string) (bool, int64, error) {
	return false, 0, nil
}
func (f *fakeTokenManager) CleanupExpired(context.Context) {}
func (f *fakeTokenManager) GetTokenExpiry() time.Duration  { return time.Hour }

// ---------- fakeSessionManager: services.SessionManager ----------

type fakeSessionManager struct {
	generateErr  error
	accessToken  string
	refreshToken string
}

func (f *fakeSessionManager) GenerateTokens(_ context.Context, _ string, _ bool) (string, string, error) {
	if f.generateErr != nil {
		return "", "", f.generateErr
	}
	if f.accessToken == "" {
		f.accessToken = "fake-access-token"
	}
	if f.refreshToken == "" {
		f.refreshToken = "fake-refresh-token"
	}
	return f.accessToken, f.refreshToken, nil
}
func (f *fakeSessionManager) RefreshTokens(context.Context, string) (string, string, error) {
	return "", "", nil
}
func (f *fakeSessionManager) RevokeUserTokens(context.Context, string) error          { return nil }
func (f *fakeSessionManager) RevokeTokenFamily(context.Context, string, string) error { return nil }
func (f *fakeSessionManager) VerifyToken(string) (*services.Claims, error)            { return nil, nil }

// ---------- fakeCaptcha: services.CaptchaVerifier ----------

// fakeCaptcha 模拟已启用且验证通过的验证码服务：默认放行（返回 nil），验证失败由 verifyErr 开关控制。
// 注：真实 CaptchaService.Verify 在未启用时返回 ErrCaptchaNotConfigured，此处放行仅用于验证 handler 的错误传播。
type fakeCaptcha struct {
	verifyErr error
}

func (f *fakeCaptcha) Verify(_, _ string) error                                { return f.verifyErr }
func (f *fakeCaptcha) VerifyWithContext(context.Context, string, string) error { return f.verifyErr }
func (f *fakeCaptcha) IsEnabled() bool                                         { return false }
func (f *fakeCaptcha) GetSiteKey() string                                      { return "" }

// ---------- fakeEmailSender: services.EmailSender ----------

type fakeEmailSender struct{}

func (f *fakeEmailSender) VerifyConnection() error                                           { return nil }
func (f *fakeEmailSender) SendVerificationEmailAsync(string, string, string, string, string) {}
func (f *fakeEmailSender) SendVerificationEmail(string, string, string, string) error        { return nil }
func (f *fakeEmailSender) IsConfigured() bool                                                { return false }
func (f *fakeEmailSender) Close()                                                            {}

// ---------- fakeUserLogStore: models.UserLogStore ----------

type fakeUserLogStore struct{}

func (f *fakeUserLogStore) Create(context.Context, *models.UserLog) error   { return nil }
func (f *fakeUserLogStore) LogChangePassword(context.Context, string) error { return nil }
func (f *fakeUserLogStore) LogRegister(context.Context, string) error       { return nil }
func (f *fakeUserLogStore) LogChangeUsername(context.Context, string, string, string) error {
	return nil
}
func (f *fakeUserLogStore) LogChangeAvatar(context.Context, string, string, string) error { return nil }
func (f *fakeUserLogStore) LogLinkMicrosoft(context.Context, string, string, string) error {
	return nil
}
func (f *fakeUserLogStore) LogUnlinkMicrosoft(context.Context, string, string, string) error {
	return nil
}
func (f *fakeUserLogStore) LogLinkGoogle(context.Context, string, string, string) error   { return nil }
func (f *fakeUserLogStore) LogUnlinkGoogle(context.Context, string, string, string) error { return nil }
func (f *fakeUserLogStore) LogDeleteAccount(context.Context, string) error                { return nil }
func (f *fakeUserLogStore) LogBanned(context.Context, string, string, *time.Time) error   { return nil }
func (f *fakeUserLogStore) LogUnbanned(context.Context, string) error                     { return nil }
func (f *fakeUserLogStore) LogOAuthAuthorize(context.Context, string, string, string, string) error {
	return nil
}
func (f *fakeUserLogStore) LogOAuthRevoke(context.Context, string, string, string) error { return nil }
func (f *fakeUserLogStore) FindByUserUID(context.Context, string, int, int) ([]*models.UserLog, int64, error) {
	return nil, 0, nil
}
func (f *fakeUserLogStore) DeleteByUserUID(context.Context, string) error    { return nil }
func (f *fakeUserLogStore) DeleteExpiredLogs(context.Context) (int64, error) { return 0, nil }

// ---------- fakeUserCache: services.UserCacheStore ----------

type fakeUserCache struct{}

func (f *fakeUserCache) Get(string) (*models.User, bool) { return nil, false }
func (f *fakeUserCache) Set(string, *models.User)        {}
func (f *fakeUserCache) GetOrLoad(_ context.Context, uid string, loader func(context.Context, string) (*models.User, error)) (*models.User, error) {
	return loader(context.Background(), uid)
}
func (f *fakeUserCache) Invalidate(string)       {}
func (f *fakeUserCache) InvalidateAll()          {}
func (f *fakeUserCache) Stats() cache.CacheStats { return cache.CacheStats{} }
func (f *fakeUserCache) Len() int                { return 0 }
func (f *fakeUserCache) ResetStats()             {}

// ---------- fakeUserConsentStore: models.UserConsentStore ----------

type fakeUserConsentStore struct{}

func (f *fakeUserConsentStore) Create(context.Context, *models.UserConsent) error        { return nil }
func (f *fakeUserConsentStore) LogConsent(context.Context, string, string, string) error { return nil }
func (f *fakeUserConsentStore) FindByUserUID(context.Context, string) ([]*models.UserConsent, error) {
	return nil, nil
}
func (f *fakeUserConsentStore) DeleteByUserUID(context.Context, string) error { return nil }

// ---------- fakeEmailWhitelist: models.EmailWhitelistStore ----------

type fakeEmailWhitelist struct {
	allowed  bool
	allowErr error
}

func (f *fakeEmailWhitelist) IsDomainAllowed(context.Context, string) (bool, string, error) {
	return f.allowed, "", f.allowErr
}
func (f *fakeEmailWhitelist) FindAll(context.Context) ([]*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *fakeEmailWhitelist) FindAllPaginated(context.Context, int, int) ([]*models.EmailWhitelist, int64, error) {
	return nil, 0, nil
}
func (f *fakeEmailWhitelist) FindByDomain(context.Context, string) (*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *fakeEmailWhitelist) FindByID(context.Context, int64) (*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *fakeEmailWhitelist) Create(context.Context, string, string, string) (*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *fakeEmailWhitelist) Update(context.Context, int64, string, string, string, bool) (*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *fakeEmailWhitelist) Delete(context.Context, int64) error                { return nil }
func (f *fakeEmailWhitelist) SetEnabled(context.Context, int64, bool) error      { return nil }
func (f *fakeEmailWhitelist) InitDefaultWhitelist(context.Context, string) error { return nil }

// ---------- fakeLimiter: middleware.RateLimiterManager ----------

type fakeLimiter struct{}

func noopHandler(c *gin.Context)                               { c.Next() }
func (f *fakeLimiter) LoginRateLimit() gin.HandlerFunc         { return noopHandler }
func (f *fakeLimiter) RegisterRateLimit() gin.HandlerFunc      { return noopHandler }
func (f *fakeLimiter) ResetPasswordRateLimit() gin.HandlerFunc { return noopHandler }
func (f *fakeLimiter) OAuthTokenRateLimit() gin.HandlerFunc    { return noopHandler }
func (f *fakeLimiter) VerifyCodeRateLimit() gin.HandlerFunc    { return noopHandler }
func (f *fakeLimiter) QRLoginRateLimit() gin.HandlerFunc       { return noopHandler }
func (f *fakeLimiter) EmailAllow(string) bool                  { return true }
func (f *fakeLimiter) EmailWaitTime(string) int                { return 0 }
func (f *fakeLimiter) DataExportAllow(string) bool             { return true }
func (f *fakeLimiter) DataExportWaitTime(string) int           { return 0 }
func (f *fakeLimiter) StopAll()                                {}
