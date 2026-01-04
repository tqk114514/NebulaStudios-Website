/**
 * internal/services/session.go
 * JWT 会话管理服务
 *
 * 功能：
 * - JWT Token 生成
 * - JWT Token 验证
 * - 会话有效期管理
 *
 * 设计原则：
 * - JWT 只存储 userId，其他数据从数据库实时获取
 * - 保证用户数据的实时性和一致性
 * - 使用 HS256 签名算法
 *
 * 依赖：
 * - github.com/golang-jwt/jwt/v5: JWT 库
 * - Config: JWT 配置
 */

package services

import (
	"auth-system/internal/utils"
	"errors"
	"fmt"
	"time"

	"auth-system/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

// ====================  错误定义 ====================

var (
	// ErrSessionNilConfig 配置为空
	ErrSessionNilConfig = errors.New("session config is nil")
	// ErrSessionEmptySecret JWT 密钥为空
	ErrSessionEmptySecret = errors.New("JWT secret is empty")
	// ErrSessionInvalidExpiry 无效的过期时间
	ErrSessionInvalidExpiry = errors.New("invalid JWT expiry duration")
	// ErrNoToken Token 为空
	ErrNoToken = errors.New("NO_TOKEN")
	// ErrTokenExpiredSession Token 已过期
	ErrTokenExpiredSession = errors.New("TOKEN_EXPIRED")
	// ErrInvalidTokenSession Token 无效
	ErrInvalidTokenSession = errors.New("INVALID_TOKEN")
	// ErrTokenError Token 错误
	ErrTokenError = errors.New("TOKEN_ERROR")
	// ErrInvalidUser 无效的用户
	ErrInvalidUser = errors.New("INVALID_USER")
	// ErrTokenGenerationFailed Token 生成失败
	ErrTokenGenerationFailed = errors.New("TOKEN_GENERATION_FAILED")
	// ErrInvalidSigningMethod 无效的签名方法
	ErrInvalidSigningMethod = errors.New("invalid signing method")
)

// ====================  常量定义 ====================

const (
	// defaultJWTExpiry 默认 JWT 过期时间
	defaultJWTExpiry = 24 * time.Hour

	// minJWTExpiry 最小 JWT 过期时间
	minJWTExpiry = 1 * time.Minute

	// maxJWTExpiry 最大 JWT 过期时间
	maxJWTExpiry = 30 * 24 * time.Hour // 30 天

	// minSecretLength 最小密钥长度
	minSecretLength = 32
)

// ====================  数据结构 ====================

// Claims JWT 声明
type Claims struct {
	UserID int64 `json:"userId"`
	jwt.RegisteredClaims
}

// SessionService Session 服务
type SessionService struct {
	jwtSecret    []byte
	jwtExpiresIn time.Duration
}

// ====================  构造函数 ====================

// NewSessionService 创建 Session 服务
// 参数：
//   - cfg: 应用配置
//
// 返回：
//   - *SessionService: Session 服务实例
func NewSessionService(cfg *config.Config) *SessionService {
	// 参数验证
	if cfg == nil {
		utils.LogPrintf("[SESSION] ERROR: Config is nil, using defaults")
		return &SessionService{
			jwtSecret:    []byte("default-secret-please-change-in-production"),
			jwtExpiresIn: defaultJWTExpiry,
		}
	}

	// 验证 JWT 密钥
	secret := cfg.JWTSecret
	if secret == "" {
		utils.LogPrintf("[SESSION] WARN: JWT secret is empty, using default")
		secret = "default-secret-please-change-in-production"
	} else if len(secret) < minSecretLength {
		utils.LogPrintf("[SESSION] WARN: JWT secret is too short (%d chars), recommended minimum is %d",
			len(secret), minSecretLength)
	}

	// 验证过期时间
	expiry := cfg.JWTExpiresIn
	if expiry <= 0 {
		utils.LogPrintf("[SESSION] WARN: Invalid JWT expiry %v, using default %v", expiry, defaultJWTExpiry)
		expiry = defaultJWTExpiry
	} else if expiry < minJWTExpiry {
		utils.LogPrintf("[SESSION] WARN: JWT expiry %v is too short, using minimum %v", expiry, minJWTExpiry)
		expiry = minJWTExpiry
	} else if expiry > maxJWTExpiry {
		utils.LogPrintf("[SESSION] WARN: JWT expiry %v is too long, using maximum %v", expiry, maxJWTExpiry)
		expiry = maxJWTExpiry
	}

	utils.LogPrintf("[SESSION] Session service initialized: expiry=%v", expiry)

	return &SessionService{
		jwtSecret:    []byte(secret),
		jwtExpiresIn: expiry,
	}
}

// NewSessionServiceWithValidation 创建 Session 服务（带验证）
// 参数：
//   - cfg: 应用配置
//
// 返回：
//   - *SessionService: Session 服务实例
//   - error: 配置无效时返回错误
func NewSessionServiceWithValidation(cfg *config.Config) (*SessionService, error) {
	// 参数验证
	if cfg == nil {
		return nil, ErrSessionNilConfig
	}

	// 验证 JWT 密钥
	if cfg.JWTSecret == "" {
		return nil, ErrSessionEmptySecret
	}

	// 验证过期时间
	if cfg.JWTExpiresIn <= 0 {
		return nil, ErrSessionInvalidExpiry
	}

	return NewSessionService(cfg), nil
}

// ====================  公开方法 ====================

// GenerateToken 生成 JWT Token
// 参数：
//   - userID: 用户 ID
//
// 返回：
//   - string: JWT Token
//   - error: 生成失败时返回错误
func (s *SessionService) GenerateToken(userID int64) (string, error) {
	// 参数验证
	if userID <= 0 {
		utils.LogPrintf("[SESSION] WARN: Invalid user ID for token generation: %d", userID)
		return "", ErrInvalidUser
	}

	// 检查服务是否已初始化
	if s == nil {
		utils.LogPrintf("[SESSION] ERROR: Session service is nil")
		return "", ErrTokenGenerationFailed
	}

	if len(s.jwtSecret) == 0 {
		utils.LogPrintf("[SESSION] ERROR: JWT secret is empty")
		return "", ErrTokenGenerationFailed
	}

	// 创建 Claims
	now := time.Now()
	claims := &Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.jwtExpiresIn)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	// 创建 Token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 签名 Token
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		utils.LogPrintf("[SESSION] ERROR: Failed to sign token: userID=%d, error=%v", userID, err)
		return "", fmt.Errorf("%w: %v", ErrTokenGenerationFailed, err)
	}

	utils.LogPrintf("[SESSION] Token generated: userID=%d, expiry=%v", userID, s.jwtExpiresIn)
	return tokenString, nil
}

// VerifyToken 验证 JWT Token
// 参数：
//   - tokenString: JWT Token 字符串
//
// 返回：
//   - *Claims: Token 声明
//   - error: 验证失败时返回错误
func (s *SessionService) VerifyToken(tokenString string) (*Claims, error) {
	// 参数验证
	if tokenString == "" {
		return nil, ErrNoToken
	}

	// 检查服务是否已初始化
	if s == nil {
		utils.LogPrintf("[SESSION] ERROR: Session service is nil")
		return nil, ErrTokenError
	}

	if len(s.jwtSecret) == 0 {
		utils.LogPrintf("[SESSION] ERROR: JWT secret is empty")
		return nil, ErrTokenError
	}

	// 解析 Token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			utils.LogPrintf("[SESSION] WARN: Unexpected signing method: %v", token.Header["alg"])
			return nil, ErrInvalidSigningMethod
		}
		return s.jwtSecret, nil
	})

	// 处理解析错误
	if err != nil {
		return nil, s.handleParseError(err)
	}

	// 验证 Token 有效性
	if token == nil {
		utils.LogPrintf("[SESSION] ERROR: Parsed token is nil")
		return nil, ErrInvalidTokenSession
	}

	// 提取 Claims
	claims, ok := token.Claims.(*Claims)
	if !ok {
		utils.LogPrintf("[SESSION] ERROR: Failed to extract claims from token")
		return nil, ErrInvalidTokenSession
	}

	// 验证 Token 是否有效
	if !token.Valid {
		utils.LogPrintf("[SESSION] WARN: Token is not valid")
		return nil, ErrInvalidTokenSession
	}

	// 验证 Claims 内容
	if err := s.validateClaims(claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// GetExpiry 获取 Token 过期时间
// 返回：
//   - time.Duration: 过期时间
func (s *SessionService) GetExpiry() time.Duration {
	if s == nil {
		return defaultJWTExpiry
	}
	return s.jwtExpiresIn
}

// IsConfigured 检查服务是否已配置
// 返回：
//   - bool: 是否已配置
func (s *SessionService) IsConfigured() bool {
	return s != nil && len(s.jwtSecret) > 0 && s.jwtExpiresIn > 0
}

// ====================  私有方法 ====================

// handleParseError 处理 Token 解析错误
// 参数：
//   - err: 原始错误
//
// 返回：
//   - error: 处理后的错误
func (s *SessionService) handleParseError(err error) error {
	// 检查是否是过期错误
	if errors.Is(err, jwt.ErrTokenExpired) {
		utils.LogPrintf("[SESSION] DEBUG: Token expired")
		return ErrTokenExpiredSession
	}

	// 检查是否是签名无效
	if errors.Is(err, jwt.ErrSignatureInvalid) {
		utils.LogPrintf("[SESSION] WARN: Invalid token signature")
		return ErrInvalidTokenSession
	}

	// 检查是否是格式错误
	if errors.Is(err, jwt.ErrTokenMalformed) {
		utils.LogPrintf("[SESSION] WARN: Malformed token")
		return ErrInvalidTokenSession
	}

	// 检查是否是未激活
	if errors.Is(err, jwt.ErrTokenNotValidYet) {
		utils.LogPrintf("[SESSION] WARN: Token not valid yet")
		return ErrInvalidTokenSession
	}

	// 其他错误
	utils.LogPrintf("[SESSION] WARN: Token parse error: %v", err)
	return ErrInvalidTokenSession
}

// validateClaims 验证 Claims 内容
// 参数：
//   - claims: Token 声明
//
// 返回：
//   - error: 验证失败时返回错误
func (s *SessionService) validateClaims(claims *Claims) error {
	if claims == nil {
		return ErrInvalidTokenSession
	}

	// 验证用户 ID
	if claims.UserID <= 0 {
		utils.LogPrintf("[SESSION] WARN: Invalid user ID in claims: %d", claims.UserID)
		return ErrInvalidUser
	}

	// 验证过期时间
	if claims.ExpiresAt == nil {
		utils.LogPrintf("[SESSION] WARN: Token has no expiry time")
		return ErrInvalidTokenSession
	}

	// 验证签发时间
	if claims.IssuedAt == nil {
		utils.LogPrintf("[SESSION] DEBUG: Token has no issued time")
		// 不返回错误，只记录日志
	}

	return nil
}
