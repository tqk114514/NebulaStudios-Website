// Package oauth 提供 OAuth 公共类型、常量、状态存储和 Cookie 辅助函数。
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

var (
	ErrOAuthNotConfigured     = errors.New("OAUTH_NOT_CONFIGURED")
	ErrOAuthStateMismatch     = errors.New("OAUTH_STATE_MISMATCH")
	ErrOAuthStateExpired      = errors.New("OAUTH_STATE_EXPIRED")
	ErrOAuthTokenExchange     = errors.New("OAUTH_TOKEN_EXCHANGE_FAILED")
	ErrOAuthUserInfo          = errors.New("OAUTH_USER_INFO_FAILED")
	ErrMicrosoftAlreadyLinked = errors.New("MICROSOFT_ALREADY_LINKED")
	ErrNotLinked              = errors.New("NOT_LINKED")
	ErrInvalidLinkToken       = errors.New("INVALID_LINK_TOKEN")
	ErrLinkTokenExpired       = errors.New("LINK_TOKEN_EXPIRED")
)

const (
	StateExpiryDuration     = 10 * time.Minute
	StateExpiryMS           = 10 * 60 * 1000
	CookieMaxAge            = 60 * 24 * 60 * 60
	HTTPClientTimeout       = 10 * time.Second
	CleanupInterval         = 5 * time.Minute
	ActionLogin             = "login"
	ActionLink              = "link"
	maxStatesCapacity       = 10000
	maxPendingLinksCapacity = 5000
)

// State OAuth state 数据，用于防止 CSRF 攻击
type State struct {
	Timestamp    int64  // 创建时间戳（毫秒）
	Action       string // 操作类型：login/link
	UserUID      string // 用户 UID（仅 link 操作）
	CodeVerifier string // PKCE code_verifier
	ReturnURL    string // 登录后重定向地址
}

// PendingLink 待确认绑定数据，当用户通过 OAuth 登录但邮箱已存在时需要确认绑定
type PendingLink struct {
	UserUID           string // 已存在用户的 UID
	ProviderID        string // 第三方账户 ID
	DisplayName       string // 第三方显示名称
	ProviderAvatarURL string // 第三方头像 URL
	Email             string // 邮箱地址
	Timestamp         int64  // 创建时间戳（毫秒）
}

// 注意：以下存储使用内存 map 实现，服务重启会丢失所有数据，多实例部署时无法共享状态。
// 当前适用于单实例部署场景，如需多实例部署请改用 Redis 存储。
// 存储带有最大容量限制，达到上限时按 FIFO 淘汰旧条目。
var (
	states         = make(map[string]*State)       // OAuth state 存储
	pendingLinks   = make(map[string]*PendingLink) // 待绑定数据存储
	stateMu        sync.RWMutex                    // state 读写锁
	linkMu         sync.RWMutex                    // pendingLinks 读写锁
	stateIndex     = make(map[string]int64)        // state 插入序号（用于 FIFO）
	pendingIndex   = make(map[string]int64)        // pendingLink 插入序号（用于 FIFO）
	stateCounter   int64                           // state 自增计数器
	pendingCounter int64                           // pendingLink 自增计数器
)

// SaveState 保存 OAuth state，达到容量上限时按 FIFO 淘汰旧条目
func SaveState(state string, data *State) {
	stateMu.Lock()
	defer stateMu.Unlock()

	if len(states) >= maxStatesCapacity {
		fifoEvictLocked(states, stateIndex, maxStatesCapacity/10)
	}

	stateCounter++
	states[state] = data
	stateIndex[state] = stateCounter
}

func GetState(state string) (*State, bool) {
	stateMu.RLock()
	data, exists := states[state]
	stateMu.RUnlock()
	return data, exists
}

func DeleteState(state string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	delete(states, state)
	delete(stateIndex, state)
}

// GetAndDeleteState 获取并删除 OAuth state（原子操作），用于防止重复提交攻击
func GetAndDeleteState(state string) (*State, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	data, exists := states[state]
	if exists {
		delete(states, state)
		delete(stateIndex, state)
	}
	return data, exists
}

// SavePendingLink 保存待绑定数据，达到容量上限时按 FIFO 淘汰旧条目
func SavePendingLink(token string, data *PendingLink) {
	linkMu.Lock()
	defer linkMu.Unlock()

	if len(pendingLinks) >= maxPendingLinksCapacity {
		fifoEvictLocked(pendingLinks, pendingIndex, maxPendingLinksCapacity/10)
	}

	pendingCounter++
	pendingLinks[token] = data
	pendingIndex[token] = pendingCounter
}

func GetPendingLink(token string) (*PendingLink, bool) {
	linkMu.RLock()
	data, exists := pendingLinks[token]
	linkMu.RUnlock()
	return data, exists
}

func DeletePendingLink(token string) {
	linkMu.Lock()
	defer linkMu.Unlock()
	delete(pendingLinks, token)
	delete(pendingIndex, token)
}

// GetAndDeletePendingLink 获取并删除待绑定数据（原子操作）
func GetAndDeletePendingLink(token string) (*PendingLink, bool) {
	linkMu.Lock()
	defer linkMu.Unlock()
	data, exists := pendingLinks[token]
	if exists {
		delete(pendingLinks, token)
		delete(pendingIndex, token)
	}
	return data, exists
}

// GenerateState 生成随机 state 用于防止 CSRF 攻击
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		utils.LogError("OAUTH", "GenerateState", err, "Failed to generate state")
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// GenerateLinkToken 生成绑定 token 用于待绑定确认流程
func GenerateLinkToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		utils.LogError("OAUTH", "GenerateLinkToken", err, "Failed to generate link token")
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// GenerateCodeVerifier 生成 PKCE code_verifier 用于防止授权码拦截攻击
func GenerateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		utils.LogError("OAUTH", "GenerateCodeVerifier", err, "Failed to generate code verifier")
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateCodeChallenge 使用 S256 方法（SHA256）生成 PKCE code_challenge
func GenerateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// SetAuthCookie 设置认证 Cookie
func SetAuthCookie(c *gin.Context, token string) {
	if token == "" {
		utils.LogWarn("OAUTH", "Attempted to set empty token cookie", "")
		return
	}
	utils.SetTokenCookieGin(c, token)
}

// RedirectWithError 重定向并附带错误参数
func RedirectWithError(c *gin.Context, baseURL, path, errorCode string) {
	c.Redirect(http.StatusFound, baseURL+path+"?error="+errorCode)
}

// RedirectWithSuccess 重定向并附带成功参数
func RedirectWithSuccess(c *gin.Context, baseURL, path, successCode string) {
	c.Redirect(http.StatusFound, baseURL+path+"?success="+successCode)
}

// StartCleanup 启动清理任务，定期清理过期的 OAuth state 和待绑定数据
func StartCleanup() {
	go func() {
		ticker := time.NewTicker(CleanupInterval)
		defer ticker.Stop()

		utils.LogInfo("OAUTH", "Cleanup task started")

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

	stateMu.Lock()
	for state, data := range states {
		if data == nil || now-data.Timestamp > StateExpiryMS {
			delete(states, state)
			delete(stateIndex, state)
			stateCount++
		}
	}
	stateMu.Unlock()

	linkMu.Lock()
	for token, data := range pendingLinks {
		if data == nil || now-data.Timestamp > StateExpiryMS {
			delete(pendingLinks, token)
			delete(pendingIndex, token)
			linkCount++
		}
	}
	linkMu.Unlock()

	if stateCount > 0 || linkCount > 0 {
		utils.LogInfo("OAUTH", fmt.Sprintf("Cleanup completed: states=%d, links=%d", stateCount, linkCount))
	}
}

// fifoEvictLocked 按 FIFO 原则淘汰最旧的 N 个条目（持有锁的情况下调用）
func fifoEvictLocked(dataMap any, indexMap map[string]int64, count int) {
	if count <= 0 {
		return
	}

	switch m := dataMap.(type) {
	case map[string]*State:
		toEvict := findOldestKeys(indexMap, count)
		for _, key := range toEvict {
			delete(m, key)
			delete(indexMap, key)
		}
	case map[string]*PendingLink:
		toEvict := findOldestKeys(indexMap, count)
		for _, key := range toEvict {
			delete(m, key)
			delete(indexMap, key)
		}
	}
}

// findOldestKeys 找出序号最小的 N 个 key
func findOldestKeys(indexMap map[string]int64, count int) []string {
	if len(indexMap) <= count {
		keys := make([]string, 0, len(indexMap))
		for k := range indexMap {
			keys = append(keys, k)
		}
		return keys
	}

	heap := make([]fifoKv, 0, count)

	for k, v := range indexMap {
		if len(heap) < count {
			heap = append(heap, fifoKv{k, v})
			if len(heap) == count {
				buildFifoMinHeap(heap)
			}
		} else if v < heap[0].value {
			heap[0] = fifoKv{k, v}
			fifoHeapify(heap, 0)
		}
	}

	result := make([]string, count)
	for i := range heap {
		result[i] = heap[i].key
	}
	return result
}

type fifoKv struct {
	key   string
	value int64
}

func buildFifoMinHeap(h []fifoKv) {
	for i := len(h)/2 - 1; i >= 0; i-- {
		fifoHeapify(h, i)
	}
}

func fifoHeapify(h []fifoKv, i int) {
	min := i
	left := 2*i + 1
	right := 2*i + 2
	if left < len(h) && h[left].value < h[min].value {
		min = left
	}
	if right < len(h) && h[right].value < h[min].value {
		min = right
	}
	if min != i {
		h[i], h[min] = h[min], h[i]
		fifoHeapify(h, min)
	}
}
