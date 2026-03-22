/**
 * internal/handlers/oauth/microsoft/api.go
 * Microsoft Graph API 调用
 *
 * 功能：
 * - 授权码换取 Token
 * - 获取用户信息
 * - 获取用户头像
 *
 * 依赖：
 * - auth-system/internal/handlers/oauth (公共错误和常量)
 * - internal/utils (日志)
 */

package microsoft

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"auth-system/internal/handlers/oauth"
	"auth-system/internal/utils"
)

// ====================  API 调用 ====================

// exchangeCodeForToken 用授权码换取 token
//
// 参数：
//   - code: 授权码
//
// 返回：
//   - map[string]interface{}: Token 响应数据
//   - error: 错误信息
func (h *MicrosoftHandler) exchangeCodeForToken(code string, codeVerifier string) (map[string]any, error) {
	if code == "" {
		return nil, fmt.Errorf("%w: empty code", oauth.ErrOAuthTokenExchange)
	}

	if codeVerifier == "" {
		return nil, fmt.Errorf("%w: empty code_verifier", oauth.ErrOAuthTokenExchange)
	}

	tokenURL := "https://login.microsoftonline.com/" + MicrosoftTenant + "/oauth2/v2.0/token"

	data := url.Values{}
	data.Set("client_id", h.clientID)
	data.Set("client_secret", h.clientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", h.redirectURI)
	data.Set("grant_type", "authorization_code")
	data.Set("code_verifier", codeVerifier)

	client := &http.Client{Timeout: oauth.HTTPClientTimeout}
	resp, err := client.Post(tokenURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: request failed: %v", oauth.ErrOAuthTokenExchange, err)
	}
	defer func(Body io.ReadCloser) {
		if Body != nil {
			_ = Body.Close()
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read response: %v", oauth.ErrOAuthTokenExchange, err)
	}

	if resp.StatusCode != http.StatusOK {
		utils.LogError("OAUTH-MS", "exchangeCodeForToken", fmt.Errorf("status %d", resp.StatusCode), fmt.Sprintf("Token exchange failed with status %d: %s", resp.StatusCode, string(body)))
		return nil, fmt.Errorf("%w: status %d", oauth.ErrOAuthTokenExchange, resp.StatusCode)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%w: failed to parse response: %v", oauth.ErrOAuthTokenExchange, err)
	}

	if errCode, ok := result["error"].(string); ok {
		errDesc, _ := result["error_description"].(string)
		utils.LogError("OAUTH-MS", "exchangeCodeForToken", fmt.Errorf("%s", errCode), fmt.Sprintf("Token exchange error: %s - %s", errCode, errDesc))
		return nil, fmt.Errorf("%w: %s", oauth.ErrOAuthTokenExchange, errCode)
	}

	return result, nil
}

// getUserInfo 获取微软用户信息
//
// 参数：
//   - accessToken: Access Token
//
// 返回：
//   - map[string]interface{}: 用户信息
//   - error: 错误信息
func (h *MicrosoftHandler) getUserInfo(accessToken string) (map[string]any, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("%w: empty access token", oauth.ErrOAuthUserInfo)
	}

	req, err := http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create request: %v", oauth.ErrOAuthUserInfo, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: oauth.HTTPClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed: %v", oauth.ErrOAuthUserInfo, err)
	}
	defer func(Body io.ReadCloser) {
		if Body != nil {
			_ = Body.Close()
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read response: %v", oauth.ErrOAuthUserInfo, err)
	}

	if resp.StatusCode != http.StatusOK {
		utils.LogError("OAUTH-MS", "getUserInfo", fmt.Errorf("status %d", resp.StatusCode), fmt.Sprintf("Get user info failed with status %d: %s", resp.StatusCode, string(body)))
		return nil, fmt.Errorf("%w: status %d", oauth.ErrOAuthUserInfo, resp.StatusCode)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%w: failed to parse response: %v", oauth.ErrOAuthUserInfo, err)
	}

	if errCode, ok := result["error"].(map[string]any); ok {
		if code, ok := errCode["code"].(string); ok {
			utils.LogError("OAUTH-MS", "getUserInfo", fmt.Errorf("%s", code), fmt.Sprintf("Get user info error: %s", code))
			return nil, fmt.Errorf("%w: %s", oauth.ErrOAuthUserInfo, code)
		}
	}

	return result, nil
}

// getAvatarData 获取微软头像
// 返回二进制头像数据和 Content-Type，失败时返回空
//
// 参数：
//   - accessToken: Access Token
//
// 返回：
//   - []byte: 头像二进制数据
//   - string: Content-Type
func (h *MicrosoftHandler) getAvatarData(accessToken string) ([]byte, string) {
	if accessToken == "" {
		utils.LogWarn("OAUTH-MS", "Empty access token for avatar request", "")
		return nil, ""
	}

	req, err := http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me/photo/$value", nil)
	if err != nil {
		utils.LogWarn("OAUTH-MS", "Failed to create avatar request", "")
		return nil, ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: oauth.HTTPClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		utils.LogWarn("OAUTH-MS", "Avatar request failed", "")
		return nil, ""
	}
	defer func(Body io.ReadCloser) {
		if Body != nil {
			_ = Body.Close()
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode != http.StatusNotFound {
			utils.LogWarn("OAUTH-MS", "Avatar request returned non-OK status", fmt.Sprintf("status=%d", resp.StatusCode))
		}
		return nil, ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.LogWarn("OAUTH-MS", "Failed to read avatar response", "")
		return nil, ""
	}

	if len(body) > 5*1024*1024 {
		utils.LogWarn("OAUTH-MS", "Avatar too large, skipping", "")
		return nil, ""
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	if !strings.HasPrefix(contentType, "image/") {
		utils.LogWarn("OAUTH-MS", "Invalid avatar content type", fmt.Sprintf("contentType=%s", contentType))
		return nil, ""
	}

	return body, contentType
}
