package user

import (
	"bytes"
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
		tokenMgr:    &testutil.FakeTokenManager{},
		captcha:     &testutil.FakeCaptcha{},
		emailSender: &testutil.FakeEmailSender{},
		storage:     &testutil.FakeStorageService{Configured: true},
		oauthGrants: &testutil.FakeOAuthGrants{},
	}

	h, err := NewUserHandler(
		deps.userRepo,
		&testutil.FakeUserLogStore{},
		deps.tokenMgr,
		deps.emailSender,
		deps.captcha,
		&testutil.FakeUserCache{},
		deps.storage,
		deps.oauthGrants,
		&testutil.FakeLimiter{},
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
	deps.tokenMgr.VerifyCodeErr = errors.New("INVALID_CODE")

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
