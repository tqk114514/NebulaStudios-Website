package qrlogin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	testEncryptKey = "test-encrypt-key-for-qr-login"
	testDeriveSalt = "test-derivation-salt"
	testQRToken    = "qr-token-abcdefghijklmnopqrstuvwxyz123456"
	testSessionTok = "session-token-abcdefghijklmnopqrstuvwxyz123456"
)

type qrTestDeps struct {
	qrRepo  *testutil.FakeQRLoginStore
	session *testutil.FakeSessionManager
	ws      *testutil.FakeWebSocket
}

func newTestQR(t *testing.T, configured bool) (*QRLoginHandler, *qrTestDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	deps := &qrTestDeps{
		qrRepo:  &testutil.FakeQRLoginStore{},
		session: &testutil.FakeSessionManager{},
		ws:      &testutil.FakeWebSocket{},
	}

	key := ""
	if configured {
		key = testEncryptKey
	}

	h, err := NewQRLoginHandler(deps.session, deps.ws, deps.qrRepo, key, testDeriveSalt)
	if err != nil {
		t.Fatalf("NewQRLoginHandler() error = %v", err)
	}
	return h, deps
}

// encryptQRToken 用与 handler 相同的派生密钥加密 token（payload: {"t": token}）
func encryptQRToken(t *testing.T, token string) string {
	t.Helper()
	key, err := utils.DeriveKeyFromString(testEncryptKey, testDeriveSalt)
	if err != nil {
		t.Fatalf("DeriveKeyFromString() error = %v", err)
	}
	payload, _ := json.Marshal(map[string]any{"t": token})
	enc, err := utils.EncryptAESGCM(payload, key)
	if err != nil {
		t.Fatalf("EncryptAESGCM() error = %v", err)
	}
	return enc
}

// ---------- Generate ----------

func TestGenerateNotConfigured(t *testing.T) {
	h, _ := newTestQR(t, false)

	w := postQRJSON(h.Generate, `{}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "QR_NOT_CONFIGURED") {
		t.Errorf("want QR_NOT_CONFIGURED, got %s", w.Body.String())
	}
}

func TestGenerateSuccess(t *testing.T) {
	h, deps := newTestQR(t, true)

	w := postQRJSON(h.Generate, `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "token") || !strings.Contains(body, "expireTime") {
		t.Errorf("response missing token/expireTime: %s", body)
	}
	if len(deps.qrRepo.Created) != 1 {
		t.Fatalf("Create not called, got %d calls", len(deps.qrRepo.Created))
	}
	tok := deps.qrRepo.Created[0]
	if tok.Status != QRStatusPending {
		t.Errorf("status = %s, want pending", tok.Status)
	}
	if tok.TokenHash == "" || tok.ExpireTime <= time.Now().UnixMilli() {
		t.Errorf("invalid token hash/expiry in created record")
	}
	// 返回的加密 token 必须能解出原 token（自举验证加密链路）
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("invalid response JSON: %v", err)
	}
	original, err := h.decryptToken(resp.Token)
	if err != nil {
		t.Fatalf("generated token not decryptable: %v", err)
	}
	if original == "" {
		t.Error("decrypted token is empty")
	}
}

func TestGenerateCreateFailed(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.CreateErr = errors.New("db down")

	w := postQRJSON(h.Generate, `{}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "QR_TOKEN_GENERATE_FAILED") {
		t.Errorf("want QR_TOKEN_GENERATE_FAILED, got %s", w.Body.String())
	}
}

// ---------- Scan ----------

func TestScanMissingToken(t *testing.T) {
	h, _ := newTestQR(t, true)

	w := postQRJSON(h.Scan, `{"token":" "}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "MISSING_TOKEN") {
		t.Errorf("want MISSING_TOKEN, got %s", w.Body.String())
	}
}

func TestScanInvalidTokenLength(t *testing.T) {
	h, _ := newTestQR(t, true)

	w := postQRJSON(h.Scan, `{"token":"short"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_TOKEN_FORMAT") {
		t.Errorf("want INVALID_TOKEN_FORMAT, got %s", w.Body.String())
	}
}

func TestScanInvalidEncryptedToken(t *testing.T) {
	h, _ := newTestQR(t, true)

	w := postQRJSON(h.Scan, `{"token":"`+strings.Repeat("x", 40)+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_TOKEN") {
		t.Errorf("want INVALID_TOKEN, got %s", w.Body.String())
	}
}

func TestScanTokenNotFound(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.FindErr = errors.New("not found")
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSON(h.Scan, `{"token":"`+enc+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_NOT_FOUND") {
		t.Errorf("want TOKEN_NOT_FOUND, got %s", w.Body.String())
	}
}

func TestScanExpiredToken(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.FindResult = &models.QRLoginToken{
		TokenHash:   utils.HashToken(testQRToken),
		Status:      QRStatusPending,
		PcIP:        "192.168.3.31",
		PcUserAgent: "Mozilla/5.0",
		ExpireTime:  time.Now().UnixMilli() - 1000,
	}
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSON(h.Scan, `{"token":"`+enc+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_EXPIRED") {
		t.Errorf("want TOKEN_EXPIRED, got %s", w.Body.String())
	}
	if len(deps.qrRepo.Deleted) != 1 {
		t.Errorf("expired token should be deleted, got %v", deps.qrRepo.Deleted)
	}
}

func TestScanSuccess(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.FindResult = &models.QRLoginToken{
		TokenHash:   utils.HashToken(testQRToken),
		Status:      QRStatusPending,
		PcIP:        "192.168.3.31",
		PcUserAgent: "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0",
		ExpireTime:  time.Now().UnixMilli() + 60000,
	}
	deps.qrRepo.UpdateSuccess = true
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSON(h.Scan, `{"token":"`+enc+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "192.168.3.31") {
		t.Errorf("want pcInfo.ip in response, got %s", w.Body.String())
	}
	// 状态迁移必须是 pending -> scanned
	if len(deps.qrRepo.UpdateCalls) != 1 || deps.qrRepo.UpdateCalls[0].From != QRStatusPending || deps.qrRepo.UpdateCalls[0].To != QRStatusScanned {
		t.Errorf("want pending->scanned transition, got %v", deps.qrRepo.UpdateCalls)
	}
	// 状态变化已通知 PC 端
	if len(deps.ws.Notifications) != 1 || deps.ws.Notifications[0]["status"] != "scanned" {
		t.Errorf("want scanned notification, got %v", deps.ws.Notifications)
	}
}

func TestScanAlreadyUsed(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.FindResult = &models.QRLoginToken{
		TokenHash:  utils.HashToken(testQRToken),
		Status:     QRStatusPending,
		ExpireTime: time.Now().UnixMilli() + 60000,
	}
	deps.qrRepo.UpdateSuccess = false
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSON(h.Scan, `{"token":"`+enc+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_ALREADY_USED") {
		t.Errorf("want TOKEN_ALREADY_USED, got %s", w.Body.String())
	}
}

// ---------- MobileConfirm ----------

func TestMobileConfirmNotLoggedIn(t *testing.T) {
	h, _ := newTestQR(t, true)
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSON(h.MobileConfirm, `{"token":"`+enc+`"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "NOT_LOGGED_IN") {
		t.Errorf("want NOT_LOGGED_IN, got %s", w.Body.String())
	}
}

func TestMobileConfirmSuccess(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.session.VerifyResult = &services.Claims{UID: "u1"}
	deps.qrRepo.FindResult = &models.QRLoginToken{
		TokenHash:  utils.HashToken(testQRToken),
		Status:     QRStatusScanned,
		ExpireTime: time.Now().UnixMilli() + 60000,
	}
	deps.qrRepo.ConfirmSuccess = true
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONWithCookie(h.MobileConfirm, `{"token":"`+enc+`"}`, "token="+testSessionTok)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	if len(deps.ws.Notifications) != 1 || deps.ws.Notifications[0]["status"] != "confirmed" {
		t.Errorf("want confirmed notification, got %v", deps.ws.Notifications)
	}
}

func TestMobileConfirmExpired(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.session.VerifyResult = &services.Claims{UID: "u1"}
	deps.qrRepo.FindResult = &models.QRLoginToken{
		TokenHash:  utils.HashToken(testQRToken),
		Status:     QRStatusScanned,
		ExpireTime: time.Now().UnixMilli() - 1000,
	}
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONWithCookie(h.MobileConfirm, `{"token":"`+enc+`"}`, "token="+testSessionTok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_EXPIRED") {
		t.Errorf("want TOKEN_EXPIRED, got %s", w.Body.String())
	}
}

func TestMobileConfirmInvalidClaims(t *testing.T) {
	// VerifyToken 返回 nil claims → 401
	h2, deps2 := newTestQR(t, true)
	deps2.session.VerifyResult = nil
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONWithCookie(h2.MobileConfirm, `{"token":"`+enc+`"}`, "token="+testSessionTok)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_SESSION") {
		t.Errorf("want INVALID_SESSION, got %s", w.Body.String())
	}
}

// ---------- MobileCancel ----------

func TestMobileCancelSuccess(t *testing.T) {
	h, deps := newTestQR(t, true)
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSON(h.MobileCancel, `{"token":"`+enc+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(deps.qrRepo.Deleted) != 1 {
		t.Errorf("token should be deleted, got %v", deps.qrRepo.Deleted)
	}
	if len(deps.ws.Notifications) != 1 || deps.ws.Notifications[0]["status"] != "cancelled" {
		t.Errorf("want cancelled notification, got %v", deps.ws.Notifications)
	}
}

// ---------- Cancel（PC 端） ----------

func TestCancelEmptyToken(t *testing.T) {
	h, _ := newTestQR(t, true)

	w := postQRJSON(h.Cancel, `{"token":""}`)
	// 总是成功以避免信息泄露
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestCancelInvalidToken(t *testing.T) {
	h, deps := newTestQR(t, true)

	w := postQRJSONPath(h.Cancel, strings.Repeat("x", 40), `{}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no info leak)", w.Code)
	}
	if len(deps.qrRepo.Deleted) != 0 {
		t.Errorf("no delete expected for invalid token, got %v", deps.qrRepo.Deleted)
	}
}

func TestCancelSuccess(t *testing.T) {
	h, deps := newTestQR(t, true)
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.Cancel, enc, `{}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(deps.qrRepo.Deleted) != 1 || deps.qrRepo.Deleted[0] != utils.HashToken(testQRToken) {
		t.Errorf("want delete of token hash, got %v", deps.qrRepo.Deleted)
	}
}

// ---------- SetSession ----------

func TestSetSessionMissingParams(t *testing.T) {
	h, _ := newTestQR(t, true)

	w := postQRJSON(h.SetSession, `{"sessionToken":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "MISSING_TOKEN") {
		t.Errorf("want MISSING_TOKEN, got %s", w.Body.String())
	}
}

func TestSetSessionInvalidToken(t *testing.T) {
	h, _ := newTestQR(t, true)

	w := postQRJSONPath(h.SetSession, strings.Repeat("x", 40), `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_TOKEN") {
		t.Errorf("want INVALID_TOKEN, got %s", w.Body.String())
	}
}

func TestSetSessionExpired(t *testing.T) {
	h, deps := newTestQR(t, true)
	// 会话 Token 先于消费验证（避免验证失败烧掉二维码），需先通过 JWT 校验
	deps.session.VerifyResult = &services.Claims{UID: "u1"}
	deps.qrRepo.ConsumeErr = errors.New("TOKEN_EXPIRED")
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.SetSession, enc, `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_EXPIRED") {
		t.Errorf("want TOKEN_EXPIRED, got %s", w.Body.String())
	}
}

func TestSetSessionAlreadyUsed(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.session.VerifyResult = &services.Claims{UID: "u1"}
	deps.qrRepo.ConsumeErr = errors.New("invalid token status")
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.SetSession, enc, `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_ALREADY_USED") {
		t.Errorf("want TOKEN_ALREADY_USED, got %s", w.Body.String())
	}
}

func TestSetSessionInvalidSessionErr(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.ConsumeErr = errors.New("INVALID_SESSION")
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.SetSession, enc, `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_SESSION") {
		t.Errorf("want INVALID_SESSION, got %s", w.Body.String())
	}
}

func TestSetSessionInvalidUserErr(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.ConsumeErr = errors.New("INVALID_USER")
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.SetSession, enc, `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_SESSION") {
		t.Errorf("want INVALID_SESSION, got %s", w.Body.String())
	}
}

func TestSetSessionNotFoundErr(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.session.VerifyResult = &services.Claims{UID: "u1"}
	deps.qrRepo.ConsumeErr = errors.New("some other error")
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.SetSession, enc, `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_NOT_FOUND") {
		t.Errorf("want TOKEN_NOT_FOUND (default), got %s", w.Body.String())
	}
}

func TestSetSessionClaimsMismatch(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.ConsumeUserUID = "u1"
	// Consume 返回 u1，但 session token 的 claims 是 u2 → 拒绝（authz 检查）
	deps.session.VerifyResult = &services.Claims{UID: "u2"}
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.SetSession, enc, `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_SESSION") {
		t.Errorf("want INVALID_SESSION, got %s", w.Body.String())
	}
}

func TestSetSessionVerifyErr(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.ConsumeUserUID = "u1"
	deps.session.VerifyErr = errors.New("bad token")
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.SetSession, enc, `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_SESSION") {
		t.Errorf("want INVALID_SESSION, got %s", w.Body.String())
	}
}

func TestSetSessionSuccess(t *testing.T) {
	h, deps := newTestQR(t, true)
	deps.qrRepo.ConsumeUserUID = "u1"
	deps.session.VerifyResult = &services.Claims{UID: "u1"}
	enc := encryptQRToken(t, testQRToken)

	w := postQRJSONPath(h.SetSession, enc, `{"sessionToken":"`+testSessionTok+`"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
}

// ---------- helpers ----------

func postQRJSON(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	return postQRJSONWithCookie(h, body, "")
}

func postQRJSONWithCookie(h gin.HandlerFunc, body, cookie string) *httptest.ResponseRecorder {
	r := gin.New()
	r.POST("/test", h)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// postQRJSONPath 以 JSON body + 路径参数调用 handler（:token 路径参数型端点）
func postQRJSONPath(h gin.HandlerFunc, pathToken, body, cookie string) *httptest.ResponseRecorder {
	r := gin.New()
	r.POST("/test/:token", h)
	url := "/test"
	if pathToken != "" {
		url += "/" + pathToken
	}
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
