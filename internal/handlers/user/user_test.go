package user

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/testutil"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// userTestDeps 测试依赖集合
type userTestDeps struct {
	userRepo    *testutil.FakeUserRepo
	consents    *testutil.FakeUserConsentStore
	tokenMgr    *testutil.FakeTokenManager
	captcha     *testutil.FakeCaptcha
	emailSender *testutil.FakeEmailSender
	storage     *testutil.FakeStorageService
	oauthGrants *testutil.FakeOAuthGrants
}

func newTestUserHandler(t *testing.T) (*UserHandler, *userTestDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	deps := &userTestDeps{
		userRepo:    testutil.NewFakeUserRepo(),
		consents:    &testutil.FakeUserConsentStore{},
		tokenMgr:    &testutil.FakeTokenManager{},
		captcha:     &testutil.FakeCaptcha{},
		emailSender: &testutil.FakeEmailSender{},
		storage:     &testutil.FakeStorageService{Configured: true},
		oauthGrants: &testutil.FakeOAuthGrants{},
	}

	h, err := NewUserHandler(
		deps.userRepo,
		&testutil.FakeUserLogStore{},
		deps.consents,
		deps.tokenMgr,
		deps.emailSender,
		deps.captcha,
		&testutil.FakeUserCache{},
		deps.storage,
		deps.oauthGrants,
		&testutil.FakeLimiter{EmailAllowed: true},
		&testutil.FakeExportToken{},
		"https://test.local",
		"https://test.local/default.png",
	)
	if err != nil {
		t.Fatalf("NewUserHandler() error = %v", err)
	}
	return h, deps
}

// postUserJSON 以登录用户（uid-1）身份发起 JSON 请求
func postUserJSON(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	r := gin.New()
	r.POST("/test", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-1")
		h(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.3.31:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// seedUser 预置一个已注册用户（带真实密码哈希）
// 哈希结果按密码缓存，避免每个测试重复跑 argon2（~400ms/次）
var cachedPasswordHashes = map[string]string{}

func seedUser(deps *userTestDeps, t *testing.T, password string) *models.User {
	t.Helper()
	hash, ok := cachedPasswordHashes[password]
	if !ok {
		var err error
		hash, err = utils.HashPassword(password)
		if err != nil {
			t.Fatalf("HashPassword error = %v", err)
		}
		cachedPasswordHashes[password] = hash
	}
	user := &models.User{
		UID:      "uid-1",
		Username: "alice",
		Email:    "alice@example.com",
		Password: hash,
	}
	deps.userRepo.Seed(user)
	return user
}

func TestUpdateUsernameSuccess(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")

	w := postUserJSON(h.UpdateUsername, `{"username":"alice2","captchaToken":"captcha-ok"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"username":"alice2"`) {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUpdateUsernameConflict(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")
	// 他人已占用 alice2
	deps.userRepo.Seed(&models.User{UID: "uid-other", Username: "alice2", Email: "other@example.com"})

	w := postUserJSON(h.UpdateUsername, `{"username":"alice2","captchaToken":"captcha-ok"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "USERNAME_ALREADY_EXISTS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUpdateUsernameCaptchaFailed(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")
	deps.captcha.VerifyErr = errors.New("captcha invalid")

	w := postUserJSON(h.UpdateUsername, `{"username":"alice2","captchaToken":"bad"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CAPTCHA_FAILED") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUpdateUsernameUnauthorized(t *testing.T) {
	h, _ := newTestUserHandler(t)
	// 无登录态（不设置 ContextKeyUID）
	r := gin.New()
	r.POST("/test", h.UpdateUsername)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"username":"alice2"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestUpdateUsernameInvalidBody(t *testing.T) {
	h, _ := newTestUserHandler(t)
	w := postUserJSON(h.UpdateUsername, `{bad json`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_REQUEST") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUpdateUsernameTooLong(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")

	w := postUserJSON(h.UpdateUsername, `{"username":"一二三四五六七八九十一二三四五六","captchaToken":"captcha-ok"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "USERNAME_TOO_LONG") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUpdateAvatarSuccess(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")

	w := postUserJSON(h.UpdateAvatar, `{"avatar_url":"https://8.8.8.8/photo.jpg"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"avatar_url":"https://8.8.8.8/photo.jpg"`) {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUpdateAvatarRemove(t *testing.T) {
	h, deps := newTestUserHandler(t)
	user := seedUser(deps, t, "Abcdef1!@#ghijklmn")
	user.AvatarURL = "microsoft" // 正在使用微软头像

	w := postUserJSON(h.UpdateAvatar, `{"avatar_url":""}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"avatar_url":"https://test.local/default.png"`) {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUpdateAvatarInvalidURL(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")

	w := postUserJSON(h.UpdateAvatar, `{"avatar_url":"https://10.0.0.1/photo.jpg"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestSendDeleteCodeSuccess(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")

	w := postUserJSON(h.SendDeleteCode, `{"captchaToken":"captcha-ok","language":"zh-CN"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	// 邮件已异步发送
	if len(deps.emailSender.SentEmails) != 1 || deps.emailSender.SentEmails[0] != "alice@example.com" {
		t.Errorf("delete code email should be sent to alice@example.com, got %v", deps.emailSender.SentEmails)
	}
}

func TestSendDeleteCodeCaptchaFailed(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")
	deps.captcha.VerifyErr = errors.New("captcha invalid")

	w := postUserJSON(h.SendDeleteCode, `{"captchaToken":"bad"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CAPTCHA_FAILED") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestDeleteAccountSuccess(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")

	w := postUserJSON(h.DeleteAccount, `{"code":"A1b2C3","password":"Abcdef1!@#ghijklmn"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	// OAuth token 已撤销 + 头像已删（幂等路径）
	if len(deps.oauthGrants.RevokedUser) != 1 || deps.oauthGrants.RevokedUser[0] != "uid-1" {
		t.Errorf("oauth tokens should be revoked for uid-1, got %v", deps.oauthGrants.RevokedUser)
	}
}

func TestDeleteAccountWrongPassword(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")

	w := postUserJSON(h.DeleteAccount, `{"code":"A1b2C3","password":"Wrong1!@#password"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "WRONG_PASSWORD") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestDeleteAccountCodeInvalid(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")
	deps.tokenMgr.VerifyCodeErr = models.ErrInvalidCode

	w := postUserJSON(h.DeleteAccount, `{"code":"bad","password":"Abcdef1!@#ghijklmn"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_CODE") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestDeleteAccountMissingParams(t *testing.T) {
	h, deps := newTestUserHandler(t)
	seedUser(deps, t, "Abcdef1!@#ghijklmn")

	w := postUserJSON(h.DeleteAccount, `{"code":"","password":""}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "MISSING_PARAMETERS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

// TestDownloadUserDataComplete 验证数据导出覆盖用户本人的全部个人数据类别、
// 产物为可独立解析的合法 JSON，且不泄露验证凭据
func TestDownloadUserDataComplete(t *testing.T) {
	h, deps := newTestUserHandler(t)
	// FakeExportToken.ValidateAndConsume 固定返回 "uid"
	deps.userRepo.Seed(&models.User{
		UID:                 "uid",
		Username:            "alice",
		Email:               "alice@example.com",
		Password:            "argon2-hash-must-not-leak",
		AvatarURL:           "https://cdn.example.com/a.webp",
		GoogleID:            sql.NullString{String: "goog-1", Valid: true},
		GoogleName:          sql.NullString{String: "Alice G", Valid: true},
		GoogleAvatarURL:     sql.NullString{String: "https://lh3.googleusercontent.com/a", Valid: true},
		MicrosoftAvatarHash: sql.NullString{String: "sha256-of-image", Valid: true},
		MicrosoftAvatarSync: true,
		TOTPEnabled:         true,
		TOTPSecret:          sql.NullString{String: "JBSWY3DPEHPK3PXP", Valid: true},
	})
	deps.consents.Consents = []*models.UserConsent{
		{UserUID: "uid", PolicyType: models.PolicyTypePrivacy, PolicyVersion: "2026-09-01.md"},
	}

	r := gin.New()
	r.GET("/api/user/export/:token", h.DownloadUserData)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/user/export/export-token", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("导出产物不是合法 JSON: %v\n%s", err, w.Body.String())
	}
	for _, key := range []string{"policy_consents", "oauth_grants", "operation_logs"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("导出缺少 %q", key)
		}
	}

	userInfo, ok := payload["user_info"].(map[string]any)
	if !ok {
		t.Fatalf("user_info 缺失或非对象: %s", w.Body.String())
	}
	for _, key := range []string{
		"username", "email", "avatar_url", "role",
		"microsoft_id", "microsoft_name", "microsoft_avatar_url", "microsoft_avatar_sync",
		"google_id", "google_name", "google_avatar_url",
		"totp_enabled", "is_banned", "ban_reason", "banned_at", "unban_at",
		"created_at", "updated_at",
	} {
		if _, ok := userInfo[key]; !ok {
			t.Errorf("user_info 缺少字段 %q", key)
		}
	}
	for _, credential := range []string{"password", "totp_secret", "microsoft_avatar_hash"} {
		if _, ok := userInfo[credential]; ok {
			t.Errorf("user_info 不得包含凭据字段 %q", credential)
		}
	}

	body := w.Body.String()
	for _, secret := range []string{"JBSWY3DPEHPK3PXP", "argon2-hash-must-not-leak", "sha256-of-image"} {
		if strings.Contains(body, secret) {
			t.Errorf("导出泄露凭据值 %q", secret)
		}
	}
}
