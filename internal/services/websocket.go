package services

import (
	"auth-system/internal/config"
	"auth-system/internal/models"
	"auth-system/internal/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/maphash"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	ErrWSMaxConnections  = errors.New("max connections reached")
	ErrWSClientNotFound  = errors.New("client not found")
	ErrWSUpgradeFailed   = errors.New("websocket upgrade failed")
	ErrWSSendBufferFull  = errors.New("send buffer full")
	ErrWSServiceShutdown = errors.New("websocket service is shutdown")
)

const (
	wsShardCount      = 8
	maxConnections    = 1000
	connectionTimeout = 5 * time.Minute
	cleanupInterval   = 1 * time.Minute
	writeWait         = 10 * time.Second
	pongWait          = 60 * time.Second
	pingPeriod        = 30 * time.Second
	maxMessageSize    = 512
	sendBufferSize    = 256
	readBufferSize    = 1024
	writeBufferSize   = 1024
)

// WSClient WebSocket 客户端
type WSClient struct {
	conn      *websocket.Conn
	token     string
	send      chan []byte
	createdAt time.Time
	closed    bool
	mu        sync.Mutex
}

// wsClientShard 客户端分片
type wsClientShard struct {
	clients map[string]*WSClient
	mu      sync.RWMutex
}

// WebSocketService 分片 WebSocket 服务
type WebSocketService struct {
	shards      [wsShardCount]*wsClientShard
	connCount   int32
	shutdown    chan struct{}
	wg          sync.WaitGroup
	isShutdown  bool
	mu          sync.RWMutex
	upgrader    websocket.Upgrader
	qrLoginRepo models.QRLoginStore
}

// WSMessage WebSocket 消息
type WSMessage struct {
	Type   string            `json:"type"`
	Status string            `json:"status,omitempty"`
	Data   map[string]string `json:"data,omitempty"`
}

// NewWebSocketService 创建 WebSocket 服务
func NewWebSocketService(cfg *config.Config, qrLoginRepo models.QRLoginStore) *WebSocketService {
	ws := &WebSocketService{
		shutdown: make(chan struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  readBufferSize,
			WriteBufferSize: writeBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}

				if cfg.CORSAllowOrigins == "" {
					utils.LogError("WS", "CheckOrigin", nil, "WebSocket origin check failed - CORS_ALLOW_ORIGINS is not configured, rejecting all origins")
					return false
				}

				allowedOrigins := strings.SplitSeq(cfg.CORSAllowOrigins, ",")
				for allowed := range allowedOrigins {
					allowed = strings.TrimSpace(allowed)
					if allowed == origin {
						return true
					}
				}

				utils.LogWarn("WS", "WebSocket origin rejected", fmt.Sprintf("origin=%s", origin))
				return false
			},
			Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
				utils.LogError("WS", "Upgrade", reason, fmt.Sprintf("WebSocket upgrade error: status=%d", status))
			},
		},
		qrLoginRepo: qrLoginRepo,
	}

	for i := range wsShardCount {
		ws.shards[i] = &wsClientShard{
			clients: make(map[string]*WSClient),
		}
	}

	ws.wg.Add(1)
	go ws.cleanup()

	utils.LogInfo("WS", fmt.Sprintf("WebSocket service initialized: shards=%d, maxConnections=%d", wsShardCount, maxConnections))

	return ws
}

// HandleQRLogin 处理扫码登录 WebSocket 连接
func (ws *WebSocketService) HandleQRLogin(c *gin.Context) {
	if ws.IsShutdown() {
		utils.LogWarn("WS", "Service is shutdown, rejecting connection", "")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "service unavailable"})
		return
	}

	token := c.Query("token")
	if token == "" {
		utils.LogWarn("WS", "Missing token in WebSocket request", "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})
		return
	}

	// 校验 token 合法性：必须存在于 qr_login_tokens 表、未过期、状态为 pending 或 scanned
	// 防止任意字符串占用连接配额，或越权接收他人状态推送
	if ws.qrLoginRepo == nil {
		utils.LogError("WS", "HandleQRLogin", nil, "qrLoginRepo not configured")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "service not configured"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	qrToken, err := ws.qrLoginRepo.FindByToken(ctx, token)
	if err != nil {
		utils.LogWarn("WS", "Invalid QR token for WebSocket", fmt.Sprintf("token=%s, err=%v", token, err))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	now := time.Now().UnixMilli()
	if qrToken.ExpireTime > 0 && now > qrToken.ExpireTime {
		utils.LogWarn("WS", "Expired QR token for WebSocket", fmt.Sprintf("token=%s", token))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token expired"})
		return
	}

	if qrToken.Status != models.QRStatusPending && qrToken.Status != models.QRStatusScanned {
		utils.LogWarn("WS", "QR token in invalid state for WebSocket", fmt.Sprintf("token=%s, status=%s", token, qrToken.Status))
		c.JSON(http.StatusConflict, gin.H{"error": "token already consumed"})
		return
	}

	if atomic.LoadInt32(&ws.connCount) >= maxConnections {
		utils.LogWarn("WS", "Max connections reached, rejecting new client", fmt.Sprintf("max=%d", maxConnections))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many connections"})
		return
	}

	conn, err := ws.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		utils.LogError("WS", "HandleConnection", err, "WebSocket upgrade failed")
		return
	}

	client := &WSClient{
		conn:      conn,
		token:     token,
		send:      make(chan []byte, sendBufferSize),
		createdAt: time.Now(),
	}

	if !ws.register(client) {
		utils.LogWarn("WS", "Failed to register client", fmt.Sprintf("token=%s", token))
		_ = conn.Close()
		return
	}

	go ws.writePump(client)
	go ws.readPump(client)
}

// NotifyStatusChange 通知状态变更
func (ws *WebSocketService) NotifyStatusChange(token, status string, data map[string]string) {
	if ws.IsShutdown() {
		return
	}

	if token == "" {
		utils.LogWarn("WS", "Empty token in NotifyStatusChange", "")
		return
	}

	shard := ws.getShard(token)

	shard.mu.RLock()
	client, ok := shard.clients[token]
	shard.mu.RUnlock()

	if !ok {
		return
	}

	message := map[string]any{
		"type":   "status",
		"status": status,
	}

	for k, v := range data {
		message[k] = v
	}

	jsonData, err := json.Marshal(message)
	if err != nil {
		utils.LogError("WS", "NotifyStatusChange", err, "Failed to marshal message")
		return
	}

	if err := ws.sendToClient(client, jsonData); err != nil {
		utils.LogWarn("WS", "Failed to send message", fmt.Sprintf("token=%s", token))
	}
}

// GetConnectionCount 获取当前连接数
// 返回：
//   - int: 当前连接数
func (ws *WebSocketService) GetConnectionCount() int {
	return int(atomic.LoadInt32(&ws.connCount))
}

// IsShutdown 检查服务是否已关闭
// 返回：
//   - bool: 是否已关闭
func (ws *WebSocketService) IsShutdown() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.isShutdown
}

// Shutdown 优雅关闭
func (ws *WebSocketService) Shutdown(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		ws.mu.Lock()
		if ws.isShutdown {
			ws.mu.Unlock()
			close(done)
			return
		}
		ws.isShutdown = true
		ws.mu.Unlock()

		close(ws.shutdown)

		ws.closeAllConnections()

		// 等待清理协程结束
		ws.wg.Wait()

		utils.LogInfo("WS", "WebSocket service shutdown complete")
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		utils.LogWarn("WS", "Shutdown timeout exceeded, forcing shutdown", "")
	}
}

// GetStats 获取服务统计信息
func (ws *WebSocketService) GetStats() map[string]any {
	stats := map[string]any{
		"connectionCount": ws.GetConnectionCount(),
		"maxConnections":  maxConnections,
		"shardCount":      wsShardCount,
		"isShutdown":      ws.IsShutdown(),
	}

	shardStats := make([]int, wsShardCount)
	for i := range wsShardCount {
		shard := ws.shards[i]
		shard.mu.RLock()
		shardStats[i] = len(shard.clients)
		shard.mu.RUnlock()
	}
	stats["shardStats"] = shardStats

	return stats
}

// getShard 获取 token 对应的分片
// 使用 maphash 哈希算法分配分片，与限流器保持一致
var wsHashSeed = maphash.MakeSeed()

func (ws *WebSocketService) getShard(token string) *wsClientShard {
	if token == "" {
		return ws.shards[0]
	}

	h := maphash.String(wsHashSeed, token)
	return ws.shards[h%wsShardCount]
}

// register 注册客户端
func (ws *WebSocketService) register(client *WSClient) bool {
	if atomic.LoadInt32(&ws.connCount) >= maxConnections {
		utils.LogWarn("WS", "Max connections reached, rejecting new client", "")
		return false
	}

	shard := ws.getShard(client.token)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if existingClient, ok := shard.clients[client.token]; ok {
		ws.closeClient(existingClient)
		atomic.AddInt32(&ws.connCount, -1)
		utils.LogInfo("WS", fmt.Sprintf("Replaced existing client: token=%s", client.token))
	}

	shard.clients[client.token] = client
	atomic.AddInt32(&ws.connCount, 1)

	utils.LogInfo("WS", fmt.Sprintf("Client registered: token=%s, total=%d", client.token, atomic.LoadInt32(&ws.connCount)))
	return true
}

// unregister 注销客户端
func (ws *WebSocketService) unregister(client *WSClient) {
	shard := ws.getShard(client.token)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, ok := shard.clients[client.token]; ok {
		delete(shard.clients, client.token)
		ws.closeClient(client)
		atomic.AddInt32(&ws.connCount, -1)
		utils.LogInfo("WS", fmt.Sprintf("Client unregistered: token=%s, total=%d", client.token, atomic.LoadInt32(&ws.connCount)))
	}
}

// closeClient 关闭客户端连接
func (ws *WebSocketService) closeClient(client *WSClient) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closed {
		return
	}
	client.closed = true

	close(client.send)
}

// sendToClient 发送消息到客户端
func (ws *WebSocketService) sendToClient(client *WSClient, message []byte) error {
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		return ErrWSClientNotFound
	}

	select {
	case client.send <- message:
		client.mu.Unlock()
		return nil
	default:
		client.mu.Unlock()
		ws.unregister(client)
		return ErrWSSendBufferFull
	}
}

// writePump 写入协程
func (ws *WebSocketService) writePump(client *WSClient) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			if err := client.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				utils.LogWarn("WS", "Failed to set write deadline", "")
				return
			}

			if !ok {
				// 通道已关闭
				if err := client.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					utils.LogDebug("WS", "Failed to write close message")
				}
				return
			}

			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				utils.LogDebug("WS", "Failed to write message")
				return
			}

		case <-ticker.C:
			if err := client.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				utils.LogWarn("WS", "Failed to set write deadline for ping", "")
				return
			}

			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				utils.LogDebug("WS", "Failed to write ping")
				return
			}
		}
	}
}

// readPump 读取协程
func (ws *WebSocketService) readPump(client *WSClient) {
	defer func() {
		ws.unregister(client)
	}()

	client.conn.SetReadLimit(maxMessageSize)

	if err := client.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		utils.LogWarn("WS", "Failed to set read deadline", "")
		return
	}

	client.conn.SetPongHandler(func(string) error {
		if err := client.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			utils.LogWarn("WS", "Failed to set read deadline in pong handler", "")
			return err
		}
		return nil
	})

	for {
		_, _, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				utils.LogDebug("WS", "Unexpected close error")
			}
			break
		}
	}
}

// cleanup 定期清理过期连接
func (ws *WebSocketService) cleanup() {
	defer ws.wg.Done()

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ws.shutdown:
			utils.LogInfo("WS", "Cleanup goroutine stopped")
			return
		case <-ticker.C:
			ws.cleanupExpired()
		}
	}
}

// cleanupExpired 清理过期连接
func (ws *WebSocketService) cleanupExpired() {
	now := time.Now()
	expired := 0

	for i := range wsShardCount {
		shard := ws.shards[i]

		var expiredClients []*WSClient

		shard.mu.Lock()
		for token, client := range shard.clients {
			if now.Sub(client.createdAt) > connectionTimeout {
				expiredClients = append(expiredClients, client)
				delete(shard.clients, token)
				atomic.AddInt32(&ws.connCount, -1)
				expired++
			}
		}
		shard.mu.Unlock()

		for _, client := range expiredClients {
			ws.closeClient(client)
		}
	}

	if expired > 0 {
		utils.LogInfo("WS", fmt.Sprintf("Cleaned up %d expired connections, remaining: %d", expired, atomic.LoadInt32(&ws.connCount)))
	}
}

// closeAllConnections 关闭所有连接
func (ws *WebSocketService) closeAllConnections() {
	for i := range wsShardCount {
		shard := ws.shards[i]

		shard.mu.Lock()
		for _, client := range shard.clients {
			if err := client.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutdown")); err != nil {
				utils.LogDebug("WS", "Failed to write close message")
			}
			ws.closeClient(client)
		}
		shard.clients = make(map[string]*WSClient)
		shard.mu.Unlock()
	}

	atomic.StoreInt32(&ws.connCount, 0)
	utils.LogInfo("WS", "All connections closed")
}
