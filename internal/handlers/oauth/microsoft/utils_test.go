package microsoft

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"testing"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"

	"github.com/golang-jwt/jwt/v5"
)

const testClientID = "test-client-id"

// storage 用接口类型接收：传 (*FakeStorageService)(nil) 会得到非空接口，
// 调用 IsConfigured() 时 panic，与生产环境"存储不可用时是 nil 接口"的行为不一致
func newTestHandler(t *testing.T, userRepo *testutil.FakeUserRepo, storage services.StorageService) *MicrosoftHandler {
	t.Helper()

	cfg := &config.Config{
		BaseURL:               "http://localhost:3000",
		MicrosoftClientID:     testClientID,
		MicrosoftClientSecret: "test-secret",
		DefaultAvatarURL:      "https://cdn.example.com/default.svg",
		EmailWhitelistDomains: "example.com",
	}

	h, err := NewMicrosoftHandler(cfg, userRepo, &testutil.FakeUserLogStore{}, &testutil.FakeSessionManager{}, &testutil.FakeUserCache{}, storage)
	if err != nil {
		t.Fatalf("NewMicrosoftHandler: %v", err)
	}
	return h
}

func TestIsValidUUID(t *testing.T) {
	tests := map[string]bool{
		"9188040d-6c67-4c5b-b112-36a304b66dad": true,
		"9188040D-6C67-4C5B-B112-36A304B66DAD": true,
		"9188040d-6c67-4c5b-b112-36a304b66da":  false, // 少一位
		"9188040d6c67-4c5b-b112-36a304b66dad":  false, // 缺分隔符
		"9188040d-6c67-4c5b-b112-36a304b66daZ": false, // 非 hex
		"":                                     false,
	}

	for in, want := range tests {
		if got := isValidUUID(in); got != want {
			t.Errorf("isValidUUID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsValidMicrosoftIssuer(t *testing.T) {
	tests := map[string]bool{
		"https://login.microsoftonline.com/common/v2.0":                               true,
		"https://login.microsoftonline.com/organizations/v2.0":                        true,
		"https://login.microsoftonline.com/consumers/v2.0":                            true,
		"https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0": true,
		"https://login.microsoftonline.com//v2.0":                                     false, // 空租户
		"https://login.microsoftonline.com/common/v1.0":                               false, // 后缀不符
		"https://evil.example.com/common/v2.0":                                        false, // 前缀不符
		"https://login.microsoftonline.com/not-a-uuid/v2.0":                           false,
	}

	for in, want := range tests {
		if got := isValidMicrosoftIssuer(in); got != want {
			t.Errorf("isValidMicrosoftIssuer(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestJWKToRSAPublicKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	pub, err := jwkToRSAPublicKey(jwk{
		Kty: "RSA",
		Kid: "kid-1",
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
	})
	if err != nil {
		t.Fatalf("jwkToRSAPublicKey: %v", err)
	}
	if pub.N.Cmp(key.PublicKey.N) != 0 || pub.E != key.PublicKey.E {
		t.Error("parsed key does not match source public key")
	}

	if _, err := jwkToRSAPublicKey(jwk{Kty: "RSA", N: "!!!not-base64url!!!", E: "AQAB"}); err == nil {
		t.Error("invalid n should return error")
	}
	if _, err := jwkToRSAPublicKey(jwk{Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), E: "!!!not-base64url!!!"}); err == nil {
		t.Error("invalid e should return error")
	}
}

// 缓存命中时不应发起网络请求（TTL 内直接返回）
func TestFetchMicrosoftJWKSCacheHit(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	microsoftJWKSCache.Lock()
	microsoftJWKSCache.keys = map[string]*rsa.PublicKey{"kid-1": &key.PublicKey}
	microsoftJWKSCache.fetchedAt = time.Now()
	microsoftJWKSCache.Unlock()
	t.Cleanup(func() {
		microsoftJWKSCache.Lock()
		microsoftJWKSCache.keys = nil
		microsoftJWKSCache.fetchedAt = time.Time{}
		microsoftJWKSCache.Unlock()
	})

	keys, err := fetchMicrosoftJWKS(context.Background())
	if err != nil {
		t.Fatalf("fetchMicrosoftJWKS: %v", err)
	}
	if _, ok := keys["kid-1"]; !ok {
		t.Error("cached key kid-1 should be returned")
	}
}

// seedJWKS 预置 JWKS 缓存并返回对应私钥，供 ID Token 验签测试使用
func seedJWKS(t *testing.T, kid string) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	microsoftJWKSCache.Lock()
	microsoftJWKSCache.keys = map[string]*rsa.PublicKey{kid: &key.PublicKey}
	microsoftJWKSCache.fetchedAt = time.Now()
	microsoftJWKSCache.Unlock()
	t.Cleanup(func() {
		microsoftJWKSCache.Lock()
		microsoftJWKSCache.keys = nil
		microsoftJWKSCache.fetchedAt = time.Time{}
		microsoftJWKSCache.Unlock()
	})

	return key
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return signed
}

func TestExtractIDTokenEmail(t *testing.T) {
	const validIss = "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0"
	h := newTestHandler(t, testutil.NewFakeUserRepo(), nil)

	// 缺少 id_token / 非字符串
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{}); got != "" {
		t.Errorf("missing id_token = %q, want empty", got)
	}
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": 123}); got != "" {
		t.Errorf("non-string id_token = %q, want empty", got)
	}

	// 非法 JWT
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": "not-a-jwt"}); got != "" {
		t.Errorf("malformed token = %q, want empty", got)
	}

	const kid = "kid-extract"
	key := seedJWKS(t, kid)

	// header 缺少 kid
	noKid := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"email": "u@example.com"})
	signedNoKid, err := noKid.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": signedNoKid}); got != "" {
		t.Errorf("token without kid = %q, want empty", got)
	}

	// 正常路径
	good := signTestIDToken(t, key, kid, jwt.MapClaims{
		"iss":   validIss,
		"aud":   testClientID,
		"email": "user@example.com",
	})
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": good}); got != "user@example.com" {
		t.Errorf("valid token email = %q, want user@example.com", got)
	}

	// audience 不匹配
	wrongAud := signTestIDToken(t, key, kid, jwt.MapClaims{"iss": validIss, "aud": "other-client", "email": "user@example.com"})
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": wrongAud}); got != "" {
		t.Errorf("wrong audience = %q, want empty", got)
	}

	// audience 为数组时须包含本应用
	audList := signTestIDToken(t, key, kid, jwt.MapClaims{"iss": validIss, "aud": []any{"a", testClientID}, "email": "user@example.com"})
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": audList}); got != "user@example.com" {
		t.Errorf("audience list = %q, want user@example.com", got)
	}

	// issuer 不合法
	badIss := signTestIDToken(t, key, kid, jwt.MapClaims{"iss": "https://evil.example.com/common/v2.0", "aud": testClientID, "email": "user@example.com"})
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": badIss}); got != "" {
		t.Errorf("bad issuer = %q, want empty", got)
	}

	// kid 不在 JWKS 中
	unknownKid := signTestIDToken(t, key, "kid-unknown", jwt.MapClaims{"iss": validIss, "aud": testClientID, "email": "user@example.com"})
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": unknownKid}); got != "" {
		t.Errorf("unknown kid = %q, want empty", got)
	}

	// 签名被篡改
	tampered := good[:len(good)-4] + "AAAA"
	if got := h.extractIDTokenEmail(context.Background(), map[string]any{"id_token": tampered}); got != "" {
		t.Errorf("tampered signature = %q, want empty", got)
	}
}

func TestParseDataURL(t *testing.T) {
	h := newTestHandler(t, testutil.NewFakeUserRepo(), nil)

	data, ct := h.parseDataURL("data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes")))
	if ct != "image/png" || string(data) != "png-bytes" {
		t.Errorf("parseDataURL = %q, %q; want image/png and png-bytes", data, ct)
	}

	// 无 ';' 时整个 header 作为 content-type
	data, ct = h.parseDataURL("data:image/webp," + base64.StdEncoding.EncodeToString([]byte("webp")))
	if ct != "image/webp" || string(data) != "webp" {
		t.Errorf("parseDataURL = %q, %q; want image/webp and webp", data, ct)
	}

	for _, bad := range []string{
		"https://example.com/avatar.png",
		"data:image/png;base64",   // 无逗号
		"data:image/png;base64,!", // 非法 base64
	} {
		if data, ct := h.parseDataURL(bad); data != nil || ct != "" {
			t.Errorf("parseDataURL(%q) = %q, %q; want nil", bad, data, ct)
		}
	}
}

func TestUploadAvatar(t *testing.T) {
	// 存储未配置时回落为 base64 data URL
	h := newTestHandler(t, testutil.NewFakeUserRepo(), nil)
	got := h.uploadAvatar(context.Background(), "u1", []byte("img"), "image/png")
	if got != "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("img")) {
		t.Errorf("fallback URL = %q", got)
	}

	if got := h.uploadAvatar(context.Background(), "u1", nil, "image/png"); got != "" {
		t.Errorf("empty image = %q, want empty", got)
	}

	// 存储可用时走存储
	storage := &testutil.FakeStorageService{Configured: true}
	h = newTestHandler(t, testutil.NewFakeUserRepo(), storage)
	got = h.uploadAvatar(context.Background(), "u2", []byte("img"), "image/png")
	if got != "https://storage.local/avatars/u2.webp" {
		t.Errorf("storage URL = %q", got)
	}
	if len(storage.Uploaded) != 1 || storage.Uploaded[0] != "u2" {
		t.Errorf("Uploaded = %v, want [u2]", storage.Uploaded)
	}
}

func TestCalculateAvatarHash(t *testing.T) {
	h := newTestHandler(t, testutil.NewFakeUserRepo(), nil)

	if got := h.calculateAvatarHash(nil); got != "" {
		t.Errorf("empty data hash = %q, want empty", got)
	}

	a := h.calculateAvatarHash([]byte("avatar-a"))
	b := h.calculateAvatarHash([]byte("avatar-b"))
	if a == "" || a == b {
		t.Errorf("hash collisions: %q vs %q", a, b)
	}
	if a != h.calculateAvatarHash([]byte("avatar-a")) {
		t.Error("hash must be deterministic")
	}
	// SHA-256 十六进制应为 64 字符
	if len(a) != 64 {
		t.Errorf("hash length = %d, want 64", len(a))
	}
}

func TestProcessAvatarAsync(t *testing.T) {
	t.Run("updates when hash changed", func(t *testing.T) {
		repo := testutil.NewFakeUserRepo()
		repo.Seed(&models.User{UID: "u1", MicrosoftAvatarSync: true})
		storage := &testutil.FakeStorageService{Configured: true}
		h := newTestHandler(t, repo, storage)

		h.processAvatarAsync("u1", "old-hash", []byte("new-avatar"), "image/png")

		if len(storage.Uploaded) != 1 {
			t.Errorf("Uploaded = %v, want one upload", storage.Uploaded)
		}
	})

	t.Run("skips when sync disabled", func(t *testing.T) {
		repo := testutil.NewFakeUserRepo()
		repo.Seed(&models.User{UID: "u2", MicrosoftAvatarSync: false})
		storage := &testutil.FakeStorageService{Configured: true}
		h := newTestHandler(t, repo, storage)

		h.processAvatarAsync("u2", "", []byte("new-avatar"), "image/png")

		if len(storage.Uploaded) != 0 {
			t.Errorf("Uploaded = %v, want none when sync disabled", storage.Uploaded)
		}
	})

	t.Run("skips when hash unchanged", func(t *testing.T) {
		repo := testutil.NewFakeUserRepo()
		repo.Seed(&models.User{UID: "u3", MicrosoftAvatarSync: true})
		storage := &testutil.FakeStorageService{Configured: true}
		h := newTestHandler(t, repo, storage)

		data := []byte("same-avatar")
		h.processAvatarAsync("u3", h.calculateAvatarHash(data), data, "image/png")

		if len(storage.Uploaded) != 0 {
			t.Errorf("Uploaded = %v, want none when hash unchanged", storage.Uploaded)
		}
	})

	t.Run("clears when avatar removed", func(t *testing.T) {
		repo := testutil.NewFakeUserRepo()
		repo.Seed(&models.User{UID: "u4", MicrosoftAvatarSync: true})
		storage := &testutil.FakeStorageService{Configured: true}
		h := newTestHandler(t, repo, storage)

		h.processAvatarAsync("u4", "old-hash", nil, "image/png")

		if len(storage.Uploaded) != 0 {
			t.Errorf("Uploaded = %v, want none when clearing", storage.Uploaded)
		}
	})
}
