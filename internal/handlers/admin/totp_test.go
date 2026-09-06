// ResetUserTOTP（超管重置用户两步验证）测试。
package admin

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/middleware"

	"github.com/gin-gonic/gin"
)

func TestResetUserTOTPSuccess(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)
	deps.userRepo.UIDs["target-uid"].TOTPEnabled = true
	deps.userRepo.UIDs["target-uid"].TOTPSecret = sql.NullString{String: "JBSWY3DPEHPK3PXP", Valid: true}

	w := postAdminJSON(h.ResetUserTOTP, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "TOTP reset") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}

	user := deps.userRepo.UIDs["target-uid"]
	if user.TOTPEnabled || user.TOTPSecret.Valid {
		t.Error("TOTP secret and flag should be cleared")
	}
	if !deps.session.RevokedUIDs["target-uid"] {
		t.Error("all sessions should be revoked after TOTP reset")
	}
}

func TestResetUserTOTPNotEnabled(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	w := postAdminJSON(h.ResetUserTOTP, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "No change") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if deps.session.RevokedUIDs["target-uid"] {
		t.Error("sessions should not be revoked when TOTP was not enabled")
	}
}

func TestResetUserTOTPUserNotFound(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)
	delete(deps.userRepo.UIDs, "target-uid")

	w := postAdminJSON(h.ResetUserTOTP, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

// 管理员自身的 TOTP 也可被重置（超管互相支援场景），无自检拦截
func TestResetUserTOTPOwnAccount(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)
	deps.userRepo.UIDs["uid-admin"].TOTPEnabled = true

	// 操作者与目标是同一人（uid-admin）
	r := gin.New()
	r.DELETE("/test/:uid", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-admin")
		h.ResetUserTOTP(c)
	})
	req := httptest.NewRequest(http.MethodDelete, "/test/uid-admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if deps.userRepo.UIDs["uid-admin"].TOTPEnabled {
		t.Error("admin's own TOTP should be cleared")
	}
}
