package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/models"

	"github.com/golang-jwt/jwt/v5"
)

func TestRefreshTokenExpiryClamping(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"below minimum", time.Minute, minRefreshTokenExpiry},
		{"above maximum", 200 * 24 * time.Hour, maxRefreshTokenExpiry},
		{"within range", 7 * 24 * time.Hour, 7 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewSessionService(&config.Config{
				JWTPrivateKey:      testECDSAPEM(t),
				RefreshTokenExpiry: tt.in,
			}, nil)
			if err != nil {
				t.Fatalf("NewSessionService: %v", err)
			}
			if s.refreshTokenExpiry != tt.want {
				t.Errorf("refreshTokenExpiry = %v, want %v", s.refreshTokenExpiry, tt.want)
			}
		})
	}
}

func TestGenerateTokensGuards(t *testing.T) {
	s := testSessionService(t, time.Hour)

	// 空 UID
	if _, _, err := s.GenerateTokens(context.Background(), "", false); err != ErrInvalidUser {
		t.Errorf("empty uid error = %v, want ErrInvalidUser", err)
	}

	// nil 服务
	var nilSvc *SessionService
	if _, _, err := nilSvc.GenerateTokens(context.Background(), "u1", false); err != ErrTokenGenerationFailed {
		t.Errorf("nil service error = %v, want ErrTokenGenerationFailed", err)
	}

	// 私钥缺失
	noKey := &SessionService{accessTokenExpiry: time.Hour}
	if _, _, err := noKey.GenerateTokens(context.Background(), "u1", false); err != ErrTokenGenerationFailed {
		t.Errorf("missing key error = %v, want ErrTokenGenerationFailed", err)
	}
}

func TestRefreshTokensErrorBranches(t *testing.T) {
	ctx := context.Background()

	// nil 服务
	var nilSvc *SessionService
	if _, _, err := nilSvc.RefreshTokens(ctx, "token"); err != ErrTokenError {
		t.Errorf("nil service error = %v, want ErrTokenError", err)
	}

	// 查找失败且非"未找到"
	s, repo := newSessionWithFakeRepo(t)
	repo.findErr = errors.New("db down")
	if _, _, err := s.RefreshTokens(ctx, "token"); err == nil ||
		!strings.Contains(err.Error(), "failed to find refresh token") {
		t.Errorf("find error = %v, want wrapped find failure", err)
	}

	// 标记使用失败且非"重放"
	s, repo = newSessionWithFakeRepo(t)
	repo.findResult = unexpiredSessionToken()
	repo.markUsedErr = errors.New("mark failed")
	if _, _, err := s.RefreshTokens(ctx, "token"); err == nil || !strings.Contains(err.Error(), "failed to mark refresh token as used") {
		t.Errorf("mark error = %v, want wrapped mark failure", err)
	}
}

func TestRevokeGuards(t *testing.T) {
	s, repo := newSessionWithFakeRepo(t)

	if err := s.RevokeUserTokens(context.Background(), ""); err != ErrInvalidUser {
		t.Errorf("empty uid error = %v, want ErrInvalidUser", err)
	}
	if err := s.RevokeTokenFamily(context.Background(), "", "fam-1"); err != ErrInvalidUser {
		t.Errorf("empty uid error = %v, want ErrInvalidUser", err)
	}
	if err := s.RevokeTokenFamily(context.Background(), "u1", ""); err == nil {
		t.Error("empty family id should be rejected")
	}

	if err := s.RevokeTokenFamily(context.Background(), "u1", "fam-1"); err != nil {
		t.Errorf("RevokeTokenFamily: %v", err)
	}
	if len(repo.revokedFamilies) != 1 || repo.revokedFamilies[0] != "fam-1" {
		t.Errorf("revokedFamilies = %v, want [fam-1]", repo.revokedFamilies)
	}
}

// 直接构造 JWT 覆盖 claims 校验分支：签名有效但内容不合规时仍必须拒绝
func TestValidateClaimsRejects(t *testing.T) {
	s := testSessionService(t, time.Hour)

	sign := func(claims *Claims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		signed, err := token.SignedString(s.privateKey)
		if err != nil {
			t.Fatalf("SignedString: %v", err)
		}
		return signed
	}

	tests := []struct {
		name   string
		claims *Claims
	}{
		{"empty uid", &Claims{RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "test-issuer",
			Audience:  jwt.ClaimStrings{"test-audience"},
		}}},
		{"no expiry", &Claims{UID: "u1", RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   "test-issuer",
			Audience: jwt.ClaimStrings{"test-audience"},
		}}},
		{"wrong issuer", &Claims{UID: "u1", RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "other-issuer",
			Audience:  jwt.ClaimStrings{"test-audience"},
		}}},
		{"wrong audience", &Claims{UID: "u1", RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "test-issuer",
			Audience:  jwt.ClaimStrings{"other-audience"},
		}}},
		{"not valid yet", &Claims{UID: "u1", RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "test-issuer",
			Audience:  jwt.ClaimStrings{"test-audience"},
		}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.VerifyToken(sign(tt.claims)); err == nil {
				t.Error("invalid claims should be rejected")
			}
		})
	}
}

func TestHandleParseErrorMapping(t *testing.T) {
	s := testSessionService(t, time.Hour)

	tests := map[error]error{
		ErrInvalidSigningMethod: ErrInvalidSigningMethod,
		jwt.ErrTokenExpired:     ErrTokenExpiredSession,
		jwt.ErrSignatureInvalid: ErrInvalidTokenSession,
		jwt.ErrTokenMalformed:   ErrInvalidTokenSession,
		jwt.ErrTokenNotValidYet: ErrInvalidTokenSession,
		errors.New("unknown"):   ErrInvalidTokenSession,
	}

	for in, want := range tests {
		if got := s.handleParseError(in); got != want {
			t.Errorf("handleParseError(%v) = %v, want %v", in, got, want)
		}
	}
}

var _ = models.ErrSessionTokenNotFound
