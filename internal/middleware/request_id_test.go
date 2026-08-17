package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

func TestRequestIDGeneratesAndEchoes(t *testing.T) {
	w := runMW(RequestID(), http.MethodGet, "/test", "", nil)

	id := w.Header().Get(RequestIDHeader)
	if !utils.ValidRequestID(id) {
		t.Fatalf("want valid generated request_id, got %q", id)
	}
	// 生成的 ID 为 16 字节随机数编码的 32 位十六进制
	if len(id) != 32 {
		t.Errorf("want 32-char hex request_id, got %q (len %d)", id, len(id))
	}
}

func TestRequestIDHonorsIncoming(t *testing.T) {
	incoming := "client-trace-abc-123"
	w := runMW(RequestID(), http.MethodGet, "/test", "", map[string]string{RequestIDHeader: incoming})

	if got := w.Header().Get(RequestIDHeader); got != incoming {
		t.Errorf("want incoming request_id echoed back %q, got %q", incoming, got)
	}
}

func TestRequestIDRejectsInvalidIncoming(t *testing.T) {
	// 含换行/控制字符的 header 不应被透传（防日志注入）
	attack := "abc\nX-Evil: injected"
	w := runMW(RequestID(), http.MethodGet, "/test", "", map[string]string{RequestIDHeader: attack})

	got := w.Header().Get(RequestIDHeader)
	if got == attack {
		t.Fatal("invalid incoming request_id must not be passed through")
	}
	if !utils.ValidRequestID(got) {
		t.Fatalf("want regenerated valid request_id, got %q", got)
	}
}

func TestRequestIDAvailableInContext(t *testing.T) {
	r := gin.New()
	var ginID, ctxID string
	r.Use(RequestID())
	r.GET("/test", func(c *gin.Context) {
		ginID = GetRequestID(c)
		ctxID = utils.RequestIDFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))

	headerID := w.Header().Get(RequestIDHeader)
	if ginID == "" || ginID != headerID {
		t.Errorf("GetRequestID(c) = %q, want %q", ginID, headerID)
	}
	if ctxID == "" || ctxID != headerID {
		t.Errorf("RequestIDFrom(ctx) = %q, want %q", ctxID, headerID)
	}
}

// TestRequestIDAbsentWithoutMiddleware GetRequestID 在未经过中间件时返回空串，不 panic
func TestRequestIDAbsentWithoutMiddleware(t *testing.T) {
	r := gin.New()
	var got string
	r.GET("/test", func(c *gin.Context) {
		got = GetRequestID(c)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	if got != "" {
		t.Errorf("want empty request_id without middleware, got %q", got)
	}
}
