package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/models"

	"github.com/golang-jwt/jwt/v5"
)

// testECDSAPEM 生成测试用 ECDSA P-256 私钥 PEM（SEC1 格式）
func testECDSAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal EC key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

func testSessionService(t *testing.T, expiry time.Duration) *SessionService {
	t.Helper()
	cfg := &config.Config{
		JWTPrivateKey:     testECDSAPEM(t),
		AccessTokenExpiry: expiry,
		JWTIssuer:         "test-issuer",
		JWTAudience:       "test-audience",
	}
	s, err := NewSessionService(cfg, nil)
	if err != nil {
		t.Fatalf("NewSessionService() error = %v", err)
	}
	return s
}

func TestNewSessionServiceValidation(t *testing.T) {
	if _, err := NewSessionService(nil, nil); !errors.Is(err, ErrSessionNilConfig) {
		t.Errorf("nil config error = %v, want ErrSessionNilConfig", err)
	}
	if _, err := NewSessionService(&config.Config{JWTPrivateKey: ""}, nil); !errors.Is(err, ErrSessionEmptyPrivateKey) {
		t.Errorf("empty key error = %v, want ErrSessionEmptyPrivateKey", err)
	}
	if _, err := NewSessionService(&config.Config{JWTPrivateKey: "not-a-pem"}, nil); !errors.Is(err, ErrSessionInvalidPrivateKey) {
		t.Errorf("invalid key error = %v, want ErrSessionInvalidPrivateKey", err)
	}

	// 默认 issuer/audience
	s, err := NewSessionService(&config.Config{JWTPrivateKey: testECDSAPEM(t)}, nil)
	if err != nil {
		t.Fatalf("valid config should not error: %v", err)
	}
	if s.jwtIssuer != "auth-system" || s.jwtAudience != "auth-system-users" {
		t.Errorf("default issuer/audience = %q/%q", s.jwtIssuer, s.jwtAudience)
	}
}

func TestNewSessionServiceExpiryClamping(t *testing.T) {
	// 极小 expiry → 钳制到最小值
	s, err := NewSessionService(&config.Config{
		JWTPrivateKey:     testECDSAPEM(t),
		AccessTokenExpiry: time.Nanosecond,
	}, nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if s.accessTokenExpiry < minAccessTokenExpiry {
		t.Errorf("access expiry %v should be clamped to >= %v", s.accessTokenExpiry, minAccessTokenExpiry)
	}

	// 极大 expiry → 钳制到最大值（3000h > maxRefreshTokenExpiry=2160h 才真正触发钳制）
	s2, err := NewSessionService(&config.Config{
		JWTPrivateKey:      testECDSAPEM(t),
		AccessTokenExpiry:  3000 * time.Hour,
		RefreshTokenExpiry: 3000 * time.Hour,
	}, nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if s2.accessTokenExpiry > maxAccessTokenExpiry {
		t.Errorf("access expiry %v should be clamped to <= %v", s2.accessTokenExpiry, maxAccessTokenExpiry)
	}
	if s2.refreshTokenExpiry > maxRefreshTokenExpiry {
		t.Errorf("refresh expiry %v should be clamped to <= %v", s2.refreshTokenExpiry, maxRefreshTokenExpiry)
	}
}

func TestGenerateTokensBanned(t *testing.T) {
	// 配置 30min，验证 banned 分支强制使用 bannedAccessTokenExpiry（15min）而非配置值
	s := testSessionService(t, 30*time.Minute)

	accessToken, refreshToken, err := s.GenerateTokens(t.Context(), "uid-123", true)
	if err != nil {
		t.Fatalf("GenerateTokens(banned) error = %v", err)
	}
	if accessToken == "" {
		t.Error("banned user should still get an access token")
	}
	if refreshToken != "" {
		t.Error("banned user should NOT get a refresh token")
	}

	claims, err := s.VerifyToken(accessToken)
	if err != nil {
		t.Fatalf("banned token should verify: %v", err)
	}
	if claims.UID != "uid-123" {
		t.Errorf("claims.UID = %q, want uid-123", claims.UID)
	}
	if claims.Banned == nil || !*claims.Banned {
		t.Error("banned claim should be true")
	}
	// 封禁 token 使用短期过期（上下界都验）
	if claims.ExpiresAt == nil {
		t.Fatal("banned token should have expiresAt")
	}
	remaining := time.Until(claims.ExpiresAt.Time)
	if remaining <= 0 || remaining > bannedAccessTokenExpiry {
		t.Errorf("banned token remaining %v should be within (0, %v]", remaining, bannedAccessTokenExpiry)
	}
}

func TestGenerateTokensInvalidUser(t *testing.T) {
	s := testSessionService(t, 15*time.Minute)
	if _, _, err := s.GenerateTokens(t.Context(), "", false); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("empty uid error = %v, want ErrInvalidUser", err)
	}
}

func TestVerifyTokenLifecycle(t *testing.T) {
	s := testSessionService(t, 15*time.Minute)

	// 正常生成 + 验证（generateAccessToken 为纯 JWT，不依赖 DB）
	accessToken, err := s.generateAccessToken("user-a", boolPtr(false), 15*time.Minute)
	if err != nil {
		t.Fatalf("generateAccessToken error = %v", err)
	}
	claims, err := s.VerifyToken(accessToken)
	if err != nil {
		t.Fatalf("VerifyToken error = %v", err)
	}
	if claims.UID != "user-a" {
		t.Errorf("claims.UID = %q", claims.UID)
	}
	if claims.Banned == nil || *claims.Banned {
		t.Error("non-banned token should have banned=false")
	}

	// 篡改签名 → 拒绝
	tampered := accessToken[:len(accessToken)-2] + "xx"
	if _, err := s.VerifyToken(tampered); err == nil {
		t.Error("tampered token should be rejected")
	}

	// 空 token
	if _, err := s.VerifyToken(""); !errors.Is(err, ErrNoToken) {
		t.Errorf("empty token error = %v, want ErrNoToken", err)
	}
}

func TestVerifyTokenExpired(t *testing.T) {
	s := testSessionService(t, 15*time.Minute)
	// 直接用过期时长生成 token
	token, err := s.generateAccessToken("user-a", boolPtr(false), -time.Minute)
	if err != nil {
		t.Fatalf("generateAccessToken error = %v", err)
	}
	if _, err := s.VerifyToken(token); !errors.Is(err, ErrTokenExpiredSession) {
		t.Errorf("expired token error = %v, want ErrTokenExpiredSession", err)
	}
}

func TestVerifyTokenRejectsWrongAlgorithm(t *testing.T) {
	s := testSessionService(t, 15*time.Minute)
	// HS256 签名的 token 应被拒绝（非 ECDSA）
	hsToken, err := signHS256("uid-1")
	if err != nil {
		t.Fatalf("signHS256 error = %v", err)
	}
	if _, err := s.VerifyToken(hsToken); err == nil {
		t.Error("HS256 token should be rejected")
	}
}

func TestVerifyTokenRejectsEmptyUID(t *testing.T) {
	s := testSessionService(t, 15*time.Minute)
	// 空 UID 的 claims → ErrInvalidUser
	token, err := s.generateAccessToken("", boolPtr(false), 5*time.Minute)
	if err != nil {
		t.Fatalf("generateAccessToken error = %v", err)
	}
	if _, err := s.VerifyToken(token); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("empty uid claims error = %v, want ErrInvalidUser", err)
	}
}

func TestGetExpiryAndIsConfigured(t *testing.T) {
	var nilSvc *SessionService
	if nilSvc.GetExpiry() != defaultAccessTokenExpiry {
		t.Error("nil service should return default expiry")
	}
	if nilSvc.IsConfigured() {
		t.Error("nil service should not be configured")
	}

	s := testSessionService(t, 30*time.Minute)
	if !s.IsConfigured() {
		t.Error("valid service should be configured")
	}
	if s.GetExpiry() != 30*time.Minute {
		t.Errorf("GetExpiry() = %v, want 30m", s.GetExpiry())
	}
}

func boolPtr(v bool) *bool { return &v }

// signHS256 生成 HS256 签名的 token（用于验证 VerifyToken 拒绝非 ECDSA 算法）
func signHS256(uid string) (string, error) {
	claims := &Claims{
		UID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte("test-secret"))
}

// ---------- RefreshTokens（mock SessionTokenStore） ----------

// fakeSessionTokenStore 本地 refresh token 仓库 fake（services 测试不能 import testutil：会循环依赖）
type fakeSessionTokenStore struct {
	findResult      *models.SessionToken
	findErr         error
	markUsedErr     error
	markUsedCalls   []int64
	created         []*models.SessionToken
	revokedFamilies []string
	revokedUsers    []string
}

func (f *fakeSessionTokenStore) Create(_ context.Context, t *models.SessionToken) error {
	f.created = append(f.created, t)
	return nil
}
func (f *fakeSessionTokenStore) FindByHash(context.Context, string) (*models.SessionToken, error) {
	return f.findResult, f.findErr
}
func (f *fakeSessionTokenStore) MarkUsed(_ context.Context, id int64) error {
	f.markUsedCalls = append(f.markUsedCalls, id)
	return f.markUsedErr
}
func (f *fakeSessionTokenStore) RevokeFamily(_ context.Context, familyID string) (int64, error) {
	f.revokedFamilies = append(f.revokedFamilies, familyID)
	return 0, nil
}
func (f *fakeSessionTokenStore) RevokeUser(_ context.Context, userUID string) (int64, error) {
	f.revokedUsers = append(f.revokedUsers, userUID)
	return 0, nil
}
func (f *fakeSessionTokenStore) DeleteExpired(context.Context) (int64, error) { return 0, nil }

// newSessionWithFakeRepo 构造注入 fake repo 的 SessionService
func newSessionWithFakeRepo(t *testing.T) (*SessionService, *fakeSessionTokenStore) {
	t.Helper()
	s := testSessionService(t, time.Hour)
	repo := &fakeSessionTokenStore{}
	s.sessionTokenRepo = repo
	return s, repo
}

func unexpiredSessionToken() *models.SessionToken {
	return &models.SessionToken{
		ID:        1,
		TokenHash: "hash",
		UserUID:   "u1",
		FamilyID:  "fam-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func TestRefreshTokensSuccess(t *testing.T) {
	s, repo := newSessionWithFakeRepo(t)
	repo.findResult = unexpiredSessionToken()

	access, refresh, err := s.RefreshTokens(context.Background(), "refresh-token-str")
	if err != nil {
		t.Fatalf("RefreshTokens() error = %v", err)
	}
	if access == "" || refresh == "" {
		t.Error("RefreshTokens() returned empty tokens")
	}
	// 旧 token 已标记使用
	if len(repo.markUsedCalls) != 1 || repo.markUsedCalls[0] != 1 {
		t.Errorf("MarkUsed calls = %v, want [1]", repo.markUsedCalls)
	}
	// 新 refresh token 已写入
	if len(repo.created) != 1 {
		t.Fatalf("new refresh token not created, got %d", len(repo.created))
	}
	if repo.created[0].UserUID != "u1" || repo.created[0].FamilyID == "" {
		t.Errorf("new token = %+v, want user u1 with new family", repo.created[0])
	}
}

func TestRefreshTokensEmptyToken(t *testing.T) {
	s, _ := newSessionWithFakeRepo(t)
	if _, _, err := s.RefreshTokens(context.Background(), ""); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("RefreshTokens(empty) error = %v, want ErrRefreshTokenInvalid", err)
	}
}

func TestRefreshTokensNotFound(t *testing.T) {
	s, repo := newSessionWithFakeRepo(t)
	repo.findErr = models.ErrSessionTokenNotFound

	if _, _, err := s.RefreshTokens(context.Background(), "refresh-token-str"); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("RefreshTokens() error = %v, want ErrRefreshTokenInvalid (未找到)", err)
	}
}

func TestRefreshTokensExpired(t *testing.T) {
	s, repo := newSessionWithFakeRepo(t)
	tok := unexpiredSessionToken()
	tok.ExpiresAt = time.Now().Add(-time.Minute)
	repo.findResult = tok

	if _, _, err := s.RefreshTokens(context.Background(), "refresh-token-str"); !errors.Is(err, ErrRefreshTokenExpired) {
		t.Fatalf("RefreshTokens() error = %v, want ErrRefreshTokenExpired", err)
	}
}

func TestRefreshTokensReuseRevokesFamily(t *testing.T) {
	s, repo := newSessionWithFakeRepo(t)
	tok := unexpiredSessionToken()
	tok.Used = true
	repo.findResult = tok

	_, _, err := s.RefreshTokens(context.Background(), "refresh-token-str")
	if !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("RefreshTokens() error = %v, want ErrRefreshTokenReused", err)
	}
	// 检测到重放必须撤销整个家族
	if len(repo.revokedFamilies) != 1 || repo.revokedFamilies[0] != "fam-1" {
		t.Errorf("RevokeFamily calls = %v, want [fam-1]", repo.revokedFamilies)
	}
}

func TestRefreshTokensMarkUsedRace(t *testing.T) {
	s, repo := newSessionWithFakeRepo(t)
	repo.findResult = unexpiredSessionToken()
	repo.markUsedErr = models.ErrSessionTokenReused // 并发竞争：MarkUsed 时报已使用

	_, _, err := s.RefreshTokens(context.Background(), "refresh-token-str")
	if !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("RefreshTokens() error = %v, want ErrRefreshTokenReused", err)
	}
	if len(repo.revokedFamilies) != 1 {
		t.Errorf("RevokeFamily should be called on mark-used race, got %v", repo.revokedFamilies)
	}
}

func TestRevokeUserTokens(t *testing.T) {
	s, repo := newSessionWithFakeRepo(t)
	if err := s.RevokeUserTokens(context.Background(), "u1"); err != nil {
		t.Fatalf("RevokeUserTokens() error = %v", err)
	}
	if len(repo.revokedUsers) != 1 || repo.revokedUsers[0] != "u1" {
		t.Errorf("RevokeUser calls = %v, want [u1]", repo.revokedUsers)
	}
	if err := s.RevokeUserTokens(context.Background(), ""); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("RevokeUserTokens(empty) error = %v, want ErrInvalidUser", err)
	}
}

func TestRevokeTokenFamily(t *testing.T) {
	s, repo := newSessionWithFakeRepo(t)
	if err := s.RevokeTokenFamily(context.Background(), "u1", "fam-1"); err != nil {
		t.Fatalf("RevokeTokenFamily() error = %v", err)
	}
	if len(repo.revokedFamilies) != 1 || repo.revokedFamilies[0] != "fam-1" {
		t.Errorf("RevokeFamily calls = %v, want [fam-1]", repo.revokedFamilies)
	}
	if err := s.RevokeTokenFamily(context.Background(), "", ""); err == nil {
		t.Error("RevokeTokenFamily(empty) should error")
	}
}
