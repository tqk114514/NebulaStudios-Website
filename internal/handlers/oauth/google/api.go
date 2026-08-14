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

// doWithProxyFailover 遍历代理列表执行请求，仅 HTTP 200 视为成功（终态）。
// 网络错误或任何非 200 状态码都自动切换到下一个代理：
//   - 代理本身故障（404/502 等）→ 重试能恢复
//   - Google 的真实错误（400 invalid_grant 等）→ 同一 code 在多个代理上结果一致，重试无害
func (h *GoogleHandler) doWithProxyFailover(op string, fn func(baseURL string) (statusCode int, body []byte, err error)) (int, []byte, error) {
	if len(h.proxyURLs) == 0 {
		return 0, nil, fmt.Errorf("no google proxy configured")
	}
	var lastStatus int
	var lastBody []byte
	var lastErr error
	for i, base := range h.proxyURLs {
		status, body, err := fn(base)
		if err == nil && status == http.StatusOK {
			return status, body, nil
		}
		lastStatus, lastBody, lastErr = status, body, err
		if i < len(h.proxyURLs)-1 {
			if err != nil {
				utils.LogWarn("OAUTH-GOOGLE", fmt.Sprintf("%s: proxy %s failed (%v), trying next", op, base, err), "")
			} else {
				utils.LogWarn("OAUTH-GOOGLE", fmt.Sprintf("%s: proxy %s returned status %d, trying next", op, base, status), "")
			}
		}
	}
	return lastStatus, lastBody, lastErr
}

// applyProxyAuthHeaders 附加代理访问凭证（Cloudflare Access Service Token），
// 由 CF 边缘拦截未认证请求，不消耗 Worker 配额。
func (h *GoogleHandler) applyProxyAuthHeaders(req *http.Request) {
	req.Header.Set("CF-Access-Client-Id", h.proxyAccessClientID)
	req.Header.Set("CF-Access-Client-Secret", h.proxyAccessClientSecret)
}

// verifyProxyEnvelope 校验代理 /token 签名响应（WorkerTokenEnvelope），成功返回 Google 原始响应体
func (h *GoogleHandler) verifyProxyEnvelope(body []byte) ([]byte, error) {
	var envelope WorkerTokenEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid envelope json: %w", err)
	}
	return h.verifier.VerifyEnvelope(&envelope)
}

// exchangeCodeForToken 通过代理用授权码换取 token，支持多代理故障转移
func (h *GoogleHandler) exchangeCodeForToken(code string, codeVerifier string) (map[string]any, error) {
	if code == "" {
		return nil, fmt.Errorf("%w: empty code", oauth.ErrOAuthTokenExchange)
	}

	if codeVerifier == "" {
		return nil, fmt.Errorf("%w: empty code_verifier", oauth.ErrOAuthTokenExchange)
	}

	data := url.Values{}
	data.Set("client_id", h.ClientID)
	data.Set("client_secret", h.ClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", h.RedirectURI)
	data.Set("grant_type", "authorization_code")
	data.Set("code_verifier", codeVerifier)
	encoded := data.Encode()

	client := &http.Client{Timeout: oauth.HTTPClientTimeout}
	status, body, err := h.doWithProxyFailover("token exchange", func(base string) (int, []byte, error) {
		req, err := http.NewRequest("POST", base+"/token", strings.NewReader(encoded))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.applyProxyAuthHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer func(Body io.ReadCloser) {
			if Body != nil {
				_ = Body.Close()
			}
		}(resp.Body)
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return resp.StatusCode, b, nil
		}
		// 代理成功响应为签名信封 {data, timestamp, signature}：验签解包，失败视为该代理不可用（触发故障转移）
		unwrapped, verr := h.verifyProxyEnvelope(b)
		if verr != nil {
			return 0, nil, fmt.Errorf("proxy envelope invalid: %w", verr)
		}
		return http.StatusOK, unwrapped, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: request failed: %v", oauth.ErrOAuthTokenExchange, err)
	}

	if status != http.StatusOK {
		utils.LogError("OAUTH-GOOGLE", "exchangeCodeForToken", fmt.Errorf("status %d", status), fmt.Sprintf("Token exchange failed with status %d: %s", status, string(body)))
		return nil, fmt.Errorf("%w: status %d", oauth.ErrOAuthTokenExchange, status)
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

// getUserInfo 通过代理获取 Google 用户信息，支持多代理故障转移
func (h *GoogleHandler) getUserInfo(accessToken string) (map[string]any, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("%w: empty access token", oauth.ErrOAuthUserInfo)
	}

	client := &http.Client{Timeout: oauth.HTTPClientTimeout}
	status, body, err := h.doWithProxyFailover("userinfo", func(base string) (int, []byte, error) {
		req, err := http.NewRequest("GET", base+"/userinfo", nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		h.applyProxyAuthHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer func(Body io.ReadCloser) {
			if Body != nil {
				_ = Body.Close()
			}
		}(resp.Body)
		b, err := io.ReadAll(resp.Body)
		return resp.StatusCode, b, err
	})
	if err != nil {
		return nil, fmt.Errorf("%w: request failed: %v", oauth.ErrOAuthUserInfo, err)
	}

	if status != http.StatusOK {
		utils.LogError("OAUTH-GOOGLE", "getUserInfo", fmt.Errorf("status %d", status), fmt.Sprintf("Get user info failed with status %d: %s", status, string(body)))
		return nil, fmt.Errorf("%w: status %d", oauth.ErrOAuthUserInfo, status)
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
