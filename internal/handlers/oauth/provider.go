/**
 * internal/handlers/oauth/provider.go
 * OAuth Provider API Handler
 *
 * 功能：
 * - 授权端点 (GET/POST /oauth/authorize)
 * - Token 端点 (POST /oauth/token)
 * - 用户信息端点 (GET /oauth/userinfo)
 * - Token 撤销端点 (POST /oauth/revoke)
 *
 * 支持的授权类型：
 * - Authorization Code (response_type=code)
 * - Refresh Token (grant_type=refresh_token)
 *
 * 支持的 Scope：
 * - openid: 用户标识 (sub)
 * - profile: 用户名和头像
 * - email: 邮箱地址
 *
 * 依赖：
 * - internal/services (OAuth 服务)
 * - internal/models (用户模型)
 * - internal/cache (用户缓存)
 * - internal/middleware (认证中间件)
 */

package oauth

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"auth-system/internal/cache"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  常量定义 ====================

const (
	// 支持的 scope
	ScopeOpenID  = "openid"
	ScopeProfile = "profile"
	ScopeEmail   = "email"
)

// 有效的 scope 集合
var validScopes = map[string]bool{
	ScopeOpenID:  true,
	ScopeProfile: true,
	ScopeEmail:   true,
}

// ====================  Handler 结构 ====================

// OAuthProviderHandler OAuth Provider Handler
type OAuthProviderHandler struct {
	oauthService   *services.OAuthService
	userRepo       *models.UserRepository
	userLogRepo    *models.UserLogRepository
	userCache      *cache.UserCache
	sessionService *services.SessionService
	baseURL        string
}

// ====================  构造函数 ====================

// NewOAuthProviderHandler 创建 OAuth Provider Handler
func NewOAuthProviderHandler(
	oauthService *services.OAuthService,
	userRepo *models.UserRepository,
	userLogRepo *models.UserLogRepository,
	userCache *cache.UserCache,
	sessionService *services.SessionService,
	baseURL string,
) *OAuthProviderHandler {
	return &OAuthProviderHandler{
		oauthService:   oauthService,
		userRepo:       userRepo,
		userLogRepo:    userLogRepo,
		userCache:      userCache,
		sessionService: sessionService,
		baseURL:        baseURL,
	}
}

// ====================  授权端点 ====================

// Authorize 授权端点 - 重定向到授权页面
// GET /oauth/authorize
//
// 查询参数：
//   - client_id: 客户端 ID（必需）
//   - redirect_uri: 回调地址（必需）
//   - response_type: 响应类型，必须为 "code"（必需）
//   - scope: 请求的权限范围（必需）
//   - state: 状态参数（推荐）
//
// 响应：
//   - 重定向到授权页面（用户已登录）
//   - 重定向到登录页面（用户未登录）
//   - 重定向到错误页面（参数无效）
func (h *OAuthProviderHandler) Authorize(c *gin.Context) {
	// 获取请求参数
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	responseType := c.Query("response_type")
	scope := c.Query("scope")
	state := c.Query("state")

	// 验证必需参数
	if clientID == "" {
		h.redirectToErrorPage(c, "invalid_request", "Missing client_id parameter")
		return
	}

	// 验证 client_id
	client, err := h.oauthService.ValidateClientID(c.Request.Context(), clientID)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Invalid client_id: %s, error=%v", clientID, err)
		h.redirectToErrorPage(c, "invalid_client", "Invalid client_id")
		return
	}

	// 验证 redirect_uri
	if redirectURI == "" {
		h.redirectToErrorPage(c, "invalid_request", "Missing redirect_uri parameter")
		return
	}

	if !h.oauthService.ValidateRedirectURI(client, redirectURI) {
		utils.LogPrintf("[OAUTH-PROVIDER] Invalid redirect_uri: %s, expected=%s", redirectURI, client.RedirectURI)
		h.redirectToErrorPage(c, "invalid_request", "Invalid redirect_uri")
		return
	}

	// 验证 response_type
	if responseType != "code" {
		h.redirectWithError(c, redirectURI, state, "unsupported_response_type", "Only 'code' response type is supported")
		return
	}

	// 验证 scope
	if scope == "" {
		h.redirectWithError(c, redirectURI, state, "invalid_scope", "Missing scope parameter")
		return
	}

	normalizedScope := h.normalizeScope(scope)
	if normalizedScope == "" {
		h.redirectWithError(c, redirectURI, state, "invalid_scope", "Invalid scope")
		return
	}

	// 检查用户登录状态
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		// 未登录，重定向到登录页面，登录后返回
		returnURL := h.buildAuthorizeURL(clientID, redirectURI, responseType, scope, state)
		loginURL := h.baseURL + "/account/login?return=" + url.QueryEscape(returnURL)
		c.Redirect(http.StatusFound, loginURL)
		return
	}

	// 获取用户信息
	user, err := h.userCache.GetOrLoad(c.Request.Context(), userID, h.userRepo.FindByID)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Failed to get user: userID=%d, error=%v", userID, err)
		h.redirectWithError(c, redirectURI, state, "server_error", "Failed to get user info")
		return
	}

	// 检查用户是否被封禁
	if user.CheckBanned() {
		utils.LogPrintf("[OAUTH-PROVIDER] Banned user attempted to authorize: userID=%d", userID)
		h.redirectWithError(c, redirectURI, state, "access_denied", "User is banned")
		return
	}

	// 重定向到授权页面（带参数）
	authPageURL := h.buildAuthPageURL(clientID, redirectURI, normalizedScope, state)
	c.Redirect(http.StatusFound, authPageURL)
}

// AuthorizeInfo 获取授权信息 API
// GET /oauth/authorize/info
//
// 查询参数：
//   - client_id: 客户端 ID（必需）
//   - redirect_uri: 回调地址（必需）
//   - scope: 请求的权限范围（必需）
//
// 响应：
//   - success: 是否成功
//   - data: 授权信息（clientName, clientDescription, scopes, username, userAvatar）
func (h *OAuthProviderHandler) AuthorizeInfo(c *gin.Context) {
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	scope := c.Query("scope")

	// 验证参数
	if clientID == "" || redirectURI == "" || scope == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success":   false,
			"errorCode": "invalid_request",
		})
		return
	}

	// 验证 client_id
	client, err := h.oauthService.ValidateClientID(c.Request.Context(), clientID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success":   false,
			"errorCode": "invalid_client",
		})
		return
	}

	// 验证 redirect_uri
	if !h.oauthService.ValidateRedirectURI(client, redirectURI) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success":   false,
			"errorCode": "invalid_request",
		})
		return
	}

	// 获取用户信息
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success":   false,
			"errorCode": "unauthorized",
		})
		return
	}

	user, err := h.userCache.GetOrLoad(c.Request.Context(), userID, h.userRepo.FindByID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":   false,
			"errorCode": "server_error",
		})
		return
	}

	// 规范化 scope
	normalizedScope := h.normalizeScope(scope)

	// 处理头像 URL：如果是 "microsoft" 标记，使用微软头像
	avatarURL := user.AvatarURL
	if avatarURL == "microsoft" && user.MicrosoftAvatarURL.Valid {
		avatarURL = user.MicrosoftAvatarURL.String
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"clientName":        client.Name,
			"clientDescription": client.Description,
			"scopes":            h.parseScopeList(normalizedScope),
			"username":          user.Username,
			"userAvatar":        avatarURL,
		},
	})
}

// AuthorizePost 授权端点 - 处理授权决定
// POST /oauth/authorize
//
// 表单参数：
//   - client_id: 客户端 ID（必需）
//   - redirect_uri: 回调地址（必需）
//   - scope: 请求的权限范围（必需）
//   - state: 状态参数（可选）
//   - decision: 用户决定，"approve" 或 "deny"（必需）
//
// 响应：
//   - 重定向到 redirect_uri（带 code 或 error）
func (h *OAuthProviderHandler) AuthorizePost(c *gin.Context) {
	// 获取表单参数
	clientID := c.PostForm("client_id")
	redirectURI := c.PostForm("redirect_uri")
	scope := c.PostForm("scope")
	state := c.PostForm("state")
	decision := c.PostForm("decision")

	// 验证必需参数
	if clientID == "" || redirectURI == "" || scope == "" {
		h.redirectToErrorPage(c, "invalid_request", "Missing required parameters")
		return
	}

	// 验证 client_id
	client, err := h.oauthService.ValidateClientID(c.Request.Context(), clientID)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Invalid client_id in POST: %s, error=%v", clientID, err)
		h.redirectToErrorPage(c, "invalid_client", "Invalid client_id")
		return
	}

	// 验证 redirect_uri
	if !h.oauthService.ValidateRedirectURI(client, redirectURI) {
		utils.LogPrintf("[OAUTH-PROVIDER] Invalid redirect_uri in POST: %s", redirectURI)
		h.redirectToErrorPage(c, "invalid_request", "Invalid redirect_uri")
		return
	}

	// 检查用户登录状态
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		h.redirectWithError(c, redirectURI, state, "access_denied", "User not logged in")
		return
	}

	// 获取用户信息并检查封禁状态
	user, err := h.userCache.GetOrLoad(c.Request.Context(), userID, h.userRepo.FindByID)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Failed to get user in POST: userID=%d, error=%v", userID, err)
		h.redirectWithError(c, redirectURI, state, "server_error", "Failed to get user info")
		return
	}

	if user.CheckBanned() {
		utils.LogPrintf("[OAUTH-PROVIDER] Banned user attempted to authorize in POST: userID=%d", userID)
		h.redirectWithError(c, redirectURI, state, "access_denied", "User is banned")
		return
	}

	// 处理用户决定
	if decision != "approve" {
		utils.LogPrintf("[OAUTH-PROVIDER] User denied authorization: userID=%d, clientID=%s", userID, clientID)
		h.redirectWithError(c, redirectURI, state, "access_denied", "User denied authorization")
		return
	}

	// 规范化 scope
	normalizedScope := h.normalizeScope(scope)
	if normalizedScope == "" {
		h.redirectWithError(c, redirectURI, state, "invalid_scope", "Invalid scope")
		return
	}

	// 生成授权码
	code, err := h.oauthService.CreateAuthorizationCode(c.Request.Context(), clientID, userID, redirectURI, normalizedScope)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Failed to create auth code: userID=%d, clientID=%s, error=%v", userID, clientID, err)
		h.redirectWithError(c, redirectURI, state, "server_error", "Failed to create authorization code")
		return
	}

	// 记录用户操作日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogOAuthAuthorize(c.Request.Context(), userID, clientID, client.Name, normalizedScope); err != nil {
			utils.LogPrintf("[OAUTH-PROVIDER] WARN: Failed to log OAuth authorize: userID=%d, error=%v", userID, err)
		}
	}

	// 重定向到回调地址
	redirectURL := h.buildRedirectURL(redirectURI, code, state)
	utils.LogPrintf("[OAUTH-PROVIDER] Authorization granted: userID=%d, clientID=%s", userID, clientID)
	c.Redirect(http.StatusFound, redirectURL)
}

// ====================  Token 端点 ====================

// Token 端点
// POST /oauth/token
//
// 支持的 grant_type：
//   - authorization_code: 用授权码换取 Token
//   - refresh_token: 刷新 Access Token
//
// 请求参数（application/x-www-form-urlencoded）：
//   - grant_type: 授权类型（必需）
//   - client_id: 客户端 ID（必需）
//   - client_secret: 客户端密钥（必需）
//   - code: 授权码（grant_type=authorization_code 时必需）
//   - redirect_uri: 回调地址（grant_type=authorization_code 时必需）
//   - refresh_token: 刷新令牌（grant_type=refresh_token 时必需）
//
// 响应：
//   - access_token: 访问令牌
//   - token_type: "Bearer"
//   - expires_in: 过期时间（秒）
//   - refresh_token: 刷新令牌
//   - scope: 授权范围
func (h *OAuthProviderHandler) Token(c *gin.Context) {
	grantType := c.PostForm("grant_type")
	clientID := c.PostForm("client_id")
	clientSecret := c.PostForm("client_secret")

	// 验证客户端凭证
	if clientID == "" || clientSecret == "" {
		h.respondTokenError(c, http.StatusUnauthorized, "invalid_client", "Missing client credentials")
		return
	}

	_, err := h.oauthService.ValidateClient(c.Request.Context(), clientID, clientSecret)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Client validation failed: clientID=%s, error=%v", clientID, err)
		h.respondTokenError(c, http.StatusUnauthorized, "invalid_client", "Invalid client credentials")
		return
	}

	// 根据 grant_type 处理
	switch grantType {
	case "authorization_code":
		h.handleAuthorizationCodeGrant(c, clientID)
	case "refresh_token":
		h.handleRefreshTokenGrant(c, clientID)
	default:
		h.respondTokenError(c, http.StatusBadRequest, "unsupported_grant_type", "Unsupported grant type")
	}
}

// handleAuthorizationCodeGrant 处理授权码换取 Token
func (h *OAuthProviderHandler) handleAuthorizationCodeGrant(c *gin.Context, clientID string) {
	code := c.PostForm("code")
	redirectURI := c.PostForm("redirect_uri")

	if code == "" {
		h.respondTokenError(c, http.StatusBadRequest, "invalid_request", "Missing code parameter")
		return
	}

	if redirectURI == "" {
		h.respondTokenError(c, http.StatusBadRequest, "invalid_request", "Missing redirect_uri parameter")
		return
	}

	// 换取 Token
	tokenResp, userID, err := h.oauthService.ExchangeAuthorizationCode(c.Request.Context(), code, clientID, redirectURI)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Code exchange failed: clientID=%s, error=%v", clientID, err)
		h.respondTokenError(c, http.StatusBadRequest, "invalid_grant", "Invalid authorization code")
		return
	}

	// 检查用户是否被封禁
	user, err := h.userCache.GetOrLoad(c.Request.Context(), userID, h.userRepo.FindByID)
	if err != nil || user.CheckBanned() {
		utils.LogPrintf("[OAUTH-PROVIDER] User banned or not found during token exchange: userID=%d", userID)
		h.respondTokenError(c, http.StatusBadRequest, "invalid_grant", "User is banned or not found")
		return
	}

	utils.LogPrintf("[OAUTH-PROVIDER] Token issued: clientID=%s, userID=%d", clientID, userID)
	c.JSON(http.StatusOK, tokenResp)
}

// handleRefreshTokenGrant 处理刷新 Token
func (h *OAuthProviderHandler) handleRefreshTokenGrant(c *gin.Context, clientID string) {
	refreshToken := c.PostForm("refresh_token")

	if refreshToken == "" {
		h.respondTokenError(c, http.StatusBadRequest, "invalid_request", "Missing refresh_token parameter")
		return
	}

	// 刷新 Token
	tokenResp, userID, err := h.oauthService.RefreshAccessToken(c.Request.Context(), refreshToken, clientID)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Token refresh failed: clientID=%s, error=%v", clientID, err)
		h.respondTokenError(c, http.StatusBadRequest, "invalid_grant", "Invalid refresh token")
		return
	}

	// 检查用户是否被封禁
	user, err := h.userCache.GetOrLoad(c.Request.Context(), userID, h.userRepo.FindByID)
	if err != nil || user.CheckBanned() {
		utils.LogPrintf("[OAUTH-PROVIDER] User banned or not found during token refresh: userID=%d", userID)
		h.respondTokenError(c, http.StatusBadRequest, "invalid_grant", "User is banned or not found")
		return
	}

	utils.LogPrintf("[OAUTH-PROVIDER] Token refreshed: clientID=%s, userID=%d", clientID, userID)
	c.JSON(http.StatusOK, tokenResp)
}

// ====================  UserInfo 端点 ====================

// UserInfo 用户信息端点
// GET /oauth/userinfo
//
// 请求头：
//   - Authorization: Bearer <access_token>
//
// 响应（根据 scope）：
//   - sub: 用户 ID（openid）
//   - username: 用户名（profile）
//   - avatar_url: 头像 URL（profile）
//   - email: 邮箱（email）
func (h *OAuthProviderHandler) UserInfo(c *gin.Context) {
	// 获取 Bearer Token
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		h.respondUserInfoError(c, http.StatusUnauthorized, "invalid_token", "Missing or invalid Authorization header")
		return
	}

	accessToken := strings.TrimPrefix(authHeader, "Bearer ")
	if accessToken == "" {
		h.respondUserInfoError(c, http.StatusUnauthorized, "invalid_token", "Missing access token")
		return
	}

	// 验证 Token
	tokenInfo, err := h.oauthService.ValidateAccessToken(c.Request.Context(), accessToken)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Access token validation failed: error=%v", err)
		h.respondUserInfoError(c, http.StatusUnauthorized, "invalid_token", "Invalid or expired access token")
		return
	}

	// 获取用户信息
	user, err := h.userCache.GetOrLoad(c.Request.Context(), tokenInfo.UserID, h.userRepo.FindByID)
	if err != nil {
		utils.LogPrintf("[OAUTH-PROVIDER] Failed to get user for userinfo: userID=%d, error=%v", tokenInfo.UserID, err)
		h.respondUserInfoError(c, http.StatusInternalServerError, "server_error", "Failed to get user info")
		return
	}

	// 检查用户是否被封禁
	if user.CheckBanned() {
		utils.LogPrintf("[OAUTH-PROVIDER] Banned user attempted to access userinfo: userID=%d", tokenInfo.UserID)
		h.respondUserInfoError(c, http.StatusForbidden, "access_denied", "User is banned")
		return
	}

	// 根据 scope 构建响应
	response := h.buildUserInfoResponse(user, tokenInfo.Scope)
	c.JSON(http.StatusOK, response)
}

// buildUserInfoResponse 根据 scope 构建用户信息响应
func (h *OAuthProviderHandler) buildUserInfoResponse(user *models.User, scope string) gin.H {
	response := gin.H{}
	scopes := strings.Split(scope, " ")

	for _, s := range scopes {
		switch s {
		case ScopeOpenID:
			response["sub"] = strconv.FormatInt(user.ID, 10)
		case ScopeProfile:
			response["username"] = user.Username
			// 处理头像 URL：如果是 "microsoft" 标记，使用微软头像
			avatarURL := user.AvatarURL
			if avatarURL == "microsoft" && user.MicrosoftAvatarURL.Valid {
				avatarURL = user.MicrosoftAvatarURL.String
			}
			response["avatar_url"] = avatarURL
		case ScopeEmail:
			response["email"] = user.Email
		}
	}

	return response
}

// ====================  Revoke 端点 ====================

// Revoke Token 撤销端点
// POST /oauth/revoke
//
// 请求参数（application/x-www-form-urlencoded）：
//   - token: 要撤销的 Token（必需）
//   - token_type_hint: Token 类型提示（可选，access_token 或 refresh_token）
//
// 响应：
//   - 始终返回 200 OK（RFC 7009）
func (h *OAuthProviderHandler) Revoke(c *gin.Context) {
	token := c.PostForm("token")

	if token == "" {
		// RFC 7009: 即使 token 为空也返回成功
		c.Status(http.StatusOK)
		return
	}

	// 撤销 Token（始终返回成功）
	_ = h.oauthService.RevokeToken(c.Request.Context(), token)

	utils.LogPrintf("[OAUTH-PROVIDER] Token revoked")
	c.Status(http.StatusOK)
}

// ====================  辅助方法 ====================

// normalizeScope 规范化 scope
// 返回空字符串表示无效 scope
func (h *OAuthProviderHandler) normalizeScope(scope string) string {
	parts := strings.Fields(scope)
	validParts := make([]string, 0, len(parts))

	for _, part := range parts {
		if validScopes[part] {
			validParts = append(validParts, part)
		}
	}

	if len(validParts) == 0 {
		return ""
	}

	return strings.Join(validParts, " ")
}

// parseScopeList 解析 scope 为列表（用于前端显示）
func (h *OAuthProviderHandler) parseScopeList(scope string) []string {
	return strings.Fields(scope)
}

// buildAuthorizeURL 构建授权 URL
func (h *OAuthProviderHandler) buildAuthorizeURL(clientID, redirectURI, responseType, scope, state string) string {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", responseType)
	params.Set("scope", scope)
	if state != "" {
		params.Set("state", state)
	}
	return h.baseURL + "/oauth/authorize?" + params.Encode()
}

// buildRedirectURL 构建重定向 URL（带授权码）
func (h *OAuthProviderHandler) buildRedirectURL(redirectURI, code, state string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI + "?code=" + code
	}

	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// redirectWithError 重定向并附带错误参数
func (h *OAuthProviderHandler) redirectWithError(c *gin.Context, redirectURI, state, errorCode, errorDesc string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		h.redirectToErrorPage(c, errorCode, errorDesc)
		return
	}

	q := u.Query()
	q.Set("error", errorCode)
	q.Set("error_description", errorDesc)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}

// redirectToErrorPage 重定向到授权页面并显示错误
func (h *OAuthProviderHandler) redirectToErrorPage(c *gin.Context, errorCode, errorDesc string) {
	params := url.Values{}
	params.Set("error", errorCode)
	if errorDesc != "" {
		params.Set("error_description", errorDesc)
	}
	c.Redirect(http.StatusFound, h.baseURL+"/account/oauth?"+params.Encode())
}

// buildAuthPageURL 构建授权页面 URL
func (h *OAuthProviderHandler) buildAuthPageURL(clientID, redirectURI, scope, state string) string {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", scope)
	if state != "" {
		params.Set("state", state)
	}
	return h.baseURL + "/account/oauth?" + params.Encode()
}

// respondTokenError 返回 Token 端点错误响应
func (h *OAuthProviderHandler) respondTokenError(c *gin.Context, status int, errorCode, errorDesc string) {
	c.JSON(status, gin.H{
		"error":             errorCode,
		"error_description": errorDesc,
	})
}

// respondUserInfoError 返回 UserInfo 端点错误响应
func (h *OAuthProviderHandler) respondUserInfoError(c *gin.Context, status int, errorCode, errorDesc string) {
	c.Header("WWW-Authenticate", "Bearer error=\""+errorCode+"\", error_description=\""+errorDesc+"\"")
	c.JSON(status, gin.H{
		"error":             errorCode,
		"error_description": errorDesc,
	})
}
