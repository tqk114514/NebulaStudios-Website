// Package testutil 提供测试用的接口 fake 实现。
// 接口已按使用方拆分（UserReadWriter 等小接口），fake 只需实现被调用方法 + no-op 补齐，
// 供 handlers 各包（auth/user/admin/oauth）的单元测试共享，避免重复实现。
package testutil

import (
	"context"
	"database/sql"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ---------- FakeUserRepo: models.UserReadWriter ----------

// FakeUserRepo 内存版用户仓库，支持 seed 预置数据与错误注入
type FakeUserRepo struct {
	Emails          map[string]*models.User
	Usernames       map[string]*models.User
	UIDs            map[string]*models.User
	CreatedUsers    []*models.User
	CreateErr       error
	FindByEmailErr  error
	BanCalls        []BannedUsers
	UnbanCalls      []string
	PasswordUpdates []string
}

// NewFakeUserRepo 创建空的内存用户仓库
func NewFakeUserRepo() *FakeUserRepo {
	return &FakeUserRepo{
		Emails:    make(map[string]*models.User),
		Usernames: make(map[string]*models.User),
		UIDs:      make(map[string]*models.User),
	}
}

// Seed 预置一个用户（email/username/uid 三索引）
func (f *FakeUserRepo) Seed(user *models.User) {
	f.Emails[user.Email] = user
	f.Usernames[user.Username] = user
	f.UIDs[user.UID] = user
}

func (f *FakeUserRepo) FindByUID(_ context.Context, uid string) (*models.User, error) {
	if u := f.UIDs[uid]; u != nil {
		return u, nil
	}
	// 未找到返回 not-found 错误：handler 经 utils.IsDatabaseNotFound 识别（真实仓库返回包装后的 DatabaseError）
	return nil, sql.ErrNoRows
}
func (f *FakeUserRepo) FindByEmail(_ context.Context, email string) (*models.User, error) {
	if f.FindByEmailErr != nil {
		return nil, f.FindByEmailErr
	}
	if u := f.Emails[email]; u != nil {
		return u, nil
	}
	// 与真实仓库一致：未找到返回 DatabaseError{NotFound}（HTTPDatabaseError 据此映射 404）
	return nil, &utils.DatabaseError{Operation: "FindByEmail", NotFound: true}
}
func (f *FakeUserRepo) FindByEmailOrUsername(_ context.Context, identifier string) (*models.User, error) {
	if u := f.Emails[identifier]; u != nil {
		return u, nil
	}
	if u := f.Usernames[identifier]; u != nil {
		return u, nil
	}
	return nil, &utils.DatabaseError{Operation: "FindByEmailOrUsername", NotFound: true}
}
func (f *FakeUserRepo) FindByUsername(_ context.Context, username string) (*models.User, error) {
	return f.Usernames[username], nil
}
func (f *FakeUserRepo) FindByMicrosoftID(context.Context, string) (*models.User, error) {
	return nil, nil
}
func (f *FakeUserRepo) FindByGoogleID(context.Context, string) (*models.User, error) { return nil, nil }
func (f *FakeUserRepo) Create(_ context.Context, user *models.User) error {
	if f.CreateErr != nil {
		return f.CreateErr
	}
	f.CreatedUsers = append(f.CreatedUsers, user)
	f.Seed(user)
	return nil
}
func (f *FakeUserRepo) Update(context.Context, string, map[string]any) error { return nil }
func (f *FakeUserRepo) UpdatePassword(_ context.Context, uid, plainPassword string) error {
	f.PasswordUpdates = append(f.PasswordUpdates, uid)
	return nil
}
func (f *FakeUserRepo) Delete(context.Context, string) error { return nil }

// ---- UserAdminStore（管理后台用，FakeUserRepo 同时满足 models.UserStore） ----

// BannedUsers 记录 Ban/Unban 调用，供断言
type BannedUsers struct {
	UserUID  string
	AdminUID string
	Reason   string
	UnbanAt  *time.Time
}

var _ models.UserAdminStore = (*FakeUserRepo)(nil)

func (f *FakeUserRepo) FindAll(context.Context, int, int, string) ([]*models.User, int64, error) {
	users := make([]*models.User, 0, len(f.UIDs))
	for _, u := range f.UIDs {
		users = append(users, u)
	}
	return users, int64(len(users)), nil
}
func (f *FakeUserRepo) GetStats(context.Context) (*models.UserStats, error) {
	return &models.UserStats{TotalUsers: int64(len(f.UIDs))}, nil
}
func (f *FakeUserRepo) Ban(_ context.Context, userUID, adminUID string, reason string, unbanAt *time.Time) error {
	f.BanCalls = append(f.BanCalls, BannedUsers{UserUID: userUID, AdminUID: adminUID, Reason: reason, UnbanAt: unbanAt})
	if u := f.UIDs[userUID]; u != nil {
		u.IsBanned = true
		if unbanAt != nil {
			u.UnbanAt = sql.NullTime{Valid: true, Time: *unbanAt}
		}
	}
	return nil
}
func (f *FakeUserRepo) Unban(_ context.Context, userUID string) error {
	f.UnbanCalls = append(f.UnbanCalls, userUID)
	if u := f.UIDs[userUID]; u != nil {
		u.IsBanned = false
	}
	return nil
}

// ---------- FakeTokenManager: services.TokenManager ----------

// FakeTokenManager 验证码管理器 fake，成功与否由 VerifyCodeErr 开关控制，其余参数不参与判定
type FakeTokenManager struct {
	VerifyCodeErr  error
	Invalidated    []string
	UseTokenResult *services.TokenResult
	UseTokenErr    error
}

func (f *FakeTokenManager) CreateToken(context.Context, string, string) (string, int64, error) {
	return "token", time.Now().Add(time.Hour).UnixMilli(), nil
}
func (f *FakeTokenManager) ValidateAndUseToken(context.Context, string) (*services.TokenResult, error) {
	if f.UseTokenErr != nil {
		return nil, f.UseTokenErr
	}
	return f.UseTokenResult, nil
}
func (f *FakeTokenManager) VerifyCode(_ context.Context, code, _, _ string) (*services.CodeResult, error) {
	if f.VerifyCodeErr != nil {
		return nil, f.VerifyCodeErr
	}
	return &services.CodeResult{Type: services.TokenTypeRegister, AlreadyVerified: false}, nil
}
func (f *FakeTokenManager) IsCodeVerified(context.Context, string, string) (bool, error) {
	return true, nil
}
func (f *FakeTokenManager) UseCode(context.Context, string, string) error { return nil }
func (f *FakeTokenManager) InvalidateCodeByEmail(_ context.Context, email string, _ *string) error {
	f.Invalidated = append(f.Invalidated, email)
	return nil
}
func (f *FakeTokenManager) CleanupExpired(context.Context) {}
func (f *FakeTokenManager) GetTokenExpiry() time.Duration  { return time.Hour }

// ---------- FakeSessionManager: services.SessionManager ----------

type FakeSessionManager struct {
	GenerateErr  error
	AccessToken  string
	RefreshToken string
	VerifyErr    error
	VerifyResult *services.Claims
}

func (f *FakeSessionManager) GenerateTokens(_ context.Context, _ string, _ bool) (string, string, error) {
	if f.GenerateErr != nil {
		return "", "", f.GenerateErr
	}
	if f.AccessToken == "" {
		f.AccessToken = "fake-access-token"
	}
	if f.RefreshToken == "" {
		f.RefreshToken = "fake-refresh-token"
	}
	return f.AccessToken, f.RefreshToken, nil
}
func (f *FakeSessionManager) RefreshTokens(context.Context, string) (string, string, error) {
	return "", "", nil
}
func (f *FakeSessionManager) RevokeUserTokens(context.Context, string) error          { return nil }
func (f *FakeSessionManager) RevokeTokenFamily(context.Context, string, string) error { return nil }
func (f *FakeSessionManager) VerifyToken(string) (*services.Claims, error) {
	if f.VerifyErr != nil {
		return nil, f.VerifyErr
	}
	return f.VerifyResult, nil
}

// ---------- FakeCaptcha: services.CaptchaVerifier ----------
// 模拟已启用且验证通过的验证码服务：默认放行（返回 nil），验证失败由 VerifyErr 开关控制。
// 注：真实 CaptchaService.Verify 在禁用（CAPTCHA_ENABLED=false）时放行，此处放行仅用于验证 handler 的错误传播。

type FakeCaptcha struct {
	VerifyErr error
}

func (f *FakeCaptcha) Verify(_, _ string) error                                { return f.VerifyErr }
func (f *FakeCaptcha) VerifyWithContext(context.Context, string, string) error { return f.VerifyErr }
func (f *FakeCaptcha) IsEnabled() bool                                         { return false }
func (f *FakeCaptcha) GetSiteKey() string                                      { return "" }

// ---------- FakeEmailSender: services.EmailSender ----------

// FakeEmailSender 记录异步发送请求的邮箱发送器
type FakeEmailSender struct {
	SentEmails []string
}

func (f *FakeEmailSender) VerifyConnection() error { return nil }
func (f *FakeEmailSender) SendVerificationEmailAsync(to, _, _, _, _ string) {
	f.SentEmails = append(f.SentEmails, to)
}
func (f *FakeEmailSender) SendVerificationEmail(string, string, string, string) error { return nil }
func (f *FakeEmailSender) IsConfigured() bool                                         { return false }
func (f *FakeEmailSender) Close()                                                     {}

// ---------- FakeUserLogStore: models.UserLogStore ----------

type FakeUserLogStore struct{}

func (f *FakeUserLogStore) Create(context.Context, *models.UserLog) error   { return nil }
func (f *FakeUserLogStore) LogChangePassword(context.Context, string) error { return nil }
func (f *FakeUserLogStore) LogRegister(context.Context, string) error       { return nil }
func (f *FakeUserLogStore) LogChangeUsername(context.Context, string, string, string) error {
	return nil
}
func (f *FakeUserLogStore) LogChangeAvatar(context.Context, string, string, string) error {
	return nil
}
func (f *FakeUserLogStore) LogEnableAvatarSync(context.Context, string, string) error  { return nil }
func (f *FakeUserLogStore) LogDisableAvatarSync(context.Context, string, string) error { return nil }
func (f *FakeUserLogStore) LogLinkMicrosoft(context.Context, string, string, string) error {
	return nil
}
func (f *FakeUserLogStore) LogUnlinkMicrosoft(context.Context, string, string, string) error {
	return nil
}
func (f *FakeUserLogStore) LogLinkGoogle(context.Context, string, string, string) error   { return nil }
func (f *FakeUserLogStore) LogUnlinkGoogle(context.Context, string, string, string) error { return nil }
func (f *FakeUserLogStore) LogDeleteAccount(context.Context, string) error                { return nil }
func (f *FakeUserLogStore) LogBanned(context.Context, string, string, *time.Time) error   { return nil }
func (f *FakeUserLogStore) LogUnbanned(context.Context, string) error                     { return nil }
func (f *FakeUserLogStore) LogOAuthAuthorize(context.Context, string, string, string, string) error {
	return nil
}
func (f *FakeUserLogStore) LogOAuthRevoke(context.Context, string, string, string) error { return nil }
func (f *FakeUserLogStore) FindByUserUID(context.Context, string, int, int) ([]*models.UserLog, int64, error) {
	return nil, 0, nil
}
func (f *FakeUserLogStore) DeleteByUserUID(context.Context, string) error    { return nil }
func (f *FakeUserLogStore) DeleteExpiredLogs(context.Context) (int64, error) { return 0, nil }

// ---------- FakeUserCache: services.UserCacheStore ----------

type FakeUserCache struct{}

func (f *FakeUserCache) Get(string) (*models.User, bool) { return nil, false }
func (f *FakeUserCache) Set(string, *models.User)        {}
func (f *FakeUserCache) GetOrLoad(_ context.Context, uid string, loader func(context.Context, string) (*models.User, error)) (*models.User, error) {
	return loader(context.Background(), uid)
}
func (f *FakeUserCache) Invalidate(string)       {}
func (f *FakeUserCache) InvalidateAll()          {}
func (f *FakeUserCache) Stats() cache.CacheStats { return cache.CacheStats{} }
func (f *FakeUserCache) Len() int                { return 0 }
func (f *FakeUserCache) ResetStats()             {}

// ---------- FakeUserConsentStore: models.UserConsentStore ----------

type FakeUserConsentStore struct{}

func (f *FakeUserConsentStore) Create(context.Context, *models.UserConsent) error        { return nil }
func (f *FakeUserConsentStore) LogConsent(context.Context, string, string, string) error { return nil }
func (f *FakeUserConsentStore) FindByUserUID(context.Context, string) ([]*models.UserConsent, error) {
	return nil, nil
}
func (f *FakeUserConsentStore) DeleteByUserUID(context.Context, string) error { return nil }

// ---------- FakeEmailWhitelist: models.EmailWhitelistStore ----------

type FakeEmailWhitelist struct {
	Allowed  bool
	AllowErr error
}

func (f *FakeEmailWhitelist) IsDomainAllowed(context.Context, string) (bool, string, error) {
	return f.Allowed, "", f.AllowErr
}
func (f *FakeEmailWhitelist) FindAll(context.Context) ([]*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *FakeEmailWhitelist) FindAllPaginated(context.Context, int, int) ([]*models.EmailWhitelist, int64, error) {
	return nil, 0, nil
}
func (f *FakeEmailWhitelist) FindByDomain(context.Context, string) (*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *FakeEmailWhitelist) FindByID(context.Context, int64) (*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *FakeEmailWhitelist) Create(context.Context, string, string, string) (*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *FakeEmailWhitelist) Update(context.Context, int64, string, string, string, bool) (*models.EmailWhitelist, error) {
	return nil, nil
}
func (f *FakeEmailWhitelist) Delete(context.Context, int64) error                { return nil }
func (f *FakeEmailWhitelist) SetEnabled(context.Context, int64, bool) error      { return nil }
func (f *FakeEmailWhitelist) InitDefaultWhitelist(context.Context, string) error { return nil }

// ---------- FakeLimiter: middleware.RateLimiterManager ----------

type FakeLimiter struct {
	EmailAllowed bool
	EmailWait    int
}

func noopHandler(c *gin.Context)                               { c.Next() }
func (f *FakeLimiter) LoginRateLimit() gin.HandlerFunc         { return noopHandler }
func (f *FakeLimiter) RegisterRateLimit() gin.HandlerFunc      { return noopHandler }
func (f *FakeLimiter) ResetPasswordRateLimit() gin.HandlerFunc { return noopHandler }
func (f *FakeLimiter) OAuthTokenRateLimit() gin.HandlerFunc    { return noopHandler }
func (f *FakeLimiter) VerifyCodeRateLimit() gin.HandlerFunc    { return noopHandler }
func (f *FakeLimiter) EmailAllow(string) bool                  { return f.EmailAllowed }
func (f *FakeLimiter) EmailWaitTime(string) int                { return f.EmailWait }
func (f *FakeLimiter) DataExportAllow(string) bool             { return true }
func (f *FakeLimiter) DataExportWaitTime(string) int           { return 0 }
func (f *FakeLimiter) StopAll()                                {}

// ---------- FakeStorageService: services.StorageService ----------

// FakeStorageService 头像存储 fake，Configured 控制 IsConfigured
type FakeStorageService struct {
	Configured   bool
	DeletedUsers []string
	Uploaded     []string
}

func (f *FakeStorageService) UploadAvatar(_ context.Context, userUID string, _ []byte) (string, error) {
	f.Uploaded = append(f.Uploaded, userUID)
	return "https://storage.local/avatars/" + userUID + ".webp", nil
}
func (f *FakeStorageService) DeleteAvatar(_ context.Context, userUID string) error {
	f.DeletedUsers = append(f.DeletedUsers, userUID)
	return nil
}
func (f *FakeStorageService) IsConfigured() bool                       { return f.Configured }
func (f *FakeStorageService) GetImgProcessor() services.ImageProcessor { return nil }

// ---------- FakeOAuthGrants: services.OAuthGrantManager ----------

// FakeOAuthGrants OAuth 授权管理 fake，Revoked 记录撤销调用
type FakeOAuthGrants struct {
	RevokedUserClient []string
	RevokedUser       []string
}

func (f *FakeOAuthGrants) GetUserGrants(context.Context, string) ([]*models.OAuthGrantWithClient, error) {
	return nil, nil
}
func (f *FakeOAuthGrants) GetClientByClientID(context.Context, string) (*models.OAuthClient, error) {
	return nil, nil
}
func (f *FakeOAuthGrants) FindUserGrant(context.Context, string, string) (*models.OAuthGrant, error) {
	return nil, nil
}
func (f *FakeOAuthGrants) RevokeUserClientTokens(_ context.Context, userUID, clientID string) error {
	f.RevokedUserClient = append(f.RevokedUserClient, userUID+"/"+clientID)
	return nil
}
func (f *FakeOAuthGrants) RevokeUserTokens(_ context.Context, userUID string) error {
	f.RevokedUser = append(f.RevokedUser, userUID)
	return nil
}

// ---------- FakeExportToken: services.ExportTokenManager ----------

type FakeExportToken struct{}

func (f *FakeExportToken) Generate(string) (string, error)          { return "export-token", nil }
func (f *FakeExportToken) ValidateAndConsume(string) (string, bool) { return "uid", true }
func (f *FakeExportToken) Stop()                                    {}

// ---------- FakeAdminLogStore: models.AdminLogStore ----------

type FakeAdminLogStore struct{}

func (f *FakeAdminLogStore) Create(context.Context, *models.AdminLog) error { return nil }
func (f *FakeAdminLogStore) LogSetRole(context.Context, string, string, string, int, int) error {
	return nil
}
func (f *FakeAdminLogStore) LogDeleteUser(context.Context, string, string, string, string) error {
	return nil
}
func (f *FakeAdminLogStore) LogBanUser(context.Context, string, string, string, string, *time.Time) error {
	return nil
}
func (f *FakeAdminLogStore) LogUnbanUser(context.Context, string, string, string) error { return nil }
func (f *FakeAdminLogStore) LogOAuthClientCreate(context.Context, string, int64, string, string) error {
	return nil
}
func (f *FakeAdminLogStore) LogOAuthClientUpdate(context.Context, string, int64, string, string) error {
	return nil
}
func (f *FakeAdminLogStore) LogOAuthClientDelete(context.Context, string, int64, string, string) error {
	return nil
}
func (f *FakeAdminLogStore) LogOAuthClientRegenerateSecret(context.Context, string, int64, string, string) error {
	return nil
}
func (f *FakeAdminLogStore) LogOAuthClientToggle(context.Context, string, int64, string, string, bool) error {
	return nil
}
func (f *FakeAdminLogStore) LogEmailWhitelistCreate(context.Context, string, *models.EmailWhitelist) error {
	return nil
}
func (f *FakeAdminLogStore) LogEmailWhitelistUpdate(context.Context, string, *models.EmailWhitelist) error {
	return nil
}
func (f *FakeAdminLogStore) LogEmailWhitelistDelete(context.Context, string, int64) error { return nil }
func (f *FakeAdminLogStore) LogDataExport(context.Context, string, int, int) error        { return nil }
func (f *FakeAdminLogStore) LogDataImport(context.Context, string, int, int) error        { return nil }
func (f *FakeAdminLogStore) FindAll(context.Context, int, int) ([]*models.AdminLogPublic, int64, error) {
	return nil, 0, nil
}

// ---------- FakeExportManager: services.ExportManager（OTA 文件导出） ----------

type FakeExportManager struct{}

func (f *FakeExportManager) GenerateOTAC(string) (string, string, time.Time) {
	return "req", "code", time.Now()
}
func (f *FakeExportManager) ValidateOTAC(string, string, string) error   { return nil }
func (f *FakeExportManager) RevokeOTAC()                                 {}
func (f *FakeExportManager) StoreFile([]byte, string) string             { return "file-token" }
func (f *FakeExportManager) RetrieveFile(string) ([]byte, string, error) { return nil, "", nil }

// ---------- FakeDataExportRepo: models.DataExportImportStore ----------

type FakeDataExportRepo struct{}

func (f *FakeDataExportRepo) QueryAllUsers(context.Context) ([]map[string]any, error) {
	return nil, nil
}
func (f *FakeDataExportRepo) QueryAllUserLogs(context.Context) ([]map[string]any, error) {
	return nil, nil
}
func (f *FakeDataExportRepo) ImportUsers(context.Context, []map[string]any) (models.ImportUsersResult, error) {
	return models.ImportUsersResult{}, nil
}
func (f *FakeDataExportRepo) ImportUserLogs(context.Context, []map[string]any) (int, int, error) {
	return 0, 0, nil
}
func (f *FakeDataExportRepo) ImportAllInTransaction(context.Context, []map[string]any, []map[string]any) (models.ImportUsersResult, int, int, error) {
	return models.ImportUsersResult{}, 0, 0, nil
}
func (f *FakeDataExportRepo) DeleteAllUsers(context.Context) error    { return nil }
func (f *FakeDataExportRepo) DeleteAllUserLogs(context.Context) error { return nil }

// ---------- FakeOAuthAdmin: services.OAuthAdminManager ----------

// OAuthToggleCall 记录一次 ToggleClient 调用
type OAuthToggleCall struct {
	ID      int64
	Enabled bool
}

// FakeOAuthAdmin OAuth 客户端管理 fake，记录创建/删除/切换调用
type FakeOAuthAdmin struct {
	Created []string
	Deleted []int64
	Toggled []OAuthToggleCall
	Client  *models.OAuthClient
}

func (f *FakeOAuthAdmin) GetClients(context.Context, int, int, string) ([]*models.OAuthClient, int64, error) {
	return nil, 0, nil
}
func (f *FakeOAuthAdmin) GetClient(context.Context, int64) (*models.OAuthClient, error) {
	if f.Client != nil {
		return f.Client, nil
	}
	return nil, &utils.DatabaseError{Operation: "GetClient", NotFound: true}
}
func (f *FakeOAuthAdmin) CreateClient(_ context.Context, name, _, _ string) (*models.OAuthClient, string, error) {
	f.Created = append(f.Created, name)
	return &models.OAuthClient{ID: 1, Name: name}, "generated-secret", nil
}
func (f *FakeOAuthAdmin) UpdateClient(context.Context, int64, string, *string, string) error {
	return nil
}
func (f *FakeOAuthAdmin) DeleteClient(_ context.Context, id int64) error {
	f.Deleted = append(f.Deleted, id)
	return nil
}
func (f *FakeOAuthAdmin) RegenerateSecret(context.Context, int64) (string, error) {
	return "new-secret", nil
}
func (f *FakeOAuthAdmin) ToggleClient(_ context.Context, id int64, enabled bool) error {
	f.Toggled = append(f.Toggled, OAuthToggleCall{ID: id, Enabled: enabled})
	return nil
}

// ---------- FakeOAuthProvider: services.OAuthProviderStore ----------

// FakeOAuthProvider OAuth 授权服务器 fake，支持成功/失败注入
type FakeOAuthProvider struct {
	Client          *models.OAuthClient
	ValidateErr     error
	ExchangeResp    *services.OAuthTokenResponse
	ExchangeUserUID string
	ExchangeErr     error
	RefreshResp     *services.OAuthTokenResponse
	RefreshUserUID  string
	RefreshErr      error
	AccessToken     *models.OAuthAccessToken
	AccessTokenErr  error
	Revoked         []string
}

func (f *FakeOAuthProvider) ValidateClientID(context.Context, string) (*models.OAuthClient, error) {
	if f.ValidateErr != nil {
		return nil, f.ValidateErr
	}
	return f.Client, nil
}
func (f *FakeOAuthProvider) ValidateRedirectURI(*models.OAuthClient, string) bool { return true }
func (f *FakeOAuthProvider) CreateAuthorizationCode(context.Context, string, string, string, string, string, string) (string, error) {
	return "auth-code", nil
}
func (f *FakeOAuthProvider) ValidateClient(context.Context, string, string) (*models.OAuthClient, error) {
	if f.ValidateErr != nil {
		return nil, f.ValidateErr
	}
	return f.Client, nil
}
func (f *FakeOAuthProvider) ExchangeAuthorizationCode(context.Context, string, string, string, string) (*services.OAuthTokenResponse, string, error) {
	if f.ExchangeErr != nil {
		return nil, "", f.ExchangeErr
	}
	return f.ExchangeResp, f.ExchangeUserUID, nil
}
func (f *FakeOAuthProvider) RefreshAccessToken(context.Context, string, string) (*services.OAuthTokenResponse, string, error) {
	if f.RefreshErr != nil {
		return nil, "", f.RefreshErr
	}
	return f.RefreshResp, f.RefreshUserUID, nil
}
func (f *FakeOAuthProvider) ValidateAccessToken(context.Context, string) (*models.OAuthAccessToken, error) {
	if f.AccessTokenErr != nil {
		return nil, f.AccessTokenErr
	}
	return f.AccessToken, nil
}
func (f *FakeOAuthProvider) RevokeToken(_ context.Context, token string) error {
	f.Revoked = append(f.Revoked, token)
	return nil
}
