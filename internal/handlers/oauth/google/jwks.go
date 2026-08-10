package google

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"

	"auth-system/internal/utils"

	"github.com/golang-jwt/jwt/v5"
)

// Google OIDC 常量
const (
	// googleIssuer Google id_token 的固定 issuer
	googleIssuer = "https://accounts.google.com"
)

var (
	ErrVerifierNotConfigured = errors.New("google id token verifier not configured")
	ErrInvalidJWKSBase64     = errors.New("GOOGLE_JWKS_SHA256 is not a valid base64 jwks document")
	ErrIDTokenMissing        = errors.New("id_token missing from token response")
	ErrIDTokenVerification   = errors.New("id_token verification failed")
	ErrKeyNotFound           = errors.New("no jwks key found for id_token kid")
	ErrInvalidClaims         = errors.New("id_token claims invalid")
)

// GoogleIDTokenClaims Google id_token 的声明（OIDC 标准字段）
type GoogleIDTokenClaims struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	jwt.RegisteredClaims
}

// googleJWK Google JWKS 中的单个 RSA 公钥条目
type googleJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// googleJWKSDocument Google /certs 返回的 JWKS 文档
type googleJWKSDocument struct {
	Keys []googleJWK `json:"keys"`
}

// GoogleIDTokenVerifier 验证 Google id_token 的 RSA 签名。
//
// 公钥来源：GOOGLE_JWKS_SHA256 配置的 JWKS 文档（base64），部署前自行从
// https://www.googleapis.com/oauth2/v3/certs 获取后编码，运行时不做任何网络请求。
// 身份（sub/email/email_verified）只从验签后的 id_token 提取。
type GoogleIDTokenVerifier struct {
	clientID string
	keys     map[string]*rsa.PublicKey // 预置公钥集（kid → 公钥）

	mu sync.RWMutex
}

// NewGoogleIDTokenVerifier 创建验证器。
// jwksBase64 为 GOOGLE_JWKS_SHA256：Google JWKS 文档的 base64（StdEncoding）。
// 为空或解码/解析失败时返回错误：公钥不可用则禁止启用 Google 登录。
func NewGoogleIDTokenVerifier(clientID, jwksBase64 string) (*GoogleIDTokenVerifier, error) {
	if clientID == "" {
		return nil, fmt.Errorf("%w: clientID is empty", ErrVerifierNotConfigured)
	}

	jwksBase64 = strings.TrimSpace(jwksBase64)
	if jwksBase64 == "" {
		return nil, ErrInvalidJWKSBase64
	}
	data, err := base64.StdEncoding.DecodeString(jwksBase64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJWKSBase64, err)
	}
	keys, err := parseJWKS(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJWKSBase64, err)
	}

	// 记录公钥集指纹，便于核对配置与获取源是否一致
	sum := sha256.Sum256(data)
	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("ID token verifier initialized: clientID=%s, keys=%d, jwksSha256=%s",
		clientID, len(keys), hex.EncodeToString(sum[:])))
	return &GoogleIDTokenVerifier{clientID: clientID, keys: keys}, nil
}

// Verify 验证 id_token 签名并校验 iss/aud/exp，返回声明。
// kid 不在预置公钥集中（Google 已轮换密钥）时拒绝，需更新 GOOGLE_JWKS_SHA256 后重启。
func (v *GoogleIDTokenVerifier) Verify(ctx context.Context, idToken string) (*GoogleIDTokenClaims, error) {
	if v == nil || idToken == "" {
		return nil, ErrIDTokenMissing
	}

	// 1. 解析头部取 kid（不验证签名）
	unverified, _, err := jwt.NewParser().ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		utils.LogWarn("OAUTH-GOOGLE", "Failed to parse id_token header", err.Error())
		return nil, fmt.Errorf("%w: %v", ErrIDTokenVerification, err)
	}
	kid, _ := unverified.Header["kid"].(string)
	if kid == "" {
		utils.LogWarn("OAUTH-GOOGLE", "id_token missing kid header", "")
		return nil, fmt.Errorf("%w: missing kid", ErrIDTokenVerification)
	}

	// 2. 取预置公钥；未知 kid 拒绝（提示更新 GOOGLE_JWKS_SHA256）
	v.mu.RLock()
	pubKey, ok := v.keys[kid]
	v.mu.RUnlock()
	if !ok {
		utils.LogWarn("OAUTH-GOOGLE", "id_token kid not found in preseeded jwks, update GOOGLE_JWKS_SHA256",
			fmt.Sprintf("kid=%s", kid))
		return nil, fmt.Errorf("%w: %v", ErrIDTokenVerification, ErrKeyNotFound)
	}

	// 3. 验证 RS256 签名（自定义 claims 必须用 ParseWithClaims）
	token, err := jwt.ParseWithClaims(idToken, &GoogleIDTokenClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return pubKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		utils.LogWarn("OAUTH-GOOGLE", "id_token signature verification failed", err.Error())
		return nil, fmt.Errorf("%w: %v", ErrIDTokenVerification, err)
	}

	claims, ok := token.Claims.(*GoogleIDTokenClaims)
	if !ok || !token.Valid {
		utils.LogWarn("OAUTH-GOOGLE", "id_token claims invalid", "")
		return nil, fmt.Errorf("%w: invalid token", ErrIDTokenVerification)
	}

	// 4. 校验 issuer（Google 固定，精确匹配）
	if claims.Issuer != googleIssuer {
		utils.LogWarn("OAUTH-GOOGLE", "id_token issuer mismatch", fmt.Sprintf("expected=%s, got=%s", googleIssuer, claims.Issuer))
		return nil, fmt.Errorf("%w: issuer mismatch", ErrInvalidClaims)
	}

	// 5. 校验 audience（必须包含本应用 client_id）
	if !slices.Contains(claims.Audience, v.clientID) {
		utils.LogWarn("OAUTH-GOOGLE", "id_token audience mismatch", fmt.Sprintf("expected clientID=%s, aud=%v", v.clientID, claims.Audience))
		return nil, fmt.Errorf("%w: audience mismatch", ErrInvalidClaims)
	}

	if claims.Sub == "" {
		utils.LogWarn("OAUTH-GOOGLE", "id_token missing sub", "")
		return nil, fmt.Errorf("%w: missing sub", ErrInvalidClaims)
	}

	return claims, nil
}

// parseJWKS 解析 JWKS 文档为 kid → RSA 公钥映射
func parseJWKS(data []byte) (map[string]*rsa.PublicKey, error) {
	var doc googleJWKSDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid jwks json: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Use != "sig" || k.Kid == "" {
			continue
		}
		pubKey, err := googleJWKToRSAPublicKey(k)
		if err != nil {
			utils.LogWarn("OAUTH-GOOGLE", "Failed to parse JWK", fmt.Sprintf("kid=%s: %v", k.Kid, err))
			continue
		}
		keys[k.Kid] = pubKey
	}
	if len(keys) == 0 {
		return nil, errors.New("no usable RSA sig keys in jwks")
	}
	return keys, nil
}

// googleJWKToRSAPublicKey 将 JWK 的 n/e 转为 *rsa.PublicKey
func googleJWKToRSAPublicKey(k googleJWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	var eInt int
	for _, b := range eBytes {
		eInt = eInt<<8 + int(b)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: eInt,
	}, nil
}
