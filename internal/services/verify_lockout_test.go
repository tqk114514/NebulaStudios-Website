// 安全审计修复验证：F2 —— 邮箱验证码防爆破锁定（同一邮箱连错达阈值后锁定）。
package services

import (
	"context"
	"database/sql"
	"testing"

	"auth-system/internal/models"
)

// TestF2_Fixed_VerifyCodeLocksAfterTooManyFailures：连续错误猜测达到阈值后返回 CODE_LOCKED，
// 锁定期间即便提交其他码也一律拒绝——修复验证码可被逐码爆破的缺口。
func TestF2_Fixed_VerifyCodeLocksAfterTooManyFailures(t *testing.T) {
	s, _, codeRepo := newTokenServiceWithFakes(t)
	codeRepo.findErr = sql.ErrNoRows // 所有码视为不存在（模拟错误的猜测码）
	ctx := context.Background()
	const email = "victim@example.com"

	// 前 codeMaxFailures-1 次错误猜测：INVALID_CODE（尚未锁定）
	for i := 0; i < codeMaxFailures-1; i++ {
		_, err := s.VerifyCode(ctx, "guess", email, TokenTypeResetPassword)
		if err != models.ErrInvalidCode {
			t.Fatalf("attempt %d: got %v, want INVALID_CODE", i+1, err)
		}
	}

	// 达到阈值的一次触发锁定
	if _, err := s.VerifyCode(ctx, "guess", email, TokenTypeResetPassword); err != models.ErrCodeLocked {
		t.Fatalf("FIX FAILED: threshold attempt should return CODE_LOCKED, got %v", err)
	}

	// 锁定期间：即使换猜测码也直接 CODE_LOCKED
	if _, err := s.VerifyCode(ctx, "another-guess", email, TokenTypeResetPassword); err != models.ErrCodeLocked {
		t.Fatalf("lockout not held: got %v, want CODE_LOCKED", err)
	}

	t.Logf("F2 FIXED: verification locked after %d failures (CODE_LOCKED); further attempts rejected", codeMaxFailures)
}
