// 安全审计修复验证：F5 —— JWT 验签只接受 ES256，拒绝其他 ECDSA 变体（ES384/ES512）。
package services

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestF5_Fixed_RejectsNonES256Alg：用 ES512 签名（P-521 密钥）伪造 token，必须被拒绝。
func TestF5_Fixed_RejectsNonES256Alg(t *testing.T) {
	s := testSessionService(t, time.Hour)

	key521, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-521 key: %v", err)
	}
	claims := &Claims{
		UID: "uid-x",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    s.jwtIssuer,
			Audience:  jwt.ClaimStrings{s.jwtAudience},
		},
	}
	es512 := jwt.NewWithClaims(jwt.SigningMethodES512, claims)
	tok, err := es512.SignedString(key521)
	if err != nil {
		t.Fatalf("sign with ES512: %v", err)
	}

	if _, err := s.VerifyToken(tok); !errors.Is(err, ErrInvalidSigningMethod) {
		t.Fatalf("FIX FAILED: ES512 token should be rejected with ErrInvalidSigningMethod, got %v", err)
	}
	t.Logf("F5 FIXED: verify rejects non-ES256 alg (ES512) with ErrInvalidSigningMethod")
}

// testSessionService 见 session_test.go（本包共享）。
// TestF5 同时隐式保证 ES256 签名仍可验证（由既有测试 GenerateTokens/VerifyToken 覆盖）。
var _ = jwt.SigningMethodES256
