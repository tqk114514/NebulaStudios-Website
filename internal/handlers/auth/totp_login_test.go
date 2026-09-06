// TOTP 登录两段式流程测试：Login 返回中转 token、LoginTOTP 完成签发。
package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/utils"
)

// totpTestCode 独立实现的 TOTP 码计算，作为被测算法的对照实现（test oracle）
func totpTestCode(t *testing.T, secret string, unixTime int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	counter := uint64(unixTime / 30)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000)
}

// seedTOTPUser 预置一个已启用 TOTP 的用户，返回其密钥
func seedTOTPUser(t *testing.T, h *AuthHandler, deps *testDeps, uid, email string) string {
	t.Helper()
	secret := deps.totpSvc.GenerateSecret()
	hash, err := utils.HashPassword("correct-horse-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	deps.userRepo.Seed(&models.User{
		UID:         uid,
		Username:    "totpuser",
		Email:       email,
		Password:    hash,
		TOTPSecret:  sql.NullString{String: secret, Valid: true},
		TOTPEnabled: true,
	})
	return secret
}

func TestLoginTOTPRequired(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedTOTPUser(t, h, deps, "uid-totp", "totp@example.com")

	w := postJSON(h.Login, `{"email":"totp@example.com","password":"correct-horse-password"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			TOTPRequired bool   `json:"totp_required"`
			PendingToken string `json:"pending_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Data.TOTPRequired || resp.Data.PendingToken == "" {
		t.Fatalf("expected totp_required with pending token, got %s", w.Body.String())
	}
	// 不应签发会话 Cookie
	if strings.Contains(w.Header().Get("Set-Cookie"), "fake-access-token") {
		t.Error("session cookie must not be issued before TOTP second step")
	}
}

func TestLoginTOTPSuccessWithCode(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	secret := seedTOTPUser(t, h, deps, "uid-totp", "totp@example.com")

	// 第一步：密码登录拿中转 token
	w := postJSON(h.Login, `{"email":"totp@example.com","password":"correct-horse-password"}`)
	var first struct {
		Data struct {
			PendingToken string `json:"pending_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &first)

	code := totpTestCode(t, secret, time.Now().Unix())
	body := fmt.Sprintf(`{"pendingToken":%q,"code":%q}`, first.Data.PendingToken, code)
	w2 := postJSON(h.LoginTOTP, body)
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Header().Get("Set-Cookie"), "fake-access-token") {
		t.Error("session cookie should be issued after successful TOTP verification")
	}
}

func TestLoginTOTPWrongPendingToken(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	secret := seedTOTPUser(t, h, deps, "uid-totp", "totp@example.com")
	code := totpTestCode(t, secret, time.Now().Unix())

	w := postJSON(h.LoginTOTP, fmt.Sprintf(`{"pendingToken":"bogus","code":%q}`, code))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), utils.ErrCodeTOTPPendingInvalid) {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestLoginTOTPInvalidCode(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedTOTPUser(t, h, deps, "uid-totp", "totp@example.com")

	w := postJSON(h.Login, `{"email":"totp@example.com","password":"correct-horse-password"}`)
	var first struct {
		Data struct {
			PendingToken string `json:"pending_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &first)

	w2 := postJSON(h.LoginTOTP, fmt.Sprintf(`{"pendingToken":%q,"code":"000000"}`, first.Data.PendingToken))
	if w2.Code != http.StatusBadRequest || !strings.Contains(w2.Body.String(), utils.ErrCodeTOTPInvalid) {
		t.Fatalf("status = %d, body=%s", w2.Code, w2.Body.String())
	}
}

func TestLoginTOTPWithRecoveryCode(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedTOTPUser(t, h, deps, "uid-totp", "totp@example.com")

	// 预置一个恢复码
	const recovery = "ABCD-2345"
	if err := deps.totpSvc.StoreRecoveryCodes(t.Context(), "uid-totp", []string{recovery}); err != nil {
		t.Fatal(err)
	}

	w := postJSON(h.Login, `{"email":"totp@example.com","password":"correct-horse-password"}`)
	var first struct {
		Data struct {
			PendingToken string `json:"pending_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &first)

	w2 := postJSON(h.LoginTOTP, fmt.Sprintf(`{"pendingToken":%q,"code":%q}`, first.Data.PendingToken, recovery))
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w2.Code, w2.Body.String())
	}
	// 恢复码单次使用：再次登录需新的恢复码
	w3 := postJSON(h.Login, `{"email":"totp@example.com","password":"correct-horse-password"}`)
	var second struct {
		Data struct {
			PendingToken string `json:"pending_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w3.Body.Bytes(), &second)
	w4 := postJSON(h.LoginTOTP, fmt.Sprintf(`{"pendingToken":%q,"code":%q}`, second.Data.PendingToken, recovery))
	if w4.Code != http.StatusBadRequest {
		t.Errorf("used recovery code must be rejected, status = %d", w4.Code)
	}
}

func TestLoginTOTPLocked(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	seedTOTPUser(t, h, deps, "uid-totp", "totp@example.com")

	for i := 0; i < 5; i++ {
		deps.totpSvc.RecordFailure("uid-totp")
	}

	w := postJSON(h.Login, `{"email":"totp@example.com","password":"correct-horse-password"}`)
	var first struct {
		Data struct {
			PendingToken string `json:"pending_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &first)

	w2 := postJSON(h.LoginTOTP, fmt.Sprintf(`{"pendingToken":%q,"code":"000000"}`, first.Data.PendingToken))
	if w2.Code != http.StatusTooManyRequests || !strings.Contains(w2.Body.String(), utils.ErrCodeTOTPLocked) {
		t.Fatalf("status = %d, body=%s", w2.Code, w2.Body.String())
	}
}

func TestLoginTOTPBannedUserCompletesLogin(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	secret := deps.totpSvc.GenerateSecret()
	bannedHash, err := utils.HashPassword("correct-horse-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	deps.userRepo.Seed(&models.User{
		UID:         "uid-banned",
		Username:    "banneduser",
		Email:       "banned@example.com",
		Password:    bannedHash,
		TOTPSecret:  sql.NullString{String: secret, Valid: true},
		TOTPEnabled: true,
		IsBanned:    true,
	})

	w := postJSON(h.Login, `{"email":"banned@example.com","password":"correct-horse-password"}`)
	var first struct {
		Data struct {
			PendingToken string `json:"pending_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &first)

	code := totpTestCode(t, secret, time.Now().Unix())
	w2 := postJSON(h.LoginTOTP, fmt.Sprintf(`{"pendingToken":%q,"code":%q}`, first.Data.PendingToken, code))
	if w2.Code != http.StatusOK {
		t.Fatalf("banned user should complete two-step login, status = %d, body=%s", w2.Code, w2.Body.String())
	}
	// 封禁用户不签发 refresh cookie
	if strings.Contains(w2.Header().Get("Set-Cookie"), "fake-refresh") {
		t.Error("refresh token cookie must not be issued for banned users")
	}
}
