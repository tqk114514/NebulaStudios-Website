// TOTP 用户侧管理端点测试：setup / enable / disable。
package user

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// totpTestCode 独立实现的 TOTP 码计算，作为被测算法的对照实现（test oracle）
func totpTestCode(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	counter := uint64(time.Now().Unix() / 30)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000)
}

// newTestTOTPHandler 组装被测 Handler，统一使用 uid-1 身份（postUserJSON 已预置）
func newTestTOTPHandler(t *testing.T) (*TOTPHandler, *testutil.FakeUserRepo, *services.TOTPService, *testutil.FakeSessionManager) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	totpSvc, err := services.NewTOTPService(testutil.NewFakeTOTPRecoveryStore())
	if err != nil {
		t.Fatalf("NewTOTPService: %v", err)
	}

	userRepo := testutil.NewFakeUserRepo()
	hash, err := utils.HashPassword("correct-horse-password")
	if err != nil {
		t.Fatal(err)
	}
	userRepo.Seed(&models.User{
		UID:      "uid-1",
		Username: "alice",
		Email:    "alice@example.com",
		Password: hash,
	})

	sessionMgr := &testutil.FakeSessionManager{}
	h, err := NewTOTPHandler(
		userRepo,
		&testutil.FakeUserLogStore{},
		&testutil.FakeCaptcha{},
		sessionMgr,
		&testutil.FakeUserCache{},
		totpSvc,
	)
	if err != nil {
		t.Fatalf("NewTOTPHandler: %v", err)
	}
	return h, userRepo, totpSvc, sessionMgr
}

// setupTOTPForUser 直接为 uid-1 写入待确认密钥，返回密钥
func setupTOTPForUser(t *testing.T, h *TOTPHandler, userRepo *testutil.FakeUserRepo) string {
	t.Helper()
	w := postUserJSON(h.SetupTOTP, `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("SetupTOTP status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Secret     string `json:"secret"`
			OTPAuthURI string `json:"otpauth_uri"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Secret == "" || !strings.HasPrefix(resp.Data.OTPAuthURI, "otpauth://totp/") {
		t.Fatalf("unexpected setup response: %s", w.Body.String())
	}
	stored, ok := userRepo.UIDs["uid-1"]
	if !ok || !stored.TOTPSecret.Valid || stored.TOTPSecret.String != resp.Data.Secret {
		t.Fatal("secret should be persisted pending confirmation")
	}
	return resp.Data.Secret
}

func TestSetupTOTPAlreadyEnabled(t *testing.T) {
	h, userRepo, _, _ := newTestTOTPHandler(t)
	userRepo.UIDs["uid-1"].TOTPEnabled = true

	w := postUserJSON(h.SetupTOTP, `{}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), utils.ErrCodeTOTPAlreadyEnabled) {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestEnableTOTPFlow(t *testing.T) {
	h, userRepo, _, _ := newTestTOTPHandler(t)
	secret := setupTOTPForUser(t, h, userRepo)

	// 错误验证码
	w := postUserJSON(h.EnableTOTP, `{"code":"000000","captchaToken":""}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), utils.ErrCodeTOTPInvalid) {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	// 正确验证码（独立实现计算，作为对照）
	code := totpTestCode(t, secret)
	w2 := postUserJSON(h.EnableTOTP, fmt.Sprintf(`{"code":%q,"captchaToken":""}`, code))
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w2.Code, w2.Body.String())
	}
	var resp struct {
		Data struct {
			RecoveryCodes []string `json:"recovery_codes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.RecoveryCodes) != 8 {
		t.Fatalf("expected 8 recovery codes, got %d", len(resp.Data.RecoveryCodes))
	}
	if !userRepo.UIDs["uid-1"].TOTPEnabled {
		t.Error("user should be TOTP enabled after successful enable")
	}
}

func TestEnableTOTPRequiresSetup(t *testing.T) {
	h, _, _, _ := newTestTOTPHandler(t)
	// 从未 setup：无密钥
	w := postUserJSON(h.EnableTOTP, `{"code":"123456","captchaToken":""}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), utils.ErrCodeTOTPNotEnabled) {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestEnableTOTPAlreadyEnabled(t *testing.T) {
	h, userRepo, _, _ := newTestTOTPHandler(t)
	setupTOTPForUser(t, h, userRepo)
	userRepo.UIDs["uid-1"].TOTPEnabled = true

	w := postUserJSON(h.EnableTOTP, `{"code":"123456","captchaToken":""}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), utils.ErrCodeTOTPAlreadyEnabled) {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestDisableTOTPFlow(t *testing.T) {
	h, userRepo, totpSvc, sessionMgr := newTestTOTPHandler(t)
	secret := setupTOTPForUser(t, h, userRepo)
	userRepo.UIDs["uid-1"].TOTPEnabled = true
	codes := totpSvc.GenerateRecoveryCodes()
	_ = totpSvc.StoreRecoveryCodes(t.Context(), "uid-1", codes)

	// 错误密码
	w := postUserJSON(h.DisableTOTP, fmt.Sprintf(`{"password":"wrong-password","code":%q,"captchaToken":""}`, codes[0]))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), utils.ErrCodeWrongPassword) {
		t.Fatalf("wrong password should be rejected, status = %d, body=%s", w.Code, w.Body.String())
	}

	// 密码正确但验证码错误
	w = postUserJSON(h.DisableTOTP, `{"password":"correct-horse-password","code":"000000","captchaToken":""}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), utils.ErrCodeTOTPInvalid) {
		t.Fatalf("invalid code should be rejected, status = %d, body=%s", w.Code, w.Body.String())
	}

	// 密码 + TOTP 码正确
	code := totpTestCode(t, secret)
	w2 := postUserJSON(h.DisableTOTP, fmt.Sprintf(`{"password":"correct-horse-password","code":%q,"captchaToken":""}`, code))
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w2.Code, w2.Body.String())
	}
	user := userRepo.UIDs["uid-1"]
	if user.TOTPEnabled || user.TOTPSecret.Valid {
		t.Error("TOTP should be fully cleared after disable")
	}
	if !sessionMgr.RevokedUIDs["uid-1"] {
		t.Error("all sessions should be revoked after disabling TOTP")
	}
}

func TestDisableTOTPWithRecoveryCode(t *testing.T) {
	h, userRepo, totpSvc, _ := newTestTOTPHandler(t)
	setupTOTPForUser(t, h, userRepo)
	userRepo.UIDs["uid-1"].TOTPEnabled = true
	codes := totpSvc.GenerateRecoveryCodes()
	_ = totpSvc.StoreRecoveryCodes(t.Context(), "uid-1", codes)

	w := postUserJSON(h.DisableTOTP, fmt.Sprintf(`{"password":"correct-horse-password","code":%q,"captchaToken":""}`, codes[0]))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if userRepo.UIDs["uid-1"].TOTPEnabled {
		t.Error("TOTP should be disabled")
	}
}

func TestDisableTOTPNotEnabled(t *testing.T) {
	h, _, _, _ := newTestTOTPHandler(t)
	w := postUserJSON(h.DisableTOTP, `{"password":"correct-horse-password","code":"123456","captchaToken":""}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), utils.ErrCodeTOTPNotEnabled) {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}
