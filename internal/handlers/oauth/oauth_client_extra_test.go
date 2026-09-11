package oauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"

	"github.com/gin-gonic/gin"
)

// serveWithUID 直接构造上下文并注入 uid（可注入空串，覆盖"有 uid 但为空"的分支）
func serveWithUID(uid string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/unlink", nil)
	c.Set(middleware.ContextKeyUID, uid)
	handler(c)
	return rec
}

// 上下文里 uid 是空串（有别于完全没有 uid）同样视为未认证
func TestUnlinkEmptyUID(t *testing.T) {
	d := newExternalProvider(t)

	rec := serveWithUID("", d.handler.Unlink)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// 解绑写库失败必须返回 500，不能让调用方以为已解绑
func TestUnlinkUpdateFailure(t *testing.T) {
	d := newExternalProvider(t)
	d.userRepo.Seed(&models.User{UID: "u1", Username: "alice"})
	d.userRepo.UpdateErr = errors.New("update failed")

	rec := serveExternal(http.MethodPost, "/unlink", "/unlink", nil, "u1", d.handler.Unlink)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "UNLINK_FAILED") {
		t.Errorf("body = %s, want UNLINK_FAILED", rec.Body.String())
	}
}
