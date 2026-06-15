package services

import (
	"auth-system/internal/models"
	"auth-system/internal/utils"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"time"

	"auth-system/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrSessionNilConfig         = errors.New("session config is nil")
	ErrSessionEmptyPrivateKey   = errors.New("ECDSA private key is empty")
	ErrSessionInvalidPrivateKey = errors.New("ECDSA private key is invalid")
	ErrNoToken                  = errors.New("NO_TOKEN")
	ErrTokenExpiredSession      = errors.New("TOKEN_EXPIRED")
	ErrInvalidTokenSession      = errors.New("INVALID_TOKEN")
	ErrTokenError               = errors.New("TOKEN_ERROR")
	ErrInvalidUser              = errors.New("INVALID_USER")
	ErrTokenGenerationFailed    = errors.New("TOKEN_GENERATION_FAILED")
	ErrInvalidSigningMethod     = errors.New("invalid signing method")
	ErrRefreshTokenExpired      = errors.New("REFRESH_TOKEN_EXPIRED")
	ErrRefreshTokenReused       = errors.New("REFRESH_TOKEN_REUSED")
	ErrRefreshTokenInvalid      = errors.New("INVALID_REFRESH_TOKEN")
)

const (
	defaultAccessTokenExpiry  = 1 * time.Hour
	bannedAccessTokenExpiry   = 15 * time.Minute
	minAccessTokenExpiry      = 1 * time.Minute
	maxAccessTokenExpiry      = 24 * time.Hour
	defaultRefreshTokenExpiry = 30 * 24 * time.Hour
	minRefreshTokenExpiry     = 1 * 24 * time.Hour
	maxRefreshTokenExpiry     = 90 * 24 * time.Hour
	refreshTokenByteSize      = 32
)

// Claims JWT 声明
type Claims struct {
	UID    string `json:"uid"`
	Banned *bool  `json:"banned,omitempty"`
	jwt.RegisteredClaims
}

// SessionService Session 服务
type SessionService struct {
	privateKey         *ecdsa.PrivateKey
	accessTokenExpiry  time.Duration
	refreshTokenExpiry time.Duration
	jwtIssuer          string
	jwtAudience        string
	sessionTokenRepo   *models.SessionTokenRepository
}

// NewSessionService 创建 Session 服务（带配置验证）
func NewSessionService(cfg *config.Config, pool *pgxpool.Pool) (*SessionService, error) {
	if cfg == nil {
		return nil, ErrSessionNilConfig
	}

	if cfg.JWTPrivateKey == "" {
		return nil, ErrSessionEmptyPrivateKey
	}

	privateKey, err := parseECDSAPrivateKey(cfg.JWTPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSessionInvalidPrivateKey, err)
	}

	issuer := cfg.JWTIssuer
	if issuer == "" {
		issuer = "auth-system"
	}
	audience := cfg.JWTAudience
	if audience == "" {
		audience = "auth-system-users"
	}

	accessExpiry := cfg.AccessTokenExpiry
	if accessExpiry <= 0 {
		accessExpiry = defaultAccessTokenExpiry
	}
	if accessExpiry < minAccessTokenExpiry {
		accessExpiry = minAccessTokenExpiry
		utils.LogWarn("SESSION", "Access token expiry adjusted to minimum", fmt.Sprintf("newExpiry=%v", accessExpiry))
	}
	if accessExpiry > maxAccessTokenExpiry {
		accessExpiry = maxAccessTokenExpiry
		utils.LogWarn("SESSION", "Access token expiry adjusted to maximum", fmt.Sprintf("newExpiry=%v", accessExpiry))
	}

	refreshExpiry := cfg.RefreshTokenExpiry
	if refreshExpiry <= 0 {
		refreshExpiry = defaultRefreshTokenExpiry
	}
	if refreshExpiry < minRefreshTokenExpiry {
		refreshExpiry = minRefreshTokenExpiry
		utils.LogWarn("SESSION", "Refresh token expiry adjusted to minimum", fmt.Sprintf("newExpiry=%v", refreshExpiry))
	}
	if refreshExpiry > maxRefreshTokenExpiry {
		refreshExpiry = maxRefreshTokenExpiry
		utils.LogWarn("SESSION", "Refresh token expiry adjusted to maximum", fmt.Sprintf("newExpiry=%v", refreshExpiry))
	}

	utils.LogInfo("SESSION", fmt.Sprintf("Session service initialized: accessExpiry=%v, refreshExpiry=%v, issuer=%s, audience=%s, alg=ES256",
		accessExpiry, refreshExpiry, issuer, audience))

	return &SessionService{
		privateKey:         privateKey,
		accessTokenExpiry:  accessExpiry,
		refreshTokenExpiry: refreshExpiry,
		jwtIssuer:          issuer,
		jwtAudience:        audience,
		sessionTokenRepo:   models.NewSessionTokenRepository(pool),
	}, nil
}

// GenerateTokens 生成 access_token + refresh_token
// banned: true = 封禁用户，只签发短期 access_token，不签发 refresh_token
func (s *SessionService) GenerateTokens(ctx context.Context, uid string, banned bool) (accessToken string, refreshToken string, err error) {
	if uid == "" {
		utils.LogWarn("SESSION", "Invalid user UID for token generation", fmt.Sprintf("uid=%s", uid))
		return "", "", ErrInvalidUser
	}

	if s == nil {
		utils.LogError("SESSION", "GenerateTokens", fmt.Errorf("session service is nil"), "")
		return "", "", ErrTokenGenerationFailed
	}

	if s.privateKey == nil {
		utils.LogError("SESSION", "GenerateTokens", fmt.Errorf("ECDSA private key is nil"), "")
		return "", "", ErrTokenGenerationFailed
	}

	var accessExpiry time.Duration
	var bannedPtr *bool
	if banned {
		accessExpiry = bannedAccessTokenExpiry
		b := true
		bannedPtr = &b
	} else {
		accessExpiry = s.accessTokenExpiry
		b := false
		bannedPtr = &b
	}

	accessToken, err = s.generateAccessToken(uid, bannedPtr, accessExpiry)
	if err != nil {
		return "", "", err
	}

	if banned {
		utils.LogInfo("SESSION", fmt.Sprintf("Banned user access token generated: uid=%s, expiry=%v", uid, accessExpiry))
		return accessToken, "", nil
	}

	refreshToken, err = s.generateRefreshToken(ctx, uid, false)
	if err != nil {
		return "", "", err
	}

	utils.LogInfo("SESSION", fmt.Sprintf("Tokens generated: uid=%s, accessExpiry=%v, refreshExpiry=%v", uid, accessExpiry, s.refreshTokenExpiry))
	return accessToken, refreshToken, nil
}

// RefreshTokens 使用 refresh_token 轮转获取新的 token 对
// 检测到已使用的 refresh_token 被重放时，撤销整个 token 家族并返回错误
func (s *SessionService) RefreshTokens(ctx context.Context, refreshTokenStr string) (newAccessToken string, newRefreshToken string, err error) {
	if refreshTokenStr == "" {
		return "", "", ErrRefreshTokenInvalid
	}

	if s == nil {
		utils.LogError("SESSION", "RefreshTokens", fmt.Errorf("session service is nil"), "")
		return "", "", ErrTokenError
	}

	tokenHash := models.HashToken(refreshTokenStr)

	existing, findErr := s.sessionTokenRepo.FindByHash(ctx, tokenHash)
	if findErr != nil {
		if errors.Is(findErr, models.ErrSessionTokenNotFound) {
			return "", "", ErrRefreshTokenInvalid
		}
		return "", "", fmt.Errorf("failed to find refresh token: %w", findErr)
	}

	if existing.IsExpired() {
		return "", "", ErrRefreshTokenExpired
	}

	if existing.Used {
		s.sessionTokenRepo.RevokeFamily(ctx, existing.FamilyID)
		utils.LogWarn("SESSION", "Refresh token reuse detected - family revoked",
			fmt.Sprintf("user_uid=%s, family_id=%s", existing.UserUID, existing.FamilyID))
		return "", "", ErrRefreshTokenReused
	}

	if markErr := s.sessionTokenRepo.MarkUsed(ctx, existing.ID); markErr != nil {
		return "", "", fmt.Errorf("failed to mark refresh token as used: %w", markErr)
	}

	newAccessToken, err = s.generateAccessToken(existing.UserUID, &existing.Banned, s.accessTokenExpiry)
	if err != nil {
		return "", "", err
	}

	newRefreshToken, err = s.generateRefreshToken(ctx, existing.UserUID, existing.Banned)
	if err != nil {
		return "", "", err
	}

	utils.LogInfo("SESSION", fmt.Sprintf("Tokens refreshed: uid=%s, family_id=%s", existing.UserUID, existing.FamilyID))
	return newAccessToken, newRefreshToken, nil
}

// RevokeUserTokens 撤销用户的所有 refresh_token
func (s *SessionService) RevokeUserTokens(ctx context.Context, uid string) error {
	if uid == "" {
		return ErrInvalidUser
	}

	_, err := s.sessionTokenRepo.RevokeUser(ctx, uid)
	return err
}

// RevokeTokenFamily 撤销指定的 token 家族
func (s *SessionService) RevokeTokenFamily(ctx context.Context, uid string, familyID string) error {
	if uid == "" {
		return ErrInvalidUser
	}
	if familyID == "" {
		return fmt.Errorf("family_id is empty")
	}

	_, err := s.sessionTokenRepo.RevokeFamily(ctx, familyID)
	return err
}

// VerifyToken 验证 JWT Token（ES256 公钥验证）
func (s *SessionService) VerifyToken(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrNoToken
	}

	if s == nil {
		utils.LogError("SESSION", "VerifyToken", fmt.Errorf("session service is nil"), "")
		return nil, ErrTokenError
	}

	if s.privateKey == nil {
		utils.LogError("SESSION", "VerifyToken", fmt.Errorf("ECDSA private key is nil"), "")
		return nil, ErrTokenError
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			utils.LogWarn("SESSION", "Unexpected signing method", fmt.Sprintf("alg=%v", token.Header["alg"]))
			return nil, ErrInvalidSigningMethod
		}
		return &s.privateKey.PublicKey, nil
	})

	if err != nil {
		return nil, s.handleParseError(err)
	}

	if token == nil {
		utils.LogError("SESSION", "VerifyToken", fmt.Errorf("parsed token is nil"), "")
		return nil, ErrInvalidTokenSession
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		utils.LogError("SESSION", "VerifyToken", fmt.Errorf("failed to extract claims"), "")
		return nil, ErrInvalidTokenSession
	}

	if !token.Valid {
		utils.LogWarn("SESSION", "Token is not valid", "")
		return nil, ErrInvalidTokenSession
	}

	if err := s.validateClaims(claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// GetExpiry 获取 Access Token 过期时间
func (s *SessionService) GetExpiry() time.Duration {
	if s == nil {
		return defaultAccessTokenExpiry
	}
	return s.accessTokenExpiry
}

// IsConfigured 检查服务是否已配置
func (s *SessionService) IsConfigured() bool {
	return s != nil && s.privateKey != nil && s.accessTokenExpiry > 0
}

// generateAccessToken 生成 access_token（JWT ES256）
func (s *SessionService) generateAccessToken(uid string, banned *bool, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UID:    uid,
		Banned: banned,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    s.jwtIssuer,
			Audience:  jwt.ClaimStrings{s.jwtAudience},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)

	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		utils.LogError("SESSION", "generateAccessToken", err, fmt.Sprintf("Failed to sign token: uid=%s", uid))
		return "", fmt.Errorf("%w: %v", ErrTokenGenerationFailed, err)
	}

	return tokenString, nil
}

// generateRefreshToken 生成 refresh_token 并写入数据库
func (s *SessionService) generateRefreshToken(ctx context.Context, uid string, banned bool) (string, error) {
	bytes := make([]byte, refreshTokenByteSize)
	if _, err := rand.Read(bytes); err != nil {
		return "", utils.LogError("SESSION", "generateRefreshToken", err, "failed to generate random bytes")
	}

	tokenStr := hex.EncodeToString(bytes)
	tokenHash := models.HashToken(tokenStr)

	familyIDBytes := make([]byte, 16)
	if _, err := rand.Read(familyIDBytes); err != nil {
		return "", utils.LogError("SESSION", "generateRefreshToken", err, "failed to generate family_id")
	}
	familyID := hex.EncodeToString(familyIDBytes)

	sessionToken := &models.SessionToken{
		TokenHash: tokenHash,
		UserUID:   uid,
		FamilyID:  familyID,
		Banned:    banned,
		ExpiresAt: time.Now().Add(s.refreshTokenExpiry),
	}

	if err := s.sessionTokenRepo.Create(ctx, sessionToken); err != nil {
		return "", err
	}

	return tokenStr, nil
}

// parseECDSAPrivateKey 解析 PEM 格式的 ECDSA 私钥
func parseECDSAPrivateKey(pemData string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err == nil {
		return key, nil
	}

	pkcs8Key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ECDSA private key: %w", err)
	}

	ecKey, ok := pkcs8Key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an ECDSA private key")
	}

	return ecKey, nil
}

// handleParseError 处理 Token 解析错误
func (s *SessionService) handleParseError(err error) error {
	if errors.Is(err, jwt.ErrTokenExpired) {
		utils.LogDebug("SESSION", "Token expired")
		return ErrTokenExpiredSession
	}

	if errors.Is(err, jwt.ErrSignatureInvalid) {
		utils.LogWarn("SESSION", "Invalid token signature", "")
		return ErrInvalidTokenSession
	}

	if errors.Is(err, jwt.ErrTokenMalformed) {
		utils.LogWarn("SESSION", "Malformed token", "")
		return ErrInvalidTokenSession
	}

	if errors.Is(err, jwt.ErrTokenNotValidYet) {
		utils.LogWarn("SESSION", "Token not valid yet", "")
		return ErrInvalidTokenSession
	}

	utils.LogWarn("SESSION", "Token parse error", fmt.Sprintf("error=%v", err))
	return ErrInvalidTokenSession
}

// validateClaims 验证 Claims 内容
func (s *SessionService) validateClaims(claims *Claims) error {
	if claims == nil {
		return ErrInvalidTokenSession
	}

	if claims.UID == "" {
		utils.LogWarn("SESSION", "Invalid user UID in claims", fmt.Sprintf("uid=%s", claims.UID))
		return ErrInvalidUser
	}

	if claims.ExpiresAt == nil {
		utils.LogWarn("SESSION", "Token has no expiry time", "")
		return ErrInvalidTokenSession
	}

	if claims.Issuer != s.jwtIssuer {
		utils.LogWarn("SESSION", "Invalid token issuer", fmt.Sprintf("expected=%s, got=%s", s.jwtIssuer, claims.Issuer))
		return ErrInvalidTokenSession
	}

	audienceValid := slices.Contains(claims.Audience, s.jwtAudience)
	if !audienceValid {
		utils.LogWarn("SESSION", "Invalid token audience", fmt.Sprintf("expected=%s, got=%v", s.jwtAudience, claims.Audience))
		return ErrInvalidTokenSession
	}

	if claims.IssuedAt == nil {
		utils.LogDebug("SESSION", "Token has no issued time")
	}

	return nil
}
