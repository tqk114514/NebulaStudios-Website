package auth

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"auth-system/internal/services"
)

// ---------- SendCode ----------

func TestSendCodeSuccess(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)

	w := postJSON(h.SendCode, `{"email":"new@test.local"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "expireTime") || !strings.Contains(body, "new@test.local") {
		t.Errorf("response missing expireTime/email: %s", body)
	}
	if len(deps.emailSender.SentEmails) != 1 || deps.emailSender.SentEmails[0] != "new@test.local" {
		t.Errorf("verification email not sent, got %v", deps.emailSender.SentEmails)
	}
}

func TestSendCodeAlreadyRegistered(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedUserWithPassword(deps, "u1", "exists@test.local", testStrongPassword)

	// 防枚举：已注册邮箱同样返回 200，但不发送邮件（dummy CreateToken 保持响应时间一致）
	w := postJSON(h.SendCode, `{"email":"exists@test.local"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (anti-enumeration), body = %s", w.Code, w.Body.String())
	}
	if len(deps.emailSender.SentEmails) != 0 {
		t.Errorf("no email should be sent for registered address, got %v", deps.emailSender.SentEmails)
	}
}

func TestSendCodeInvalidEmail(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	w := postJSON(h.SendCode, `{"email":"not-an-email"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_") {
		t.Errorf("want INVALID_* error, got %s", w.Body.String())
	}
}

func TestSendCodeWhitelistDenied(t *testing.T) {
	h, deps := newTestAuthHandler(t, true)
	deps.whitelist.Allowed = false

	w := postJSON(h.SendCode, `{"email":"blocked@test.local"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "EMAIL_DOMAIN_NOT_ALLOWED") {
		t.Errorf("want EMAIL_DOMAIN_NOT_ALLOWED, got %s", w.Body.String())
	}
}

func TestSendCodeCaptchaFailed(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.captcha.VerifyErr = errors.New("captcha invalid")

	w := postJSON(h.SendCode, `{"email":"new@test.local"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "CAPTCHA_FAILED") {
		t.Errorf("want CAPTCHA_FAILED, got %s", w.Body.String())
	}
}

func TestSendCodeRateLimited(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.limiter.EmailAllowed = false

	w := postJSON(h.SendCode, `{"email":"new@test.local"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if !strings.Contains(w.Body.String(), "RATE_LIMIT") {
		t.Errorf("want RATE_LIMIT, got %s", w.Body.String())
	}
}

// ---------- VerifyEmail ----------

func TestVerifyEmailSuccess(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.tokenMgr.UseTokenResult = &services.TokenResult{Code: "123456", Email: "new@test.local"}

	w := postJSON(h.VerifyEmail, `{"token":"valid-token"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "123456") || !strings.Contains(body, "new@test.local") {
		t.Errorf("response missing code/email: %s", body)
	}
}

func TestVerifyEmailNoToken(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	w := postJSON(h.VerifyEmail, `{"token":"  "}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "NO_TOKEN") {
		t.Errorf("want NO_TOKEN, got %s", w.Body.String())
	}
}

func TestVerifyEmailInvalidToken(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.tokenMgr.UseTokenErr = errors.New("INVALID_TOKEN")

	w := postJSON(h.VerifyEmail, `{"token":"bad-token"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_TOKEN") {
		t.Errorf("want INVALID_TOKEN, got %s", w.Body.String())
	}
}

func TestVerifyEmailNilResult(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	// fail-closed：ValidateAndUseToken 返回 nil 结果视为内部错误
	w := postJSON(h.VerifyEmail, `{"token":"some-token"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_INVALID") {
		t.Errorf("want TOKEN_INVALID, got %s", w.Body.String())
	}
}

// ---------- CheckCodeExpiry ----------

func TestCheckCodeExpiryNotExpired(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.tokenMgr.CodeExpired = false
	deps.tokenMgr.CodeExpireTime = 1234567890

	w := getQuery(h.CheckCodeExpiry, "/api/auth/code-expiry?email=a@b.c")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"expired":false`) || !strings.Contains(body, "1234567890") {
		t.Errorf("want expired:false + expireTime, got %s", body)
	}
}

func TestCheckCodeExpiryExpired(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.tokenMgr.CodeExpired = true

	w := getQuery(h.CheckCodeExpiry, "/api/auth/code-expiry?email=a@b.c")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"expired":true`) {
		t.Errorf("want expired:true, got %s", w.Body.String())
	}
}

func TestCheckCodeExpiryEmptyEmail(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	w := getQuery(h.CheckCodeExpiry, "/api/auth/code-expiry")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "MISSING_PARAMETERS") {
		t.Errorf("want MISSING_PARAMETERS, got %s", w.Body.String())
	}
}

// ---------- VerifyCode ----------

func TestVerifyCodeSuccess(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	w := postJSON(h.VerifyCode, `{"code":"123456","email":"a@b.c","tokenType":"register"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeMissingParams(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	w := postJSON(h.VerifyCode, `{"code":"123456"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "MISSING_PARAMETERS") {
		t.Errorf("want MISSING_PARAMETERS, got %s", w.Body.String())
	}
}

func TestVerifyCodeInvalid(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.tokenMgr.VerifyCodeErr = errors.New("INVALID_CODE")

	w := postJSON(h.VerifyCode, `{"code":"000000","email":"a@b.c","tokenType":"register"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_CODE") {
		t.Errorf("want INVALID_CODE, got %s", w.Body.String())
	}
}
