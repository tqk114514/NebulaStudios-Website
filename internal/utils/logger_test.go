package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newTestZapLogger 构造写入内存 buffer 的 JSON zapLogger，用于断言输出结构
func newTestZapLogger() (*zapLogger, *bytes.Buffer) {
	var buf bytes.Buffer
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zapcore.EncoderConfig{
			TimeKey:    "time",
			LevelKey:   "level",
			MessageKey: "msg",
		}),
		zapcore.AddSync(&buf),
		zapcore.DebugLevel,
	)
	zl := zap.New(core)
	return &zapLogger{zap: zl, sugar: zl.Sugar()}, &buf
}

func decodeLogLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("invalid JSON log line %q: %v", line, err)
	}
	return m
}

func TestZapLoggerStructuredFields(t *testing.T) {
	l, buf := newTestZapLogger()
	l.Info("CACHE", "user loaded", "uid", "abc123", "count", 3)

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if m["msg"] != "user loaded" {
		t.Errorf("msg = %v, want %q", m["msg"], "user loaded")
	}
	if m["category"] != "CACHE" {
		t.Errorf("category = %v, want %q", m["category"], "CACHE")
	}
	if m["uid"] != "abc123" {
		t.Errorf("uid = %v, want %q", m["uid"], "abc123")
	}
	if m["count"] != float64(3) {
		t.Errorf("count = %v, want 3", m["count"])
	}
}

func TestZapLoggerFieldMasking(t *testing.T) {
	l, buf := newTestZapLogger()
	l.Info("USER", "user created", "email", "alice@example.com", "ip", "192.168.1.100")

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if m["email"] != "a***@e***.com" {
		t.Errorf("email field = %v, want masked %q", m["email"], "a***@e***.com")
	}
	if m["ip"] != "192.168.***.***" {
		t.Errorf("ip field = %v, want masked %q", m["ip"], "192.168.***.***")
	}
}

func TestZapLoggerMessageMasking(t *testing.T) {
	l, buf := newTestZapLogger()
	l.Info("MAIL", "email sent to alice@example.com")

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if strings.Contains(m["msg"].(string), "alice@example.com") {
		t.Errorf("msg = %q, should be masked", m["msg"])
	}
}

// TestZapLoggerLegacyContextFolded 验证旧式调用（奇数个参数，末尾裸字符串）不丢信息：
// 裸字符串被并入 message 而非作为悬空 key 丢弃。
func TestZapLoggerLegacyContextFolded(t *testing.T) {
	l, buf := newTestZapLogger()
	l.Warn("CACHE", "invalid uid for Get", "uid=abc123")

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if m["msg"] != "invalid uid for Get: uid=abc123" {
		t.Errorf("msg = %v, want folded context", m["msg"])
	}
	if _, ok := m["uid=abc123"]; ok {
		t.Error("bare string context should not be emitted as a key")
	}
}

func TestLogErrorStructured(t *testing.T) {
	prev := loggerInstance
	t.Cleanup(func() { loggerInstance = prev })

	l, buf := newTestZapLogger()
	SetLogger(l)

	orig := errors.New("boom")
	err := LogError("TEST", "DoThing", orig, "uid", "u1")
	if err == nil {
		t.Fatal("LogError with non-nil error should return wrapped error")
	}
	if !errors.Is(err, orig) {
		t.Error("wrapped error should unwrap to original")
	}

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if m["category"] != "TEST" {
		t.Errorf("category = %v, want TEST", m["category"])
	}
	if m["msg"] != "DoThing failed" {
		t.Errorf("msg = %v, want %q", m["msg"], "DoThing failed")
	}
	if m["error"] != "boom" {
		t.Errorf("error field = %v, want %q", m["error"], "boom")
	}
	if m["uid"] != "u1" {
		t.Errorf("uid = %v, want %q", m["uid"], "u1")
	}
}

func TestLogErrorNil(t *testing.T) {
	if err := LogError("TEST", "Op", nil); err != nil {
		t.Errorf("nil error should return nil, got %v", err)
	}
}

// withTestLogger 将测试 zapLogger 设为全局实例，返回恢复函数
func withTestLogger(l Logger) func() {
	prev := loggerInstance
	loggerInstance = l
	return func() { loggerInstance = prev }
}

func TestZapLoggerWithAttachesFields(t *testing.T) {
	l, buf := newTestZapLogger()
	l.With(RequestIDKey, "req-abc").Info("HTTP", "Request", "path", "/x")

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if m[RequestIDKey] != "req-abc" {
		t.Errorf("request_id field = %v, want %q", m[RequestIDKey], "req-abc")
	}
	if m["category"] != "HTTP" {
		t.Errorf("category = %v, want HTTP", m["category"])
	}
	if m["path"] != "/x" {
		t.Errorf("path = %v, want /x", m["path"])
	}
}

func TestLoggerFromContextAttachesRequestID(t *testing.T) {
	l, buf := newTestZapLogger()
	restore := withTestLogger(l)
	defer restore()

	ctx := WithRequestID(context.Background(), "req-abc-123")
	LoggerFromContext(ctx).Info("AUTH", "User logged in")

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if m[RequestIDKey] != "req-abc-123" {
		t.Errorf("request_id field = %v, want %q", m[RequestIDKey], "req-abc-123")
	}
	if m["category"] != "AUTH" {
		t.Errorf("category = %v, want AUTH", m["category"])
	}
}

func TestLoggerFromContextWithoutIDNoField(t *testing.T) {
	l, buf := newTestZapLogger()
	restore := withTestLogger(l)
	defer restore()

	LoggerFromContext(context.Background()).Info("AUTH", "msg")
	if strings.Contains(buf.String(), RequestIDKey) {
		t.Errorf("no request_id in ctx: output should not contain request_id, got %s", buf.String())
	}
}

func TestLogInfoCtxAttachesRequestID(t *testing.T) {
	l, buf := newTestZapLogger()
	restore := withTestLogger(l)
	defer restore()

	ctx := WithRequestID(context.Background(), "req-xyz")
	LogInfoCtx(ctx, "HTTP", "Request", "status", 200)

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if m[RequestIDKey] != "req-xyz" {
		t.Errorf("request_id field = %v, want %q", m[RequestIDKey], "req-xyz")
	}
	if m["status"] != float64(200) {
		t.Errorf("status = %v, want 200", m["status"])
	}
}

func TestLogErrorCtxAttachesRequestID(t *testing.T) {
	l, buf := newTestZapLogger()
	restore := withTestLogger(l)
	defer restore()

	orig := errors.New("boom")
	err := LogErrorCtx(WithRequestID(context.Background(), "req-err"), "TEST", "Op", orig, "uid", "u1")
	if err == nil || !errors.Is(err, orig) {
		t.Fatal("LogErrorCtx should wrap the original error")
	}

	m := decodeLogLine(t, strings.TrimSpace(buf.String()))
	if m[RequestIDKey] != "req-err" {
		t.Errorf("request_id field = %v, want %q", m[RequestIDKey], "req-err")
	}
	if m["error"] != "boom" {
		t.Errorf("error field = %v, want %q", m["error"], "boom")
	}
}

func TestLogErrorCtxNil(t *testing.T) {
	if err := LogErrorCtx(context.Background(), "TEST", "Op", nil); err != nil {
		t.Errorf("nil error should return nil, got %v", err)
	}
}

func TestNewRequestIDIsValidAndUnique(t *testing.T) {
	seen := make(map[string]bool, 100)
	for range 100 {
		id := NewRequestID()
		if !ValidRequestID(id) {
			t.Fatalf("NewRequestID returned invalid id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate request_id %q", id)
		}
		seen[id] = true
	}
}

func TestValidRequestID(t *testing.T) {
	valid := []string{"abcdef12", "ABC-DEF_123", "0123456789abcdef", "a-b_c-d-e-f-g-h"}
	for _, id := range valid {
		if !ValidRequestID(id) {
			t.Errorf("ValidRequestID(%q) = false, want true", id)
		}
	}

	invalid := []string{"", "short", "abc\ndef", "a b", "含中文", "a.b", "abc:def"}
	for _, id := range invalid {
		if ValidRequestID(id) {
			t.Errorf("ValidRequestID(%q) = true, want false", id)
		}
	}
}
