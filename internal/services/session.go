package services

import (
	"auth-system/internal/utils"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"time"

	"auth-system/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrSessionNilConfig         = errors.New("session config is nil")
	ErrSessionEmptyPrivateKey   = errors.New("ECDSA private key is empty")
	ErrSessionInvalidPrivateKey = errors.New("ECDSA private key is invalid")
	ErrSessionInvalidExpiry     = errors.New("invalid JWT expiry duration")
	ErrNoToken                  = errors.New("NO_TOKEN")
	ErrTokenExpiredSession      = errors.New("TOKEN_EXPIRED")
	ErrInvalidTokenSession      = errors.New("INVALID_TOKEN")
	ErrTokenError               = errors.New("TOKEN_ERROR")
	ErrInvalidUser              = errors.New("INVALID_USER")
	ErrTokenGenerationFailed    = errors.New("TOKEN_GENERATION_FAILED")
	ErrInvalidSigningMethod     = errors.New("invalid signing method")
)

const (
	defaultJWTExpiry = 24 * time.Hour
	minJWTExpiry     = 1 * time.Minute
	maxJWTExpiry     = 30 * 24 * time.Hour
)

// Claims JWT 声明
type Claims struct {
	UID string `json:"uid"`
	jwt.RegisteredClaims
}

// SessionService Session 服务
type SessionService struct {
	privateKey   *ecdsa.PrivateKey
	jwtExpiresIn time.Duration
	jwtIssuer    string
	jwtAudience  string
}

// NewSessionService 创建 Session 服务（带配置验证）
func NewSessionService(cfg *config.Config) (*SessionService, error) {
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

	if cfg.JWTExpiresIn <= 0 {
		return nil, ErrSessionInvalidExpiry
	}

	issuer := cfg.JWTIssuer
	if issuer == "" {
		issuer = "auth-system"
	}
	audience := cfg.JWTAudience
	if audience == "" {
		audience = "auth-system-users"
	}

	expiry := cfg.JWTExpiresIn
	if expiry < minJWTExpiry {
		expiry = minJWTExpiry
		utils.LogWarn("SESSION", "JWT expiry adjusted to minimum", fmt.Sprintf("newExpiry=%v", expiry))
	}
	if expiry > maxJWTExpiry {
		expiry = maxJWTExpiry
		utils.LogWarn("SESSION", "JWT expiry adjusted to maximum", fmt.Sprintf("newExpiry=%v", expiry))
	}

	utils.LogInfo("SESSION", fmt.Sprintf("Session service initialized: expiry=%v, issuer=%s, audience=%s, alg=ES256", expiry, issuer, audience))

	return &SessionService{
		privateKey:   privateKey,
		jwtExpiresIn: expiry,
		jwtIssuer:    issuer,
		jwtAudience:  audience,
	}, nil
}

// GenerateToken 生成 JWT Token（ES256 签名）
func (s *SessionService) GenerateToken(uid string) (string, error) {
	if uid == "" {
		utils.LogWarn("SESSION", "Invalid user UID for token generation", fmt.Sprintf("uid=%s", uid))
		return "", ErrInvalidUser
	}

	if s == nil {
		utils.LogError("SESSION", "GenerateToken", fmt.Errorf("session service is nil"), "")
		return "", ErrTokenGenerationFailed
	}

	if s.privateKey == nil {
		utils.LogError("SESSION", "GenerateToken", fmt.Errorf("ECDSA private key is nil"), "")
		return "", ErrTokenGenerationFailed
	}

	now := time.Now()
	claims := &Claims{
		UID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.jwtExpiresIn)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    s.jwtIssuer,
			Audience:  jwt.ClaimStrings{s.jwtAudience},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)

	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		utils.LogError("SESSION", "GenerateToken", err, fmt.Sprintf("Failed to sign token: uid=%s", uid))
		return "", fmt.Errorf("%w: %v", ErrTokenGenerationFailed, err)
	}

	utils.LogInfo("SESSION", fmt.Sprintf("Token generated: uid=%s, expiry=%v", uid, s.jwtExpiresIn))
	return tokenString, nil
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

// GetExpiry 获取 Token 过期时间
func (s *SessionService) GetExpiry() time.Duration {
	if s == nil {
		return defaultJWTExpiry
	}
	return s.jwtExpiresIn
}

// IsConfigured 检查服务是否已配置
func (s *SessionService) IsConfigured() bool {
	return s != nil && s.privateKey != nil && s.jwtExpiresIn > 0
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
		// 不返回错误，只记录日志
	}

	return nil
}
