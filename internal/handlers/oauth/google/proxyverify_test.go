package google

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testClientID = "test-client-id.apps.googleusercontent.com"

// newTestVerifier 生成 Ed25519 密钥对并构造验签器，返回验签器与私钥（用于构造签名）
func newTestVerifier(t *testing.T) (*WorkerTokenVerifier, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	v, err := NewWorkerTokenVerifier(testClientID, string(pemEncodePublicKey(t, pub)))
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	return v, priv
}

// pemEncodePublicKey 按 WORKER_SIGNING_PUBLIC_KEY 配置格式（PEM 全文）编码公钥
func pemEncodePublicKey(t *testing.T, pub ed25519.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// makeEnvelope 按 Worker 端相同算法构造签名信封
func makeEnvelope(t *testing.T, priv ed25519.PrivateKey, data string, timestamp int64) WorkerTokenEnvelope {
	t.Helper()
	message := strconv.FormatInt(timestamp, 10) + "\n" + data
	sig := ed25519.Sign(priv, []byte(message))
	return WorkerTokenEnvelope{
		Data:      data,
		Timestamp: timestamp,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
}

// makeIDToken 构造 JWT 字符串（ParseUnverified 不校验签名，第三段仅占位）
func makeIDToken(t *testing.T, mutate func(*GoogleIDTokenClaims)) string {
	t.Helper()
	claims := &GoogleIDTokenClaims{
		Sub:           "google-sub-123",
		Email:         "user@example.com",
		EmailVerified: true,
		Name:          "Test User",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    googleIssuer,
			Audience:  jwt.ClaimStrings{testClientID},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	if mutate != nil {
		mutate(claims)
	}
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-kid"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	enc := base64.RawURLEncoding
	return fmt.Sprintf("%s.%s.%s", enc.EncodeToString(header), enc.EncodeToString(payload), "AA")
}

// ---------- NewWorkerTokenVerifier ----------

func TestNewWorkerTokenVerifier_EmptyKey(t *testing.T) {
	if _, err := NewWorkerTokenVerifier(testClientID, ""); err == nil {
		t.Fatal("expected error for empty public key")
	}
}

func TestNewWorkerTokenVerifier_NotPEM(t *testing.T) {
	if _, err := NewWorkerTokenVerifier(testClientID, "not a pem"); err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}

func TestNewWorkerTokenVerifier_NotEd25519(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal ecdsa public key: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	if _, err := NewWorkerTokenVerifier(testClientID, pemStr); err == nil {
		t.Fatal("expected error for non-Ed25519 public key")
	}
}

func TestNewWorkerTokenVerifier_EmptyClientID(t *testing.T) {
	if _, err := NewWorkerTokenVerifier("", "anything"); err == nil {
		t.Fatal("expected error for empty clientID")
	}
}

// ---------- VerifyEnvelope ----------

func TestVerifyEnvelope_Valid(t *testing.T) {
	v, priv := newTestVerifier(t)
	data := `{"access_token":"at","id_token":"it"}`
	env := makeEnvelope(t, priv, data, time.Now().Unix())

	got, err := v.VerifyEnvelope(&env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != data {
		t.Fatalf("unwrapped data mismatch: %s", got)
	}
}

func TestVerifyEnvelope_WrongKey(t *testing.T) {
	v, _ := newTestVerifier(t)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	env := makeEnvelope(t, other, "{}", time.Now().Unix())

	if _, err := v.VerifyEnvelope(&env); err == nil {
		t.Fatal("expected signature error for wrong key")
	}
}

func TestVerifyEnvelope_TamperedData(t *testing.T) {
	v, priv := newTestVerifier(t)
	env := makeEnvelope(t, priv, `{"a":1}`, time.Now().Unix())
	env.Data = `{"a":2}` // 签名后篡改内容

	if _, err := v.VerifyEnvelope(&env); err == nil {
		t.Fatal("expected signature error for tampered data")
	}
}

func TestVerifyEnvelope_StaleTimestamp(t *testing.T) {
	v, priv := newTestVerifier(t)
	stale := makeEnvelope(t, priv, "{}", time.Now().Add(-10*time.Minute).Unix())
	if _, err := v.VerifyEnvelope(&stale); err == nil {
		t.Fatal("expected error for stale timestamp")
	}

	future := makeEnvelope(t, priv, "{}", time.Now().Add(10*time.Minute).Unix())
	if _, err := v.VerifyEnvelope(&future); err == nil {
		t.Fatal("expected error for future timestamp")
	}
}

func TestVerifyEnvelope_BadSignatureBase64(t *testing.T) {
	v, _ := newTestVerifier(t)
	env := WorkerTokenEnvelope{Data: "{}", Timestamp: time.Now().Unix(), Signature: "!!!"}
	if _, err := v.VerifyEnvelope(&env); err == nil {
		t.Fatal("expected error for bad signature base64")
	}
}

// ---------- VerifyIDTokenClaims ----------

func TestVerifyIDTokenClaims_Valid(t *testing.T) {
	v, _ := newTestVerifier(t)
	idToken := makeIDToken(t, nil)

	claims, err := v.VerifyIDTokenClaims(context.Background(), idToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Sub != "google-sub-123" || claims.Email != "user@example.com" || !claims.EmailVerified {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestVerifyIDTokenClaims_Invalid(t *testing.T) {
	v, _ := newTestVerifier(t)
	cases := []struct {
		name   string
		mutate func(*GoogleIDTokenClaims)
	}{
		{"wrong issuer", func(c *GoogleIDTokenClaims) { c.Issuer = "https://evil.example" }},
		{"wrong audience", func(c *GoogleIDTokenClaims) { c.Audience = jwt.ClaimStrings{"other-app"} }},
		{"expired", func(c *GoogleIDTokenClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }},
		{"missing exp", func(c *GoogleIDTokenClaims) { c.ExpiresAt = nil }},
		{"missing sub", func(c *GoogleIDTokenClaims) { c.Sub = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idToken := makeIDToken(t, tc.mutate)
			if _, err := v.VerifyIDTokenClaims(context.Background(), idToken); err == nil {
				t.Fatal("expected claims error")
			}
		})
	}
}

func TestVerifyIDTokenClaims_EmptyOrMalformed(t *testing.T) {
	v, _ := newTestVerifier(t)
	if _, err := v.VerifyIDTokenClaims(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty token")
	}
	if _, err := v.VerifyIDTokenClaims(context.Background(), "not-a-jwt"); err == nil {
		t.Fatal("expected error for malformed token")
	}
}
