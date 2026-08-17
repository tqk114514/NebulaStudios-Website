package google

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"auth-system/internal/utils"

	"github.com/golang-jwt/jwt/v5"
)

// Google OIDC 常量
const (
	// googleIssuer Google id_token 的固定 issuer
	googleIssuer = "https://accounts.google.com"

	// workerResponseMaxAge 代理签名响应允许的时间戳偏移（±300s），防截获重放
	workerResponseMaxAge = 300 * time.Second
)

var (
	ErrVerifierNotConfigured  = errors.New("google id token verifier not configured")
	ErrInvalidWorkerPublicKey = errors.New("WORKER_SIGNING_PUBLIC_KEY is not a valid PEM Ed25519 public key")
	ErrIDTokenMissing         = errors.New("id_token missing from token response")
	ErrWorkerSignature        = errors.New("worker response signature verification failed")
	ErrWorkerTimestampStale   = errors.New("worker response timestamp out of allowed window")
	ErrInvalidClaims          = errors.New("id_token claims invalid")
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

// WorkerTokenEnvelope 代理 /token 端点的签名响应格式（见 google-oauth-proxy.js）
type WorkerTokenEnvelope struct {
	Data      string `json:"data"`      // Google oauth2/token 原始响应体（逐字节原文）
	Timestamp int64  `json:"timestamp"` // 签名时间戳（Unix 秒）
	Signature string `json:"signature"` // base64(Ed25519(timestamp + "\n" + data))
}

// WorkerTokenVerifier 验证代理 Worker 对 Google token 响应的签名背书，并校验 id_token 业务声明。
//
// 信任链：Google（Worker 现场用 /certs 验 id_token 的 RS256 签名，防"Google→代理"段被替换）
// → Worker（Ed25519 私钥签名背书）→ 本服务（WORKER_SIGNING_PUBLIC_KEY 预置公钥验签）。
// 本服务不持有 Google 公钥，JWKS 轮换由 Worker 自动消化（设计详见 TODO.md ADR-6）。
type WorkerTokenVerifier struct {
	clientID string
	pubKey   ed25519.PublicKey
}

// NewWorkerTokenVerifier 创建验证器。
// publicKeyPEM 为 WORKER_SIGNING_PUBLIC_KEY：Ed25519 公钥 PEM 全文
// （与 JWT_PRIVATE_KEY 的配置方式一致，由 openssl pkey -pubout 生成，多行值用引号包裹）。
// 为空或解析失败时返回错误：验签公钥不可用则禁止启用 Google 登录（fail-closed）。
func NewWorkerTokenVerifier(clientID, publicKeyPEM string) (*WorkerTokenVerifier, error) {
	if clientID == "" {
		return nil, fmt.Errorf("%w: clientID is empty", ErrVerifierNotConfigured)
	}

	publicKeyPEM = strings.TrimSpace(publicKeyPEM)
	if publicKeyPEM == "" {
		return nil, ErrInvalidWorkerPublicKey
	}
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block found", ErrInvalidWorkerPublicKey)
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidWorkerPublicKey, err)
	}
	pubKey, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: not an Ed25519 public key", ErrInvalidWorkerPublicKey)
	}

	utils.LogInfo("OAUTH-GOOGLE", "Worker token verifier initialized", "client_id", clientID)
	return &WorkerTokenVerifier{clientID: clientID, pubKey: pubKey}, nil
}

// VerifyEnvelope 验证代理签名信封（时间戳新鲜度 + Ed25519 签名），返回 Google 原始响应体。
func (v *WorkerTokenVerifier) VerifyEnvelope(env *WorkerTokenEnvelope) ([]byte, error) {
	if v == nil || env == nil {
		return nil, ErrVerifierNotConfigured
	}

	now := time.Now()
	if d := now.Sub(time.Unix(env.Timestamp, 0)); d > workerResponseMaxAge || d < -workerResponseMaxAge {
		return nil, fmt.Errorf("%w: ts=%d now=%d", ErrWorkerTimestampStale, env.Timestamp, now.Unix())
	}

	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return nil, fmt.Errorf("%w: bad signature base64: %v", ErrWorkerSignature, err)
	}
	message := strconv.FormatInt(env.Timestamp, 10) + "\n" + env.Data
	if !ed25519.Verify(v.pubKey, []byte(message), sig) {
		return nil, ErrWorkerSignature
	}

	return []byte(env.Data), nil
}

// VerifyIDTokenClaims 校验 id_token 业务声明（iss/aud/exp/sub）。
// id_token 的 Google 签名已由代理 Worker 现场验过（VerifyEnvelope 信任链），
// 此处不重复验签，仅做本应用的业务语义校验。
func (v *WorkerTokenVerifier) VerifyIDTokenClaims(ctx context.Context, idToken string) (*GoogleIDTokenClaims, error) {
	if v == nil || idToken == "" {
		return nil, ErrIDTokenMissing
	}

	// 解析声明（不验证签名——签名背书已由 VerifyEnvelope 完成）
	token, _, err := jwt.NewParser().ParseUnverified(idToken, &GoogleIDTokenClaims{})
	if err != nil {
		utils.LogWarnCtx(ctx, "OAUTH-GOOGLE", "Failed to parse id_token claims", "error", err)
		return nil, fmt.Errorf("%w: %v", ErrInvalidClaims, err)
	}
	claims, ok := token.Claims.(*GoogleIDTokenClaims)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected claims type", ErrInvalidClaims)
	}

	// 校验 issuer（Google 固定，精确匹配）
	if claims.Issuer != googleIssuer {
		utils.LogWarnCtx(ctx, "OAUTH-GOOGLE", "id_token issuer mismatch", "expected", googleIssuer, "got", claims.Issuer)
		return nil, fmt.Errorf("%w: issuer mismatch", ErrInvalidClaims)
	}

	// 校验 audience（必须包含本应用 client_id）
	if !slices.Contains(claims.Audience, v.clientID) {
		utils.LogWarnCtx(ctx, "OAUTH-GOOGLE", "id_token audience mismatch", "expected_client_id", v.clientID, "aud", claims.Audience)
		return nil, fmt.Errorf("%w: audience mismatch", ErrInvalidClaims)
	}

	// 校验过期（ParseUnverified 不做，需手动）
	if claims.ExpiresAt == nil || !claims.ExpiresAt.After(time.Now()) {
		utils.LogWarnCtx(ctx, "OAUTH-GOOGLE", "id_token expired or missing exp")
		return nil, fmt.Errorf("%w: expired", ErrInvalidClaims)
	}

	if claims.Sub == "" {
		utils.LogWarnCtx(ctx, "OAUTH-GOOGLE", "id_token missing sub")
		return nil, fmt.Errorf("%w: missing sub", ErrInvalidClaims)
	}

	return claims, nil
}
