package google

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"auth-system/internal/handlers/oauth"

	"github.com/golang-jwt/jwt/v5"
)

const testClientID = "test-client-id-123"

// newTestKeyPair 生成测试 RSA 密钥对及其 JWK 表示
func newTestKeyPair(t *testing.T, kid string) (*rsa.PrivateKey, googleJWK) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	jwk := googleJWK{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
	return key, jwk
}

// jwksBase64 构造 JWKS 文档并 base64 编码（与 GOOGLE_JWKS_SHA256 配置格式一致）
func jwksBase64(keys ...googleJWK) string {
	doc := googleJWKSDocument{Keys: keys}
	b, _ := json.Marshal(doc)
	return base64.StdEncoding.EncodeToString(b)
}

// signIDToken 用测试私钥签发 id_token（模拟 Google）
func signIDToken(t *testing.T, key *rsa.PrivateKey, kid, iss, aud, sub, email string, verified bool) string {
	t.Helper()
	now := time.Now()
	claims := GoogleIDTokenClaims{
		Sub:           sub,
		Email:         email,
		EmailVerified: verified,
		Name:          "Test User",
		Picture:       "https://p.example/avatar.png",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    iss,
			Audience:  jwt.ClaimStrings{aud},
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	s, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

func TestNewGoogleIDTokenVerifier_EmptyJWKS(t *testing.T) {
	// 未配置公钥 → 禁止启用 Google 登录
	_, err := NewGoogleIDTokenVerifier(testClientID, "")
	if !errors.Is(err, ErrInvalidJWKSBase64) {
		t.Fatalf("expected ErrInvalidJWKSBase64, got %v", err)
	}
}

func TestNewGoogleIDTokenVerifier_InvalidBase64(t *testing.T) {
	_, err := NewGoogleIDTokenVerifier(testClientID, "!!!not-base64!!!")
	if !errors.Is(err, ErrInvalidJWKSBase64) {
		t.Fatalf("expected ErrInvalidJWKSBase64, got %v", err)
	}
}

func TestVerify_Success(t *testing.T) {
	key, jwk := newTestKeyPair(t, "kid-1")
	v, err := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	idToken := signIDToken(t, key, "kid-1", googleIssuer, testClientID, "user-1", "user@example.com", true)
	claims, err := v.Verify(context.Background(), idToken)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Sub != "user-1" || claims.Email != "user@example.com" || !claims.EmailVerified {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestVerify_WrongSignature(t *testing.T) {
	// 有效公钥 + 用另一个私钥签发的 token（模拟攻击者伪造身份）
	_, jwk := newTestKeyPair(t, "kid-1")
	attackerKey, _ := newTestKeyPair(t, "kid-1")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))

	idToken := signIDToken(t, attackerKey, "kid-1", googleIssuer, testClientID, "user-1", "victim@example.com", true)
	if _, err := v.Verify(context.Background(), idToken); err == nil {
		t.Fatal("expected verification failure for forged token")
	}
}

func TestVerify_WrongAudience(t *testing.T) {
	key, jwk := newTestKeyPair(t, "kid-1")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))

	idToken := signIDToken(t, key, "kid-1", googleIssuer, "attacker-client-id", "user-1", "user@example.com", true)
	if _, err := v.Verify(context.Background(), idToken); err == nil {
		t.Fatal("expected audience mismatch failure")
	}
}

func TestVerify_WrongIssuer(t *testing.T) {
	key, jwk := newTestKeyPair(t, "kid-1")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))

	idToken := signIDToken(t, key, "kid-1", "https://evil.example.com", testClientID, "user-1", "user@example.com", true)
	if _, err := v.Verify(context.Background(), idToken); err == nil {
		t.Fatal("expected issuer mismatch failure")
	}
}

func TestVerify_UnknownKid(t *testing.T) {
	// Google 已轮换密钥（kid 不在预置公钥集）→ 拒绝，需更新 GOOGLE_JWKS_SHA256
	_, jwk := newTestKeyPair(t, "kid-1")
	newKey, _ := newTestKeyPair(t, "kid-2")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))

	idToken := signIDToken(t, newKey, "kid-2", googleIssuer, testClientID, "user-1", "user@example.com", true)
	if _, err := v.Verify(context.Background(), idToken); err == nil {
		t.Fatal("expected failure for unknown kid")
	}
}

func TestVerify_MultiKey(t *testing.T) {
	// JWKS 含多个密钥（Google 新旧并存）：任一 kid 均可验签
	key1, jwk1 := newTestKeyPair(t, "kid-1")
	key2, jwk2 := newTestKeyPair(t, "kid-2")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk1, jwk2))

	for _, k := range []struct {
		key *rsa.PrivateKey
		kid string
	}{{key1, "kid-1"}, {key2, "kid-2"}} {
		idToken := signIDToken(t, k.key, k.kid, googleIssuer, testClientID, "user-1", "user@example.com", true)
		if _, err := v.Verify(context.Background(), idToken); err != nil {
			t.Fatalf("verify with kid %s: %v", k.kid, err)
		}
	}
}

func TestVerify_ExpiredToken_Fails(t *testing.T) {
	key, jwk := newTestKeyPair(t, "kid-1")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))

	now := time.Now()
	claims := GoogleIDTokenClaims{
		Sub: "user-1", Email: "user@example.com", EmailVerified: true,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    googleIssuer,
			Audience:  jwt.ClaimStrings{testClientID},
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(now.Add(-10 * time.Minute)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-5 * time.Minute)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "kid-1"
	idToken, _ := tok.SignedString(key)

	if _, err := v.Verify(context.Background(), idToken); err == nil {
		t.Fatal("expected failure for expired token")
	}
}

func TestVerify_AlgConfusion_Fails(t *testing.T) {
	// 攻击者尝试用 HS256 + 公钥 n 作为对称密钥伪造 → 必须拒绝
	key, jwk := newTestKeyPair(t, "kid-1")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": googleIssuer, "aud": testClientID, "sub": "user-1",
		"email": "victim@example.com", "email_verified": true,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	}
	hmacKey := x509.MarshalPKCS1PublicKey(&key.PublicKey)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["kid"] = "kid-1"
	idToken, _ := tok.SignedString(hmacKey)

	if _, err := v.Verify(context.Background(), idToken); err == nil {
		t.Fatal("expected failure for HS256 alg confusion token")
	}
}

func TestVerify_TamperedEmail_Fails(t *testing.T) {
	key, jwk := newTestKeyPair(t, "kid-1")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))

	idToken := signIDToken(t, key, "kid-1", googleIssuer, testClientID, "sub-1", "victim@example.com", true)
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		t.Fatal("malformed jwt")
	}
	tampered := parts[0] + "." + parts[1] + "." + "AAAA" // 破坏签名
	if _, err := v.Verify(context.Background(), tampered); err == nil {
		t.Fatal("expected failure for tampered token")
	}
}

func TestParseIdentity_FromIDToken(t *testing.T) {
	// userinfo 伪造 email 时，身份必须来自验签后的 id_token
	key, jwk := newTestKeyPair(t, "kid-1")
	v, err := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	h := &GoogleHandler{ExternalProviderHandler: &oauth.ExternalProviderHandler{}, verifier: v}

	idToken := signIDToken(t, key, "kid-1", googleIssuer, testClientID, "real-user-sub", "real@example.com", true)
	tokenData := map[string]any{"id_token": idToken}
	userInfo := map[string]any{
		"id": "attacker-fake-id", "email": "victim@example.com", "email_verified": true, "name": "Attacker",
	}

	identity := h.parseIdentity(tokenData, userInfo)
	if identity.ProviderID != "real-user-sub" {
		t.Fatalf("ProviderID should come from id_token sub, got %q", identity.ProviderID)
	}
	if identity.Email != "real@example.com" {
		t.Fatalf("Email should come from verified id_token, got %q", identity.Email)
	}
}

func TestParseIdentity_MissingIDToken_Refuses(t *testing.T) {
	_, jwk := newTestKeyPair(t, "kid-1")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))
	h := &GoogleHandler{ExternalProviderHandler: &oauth.ExternalProviderHandler{}, verifier: v}

	// 无 id_token（仅靠 userinfo）→ 必须拒绝
	identity := h.parseIdentity(map[string]any{}, map[string]any{
		"id": "attacker-fake-id", "email": "victim@example.com", "email_verified": true,
	})
	if identity.ProviderID != "" {
		t.Fatalf("identity must be empty without valid id_token, got %+v", identity)
	}
}

func TestParseIdentity_UnverifiedEmail(t *testing.T) {
	key, jwk := newTestKeyPair(t, "kid-1")
	v, _ := NewGoogleIDTokenVerifier(testClientID, jwksBase64(jwk))
	h := &GoogleHandler{ExternalProviderHandler: &oauth.ExternalProviderHandler{}, verifier: v}

	idToken := signIDToken(t, key, "kid-1", googleIssuer, testClientID, "sub-1", "unverified@example.com", false)
	identity := h.parseIdentity(map[string]any{"id_token": idToken}, nil)
	if identity.Email != "" {
		t.Fatalf("unverified email must be ignored, got %q", identity.Email)
	}
	if identity.ProviderID != "sub-1" {
		t.Fatalf("ProviderID must still be set, got %q", identity.ProviderID)
	}
}
