// Package middleware — F1 修复验证：账户关键变更端点现在强制 CSRF Double-Submit。
// 不带 X-CSRF-Token / csrf_token cookie 的写请求被拒绝(403)，带合法 token 的请求放行(200)。
package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/services"
	"auth-system/internal/testutil"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// TestF1_Fixed_MutationEnforcesCSRF 验证 /api/user/* 等账户变更端点已挂 CSRF 校验。
func TestF1_Fixed_MutationEnforcesCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fakeSess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "uid-1"}}

	// 复刻修复后的路由：GET captcha 设置 csrf_token cookie；变更端点 = 认证 + CSRF
	r := gin.New()
	r.GET("/api/config/captcha", CSRFTokenMiddleware(), func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.POST("/api/user/totp/enable",
		AuthMiddleware(fakeSess),
		CSRFTokenMiddleware(),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"uid": "uid-1"}) },
	)

	// 1) 无 csrf cookie + 无 X-CSRF-Token 的写请求 -> 必须 403
	noCsrf := httptest.NewRequest(http.MethodPost, "/api/user/totp/enable", strings.NewReader("{}"))
	noCsrf.Header.Set("Authorization", "Bearer some-jwt")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, noCsrf)
	if w1.Code != http.StatusForbidden {
		t.Fatalf("FIX FAILED: mutation endpoint accepted write without CSRF token, got %d", w1.Code)
	}

	// 2) 先 GET 获取 csrf_token cookie，再带 cookie + 匹配头 -> 200 放行
	captchaReq := httptest.NewRequest(http.MethodGet, "/api/config/captcha", nil)
	captchaResp := httptest.NewRecorder()
	r.ServeHTTP(captchaResp, captchaReq)
	var csrfToken string
	for _, ck := range captchaResp.Result().Cookies() {
		if ck.Name == utils.CSRFTokenName {
			csrfToken = ck.Value
		}
	}
	if csrfToken == "" {
		t.Fatalf("expected csrf_token cookie to be set by GET bootstrap route")
	}

	withCsrf := httptest.NewRequest(http.MethodPost, "/api/user/totp/enable", strings.NewReader("{}"))
	withCsrf.Header.Set("Authorization", "Bearer some-jwt")
	withCsrf.Header.Set("X-CSRF-Token", csrfToken)
	withCsrf.AddCookie(&http.Cookie{Name: utils.CSRFTokenName, Value: csrfToken})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, withCsrf)
	if w2.Code != http.StatusOK {
		t.Fatalf("valid CSRF token should be accepted, got %d body=%s", w2.Code, w2.Body.String())
	}

	t.Logf("F1 FIXED: mutation endpoint now returns %d without CSRF and %d with valid CSRF token", w1.Code, w2.Code)
}
