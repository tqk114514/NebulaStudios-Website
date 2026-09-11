package services

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateOTACInvalidatesPrevious(t *testing.T) {
	svc := NewExportService()

	req1, code1, expires1 := svc.GenerateOTAC("admin-1")
	if req1 == "" || code1 == "" {
		t.Fatalf("GenerateOTAC = %q, %q; want non-empty", req1, code1)
	}
	if !expires1.After(time.Now()) {
		t.Errorf("expiresAt = %v, want future time", expires1)
	}

	// 再次生成：旧 OTAC 立即失效（同一时刻只允许一个有效 OTAC）
	req2, _, _ := svc.GenerateOTAC("admin-1")
	if req2 == req1 {
		t.Fatal("second GenerateOTAC should produce a new request ID")
	}
	if err := svc.ValidateOTAC(req1, code1, "admin-1"); err == nil {
		t.Error("previous OTAC should no longer be valid")
	}
	if err := svc.ValidateOTAC(req2, svc.currentOTAC.Code, "admin-1"); err != nil {
		t.Errorf("current OTAC should be valid: %v", err)
	}
}

func TestValidateOTACRejectsMismatches(t *testing.T) {
	svc := NewExportService()
	if err := svc.ValidateOTAC("any", "code", "admin-1"); err == nil {
		t.Error("without active OTAC should fail")
	}

	req, code, _ := svc.GenerateOTAC("admin-1")

	if err := svc.ValidateOTAC("wrong-request", code, "admin-1"); err == nil ||
		!strings.Contains(err.Error(), "request ID mismatch") {
		t.Errorf("request ID mismatch error = %v", err)
	}

	// OTAC 绑定生成者：他人不得使用
	if err := svc.ValidateOTAC(req, code, "admin-2"); err == nil ||
		!strings.Contains(err.Error(), "user mismatch") {
		t.Errorf("user mismatch error = %v", err)
	}

	// 错误码：前两次提示剩余尝试次数，第三次直接作废
	if err := svc.ValidateOTAC(req, "wrong-code", "admin-1"); err == nil ||
		!strings.Contains(err.Error(), "attempt 1/3") {
		t.Errorf("first failure error = %v", err)
	}
	if err := svc.ValidateOTAC(req, "wrong-code", "admin-1"); err == nil ||
		!strings.Contains(err.Error(), "attempt 2/3") {
		t.Errorf("second failure error = %v", err)
	}
	if err := svc.ValidateOTAC(req, "wrong-code", "admin-1"); err == nil ||
		!strings.Contains(err.Error(), "invalidated") {
		t.Errorf("third failure error = %v", err)
	}
	// 作废后即使码正确也不再有效
	if err := svc.ValidateOTAC(req, code, "admin-1"); err == nil {
		t.Error("OTAC should be invalidated after max attempts")
	}
}

// 过期的 OTAC 应被清除并返回过期错误
func TestValidateOTACExpired(t *testing.T) {
	svc := NewExportService()
	req, code, _ := svc.GenerateOTAC("admin-1")

	svc.mu.Lock()
	svc.currentOTAC.CreatedAt = time.Now().Add(-otacTTL - time.Minute)
	svc.mu.Unlock()

	err := svc.ValidateOTAC(req, code, "admin-1")
	if err == nil || !strings.Contains(err.Error(), "OTAC expired") {
		t.Fatalf("error = %v, want OTAC expired", err)
	}
	if svc.currentOTAC != nil {
		t.Error("expired OTAC should be cleared")
	}
}

// 验证成功后一次性销毁，防止重放
func TestValidateOTACConsumed(t *testing.T) {
	svc := NewExportService()
	req, code, _ := svc.GenerateOTAC("admin-1")

	if err := svc.ValidateOTAC(req, code, "admin-1"); err != nil {
		t.Fatalf("valid OTAC rejected: %v", err)
	}
	if err := svc.ValidateOTAC(req, code, "admin-1"); err == nil {
		t.Error("OTAC must be single-use")
	}
}

func TestRevokeOTAC(t *testing.T) {
	svc := NewExportService()
	req, code, _ := svc.GenerateOTAC("admin-1")

	svc.RevokeOTAC()

	if err := svc.ValidateOTAC(req, code, "admin-1"); err == nil {
		t.Error("revoked OTAC should not validate")
	}
	if svc.currentOTAC != nil {
		t.Error("currentOTAC should be cleared after revoke")
	}
}

func TestStoreAndRetrieveFile(t *testing.T) {
	svc := NewExportService()

	token := svc.StoreFile([]byte("backup-content"), "backup.enc")
	if token == "" {
		t.Fatal("StoreFile should return a token")
	}

	data, filename, err := svc.RetrieveFile(token)
	if err != nil {
		t.Fatalf("RetrieveFile: %v", err)
	}
	if string(data) != "backup-content" || filename != "backup.enc" {
		t.Errorf("got %q/%q, want backup-content/backup.enc", data, filename)
	}

	// 一次性：再次取用应失败
	if _, _, err := svc.RetrieveFile(token); err == nil {
		t.Error("file token should be single-use")
	}
	if _, _, err := svc.RetrieveFile("unknown-token"); err == nil {
		t.Error("unknown token should fail")
	}
}

// 两个不同文件应得到不同 token，互不覆盖
func TestStoreFileUniqueTokens(t *testing.T) {
	svc := NewExportService()

	first := svc.StoreFile([]byte("a"), "a.enc")
	second := svc.StoreFile([]byte("b"), "b.enc")
	if first == second {
		t.Fatal("two stored files must not share a token")
	}

	if data, _, err := svc.RetrieveFile(first); err != nil || string(data) != "a" {
		t.Errorf("first file = %q, %v; want a", data, err)
	}
	if data, _, err := svc.RetrieveFile(second); err != nil || string(data) != "b" {
		t.Errorf("second file = %q, %v; want b", data, err)
	}
}
