package services

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------- RFC 6238 官方测试向量（SHA1）----------

// RFC 6238 附录 B 的密钥为 ASCII "12345678901234567890"（base32: GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ），
// 官方向量为 8 位码，本实现为 6 位，取其后 6 位。
const rfc6238Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPRFC6238Vectors(t *testing.T) {
	vectors := []struct {
		unix int64
		want string // RFC 8 位向量的后 6 位
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}

	for _, v := range vectors {
		code, _, err := totpCodeAt(rfc6238Secret, v.unix, 0)
		if err != nil {
			t.Fatalf("totpCodeAt(%d): %v", v.unix, err)
		}
		if code != v.want {
			t.Errorf("T=%d: got %s, want %s", v.unix, code, v.want)
		}
	}
}

func TestTOTPVerifyCodeWindow(t *testing.T) {
	tUnix := int64(59)
	valid := "287082"

	// 当前时间片应通过
	if _, ok, err := verifyCodeAt(rfc6238Secret, valid, tUnix); err != nil || !ok {
		t.Errorf("code valid at current step should pass, got ok=%v err=%v", ok, err)
	}

	// 相邻时间片（±1 窗口）的码应通过
	prevCode, _, err := totpCodeAt(rfc6238Secret, tUnix-30, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := verifyCodeAt(rfc6238Secret, prevCode, tUnix); !ok {
		t.Errorf("code from previous step should pass within skew window, got fail for %s", prevCode)
	}

	// 超出窗口的码应失败
	farCode, _, err := totpCodeAt(rfc6238Secret, tUnix-150, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := verifyCodeAt(rfc6238Secret, farCode, tUnix); ok {
		t.Errorf("code from 5 steps away should fail, got pass for %s", farCode)
	}

	// 真实时钟冒烟：密钥生成的码在当前时间应通过
	svc := newTestTOTPService(t)
	secret := svc.GenerateSecret()
	nowCode, _, err := totpCodeAt(secret, time.Now().Unix(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !svc.VerifyCode(secret, nowCode) {
		t.Errorf("live-generated code should pass VerifyCode, got fail for %s", nowCode)
	}
}

func TestTOTPVerifyCodeRejectsBadInput(t *testing.T) {
	svc := newTestTOTPService(t)

	for _, bad := range []string{"", "12345", "1234567", "abcdef", "12 45a"} {
		if svc.VerifyCode(rfc6238Secret, bad) {
			t.Errorf("invalid code %q should fail", bad)
		}
	}
	if svc.VerifyCode("not-base32!!!", "123456") {
		t.Error("invalid secret should fail, not panic")
	}
}

func TestTOTPVerifyLoginCodeReplayProtection(t *testing.T) {
	svc := newTestTOTPService(t)
	const uid = "uid-replay"

	// 当前时间片的码
	code, _, err := totpCodeAt(rfc6238Secret, time.Now().Unix(), 0)
	if err != nil {
		t.Fatal(err)
	}

	if !svc.VerifyLoginCode(rfc6238Secret, uid, code) {
		t.Fatal("first use of the code should pass")
	}
	if svc.VerifyLoginCode(rfc6238Secret, uid, code) {
		t.Error("same code reused in the same step should be rejected (replay)")
	}

	// 同一时间片，另一个用户不受影响
	if !svc.VerifyLoginCode(rfc6238Secret, "uid-other", code) {
		t.Error("replay protection must be per-user")
	}
}

// ---------- 中转 pending token ----------

func TestTOTPPendingTokenLifecycle(t *testing.T) {
	svc := newTestTOTPService(t)

	token, err := svc.CreatePendingToken("uid-1")
	if err != nil || token == "" {
		t.Fatalf("CreatePendingToken() = %q, %v", token, err)
	}

	// 归属正确
	uid, ok := svc.ConsumePendingToken(token)
	if !ok || uid != "uid-1" {
		t.Fatalf("ConsumePendingToken() = %q, %v, want uid-1, true", uid, ok)
	}
	// 单次使用
	if _, ok := svc.ConsumePendingToken(token); ok {
		t.Error("pending token should be single-use")
	}
	// 无效 token
	if _, ok := svc.ConsumePendingToken("garbage"); ok {
		t.Error("invalid token should not consume")
	}
	// 同一用户重复创建时旧 token 被撤销
	first, _ := svc.CreatePendingToken("uid-2")
	second, _ := svc.CreatePendingToken("uid-2")
	if _, ok := svc.ConsumePendingToken(first); ok {
		t.Error("old pending token should be invalidated by a newer one")
	}
	if uid, ok := svc.ConsumePendingToken(second); !ok || uid != "uid-2" {
		t.Error("newest pending token should work")
	}
}

// ---------- 恢复码 ----------

func TestTOTPRecoveryCodes(t *testing.T) {
	svc := newTestTOTPService(t)
	codes := svc.GenerateRecoveryCodes()

	if len(codes) != totpRecoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(codes), totpRecoveryCodeCount)
	}
	seen := make(map[string]bool)
	for _, c := range codes {
		if len(c) != 9 || c[4] != '-' {
			t.Errorf("recovery code %q has wrong format", c)
		}
		if seen[c] {
			t.Errorf("duplicate recovery code %q", c)
		}
		seen[c] = true
		for _, r := range strings.ReplaceAll(c, "-", "") {
			if !strings.ContainsRune(recoveryCodeAlphabet, r) {
				t.Errorf("recovery code %q contains character outside alphabet", c)
			}
		}
	}

	const uid = "uid-recovery"
	if err := svc.StoreRecoveryCodes(context.Background(), uid, codes); err != nil {
		t.Fatalf("StoreRecoveryCodes: %v", err)
	}

	// 归属用户的恢复码可用
	ok, err := svc.ConsumeRecoveryCode(context.Background(), uid, codes[0])
	if err != nil || !ok {
		t.Fatalf("ConsumeRecoveryCode(own) = %v, %v, want true, nil", ok, err)
	}
	// 单次使用
	if ok, _ := svc.ConsumeRecoveryCode(context.Background(), uid, codes[0]); ok {
		t.Error("recovery code should be single-use")
	}
	// 归属校验：其他用户不能消费
	if ok, _ := svc.ConsumeRecoveryCode(context.Background(), "uid-else", codes[1]); ok {
		t.Error("another user must not consume someone else's recovery code")
	}
	// 归一化：小写与去连字符均可用
	if err := svc.StoreRecoveryCodes(context.Background(), uid, []string{"ABCD-2345"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := svc.ConsumeRecoveryCode(context.Background(), uid, "abcd2345"); !ok {
		t.Error("normalized input (lowercase, no dash) should be accepted")
	}
}

// ---------- 防爆破锁定 ----------

func TestTOTPLockout(t *testing.T) {
	svc := newTestTOTPService(t)
	const uid = "uid-lock"

	if svc.IsLocked(uid) {
		t.Fatal("fresh user should not be locked")
	}
	for i := 0; i < totpMaxFailures; i++ {
		svc.RecordFailure(uid)
	}
	if !svc.IsLocked(uid) {
		t.Error("user should be locked after max failures")
	}
	// 其他用户不受影响
	if svc.IsLocked("uid-else") {
		t.Error("lockout must be per-user")
	}
}

// ---------- URI 构建 ----------

func TestOTPAuthURI(t *testing.T) {
	svc := newTestTOTPService(t)
	uri := svc.OTPAuthURI("user@example.com", "JBSWY3DPEHPK3PXP")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Errorf("uri should start with otpauth://totp/, got %s", uri)
	}
	if !strings.Contains(uri, "secret=JBSWY3DPEHPK3PXP") ||
		!strings.Contains(uri, "issuer=Nebula+Studios") ||
		!strings.Contains(uri, "algorithm=SHA1") ||
		!strings.Contains(uri, "digits=6") ||
		!strings.Contains(uri, "period=30") {
		t.Errorf("uri missing standard params: %s", uri)
	}
	if strings.Contains(uri, " ") {
		t.Errorf("uri must not contain raw spaces: %s", uri)
	}
}

// ---------- 状态清理 ----------

func TestTOTPCleanupExpired(t *testing.T) {
	svc := newTestTOTPService(t)
	token, err := svc.CreatePendingToken("uid-clean")
	if err != nil {
		t.Fatal(err)
	}
	// 手动将条目改为已过期
	svc.mu.Lock()
	for _, entry := range svc.pending {
		entry.expiresAt = time.Now().Add(-time.Minute)
	}
	svc.mu.Unlock()

	svc.CleanupExpired()

	if _, ok := svc.ConsumePendingToken(token); ok {
		t.Error("expired pending token should have been cleaned up")
	}
}

// ---------- 测试辅助 ----------

// stubRecoveryStore 内存版恢复码仓库（services 包测试不能依赖 testutil，会形成 import cycle）
type stubRecoveryStore struct {
	byUID map[string]map[string]bool // uid -> 未使用哈希集合
}

func newStubRecoveryStore() *stubRecoveryStore {
	return &stubRecoveryStore{byUID: make(map[string]map[string]bool)}
}

func (s *stubRecoveryStore) CreateBatch(_ context.Context, userUID string, codeHashes []string) error {
	s.byUID[userUID] = make(map[string]bool)
	for _, h := range codeHashes {
		s.byUID[userUID][h] = true
	}
	return nil
}

func (s *stubRecoveryStore) Consume(_ context.Context, codeHash string) (string, bool, error) {
	for uid, hashes := range s.byUID {
		if hashes[codeHash] {
			delete(hashes, codeHash)
			return uid, true, nil
		}
	}
	return "", false, nil
}

func (s *stubRecoveryStore) DeleteByUserUID(_ context.Context, userUID string) error {
	delete(s.byUID, userUID)
	return nil
}

func newTestTOTPService(t *testing.T) *TOTPService {
	t.Helper()
	svc, err := NewTOTPService(newStubRecoveryStore())
	if err != nil {
		t.Fatalf("NewTOTPService: %v", err)
	}
	return svc
}
