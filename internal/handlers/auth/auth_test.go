package auth

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/config"
	"auth-system/internal/models"
	"auth-system/internal/testutil"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// testDeps 测试依赖集合
type testDeps struct {
	userRepo    *testutil.FakeUserRepo
	tokenMgr    *testutil.FakeTokenManager
	sessionMgr  *testutil.FakeSessionManager
	captcha     *testutil.FakeCaptcha
	whitelist   *testutil.FakeEmailWhitelist
	limiter     *testutil.FakeLimiter
	emailSender *testutil.FakeEmailSender
}

func newTestAuthHandler(t *testing.T, useWhitelist bool) (*AuthHandler, *testDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	deps := &testDeps{
		userRepo:    testutil.NewFakeUserRepo(),
		tokenMgr:    &testutil.FakeTokenManager{},
		sessionMgr:  &testutil.FakeSessionManager{},
		captcha:     &testutil.FakeCaptcha{},
		limiter:     &testutil.FakeLimiter{EmailAllowed: true},
		emailSender: &testutil.FakeEmailSender{},
	}

	var whitelist models.EmailWhitelistStore
	if useWhitelist {
		deps.whitelist = &testutil.FakeEmailWhitelist{Allowed: true}
		whitelist = deps.whitelist
	}

	cfg := &config.Config{BaseURL: "https://test.local"}
	h, err := NewAuthHandler(
		cfg,
		deps.userRepo,
		&testutil.FakeUserLogStore{},
		&testutil.FakeUserConsentStore{},
		deps.tokenMgr,
		deps.sessionMgr,
		deps.emailSender,
		deps.captcha,
		&testutil.FakeUserCache{},
		whitelist,
		deps.limiter,
	)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v", err)
	}
	return h, deps
}

func postJSON(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	r := gin.New()
	r.POST("/test", h)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.3.31:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// getQuery 以 GET 方式调用 handler，用于 query 参数类端点
func getQuery(h gin.HandlerFunc, url string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, url, nil)
	h(c)
	return w
}

func validRegisterBody() string {
	return `{"username":"alice","email":"alice@example.com","password":"Abcdef1!@#ghijklmn","verificationCode":"A1b2C3"}`
}

func TestRegisterSuccess(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	w := postJSON(h.Register, validRegisterBody())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if len(deps.userRepo.CreatedUsers) != 1 {
		t.Fatalf("created users = %d, want 1", len(deps.userRepo.CreatedUsers))
	}
	created := deps.userRepo.CreatedUsers[0]
	if created.Username != "alice" || created.Email != "alice@example.com" {
		t.Errorf("created user = %+v", created)
	}
	// 密码必须哈希存储，且能验证通过
	if created.Password == "Abcdef1!@#ghijklmn" {
		t.Error("password must be hashed, not stored in plaintext")
	}
	if ok, _ := utils.VerifyPassword("Abcdef1!@#ghijklmn", created.Password); !ok {
		t.Error("hashed password should verify")
	}
	// 验证码应被消费
	if len(deps.tokenMgr.Invalidated) != 1 || deps.tokenMgr.Invalidated[0] != "alice@example.com" {
		t.Errorf("verification code should be invalidated once, got %v", deps.tokenMgr.Invalidated)
	}
}

func TestRegisterInvalidBody(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)
	w := postJSON(h.Register, `{"username":`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_REQUEST") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestRegisterInvalidUsername(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)
	w := postJSON(h.Register, `{"username":"   ","email":"alice@example.com","password":"Abcdef1!@#ghijklmn","verificationCode":"A1b2C3"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_USERNAME") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestRegisterInvalidEmail(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)
	w := postJSON(h.Register, `{"username":"alice","email":"not-an-email","password":"Abcdef1!@#ghijklmn","verificationCode":"A1b2C3"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_EMAIL") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestRegisterPasswordTooShort(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)
	w := postJSON(h.Register, `{"username":"alice","email":"alice@example.com","password":"short","verificationCode":"A1b2C3"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "PASSWORD_TOO_SHORT") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestRegisterEmptyCode(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)
	w := postJSON(h.Register, `{"username":"alice","email":"alice@example.com","password":"Abcdef1!@#ghijklmn","verificationCode":""}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "MISSING_PARAMETERS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestRegisterInvalidVerificationCode(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.tokenMgr.VerifyCodeErr = models.ErrInvalidCode
	w := postJSON(h.Register, validRegisterBody())
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_CODE") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.CreatedUsers) != 0 {
		t.Error("user must not be created when code is invalid")
	}
}

func TestRegisterWhitelistDenied(t *testing.T) {
	h, deps := newTestAuthHandler(t, true)
	deps.whitelist.Allowed = false
	w := postJSON(h.Register, validRegisterBody())
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "EMAIL_DOMAIN_NOT_ALLOWED") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.CreatedUsers) != 0 {
		t.Error("user must not be created when domain is not allowed")
	}
}

func TestRegisterEmailExists(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.userRepo.Seed(&models.User{Username: "bob", Email: "alice@example.com", UID: "uid-bob"})
	w := postJSON(h.Register, validRegisterBody())
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "EMAIL_ALREADY_EXISTS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.CreatedUsers) != 0 {
		t.Error("user must not be created when email exists")
	}
}

func TestRegisterUsernameExists(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.userRepo.Seed(&models.User{Username: "alice", Email: "bob@example.com", UID: "uid-bob"})
	w := postJSON(h.Register, validRegisterBody())
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "USERNAME_ALREADY_EXISTS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.CreatedUsers) != 0 {
		t.Error("user must not be created when username exists")
	}
}

func TestRegisterCreateConflict(t *testing.T) {
	// Create 权威冲突检查：预检查通过但 Create 返回 ErrEmailExists（并发窗口）
	h, deps := newTestAuthHandler(t, false)
	deps.userRepo.CreateErr = models.ErrEmailExists
	w := postJSON(h.Register, validRegisterBody())
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "EMAIL_ALREADY_EXISTS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestLoginSuccess(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	hash, _ := utils.HashPassword("Abcdef1!@#ghijklmn")
	deps.userRepo.Seed(&models.User{Username: "alice", Email: "alice@example.com", UID: "uid-1", Password: hash})

	w := postJSON(h.Login, `{"email":"alice@example.com","password":"Abcdef1!@#ghijklmn"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"username":"alice"`) {
		t.Errorf("response should contain username, got %s", w.Body.String())
	}
	// 认证 Cookie 已设置
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Error("login should set cookies")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	hash, _ := utils.HashPassword("Abcdef1!@#ghijklmn")
	deps.userRepo.Seed(&models.User{Username: "alice", Email: "alice@example.com", UID: "uid-1", Password: hash})

	w := postJSON(h.Login, `{"email":"alice@example.com","password":"Wrong1!@#password"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_CREDENTIALS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestLoginUserNotFound(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)
	// 用户不存在 → dummy 密码验证路径 → INVALID_CREDENTIALS（防止时序枚举）
	w := postJSON(h.Login, `{"email":"nobody@example.com","password":"Abcdef1!@#ghijklmn"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_CREDENTIALS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestLoginCaptchaFailed(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.captcha.VerifyErr = errors.New("captcha invalid")
	w := postJSON(h.Login, `{"email":"alice@example.com","password":"Abcdef1!@#ghijklmn"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CAPTCHA_FAILED") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestLoginEmptyParams(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)
	w := postJSON(h.Login, `{"email":"","password":""}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "MISSING_PARAMETERS") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}
