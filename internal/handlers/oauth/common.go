/**
 * internal/handlers/oauth/common.go
 * OAuth 公共定义和工具函数
 *
 * 功能：
 * - 错误定义
 * - 常量定义
 * - 公共数据结构
 * - 全局存储（state、pendingLinks）
 * - 辅助函数
 *
 * 依赖：
 * - internal/utils (日志)
 * - github.com/gin-gonic/gin
 */

package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	// ErrOAuthNotConfigured OAuth 未配置
	ErrOAuthNotConfigured = errors.New("OAUTH_NOT_CONFIGURED")

	// ErrOAuthStateMismatch OAuth state 不匹配
	ErrOAuthStateMismatch = errors.New("OAUTH_STATE_MISMATCH")

	// ErrOAuthStateExpired OAuth state 已过期
	ErrOAuthStateExpired = errors.New("OAUTH_STATE_EXPIRED")

	// ErrOAuthTokenExchange Token 交换失败
	ErrOAuthTokenExchange = errors.New("OAUTH_TOKEN_EXCHANGE_FAILED")

	// ErrOAuthUserInfo 获取用户信息失败
	ErrOAuthUserInfo = errors.New("OAUTH_USER_INFO_FAILED")

	// ErrMicrosoftAlreadyLinked 微软账户已被其他用户绑定
	ErrMicrosoftAlreadyLinked = errors.New("MICROSOFT_ALREADY_LINKED")

	// ErrNotLinked 未绑定微软账户
	ErrNotLinked = errors.New("NOT_LINKED")

	// ErrInvalidLinkToken 无效的绑定 Token
	ErrInvalidLinkToken = errors.New("INVALID_LINK_TOKEN")

	// ErrLinkTokenExpired 绑定 Token 已过期
	ErrLinkTokenExpired = errors.New("LINK_TOKEN_EXPIRED")
)

// ====================  常量定义 ====================

const (
	// StateExpiryDuration State 过期时间
	StateExpiryDuration = 10 * time.Minute

	// StateExpiryMS State 过期时间（毫秒）
	StateExpiryMS = 10 * 60 * 1000

	// CookieMaxAge OAuth Cookie 有效期（60 天）
	CookieMaxAge = 60 * 24 * 60 * 60

	// HTTPClientTimeout HTTP 客户端超时时间
	HTTPClientTimeout = 10 * time.Second

	// CleanupInterval 清理任务间隔
	CleanupInterval = 5 * time.Minute

	// ActionLogin 登录操作
	ActionLogin = "login"

	// ActionLink 绑定操作
	ActionLink = "link"
)

// ====================  数据结构 ====================

// State OAuth state 数据
// 用于防止 CSRF 攻击，存储授权请求的上下文
type State struct {
	Timestamp int64  // 创建时间戳（毫秒）
	Action    string // 操作类型：login/link
	UserID    int64  // 用户 ID（仅 link 操作）
}

// PendingLink 待确认绑定数据
// 当用户通过 OAuth 登录但邮箱已存在时，需要确认绑定
type PendingLink struct {
	UserID             int64  // 已存在用户的 ID
	ProviderID         string // 第三方账户 ID
	DisplayName        string // 第三方显示名称
	ProviderAvatarURL  string // 第三方头像 URL
	Email              string // 邮箱地址
	Timestamp          int64  // 创建时间戳（毫秒）
}

// ====================  全局存储 ====================

// 注意：以下存储使用内存 map 实现，存在以下限制：
// 1. 服务重启会丢失所有数据（正在进行的 OAuth 流程会失败）
// 2. 多实例部署时无法共享状态（需要 sticky session 或改用 Redis）
// 当前适用于单实例部署场景，如需多实例部署请改用 Redis 存储
var (
	states       = make(map[string]*State)       // OAuth state 存储
	pendingLinks = make(map[string]*PendingLink) // 待绑定数据存储
	stateMu      sync.RWMutex                    // state 读写锁
	linkMu       sync.RWMutex                    // pendingLinks 读写锁
)

// ====================  State 管理 ====================

// SaveState 保存 OAuth state
//
// 参数：
//   - state: state 字符串
//   - data: state 数据
func SaveState(state string, data *State) {
	stateMu.Lock()
	states[state] = data
	stateMu.Unlock()
}

// GetState 获取 OAuth state
//
// 参数：
//   - state: state 字符串
//
// 返回：
//   - *State: state 数据
//   - bool: 是否存在
func GetState(state string) (*State, bool) {
	stateMu.RLock()
	data, exists := states[state]
	stateMu.RUnlock()
	return data, exists
}

// DeleteState 删除 OAuth state
//
// 参数：
//   - state: state 字符串
func DeleteState(state string) {
	stateMu.Lock()
	delete(states, state)
	stateMu.Unlock()
}

// GetAndDeleteState 获取并删除 OAuth state（原子操作）
// 用于防止重复提交攻击
//
// 参数：
//   - state: state 字符串
//
// 返回：
//   - *State: state 数据
//   - bool: 是否存在
func GetAndDeleteState(state string) (*State, bool) {
	stateMu.Lock()
	data, exists := states[state]
	if exists {
		delete(states, state)
	}
	stateMu.Unlock()
	return data, exists
}

// ====================  PendingLink 管理 ====================

// SavePendingLink 保存待绑定数据
//
// 参数：
//   - token: 绑定 token
//   - data: 待绑定数据
func SavePendingLink(token string, data *PendingLink) {
	linkMu.Lock()
	pendingLinks[token] = data
	linkMu.Unlock()
}

// GetPendingLink 获取待绑定数据
//
// 参数：
//   - token: 绑定 token
//
// 返回：
//   - *PendingLink: 待绑定数据
//   - bool: 是否存在
func GetPendingLink(token string) (*PendingLink, bool) {
	linkMu.RLock()
	data, exists := pendingLinks[token]
	linkMu.RUnlock()
	return data, exists
}

// DeletePendingLink 删除待绑定数据
//
// 参数：
//   - token: 绑定 token
func DeletePendingLink(token string) {
	linkMu.Lock()
	delete(pendingLinks, token)
	linkMu.Unlock()
}

// GetAndDeletePendingLink 获取并删除待绑定数据（原子操作）
//
// 参数：
//   - token: 绑定 token
//
// 返回：
//   - *PendingLink: 待绑定数据
//   - bool: 是否存在
func GetAndDeletePendingLink(token string) (*PendingLink, bool) {
	linkMu.Lock()
	data, exists := pendingLinks[token]
	if exists {
		delete(pendingLinks, token)
	}
	linkMu.Unlock()
	return data, exists
}

// ====================  辅助函数 ====================

// GenerateState 生成随机 state
// 用于防止 CSRF 攻击
//
// 返回：
//   - string: 32 字符的十六进制字符串
//   - error: 随机数生成错误
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to generate state: %v", err)
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// GenerateLinkToken 生成绑定 token
// 用于待绑定确认流程
//
// 返回：
//   - string: 48 字符的十六进制字符串
//   - error: 随机数生成错误
func GenerateLinkToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to generate link token: %v", err)
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SetAuthCookie 设置认证 Cookie
//
// 参数：
//   - c: Gin 上下文
//   - token: JWT Token
func SetAuthCookie(c *gin.Context, token string) {
	if token == "" {
		utils.LogPrintf("[OAUTH] WARN: Attempted to set empty token cookie")
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "token",
		Value:    token,
		MaxAge:   CookieMaxAge,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// RedirectWithError 重定向并附带错误参数
//
// 参数：
//   - c: Gin 上下文
//   - baseURL: 基础 URL
//   - path: 重定向路径
//   - errorCode: 错误代码
func RedirectWithError(c *gin.Context, baseURL, path, errorCode string) {
	c.Redirect(http.StatusFound, baseURL+path+"?error="+errorCode)
}

// RedirectWithSuccess 重定向并附带成功参数
//
// 参数：
//   - c: Gin 上下文
//   - baseURL: 基础 URL
//   - path: 重定向路径
//   - successCode: 成功代码
func RedirectWithSuccess(c *gin.Context, baseURL, path, successCode string) {
	c.Redirect(http.StatusFound, baseURL+path+"?success="+successCode)
}

// RespondError 返回错误响应
//
// 参数：
//   - c: Gin 上下文
//   - status: HTTP 状态码
//   - errorCode: 错误代码
func RespondError(c *gin.Context, status int, errorCode string) {
	c.JSON(status, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
}

// RespondSuccess 返回成功响应
//
// 参数：
//   - c: Gin 上下文
//   - data: 响应数据
func RespondSuccess(c *gin.Context, data gin.H) {
	response := gin.H{"success": true}
	for k, v := range data {
		response[k] = v
	}
	c.JSON(http.StatusOK, response)
}

// ====================  清理任务 ====================

// StartCleanup 启动清理任务
// 定期清理过期的 OAuth state 和待绑定数据
func StartCleanup() {
	go func() {
		ticker := time.NewTicker(CleanupInterval)
		defer ticker.Stop()

		utils.LogPrintf("[OAUTH] Cleanup task started")

		for range ticker.C {
			cleanupExpiredData()
		}
	}()
}

// cleanupExpiredData 清理过期数据
func cleanupExpiredData() {
	now := time.Now().UnixMilli()
	stateCount := 0
	linkCount := 0

	// 清理过期的 OAuth state
	stateMu.Lock()
	for state, data := range states {
		if data == nil || now-data.Timestamp > StateExpiryMS {
			delete(states, state)
			stateCount++
		}
	}
	stateMu.Unlock()

	// 清理过期的待绑定数据
	linkMu.Lock()
	for token, data := range pendingLinks {
		if data == nil || now-data.Timestamp > StateExpiryMS {
			delete(pendingLinks, token)
			linkCount++
		}
	}
	linkMu.Unlock()

	// 仅在有清理时记录日志
	if stateCount > 0 || linkCount > 0 {
		utils.LogPrintf("[OAUTH] Cleanup completed: states=%d, links=%d", stateCount, linkCount)
	}
}
