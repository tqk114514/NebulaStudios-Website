package user

import (
	"bytes"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// totpRequest 以指定 uid 身份发起请求；uid 为空表示未认证
func totpRequest(method, path, body, uid string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, path, func(c *gin.Context) {
		if uid != "" {
			c.Set(middleware.ContextKeyUID, uid)
		}
		handler(c)
	})

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "192.168.3.31:1234"

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// seedTOTPUser 在已预置（含密码哈希）的用户上设置 TOTP 字段，避免重建用户时丢失密码
func seedTOTPUser(t *testing.T, deps *testutil.FakeUserRepo, enabled bool) *models.User {
	t.Helper()

	user, ok := deps.UIDs["uid-1"]
	if !ok {
		t.Fatal("uid-1 should be seeded by newTestTOTPHandler")
	}
	user.TOTPEnabled = enabled
	user.TOTPSecret = sql.NullString{String: "JBSWY3DPEHPK3PXP", Valid: true}
	return user
}

func TestSetupTOTPGuards(t *testing.T) {
	// 未认证
	h, _, _, _ := newTestTOTPHandler(t)
	rec := totpRequest(http.MethodPost, "/totp/setup", "", "", h.SetupTOTP)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no uid status = %d, want 401", rec.Code)
	}

	// 用户不存在
	rec = totpRequest(http.MethodPost, "/totp/setup", "", "ghost", h.SetupTOTP)
	if rec.Code == http.StatusOK {
		t.Errorf("missing user status = %d, want not 200", rec.Code)
	}
}

// 写入密钥失败必须报错，且不能让调用方以为已完成 setup
func TestSetupTOTPWriteFailure(t *testing.T) {
	h, deps, _, _ := newTestTOTPHandler(t)
	deps.SetTOTPSecretErr = errors.New("write failed")

	rec := totpRequest(http.MethodPost, "/totp/setup", "", "uid-1", h.SetupTOTP)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestEnableTOTPGuards(t *testing.T) {
	h, deps, _, _ := newTestTOTPHandler(t)

	// 未认证
	rec := totpRequest(http.MethodPost, "/totp/enable", `{"code":"123456"}`, "", h.EnableTOTP)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no uid status = %d, want 401", rec.Code)
	}

	// 空验证码
	rec = totpRequest(http.MethodPost, "/totp/enable", `{"code":"   "}`, "uid-1", h.EnableTOTP)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "MISSING_PARAMETERS") {
		t.Errorf("empty code: status = %d body = %s", rec.Code, rec.Body.String())
	}

	// 未完成 setup（无密钥）
	rec = totpRequest(http.MethodPost, "/totp/enable", `{"code":"123456"}`, "uid-1", h.EnableTOTP)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "TOTP_NOT_ENABLED") {
		t.Errorf("without secret: status = %d body = %s", rec.Code, rec.Body.String())
	}

	// 用户不存在
	rec = totpRequest(http.MethodPost, "/totp/enable", `{"code":"123456"}`, "ghost", h.EnableTOTP)
	if rec.Code == http.StatusOK {
		t.Errorf("missing user status = %d, want not 200", rec.Code)
	}

	// 验证码错误
	seedTOTPUser(t, deps, false)
	rec = totpRequest(http.MethodPost, "/totp/enable", `{"code":"000000"}`, "uid-1", h.EnableTOTP)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "TOTP_INVALID") {
		t.Errorf("invalid code: status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestDisableTOTPGuards(t *testing.T) {
	h, deps, _, _ := newTestTOTPHandler(t)

	// 未认证
	rec := totpRequest(http.MethodPost, "/totp/disable", `{"password":"x","code":"123456"}`, "", h.DisableTOTP)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no uid status = %d, want 401", rec.Code)
	}

	// 非法 JSON
	rec = totpRequest(http.MethodPost, "/totp/disable", `{`, "uid-1", h.DisableTOTP)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid json status = %d, want 400", rec.Code)
	}

	// 用户不存在
	rec = totpRequest(http.MethodPost, "/totp/disable", `{"password":"x","code":"123456"}`, "ghost", h.DisableTOTP)
	if rec.Code == http.StatusOK {
		t.Errorf("missing user status = %d, want not 200", rec.Code)
	}

	// 密码错误
	seedTOTPUser(t, deps, true)
	rec = totpRequest(http.MethodPost, "/totp/disable",
		`{"password":"wrong-password","code":"123456"}`, "uid-1", h.DisableTOTP)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "WRONG_PASSWORD") {
		t.Errorf("wrong password: status = %d body = %s", rec.Code, rec.Body.String())
	}

	// 验证码与恢复码均不通过
	rec = totpRequest(http.MethodPost, "/totp/disable",
		`{"password":"correct-horse-password","code":"000000"}`, "uid-1", h.DisableTOTP)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "TOTP_INVALID") {
		t.Errorf("invalid code: status = %d body = %s", rec.Code, rec.Body.String())
	}
}

// 关闭两步验证必须撤销全部会话；撤销失败只告警，不影响关闭结果
func TestDisableTOTPRevokesSessions(t *testing.T) {
	h, deps, _, sessionMgr := newTestTOTPHandler(t)
	seedTOTPUser(t, deps, true)

	// 用恢复码路径通过验证：fake 的 ConsumedRecovery 为 true
	totpSvc := &testutil.FakeTOTPManager{VerifyCodeResult: false, ConsumedRecovery: true}
	_ = totpSvc

	// 直接构造一个使用 fake TOTP 管理器的 handler
	fakeHandler, err := NewTOTPHandler(deps, &testutil.FakeUserLogStore{}, &testutil.FakeCaptcha{},
		sessionMgr, &testutil.FakeUserCache{}, totpSvc)
	if err != nil {
		t.Fatalf("NewTOTPHandler: %v", err)
	}
	_ = h

	rec := totpRequest(http.MethodPost, "/totp/disable",
		`{"password":"correct-horse-password","code":"ABCD-2345"}`, "uid-1", fakeHandler.DisableTOTP)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !sessionMgr.RevokedUIDs["uid-1"] {
		t.Error("disabling TOTP should revoke all user tokens")
	}

	// 撤销失败只告警：关闭流程仍应成功
	sessionMgr.RevokeErr = errors.New("revoke failed")
	seedTOTPUser(t, deps, true)

	rec = totpRequest(http.MethodPost, "/totp/disable",
		`{"password":"correct-horse-password","code":"ABCD-2345"}`, "uid-1", fakeHandler.DisableTOTP)
	if rec.Code != http.StatusOK {
		t.Errorf("revoke failure status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// 未启用 TOTP 时关闭应返回冲突，而不是静默成功
func TestDisableTOTPWhenNotEnabled(t *testing.T) {
	h, deps, _, _ := newTestTOTPHandler(t)
	seedTOTPUser(t, deps, false)

	rec := totpRequest(http.MethodPost, "/totp/disable",
		`{"password":"correct-horse-password","code":"123456"}`, "uid-1", h.DisableTOTP)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "TOTP_NOT_ENABLED") {
		t.Errorf("status = %d body = %s, want 409 TOTP_NOT_ENABLED", rec.Code, rec.Body.String())
	}
}
