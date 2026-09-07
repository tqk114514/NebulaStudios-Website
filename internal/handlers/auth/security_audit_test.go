// 安全审计修复验证：F3 —— 注册现在用 UseCode 原子消费验证码，重放/已消费的码会被拒绝。
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"auth-system/internal/config"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"
)

func auditJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// auditTokenMgr fake：记录 UseCode 调用、可按需返回 UseCode 错误（模拟"码已被消费/重放"）。
type auditTokenMgr struct {
	*testutil.FakeTokenManager
	useCodeErr   error
	useCodeCalls int
}

func (m *auditTokenMgr) VerifyCode(_ context.Context, _, _, _ string) (*services.CodeResult, error) {
	return &services.CodeResult{Type: services.TokenTypeRegister}, nil
}

func (m *auditTokenMgr) UseCode(context.Context, string, string) error {
	m.useCodeCalls++
	return m.useCodeErr
}

func newAuditHandler(t *testing.T, tm services.TokenManager) (*AuthHandler, *testutil.FakeUserRepo) {
	t.Helper()
	totpSvc, err := services.NewTOTPService(&testutil.FakeTOTPRecoveryStore{})
	if err != nil {
		t.Fatalf("NewTOTPService: %v", err)
	}
	repo := testutil.NewFakeUserRepo()
	h, err := NewAuthHandler(
		&config.Config{BaseURL: "https://test.local"},
		repo,
		&testutil.FakeUserLogStore{},
		&testutil.FakeUserConsentStore{},
		tm,
		&testutil.FakeSessionManager{},
		&testutil.FakeEmailSender{},
		&testutil.FakeCaptcha{},
		&testutil.FakeUserCache{},
		nil,
		&testutil.FakeLimiter{},
		totpSvc,
	)
	if err != nil {
		t.Fatalf("NewAuthHandler: %v", err)
	}
	return h, repo
}

// TestF3_Fixed_RegisterRejectsReplayedCode：码已被消费（重放）时，Register 必须拒绝且不建号。
func TestF3_Fixed_RegisterRejectsReplayedCode(t *testing.T) {
	tm := &auditTokenMgr{FakeTokenManager: &testutil.FakeTokenManager{}, useCodeErr: models.ErrCodeUsed}
	h, repo := newAuditHandler(t, tm)

	body := auditJSON(map[string]any{
		"username":         "alice",
		"email":            "alice@example.com",
		"password":         "Abcdef1!@#ghijklmn",
		"verificationCode": "REUSED",
	})
	w := postJSON(h.Register, body)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CODE_USED") {
		t.Fatalf("FIX FAILED: replayed code should be rejected with CODE_USED, got status=%d body=%s", w.Code, w.Body.String())
	}
	if tm.useCodeCalls != 1 {
		t.Fatalf("Register must call UseCode (atomic consume) exactly once, got %d", tm.useCodeCalls)
	}
	if len(repo.CreatedUsers) != 0 {
		t.Fatalf("FIX FAILED: user must NOT be created for a replayed code, created=%d", len(repo.CreatedUsers))
	}
	t.Logf("F3 FIXED: Register rejected replayed code (CODE_USED) after atomic UseCode; no user created")
}
