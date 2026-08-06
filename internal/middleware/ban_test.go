package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// runBan 用 BanCheckMiddleware + 可选的 Auth 前置挂载测试 handler
func runBan(mw gin.HandlerFunc, method, path, cookie string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(mw)
	r.Any("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	req := httptest.NewRequest(method, path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestBanCheckNotLoggedIn(t *testing.T) {
	cache := &testutil.FakeUserCache{}
	repo := testutil.NewFakeUserRepo()
	sess := &testutil.FakeSessionManager{}

	w := runBan(BanCheckMiddleware(cache, repo, sess), http.MethodGet, "/test", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (未登录放行)", w.Code)
	}
}

func TestBanCheckBannedUser(t *testing.T) {
	cache := &testutil.FakeUserCache{}
	repo := testutil.NewFakeUserRepo()
	repo.Seed(&models.User{
		UID:       "u1",
		Username:  "alice",
		Email:     "a@b.c",
		IsBanned:  true,
		BanReason: sql.NullString{String: "spam", Valid: true},
	})
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}

	w := runBan(BanCheckMiddleware(cache, repo, sess), http.MethodGet, "/test", "token=valid")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "USER_BANNED") {
		t.Errorf("want USER_BANNED, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "spam") {
		t.Errorf("want banReason in response, got %s", w.Body.String())
	}
}

func TestBanCheckPermanentBanFlag(t *testing.T) {
	cache := &testutil.FakeUserCache{}
	repo := testutil.NewFakeUserRepo()
	repo.Seed(&models.User{
		UID:      "u1",
		Username: "alice",
		Email:    "a@b.c",
		IsBanned: true, // UnbanAt 无效 → 永久封禁
	})
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}

	w := runBan(BanCheckMiddleware(cache, repo, sess), http.MethodGet, "/test", "token=valid")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"permanent":true`) {
		t.Errorf("want permanent flag, got %s", w.Body.String())
	}
}

func TestBanCheckExpiredBanAutoUnban(t *testing.T) {
	cache := &testutil.FakeUserCache{}
	repo := testutil.NewFakeUserRepo()
	repo.Seed(&models.User{
		UID:      "u1",
		Username: "alice",
		Email:    "a@b.c",
		IsBanned: true,
		UnbanAt:  sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true}, // 已过期
	})
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}

	w := runBan(BanCheckMiddleware(cache, repo, sess), http.MethodGet, "/test", "token=valid")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (过期封禁放行)", w.Code)
	}

	// 自动解封是异步 goroutine，WaitAutoUnban 提供 happens-before 同步（避免轮询竞态）
	WaitAutoUnban()
	if len(repo.UnbanCalls) != 1 || repo.UnbanCalls[0] != "u1" {
		t.Errorf("want auto-unban called for u1, got %v", repo.UnbanCalls)
	}
}

func TestBanCheckNormalUser(t *testing.T) {
	cache := &testutil.FakeUserCache{}
	repo := testutil.NewFakeUserRepo()
	repo.Seed(&models.User{UID: "u1", Username: "alice", Email: "a@b.c"})
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}

	w := runBan(BanCheckMiddleware(cache, repo, sess), http.MethodGet, "/test", "token=valid")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(repo.UnbanCalls) != 0 {
		t.Errorf("no unban expected for normal user, got %v", repo.UnbanCalls)
	}
}
