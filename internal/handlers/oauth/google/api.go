package google

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

// exchangeCodeForToken 通过代理用授权码换取 token
func (h *GoogleHandler) exchangeCodeForToken(code string, codeVerifier string) (map[string]any, error) {
	if code == "" {
		return nil, fmt.Errorf("%w: empty code", oauth.ErrOAuthTokenExchange)
	}

	if codeVerifier == "" {
		return nil, fmt.Errorf("%w: empty code_verifier", oauth.ErrOAuthTokenExchange)
	}

	data := url.Values{}
	data.Set("client_id", h.clientID)
	data.Set("client_secret", h.clientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", h.redirectURI)
	data.Set("grant_type", "authorization_code")
	data.Set("code_verifier", codeVerifier)

	client := &http.Client{Timeout: oauth.HTTPClientTimeout}
	resp, err := client.Post(h.proxyURL+"/token", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
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
		utils.LogError("OAUTH-GOOGLE", "exchangeCodeForToken", fmt.Errorf("status %d", resp.StatusCode), fmt.Sprintf("Token exchange failed with status %d: %s", resp.StatusCode, string(body)))
		return nil, fmt.Errorf("%w: status %d", oauth.ErrOAuthTokenExchange, resp.StatusCode)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%w: failed to parse response: %v", oauth.ErrOAuthTokenExchange, err)
	}

	if errCode, ok := result["error"].(string); ok {
		errDesc, _ := result["error_description"].(string)
		utils.LogError("OAUTH-GOOGLE", "exchangeCodeForToken", fmt.Errorf("%s", errCode), fmt.Sprintf("Token exchange error: %s - %s", errCode, errDesc))
		return nil, fmt.Errorf("%w: %s", oauth.ErrOAuthTokenExchange, errCode)
	}

	return result, nil
}

// getUserInfo 通过代理获取 Google 用户信息
func (h *GoogleHandler) getUserInfo(accessToken string) (map[string]any, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("%w: empty access token", oauth.ErrOAuthUserInfo)
	}

	req, err := http.NewRequest("GET", h.proxyURL+"/userinfo", nil)
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
		utils.LogError("OAUTH-GOOGLE", "getUserInfo", fmt.Errorf("status %d", resp.StatusCode), fmt.Sprintf("Get user info failed with status %d: %s", resp.StatusCode, string(body)))
		return nil, fmt.Errorf("%w: status %d", oauth.ErrOAuthUserInfo, resp.StatusCode)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%w: failed to parse response: %v", oauth.ErrOAuthUserInfo, err)
	}

	if errCode, ok := result["error"].(map[string]any); ok {
		if code, ok := errCode["code"].(string); ok {
			utils.LogError("OAUTH-GOOGLE", "getUserInfo", fmt.Errorf("%s", code), fmt.Sprintf("Get user info error: %s", code))
			return nil, fmt.Errorf("%w: %s", oauth.ErrOAuthUserInfo, code)
		}
	}

	return result, nil
}