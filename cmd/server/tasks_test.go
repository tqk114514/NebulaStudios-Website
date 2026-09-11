package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"auth-system/internal/middleware"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

func TestShouldSkipLog(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/assets/app.js", true},
		{"/policy-content/privacy.md", true},
		{"/avatars/u1.webp", true},
		{"/style.css", true},
		{"/logo.png", true},
		{"/photo.jpg", true},
		{"/favicon.ico", true},
		{"/font.woff", true},
		{"/font.woff2", true},
		{"/", false},
		{"/api/auth/login", false},
		{"/admin/api/stats", false},
		{"/oauth/authorize", false},
	}

	for _, tt := range tests {
		if got := shouldSkipLog(tt.path); got != tt.want {
			t.Errorf("shouldSkipLog(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// 日志中间件应放行请求并按状态码分级记录（此处只断言不干扰响应，日志本身不做断言）
func TestLoggerMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		path   string
		status int
	}{
		{"success", "/api/ok", http.StatusOK},
		{"client error", "/api/bad", http.StatusBadRequest},
		{"server error", "/api/fail", http.StatusInternalServerError},
		{"skipped asset", "/assets/app.js", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(middleware.RequestID())
			r.Use(loggerMiddleware())
			r.GET(tt.path, func(c *gin.Context) { c.Status(tt.status) })

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

// 依赖缺失时后台任务应安全退出，而不是空指针崩溃
func TestBackgroundTasksWithNilDependencies(t *testing.T) {
	runTokenCleanup(nil)
	runTOTPCleanup(nil)
	runUserLogCleanup(nil, nil)
}

// waitFor 轮询等待条件成立，避免用固定 sleep 导致偶发失败
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestRunUserLogCleanupSweeps(t *testing.T) {
	// 首次清扫在启动后立即执行（不等 24h ticker）
	logs := &testutil.FakeUserLogStore{}
	consents := &testutil.FakeUserConsentStore{}

	go runUserLogCleanup(logs, consents)

	if !waitFor(t, time.Second, func() bool { return logs.DeleteExpiredLogsCalls > 0 }) {
		t.Fatal("DeleteExpiredLogs should be called on startup sweep")
	}
	if !waitFor(t, time.Second, func() bool { return consents.DeleteExpiredConsentsCalls > 0 }) {
		t.Error("DeleteExpiredConsents should be called when consent repo is configured")
	}
}

func TestRunUserLogCleanupWithoutConsentRepo(t *testing.T) {
	logs := &testutil.FakeUserLogStore{}

	go runUserLogCleanup(logs, nil)

	if !waitFor(t, time.Second, func() bool { return logs.DeleteExpiredLogsCalls > 0 }) {
		t.Fatal("DeleteExpiredLogs should be called on startup sweep")
	}
}

// 清扫失败只记录日志，不应中断后台任务
func TestRunUserLogCleanupErrors(t *testing.T) {
	logs := &testutil.FakeUserLogStore{DeleteExpiredLogsErr: errCleanupFailed}
	consents := &testutil.FakeUserConsentStore{DeleteExpiredConsentsErr: errCleanupFailed}

	go runUserLogCleanup(logs, consents)

	if !waitFor(t, time.Second, func() bool { return logs.DeleteExpiredLogsCalls > 0 }) {
		t.Fatal("cleanup should be attempted even if it fails")
	}
	if !waitFor(t, time.Second, func() bool { return consents.DeleteExpiredConsentsCalls > 0 }) {
		t.Error("consent cleanup should be attempted even if it fails")
	}
}

func TestStartBackgroundTasks(t *testing.T) {
	logs := &testutil.FakeUserLogStore{}
	consents := &testutil.FakeUserConsentStore{}

	repos := &Repos{UserLogRepo: logs, UserConsentRepo: consents}
	svcs := &Services{
		TokenService: &testutil.FakeTokenManager{},
		TOTPService:  &testutil.FakeTOTPManager{},
	}

	startBackgroundTasks(nil, repos, svcs)

	// 保留期清扫在启动时立即跑一次，可据此确认任务确实被拉起
	if !waitFor(t, time.Second, func() bool { return logs.DeleteExpiredLogsCalls > 0 }) {
		t.Error("retention cleanup should have been started")
	}
}

var errCleanupFailed = errTest("cleanup failed")

type errTest string

func (e errTest) Error() string { return string(e) }
