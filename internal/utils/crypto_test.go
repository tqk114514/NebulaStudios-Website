package utils

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateSecureToken(t *testing.T) {
	token, err := GenerateSecureToken()
	if err != nil {
		t.Fatalf("GenerateSecureToken() error = %v", err)
	}
	if len(token) != 16 {
		t.Errorf("token length = %d, want 16", len(token))
	}
	// hex 字符集
	if _, err := hex.DecodeString(token); err != nil {
		t.Errorf("token %q is not valid hex: %v", token, err)
	}
	// 唯一性
	token2, _ := GenerateSecureToken()
	if token == token2 {
		t.Error("two tokens should differ")
	}
}

func TestGenerateCode(t *testing.T) {
	code, err := GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}
	if len(code) != 6 {
		t.Errorf("code length = %d, want 6", len(code))
	}
	// 不应包含易混淆字符 0/O/I/l
	for _, c := range code {
		if strings.ContainsRune("0OIl", c) {
			t.Errorf("code %q contains confusing char %q", code, c)
		}
	}
	// 通过 ValidateCode 往返
	if !ValidateCode(code).Valid {
		t.Errorf("generated code %q should pass ValidateCode", code)
	}
}

func TestGenerateUID(t *testing.T) {
	uid, err := GenerateUID()
	if err != nil {
		t.Fatalf("GenerateUID() error = %v", err)
	}
	if len(uid) != 16 {
		t.Errorf("uid length = %d, want 16", len(uid))
	}
	for _, c := range uid {
		if !strings.ContainsRune(uidChars, c) {
			t.Errorf("uid %q contains char %q outside charset", uid, c)
		}
	}
	uid2, _ := GenerateUID()
	if uid == uid2 {
		t.Error("two uids should differ")
	}
}

func TestHashPasswordAndVerify(t *testing.T) {
	hash, err := HashPassword("Abcdef1!@#ghijklmn")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash should start with $argon2id$, got %q", hash[:20])
	}

	ok, err := VerifyPassword("Abcdef1!@#ghijklmn", hash)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("correct password should verify")
	}

	ok, err = VerifyPassword("Wrong1!@#password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword(wrong) error = %v", err)
	}
	if ok {
		t.Error("wrong password should not verify")
	}

	// 相同密码两次哈希 salt 不同 → 哈希串不同，但都能验证
	hash2, _ := HashPassword("Abcdef1!@#ghijklmn")
	if hash == hash2 {
		t.Error("same password should produce different hashes (random salt)")
	}
	ok, _ = VerifyPassword("Abcdef1!@#ghijklmn", hash2)
	if !ok {
		t.Error("second hash should also verify")
	}
}

func TestHashPasswordEmpty(t *testing.T) {
	if _, err := HashPassword(""); err != ErrEmptyPassword {
		t.Errorf("HashPassword(\"\") error = %v, want ErrEmptyPassword", err)
	}
	if _, err := VerifyPassword("", "$argon2id$x"); err != ErrEmptyPassword {
		t.Errorf("VerifyPassword(\"\") error = %v, want ErrEmptyPassword", err)
	}
}

func TestVerifyPasswordInvalidHash(t *testing.T) {
	cases := []string{
		"",
		"plaintext-not-a-hash",
		"$argon2id$broken",                     // 分段不足
		"$argon2id$v=19$m=0,t=0,p=0$AAAA$AAAA", // 零参数
		"$argon2id$v=19$m=64,t=1,p=1$!!!$AAAA", // salt 非法 base64
		"$argon2id$v=19$m=64,t=1,p=1$AAAA$!!!", // hash 非法 base64
	}
	for _, c := range cases {
		if _, err := VerifyPassword("Abcdef1!@#ghijklmn", c); err == nil {
			t.Errorf("VerifyPassword with %q should return error", c)
		}
	}
}

func TestHashToken(t *testing.T) {
	h1 := HashToken("token-abc")
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA-256 hex)", len(h1))
	}
	if HashToken("token-abc") != h1 {
		t.Error("same token should hash deterministically")
	}
	if HashToken("token-abd") == h1 {
		t.Error("different tokens should hash differently")
	}
}

func TestS256CodeChallenge(t *testing.T) {
	challenge := S256CodeChallenge("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~")
	if len(challenge) != 43 {
		t.Errorf("challenge length = %d, want 43 (base64url of SHA-256)", len(challenge))
	}
	// RFC 7636 Appendix B 官方测试向量：
	// code_verifier = dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk
	// S256 challenge = E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM
	got := S256CodeChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got != want {
		t.Errorf("RFC 7636 vector mismatch: got %q, want %q", got, want)
	}
	if S256CodeChallenge("verifier-a") == S256CodeChallenge("verifier-b") {
		t.Error("different verifiers should produce different challenges")
	}
}

func TestVerifyPKCE(t *testing.T) {
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	challenge := S256CodeChallenge(verifier)

	if !VerifyPKCE(verifier, challenge, "S256") {
		t.Error("matching S256 verifier should verify")
	}
	if VerifyPKCE("different-verifier-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ", challenge, "S256") {
		t.Error("mismatched verifier should not verify")
	}
	if !VerifyPKCE(verifier, verifier, "plain") {
		t.Error("matching plain verifier should verify")
	}
	// fail-closed：空 challenge
	if VerifyPKCE(verifier, "", "S256") {
		t.Error("empty challenge must fail closed")
	}
	if VerifyPKCE("", challenge, "S256") {
		t.Error("empty verifier must fail")
	}
	// 未知 method
	if VerifyPKCE(verifier, challenge, "NONE") {
		t.Error("unknown method must fail")
	}
}

func TestValidateCodeVerifier(t *testing.T) {
	valid := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~" // 64 chars, all allowed
	if !ValidateCodeVerifier(valid) {
		t.Error("valid verifier should pass")
	}
	if ValidateCodeVerifier("short") {
		t.Error("short verifier should fail")
	}
	if !ValidateCodeVerifier(valid + "x") {
		t.Error("65-char verifier should pass")
	}
	if ValidateCodeVerifier(strings.Repeat("a", 200)) {
		t.Error("200-char verifier should fail (over 128)")
	}
	if ValidateCodeVerifier("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~!") {
		t.Error("verifier with '!' should fail")
	}
}

func TestValidateCodeChallenge(t *testing.T) {
	challenge := S256CodeChallenge("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~")
	if !ValidateCodeChallenge(challenge, "S256") {
		t.Error("valid S256 challenge should pass")
	}
	if ValidateCodeChallenge("short", "S256") {
		t.Error("short S256 challenge should fail")
	}
	if !ValidateCodeChallenge("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~", "plain") {
		t.Error("valid plain challenge should pass")
	}
	if ValidateCodeChallenge("", "S256") {
		t.Error("empty challenge should fail")
	}
	if ValidateCodeChallenge(challenge, "") {
		t.Error("empty method should fail")
	}
	if ValidateCodeChallenge(challenge, "unknown") {
		t.Error("unknown method should fail")
	}
}
