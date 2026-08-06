package services

import (
	"testing"
	"time"
)

func newExportTokenForTest(t *testing.T) *ExportTokenService {
	t.Helper()
	svc, err := NewExportTokenService()
	if err != nil {
		t.Fatalf("NewExportTokenService() error = %v", err)
	}
	t.Cleanup(svc.Stop)
	return svc
}

func TestExportTokenGenerateAndConsume(t *testing.T) {
	svc := newExportTokenForTest(t)

	token, err := svc.Generate("u1")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(token) != 64 { // 32 字节 hex
		t.Errorf("token length = %d, want 64", len(token))
	}

	uid, ok := svc.ValidateAndConsume(token)
	if !ok || uid != "u1" {
		t.Fatalf("ValidateAndConsume() = (%q, %v), want (u1, true)", uid, ok)
	}
}

func TestExportTokenOneTimeUse(t *testing.T) {
	svc := newExportTokenForTest(t)

	token, _ := svc.Generate("u1")
	svc.ValidateAndConsume(token)

	// 一次性：第二次消费必须失败（防重放）
	if _, ok := svc.ValidateAndConsume(token); ok {
		t.Error("ValidateAndConsume() second time = true, want false (一次性)")
	}
}

func TestExportTokenInvalidToken(t *testing.T) {
	svc := newExportTokenForTest(t)

	if _, ok := svc.ValidateAndConsume("nonexistent-token"); ok {
		t.Error("ValidateAndConsume(invalid) = true, want false")
	}
}

func TestExportTokenExpired(t *testing.T) {
	svc := newExportTokenForTest(t)

	token, _ := svc.Generate("u1")
	// 手工把过期时间改为过去（同包可访问私有字段）
	entry, ok := svc.cache.Get(token)
	if !ok {
		t.Fatal("token not in cache")
	}
	entry.ExpiresAt = time.Now().Add(-time.Minute)

	if _, ok := svc.ValidateAndConsume(token); ok {
		t.Error("ValidateAndConsume(expired) = true, want false")
	}
}

func TestExportTokenCleanupExpired(t *testing.T) {
	svc := newExportTokenForTest(t)

	token, _ := svc.Generate("u1")
	entry, _ := svc.cache.Get(token)
	entry.ExpiresAt = time.Now().Add(-time.Minute)

	svc.cleanupExpired()
	if _, exists := svc.cache.Get(token); exists {
		t.Error("expired token should be removed by cleanup")
	}
}

func TestExportTokenStopIdempotent(t *testing.T) {
	svc := newExportTokenForTest(t)

	svc.Stop()
	svc.Stop() // 幂等，不应 panic
}
