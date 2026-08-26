package microsoft

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"auth-system/internal/utils"

	"github.com/golang-jwt/jwt/v5"
)

// microsoftJWKSCache Microsoft JWKS 公钥缓存，避免每次 ID Token 验证都请求 JWKS 端点
var microsoftJWKSCache struct {
	sync.RWMutex
	keys      map[string]*rsa.PublicKey // kid -> public key
	fetchedAt time.Time
}

const (
	microsoftJWKSURL  = "https://login.microsoftonline.com/" + MicrosoftTenant + "/discovery/v2.0/keys"
	microsoftJWKSTTL  = 24 * time.Hour
	microsoftIssuerV2 = "https://login.microsoftonline.com/" + MicrosoftTenant + "/v2.0"
)

// jwk JSON Web Key 结构（仅提取验证 ID Token 所需字段）
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetchMicrosoftJWKS 获取 Microsoft JWKS 并缓存，TTL 内直接返回缓存
func fetchMicrosoftJWKS(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	microsoftJWKSCache.RLock()
	if time.Since(microsoftJWKSCache.fetchedAt) < microsoftJWKSTTL && len(microsoftJWKSCache.keys) > 0 {
		keys := microsoftJWKSCache.keys
		microsoftJWKSCache.RUnlock()
		return keys, nil
	}
	microsoftJWKSCache.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, microsoftJWKSURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create JWKS request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer func(Body io.ReadCloser) { _ = Body.Close() }(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read JWKS response: %w", err)
	}

	var jwks struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("parse JWKS JSON: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Use != "sig" || k.Kid == "" {
			continue
		}
		pubKey, err := jwkToRSAPublicKey(k)
		if err != nil {
			utils.LogWarnCtx(ctx, "OAUTH-MS", "Failed to parse JWK", "kid", k.Kid, "error", err)
			continue
		}
		keys[k.Kid] = pubKey
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no valid RSA signing keys in JWKS")
	}

	microsoftJWKSCache.Lock()
	microsoftJWKSCache.keys = keys
	microsoftJWKSCache.fetchedAt = time.Now()
	microsoftJWKSCache.Unlock()

	return keys, nil
}

// jwkToRSAPublicKey 将 JWK 的 n/e base64url 参数转换为 *rsa.PublicKey
func jwkToRSAPublicKey(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	// e 通常是 AQAB (65537)，需要转成 int
	var eInt int
	for _, b := range eBytes {
		eInt = eInt<<8 + int(b)
	}

	pubKey := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: eInt,
	}
	return pubKey, nil
}

// isValidMicrosoftIssuer 校验 ID token 的 issuer 是否合法。
// 使用 common 租户时，Microsoft 返回的 iss 为用户实际所属租户的 ID 而非 "common"：
//   - 个人账户：https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0
//   - 工作/学校账户：https://login.microsoftonline.com/{tenant-guid}/v2.0
//
// 校验策略：前缀必须为 https://login.microsoftonline.com/，后缀必须为 /v2.0，
// 中间部分为租户 ID（UUID 格式或 "common"）。
func isValidMicrosoftIssuer(iss string) bool {
	const prefix = "https://login.microsoftonline.com/"
	const suffix = "/v2.0"
	if !strings.HasPrefix(iss, prefix) || !strings.HasSuffix(iss, suffix) {
		return false
	}
	tenant := strings.TrimSuffix(strings.TrimPrefix(iss, prefix), suffix)
	if tenant == "" {
		return false
	}
	// 接受 "common"、"organizations"、"consumers" 或 UUID 格式的租户 ID
	if tenant == "common" || tenant == "organizations" || tenant == "consumers" {
		return true
	}
	// 校验 UUID 格式（8-4-4-4-12 hex）
	return isValidUUID(tenant)
}

// isValidUUID 校验字符串是否为标准 UUID 格式（8-4-4-4-12 hex）
func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// extractIDTokenEmail 验证 ID Token 签名后提取 email claim。
// 个人微软账户的 msUser.mail 可能是别名（如 xxx@outlook.com），
// 而 ID Token 中的 email claim 才是用户真正绑定的邮箱。
// 验证失败（签名无效、过期、issuer/audience 不匹配）时返回空字符串。
func (h *MicrosoftHandler) extractIDTokenEmail(ctx context.Context, tokenData map[string]any) string {
	idToken, ok := tokenData["id_token"].(string)
	if !ok || idToken == "" {
		return ""
	}

	// 先解析 header 获取 kid
	unverifiedToken, _, err := jwt.NewParser().ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		utils.LogWarnCtx(ctx, "OAUTH-MS", "Failed to parse ID token header", "error", err)
		return ""
	}

	kid := ""
	if kidVal, ok := unverifiedToken.Header["kid"].(string); ok {
		kid = kidVal
	}
	if kid == "" {
		utils.LogWarnCtx(ctx, "OAUTH-MS", "ID token missing kid header")
		return ""
	}

	// 获取 JWKS 公钥
	keys, err := fetchMicrosoftJWKS(ctx)
	if err != nil {
		utils.LogErrorCtx(ctx, "OAUTH-MS", "extractIDTokenEmail", err, "Failed to fetch JWKS")
		return ""
	}

	pubKey, ok := keys[kid]
	if !ok {
		utils.LogWarnCtx(ctx, "OAUTH-MS", "ID token kid not found in JWKS", "kid", kid)
		return ""
	}

	// 解析并验证签名
	token, err := jwt.Parse(idToken, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return pubKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		utils.LogWarnCtx(ctx, "OAUTH-MS", "ID token signature verification failed", "error", err)
		return ""
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return ""
	}

	// 校验 issuer
	// 使用 common 租户时，ID token 的 iss 不是 "common" 而是用户实际所属租户的 ID：
	//   - 个人账户：https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0
	//   - 工作/学校账户：https://login.microsoftonline.com/{tenant-guid}/v2.0
	// 因此需要校验 iss 前缀和格式，而非精确匹配 "common"
	iss, _ := claims["iss"].(string)
	if !isValidMicrosoftIssuer(iss) {
		utils.LogWarnCtx(ctx, "OAUTH-MS", "ID token issuer mismatch", "iss", iss)
		return ""
	}

	// 校验 audience（必须是本应用的 client_id）
	audValid := false
	switch aud := claims["aud"].(type) {
	case string:
		audValid = aud == h.ClientID
	case []any:
		for _, a := range aud {
			if s, ok := a.(string); ok && s == h.ClientID {
				audValid = true
				break
			}
		}
	}
	if !audValid {
		utils.LogWarnCtx(ctx, "OAUTH-MS", "ID token audience mismatch", "client_id", h.ClientID)
		return ""
	}

	email, _ := claims["email"].(string)
	return email
}

// parseDataURL 解析 data URL，返回二进制数据和 content-type
func (h *MicrosoftHandler) parseDataURL(dataURL string) ([]byte, string) {
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, ""
	}

	commaIdx := strings.Index(dataURL, ",")
	if commaIdx == -1 {
		return nil, ""
	}

	header := dataURL[5:commaIdx]
	contentType := "image/jpeg"
	if before, _, ok := strings.Cut(header, ";"); ok {
		contentType = before
	} else {
		contentType = header
	}

	base64Data := dataURL[commaIdx+1:]
	imageData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		utils.LogWarn("OAUTH-MS", "Failed to decode base64 avatar")
		return nil, ""
	}

	return imageData, contentType
}

// uploadAvatar 上传头像到本地存储，如果存储未初始化则返回 base64 data URL
func (h *MicrosoftHandler) uploadAvatar(ctx context.Context, userUID string, imageData []byte, contentType string) string {
	if len(imageData) == 0 {
		return ""
	}

	if h.StorageService != nil && h.StorageService.IsConfigured() {
		avatarURL, err := h.StorageService.UploadAvatar(ctx, userUID, imageData)
		if err != nil {
			utils.LogWarn("OAUTH-MS", "Failed to upload avatar to storage, falling back to base64", "user_uid", userUID)
		} else {
			return avatarURL
		}
	}

	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(imageData)
}

func (h *MicrosoftHandler) calculateAvatarHash(imageData []byte) string {
	if len(imageData) == 0 {
		return ""
	}
	hash := sha256.Sum256(imageData)
	return hex.EncodeToString(hash[:])
}

// processAvatarAsync 异步处理头像上传，在后台 goroutine 中执行，不阻塞登录流程
func (h *MicrosoftHandler) processAvatarAsync(userUID string, oldAvatarHash string, avatarData []byte, avatarContentType string) {
	defer func() {
		if r := recover(); r != nil {
			utils.LogError("OAUTH-MS", "processAvatarAsync", fmt.Errorf("panic: %v", r), "user_uid", userUID)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 用户已关闭微软头像自动同步（移除头像场景）时跳过，不再下载/存储/更新
	if user, err := h.UserRepo.FindByUID(ctx, userUID); err == nil && !user.MicrosoftAvatarSync {
		utils.LogInfo("OAUTH-MS", "Avatar sync disabled, skipping", "user_uid", userUID)
		return
	}

	newAvatarHash := h.calculateAvatarHash(avatarData)

	if newAvatarHash != "" && newAvatarHash != oldAvatarHash {
		microsoftAvatarURL := h.uploadAvatar(ctx, userUID, avatarData, avatarContentType)

		err := h.UserRepo.Update(ctx, userUID, map[string]any{
			"microsoft_avatar_url":  microsoftAvatarURL,
			"microsoft_avatar_hash": newAvatarHash,
		})
		if err != nil {
			utils.LogError("OAUTH-MS", "processAvatarAsync", err, "user_uid", userUID)
			return
		}

		h.UserCache.Invalidate(userUID)
		utils.LogInfo("OAUTH-MS", "Avatar updated async", "user_uid", userUID)

	} else if newAvatarHash == "" && oldAvatarHash != "" {
		err := h.UserRepo.Update(ctx, userUID, map[string]any{
			"microsoft_avatar_url":  nil,
			"microsoft_avatar_hash": nil,
		})
		if err != nil {
			utils.LogError("OAUTH-MS", "processAvatarAsync", err, "user_uid", userUID)
			return
		}

		h.UserCache.Invalidate(userUID)
		utils.LogInfo("OAUTH-MS", "Avatar cleared async", "user_uid", userUID)

	} else {
		utils.LogInfo("OAUTH-MS", "Avatar unchanged, skipping", "user_uid", userUID)
	}
}
