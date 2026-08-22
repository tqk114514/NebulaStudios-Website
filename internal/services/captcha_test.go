package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"auth-system/internal/config"
)

// fakeTransport 拦截所有 HTTP 请求并返回模拟 Turnstile 响应
type fakeTransport struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (f fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f.fn(req)
}

// newFakeCaptcha 构造已启用但走 fakeTransport 的验证服务
func newFakeCaptcha(fn func(req *http.Request) (*http.Response, error)) *CaptchaService {
	usedTokens, _ := lru.New[string, time.Time](100)
	s := &CaptchaService{
		siteKey:    "test-site-key",
		secretKey:  "test-secret-key",
		enabled:    true,
		usedTokens: usedTokens,
	}
	s.client = &http.Client{Transport: fakeTransport{fn: fn}, Timeout: 2 * time.Second}
	return s
}

func turnstileResponse(t *testing.T, success bool, errCodes []string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"success":     success,
		"error-codes": errCodes,
		"hostname":    "example.com",
	})
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestCaptchaVerifySuccess(t *testing.T) {
	s := newFakeCaptcha(func(req *http.Request) (*http.Response, error) {
		// 断言请求目标与构造：真实 URL、POST 方法
		if req.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.Method)
		}
		if req.URL.String() != captchaVerifyURL {
			t.Errorf("URL = %s, want %s", req.URL, captchaVerifyURL)
		}
		// 断言请求体包含 secret/response/remoteip
		body, _ := io.ReadAll(req.Body)
		for _, want := range []string{"test-secret-key", "tok-123", "1.2.3.4"} {
			if !strings.Contains(string(body), want) {
				t.Errorf("request body missing %q: %s", want, body)
			}
		}
		if req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", req.Header.Get("Content-Type"))
		}
		return turnstileResponse(t, true, nil), nil
	})

	if err := s.Verify("tok-123", "1.2.3.4"); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestCaptchaVerifyFailed(t *testing.T) {
	s := newFakeCaptcha(func(req *http.Request) (*http.Response, error) {
		return turnstileResponse(t, false, []string{"invalid-input-response"}), nil
	})

	err := s.Verify("tok-123", "")
	if !errors.Is(err, ErrCaptchaFailed) {
		t.Fatalf("Verify() error = %v, want ErrCaptchaFailed", err)
	}
}

func TestCaptchaVerifyReplayDetected(t *testing.T) {
	s := newFakeCaptcha(func(req *http.Request) (*http.Response, error) {
		return turnstileResponse(t, true, nil), nil
	})

	if err := s.Verify("tok-123", ""); err != nil {
		t.Fatalf("first Verify() error = %v", err)
	}
	// 同一 token 复用 → 本地防重放拒绝
	err := s.Verify("tok-123", "")
	if !errors.Is(err, ErrCaptchaFailed) {
		t.Fatalf("replay Verify() error = %v, want ErrCaptchaFailed", err)
	}
}

func TestCaptchaVerifyFailureRollsBackReservation(t *testing.T) {
	call := 0
	s := newFakeCaptcha(func(req *http.Request) (*http.Response, error) {
		call++
		if call == 1 {
			return turnstileResponse(t, false, []string{"internal-error"}), nil
		}
		return turnstileResponse(t, true, nil), nil
	})

	if err := s.Verify("tok-123", ""); err == nil {
		t.Fatal("first Verify() should fail")
	}
	// 失败回滚预占，同 token 可重试
	if err := s.Verify("tok-123", ""); err != nil {
		t.Fatalf("retry Verify() error = %v, want success (预占已回滚)", err)
	}
}

func TestCaptchaVerifyNon200(t *testing.T) {
	s := newFakeCaptcha(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})

	err := s.Verify("tok-123", "")
	if !errors.Is(err, ErrCaptchaFailed) {
		t.Fatalf("Verify() error = %v, want ErrCaptchaFailed (非 200)", err)
	}
}

func TestCaptchaVerifyNetworkError(t *testing.T) {
	s := newFakeCaptcha(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})

	err := s.Verify("tok-123", "")
	if !errors.Is(err, ErrCaptchaNetworkErr) {
		t.Fatalf("Verify() error = %v, want ErrCaptchaNetworkErr", err)
	}
}

func TestCaptchaVerifyNotEnabled(t *testing.T) {
	usedTokens, _ := lru.New[string, time.Time](10)
	s := &CaptchaService{enabled: false, usedTokens: usedTokens}
	// CAPTCHA_ENABLED=false：空 token / 任意 token 一律放行
	if err := s.Verify("", ""); err != nil {
		t.Fatalf("Verify(empty) error = %v, want nil (disabled)", err)
	}
	if err := s.Verify("tok-123", ""); err != nil {
		t.Fatalf("Verify() error = %v, want nil (disabled)", err)
	}
	if s.IsEnabled() {
		t.Error("IsEnabled() = true for disabled service")
	}
	if s.GetSiteKey() != "" {
		t.Error("GetSiteKey() should be empty for disabled service")
	}
}

func TestNewCaptchaServiceDisabled(t *testing.T) {
	s, err := NewCaptchaService(&config.Config{CaptchaEnabled: false})
	if err != nil {
		t.Fatalf("NewCaptchaService(disabled) error = %v", err)
	}
	if s.IsEnabled() {
		t.Error("IsEnabled() = true for disabled service")
	}
	if err := s.Verify("anything", "1.2.3.4"); err != nil {
		t.Errorf("Verify() error = %v, want nil (disabled)", err)
	}
}

func TestNewCaptchaServiceEnabledWithoutKeys(t *testing.T) {
	if _, err := NewCaptchaService(&config.Config{CaptchaEnabled: true}); err == nil {
		t.Fatal("NewCaptchaService(enabled, no keys) should return error")
	}
}

func TestNewCaptchaServiceNilConfig(t *testing.T) {
	if _, err := NewCaptchaService(nil); err == nil {
		t.Fatal("NewCaptchaService(nil) should return error")
	}
}

func TestCaptchaVerifyEmptyToken(t *testing.T) {
	usedTokens, _ := lru.New[string, time.Time](10)
	s := &CaptchaService{enabled: true, usedTokens: usedTokens}

	err := s.Verify("", "")
	if !errors.Is(err, ErrCaptchaEmptyToken) {
		t.Fatalf("Verify(empty) error = %v, want ErrCaptchaEmptyToken", err)
	}
	err = s.Verify("   ", "")
	if !errors.Is(err, ErrCaptchaEmptyToken) {
		t.Fatalf("Verify(whitespace) error = %v, want ErrCaptchaEmptyToken", err)
	}
}

func TestCaptchaVerifyWithContextCancel(t *testing.T) {
	s := newFakeCaptcha(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 已取消的 ctx
	err := s.VerifyWithContext(ctx, "tok-123", "")
	if !errors.Is(err, ErrCaptchaNetworkErr) && !errors.Is(err, ErrCaptchaTimeout) {
		t.Fatalf("VerifyWithContext() error = %v, want network/timeout error", err)
	}
}

func TestFormatCaptchaErrorCodes(t *testing.T) {
	if got := formatCaptchaErrorCodes(nil); got != "unknown error" {
		t.Errorf("nil codes = %q, want unknown error", got)
	}
	got := formatCaptchaErrorCodes([]string{"invalid-input-response", "unknown-code"})
	if !strings.Contains(got, "Token is invalid or malformed") || !strings.Contains(got, "unknown-code") {
		t.Errorf("formatCaptchaErrorCodes() = %q", got)
	}
}

func TestTruncateCaptchaKey(t *testing.T) {
	if truncateCaptchaKey("", 8) != "(empty)" {
		t.Error("empty key should return (empty)")
	}
	if truncateCaptchaKey("short", 8) != "short" {
		t.Error("short key should pass through")
	}
	if truncateCaptchaKey("abcdefghijkl", 8) != "abcdefgh..." {
		t.Error("long key should be truncated")
	}
}
