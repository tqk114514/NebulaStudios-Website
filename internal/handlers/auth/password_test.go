package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// 统一测试密码：满足 ValidatePassword 强度要求（最小 16 字符）
const testStrongPassword = "Abcdef1!@#ghijklmn"

// 重置/修改后的新密码：不同且同样满足强度
const testNewPassword = "BrandNewPassword9!"

func seedUserWithPassword(deps *testDeps, uid, email, password string) *models.User {
	hash, err := utils.HashPassword(password)
	if err != nil {
		panic(err)
	}
	u := &models.User{UID: uid, Email: email, Username: "testuser", Password: hash}
	deps.userRepo.Seed(u)
	return u
}

func TestSendResetCodeSuccess(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedUserWithPassword(deps, "u1", "reset@test.local", testStrongPassword)

	w := postJSON(h.SendResetCode, `{"email":"reset@test.local"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "expireTime") {
		t.Errorf("response missing expireTime: %s", w.Body.String())
	}
	if len(deps.emailSender.SentEmails) != 1 || deps.emailSender.SentEmails[0] != "reset@test.local" {
		t.Errorf("reset email not sent, got %v", deps.emailSender.SentEmails)
	}
}

func TestSendResetCodeNonexistentEmail(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	// 防枚举：不存在的邮箱同样返回 200 + expireTime（走 dummy 分支）
	w := postJSON(h.SendResetCode, `{"email":"nobody@test.local"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (anti-enumeration), body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "expireTime") {
		t.Errorf("response missing expireTime: %s", w.Body.String())
	}
}

func TestSendResetCodeCaptchaFailed(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.captcha.VerifyErr = errors.New("captcha invalid")

	w := postJSON(h.SendResetCode, `{"email":"reset@test.local"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "CAPTCHA_FAILED") {
		t.Errorf("want CAPTCHA_FAILED, got %s", w.Body.String())
	}
}

func TestSendResetCodeRateLimited(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.limiter.EmailAllowed = false

	w := postJSON(h.SendResetCode, `{"email":"reset@test.local"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if !strings.Contains(w.Body.String(), "RATE_LIMIT") {
		t.Errorf("want RATE_LIMIT, got %s", w.Body.String())
	}
}

func TestSendResetCodeEmptyEmail(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	w := postJSON(h.SendResetCode, `{"email":"  "}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "MISSING_PARAMETERS") {
		t.Errorf("want MISSING_PARAMETERS, got %s", w.Body.String())
	}
}

func TestResetPasswordSuccess(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	u := seedUserWithPassword(deps, "u1", "reset@test.local", testStrongPassword)

	body := `{"email":"reset@test.local","code":"123456","password":"NewPassword2!xabc"}`
	w := postJSON(h.ResetPassword, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.PasswordUpdates) != 1 || deps.userRepo.PasswordUpdates[0] != u.UID {
		t.Errorf("UpdatePassword not called with %s, got %v", u.UID, deps.userRepo.PasswordUpdates)
	}
}

func TestResetPasswordInvalidCode(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedUserWithPassword(deps, "u1", "reset@test.local", testStrongPassword)
	deps.tokenMgr.VerifyCodeErr = errors.New("INVALID_CODE")

	body := `{"email":"reset@test.local","code":"000000","password":"NewPassword2!xabc"}`
	w := postJSON(h.ResetPassword, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_CODE") {
		t.Errorf("want INVALID_CODE, got %s", w.Body.String())
	}
	if len(deps.userRepo.PasswordUpdates) != 0 {
		t.Errorf("UpdatePassword should not be called on invalid code, got %v", deps.userRepo.PasswordUpdates)
	}
}

func TestResetPasswordSamePassword(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedUserWithPassword(deps, "u1", "reset@test.local", testStrongPassword)

	body := `{"email":"reset@test.local","code":"123456","password":"` + testStrongPassword + `"}`
	w := postJSON(h.ResetPassword, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "SAME_PASSWORD") {
		t.Errorf("want SAME_PASSWORD, got %s", w.Body.String())
	}
	if len(deps.userRepo.PasswordUpdates) != 0 {
		t.Errorf("UpdatePassword should not be called on same password, got %v", deps.userRepo.PasswordUpdates)
	}
}

func TestResetPasswordWeakPassword(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedUserWithPassword(deps, "u1", "reset@test.local", testStrongPassword)

	body := `{"email":"reset@test.local","code":"123456","password":"abc"}`
	w := postJSON(h.ResetPassword, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if len(deps.userRepo.PasswordUpdates) != 0 {
		t.Errorf("UpdatePassword should not be called for weak password")
	}
}

func TestResetPasswordUserNotFound(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	body := `{"email":"nobody@test.local","code":"123456","password":"NewPassword2!xabc"}`
	w := postJSON(h.ResetPassword, body)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "USER_NOT_FOUND") {
		t.Errorf("want USER_NOT_FOUND, got %s", w.Body.String())
	}
}

func TestResetPasswordMissingParams(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	w := postJSON(h.ResetPassword, `{"email":"a@b.c"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "MISSING_PARAMETERS") {
		t.Errorf("want MISSING_PARAMETERS, got %s", w.Body.String())
	}
}

func TestChangePasswordSuccess(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	u := seedUserWithPassword(deps, "u1", "reset@test.local", testStrongPassword)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.ContextKeyUID, u.UID)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password",
		strings.NewReader(`{"currentPassword":"`+testStrongPassword+`","newPassword":"`+testNewPassword+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ChangePassword(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.PasswordUpdates) != 1 || deps.userRepo.PasswordUpdates[0] != u.UID {
		t.Errorf("UpdatePassword not called with %s, got %v", u.UID, deps.userRepo.PasswordUpdates)
	}
}

func TestChangePasswordWrongCurrent(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	u := seedUserWithPassword(deps, "u1", "reset@test.local", testStrongPassword)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.ContextKeyUID, u.UID)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password",
		strings.NewReader(`{"currentPassword":"WrongPass9!x","newPassword":"`+testNewPassword+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ChangePassword(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "WRONG_PASSWORD") {
		t.Errorf("want WRONG_PASSWORD, got %s", w.Body.String())
	}
	if len(deps.userRepo.PasswordUpdates) != 0 {
		t.Errorf("UpdatePassword should not be called on wrong current password")
	}
}

func TestChangePasswordSamePassword(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	u := seedUserWithPassword(deps, "u1", "reset@test.local", testStrongPassword)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.ContextKeyUID, u.UID)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password",
		strings.NewReader(`{"currentPassword":"`+testStrongPassword+`","newPassword":"`+testStrongPassword+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ChangePassword(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "SAME_PASSWORD") {
		t.Errorf("want SAME_PASSWORD, got %s", w.Body.String())
	}
}

func TestChangePasswordUnauthorized(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password",
		strings.NewReader(`{"currentPassword":"x","newPassword":"y"}`))

	h.ChangePassword(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
