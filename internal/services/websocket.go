/**
 * internal/services/websocket.go
 * 分片 WebSocket 服务
 *
 * 功能：
 * - 分片连接管理（减少锁竞争）
 * - 扫码登录状态推送
 * - 连接池限制（防止资源耗尽）
 * - 过期连接清理
 * - 优雅关闭
 *
 * 设计说明：
 * - 使用 8 个分片减少锁竞争
 * - 最大连接数 1000，防止资源耗尽
 * - 连接超时 5 分钟，自动清理
 * - 支持 Ping/Pong 心跳保活
 *
 * 依赖：
 * - github.com/gorilla/websocket: WebSocket 库
 */

package services

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ====================  错误定义 ====================

var (
	// ErrWSMaxConnections 达到最大连接数
	ErrWSMaxConnections = errors.New("max connections reached")
	// ErrWSClientNotFound 客户端未找到
	ErrWSClientNotFound = errors.New("client not found")
	// ErrWSUpgradeFailed WebSocket 升级失败
	ErrWSUpgradeFailed = errors.New("websocket upgrade failed")
	// ErrWSSendBufferFull 发送缓冲区已满
	ErrWSSendBufferFull = errors.New("send buffer full")
	// ErrWSServiceShutdown 服务已关闭
	ErrWSServiceShutdown = errors.New("websocket service is shutdown")
)

// ====================  常量定义 ====================

const (
	// wsShardCount 分片数量
	wsShardCount = 8

	// maxConnections 最大连接数
	maxConnections = 1000

	// connectionTimeout 连接超时时间
	connectionTimeout = 5 * time.Minute

	// cleanupInterval 清理间隔
	cleanupInterval = 1 * time.Minute

	// writeWait 写入超时
	writeWait = 10 * time.Second

	// pongWait Pong 等待时间
	pongWait = 60 * time.Second

	// pingPeriod Ping 周期（必须小于 pongWait）
	pingPeriod = 30 * time.Second

	// maxMessageSize 最大消息大小
	maxMessageSize = 512

	// sendBufferSize 发送缓冲区大小
	sendBufferSize = 256

	// readBufferSize 读取缓冲区大小
	readBufferSize = 1024

	// writeBufferSize 写入缓冲区大小
	writeBufferSize = 1024
)

// upgrader WebSocket 升级器
var upgrader = websocket.Upgrader{
	ReadBufferSize:  readBufferSize,
	WriteBufferSize: writeBufferSize,
	CheckOrigin: func(r *http.Request) bool {
		// 生产环境应该检查 Origin
		// 这里允许所有来源，因为有其他安全措施
		return true
	},
	Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
		log.Printf("[WS] ERROR: Upgrade error: status=%d, reason=%v", status, reason)
	},
}

// ====================  数据结构 ====================

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
	shards    [wsShardCount]*wsClientShard
	connCount int32
	shutdown  chan struct{}
	wg        sync.WaitGroup
	isShutdown bool
	mu        sync.RWMutex
}

// WSMessage WebSocket 消息
type WSMessage struct {
	Type   string            `json:"type"`
	Status string            `json:"status,omitempty"`
	Data   map[string]string `json:"data,omitempty"`
}

// ====================  构造函数 ====================

// NewWebSocketService 创建 WebSocket 服务
// 返回：
//   - *WebSocketService: WebSocket 服务实例
func NewWebSocketService() *WebSocketService {
	ws := &WebSocketService{
		shutdown: make(chan struct{}),
	}

	// 初始化所有分片
	for i := 0; i < wsShardCount; i++ {
		ws.shards[i] = &wsClientShard{
			clients: make(map[string]*WSClient),
		}
	}

	// 启动清理协程
	ws.wg.Add(1)
	go ws.cleanup()

	log.Printf("[WS] WebSocket service initialized: shards=%d, maxConnections=%d", wsShardCount, maxConnections)

	return ws
}

// ====================  公开方法 ====================

// HandleQRLogin 处理扫码登录 WebSocket 连接
// 参数：
//   - c: Gin Context
func (ws *WebSocketService) HandleQRLogin(c *gin.Context) {
	// 检查服务是否已关闭
	if ws.IsShutdown() {
		log.Println("[WS] WARN: Service is shutdown, rejecting connection")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "service unavailable"})
		return
	}

	// 获取 Token
	token := c.Query("token")
	if token == "" {
		log.Println("[WS] WARN: Missing token in WebSocket request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})
		return
	}

	// 检查连接数限制
	if atomic.LoadInt32(&ws.connCount) >= maxConnections {
		log.Printf("[WS] WARN: Max connections reached (%d), rejecting new client", maxConnections)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many connections"})
		return
	}

	// 升级到 WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] ERROR: WebSocket upgrade failed: %v", err)
		return
	}

	// 创建客户端
	client := &WSClient{
		conn:      conn,
		token:     token,
		send:      make(chan []byte, sendBufferSize),
		createdAt: time.Now(),
	}

	// 注册客户端
	if !ws.register(client) {
		log.Printf("[WS] WARN: Failed to register client: token=%s", token)
		conn.Close()
		return
	}

	// 启动读写协程
	go ws.writePump(client)
	go ws.readPump(client)
}

// NotifyStatusChange 通知状态变更
// 参数：
//   - token: 客户端 Token
//   - status: 状态
//   - data: 附加数据
func (ws *WebSocketService) NotifyStatusChange(token, status string, data map[string]string) {
	// 检查服务是否已关闭
	if ws.IsShutdown() {
		return
	}

	// 参数验证
	if token == "" {
		log.Println("[WS] WARN: Empty token in NotifyStatusChange")
		return
	}

	// 获取客户端
	shard := ws.getShard(token)

	shard.mu.RLock()
	client, ok := shard.clients[token]
	shard.mu.RUnlock()

	if !ok {
		// 客户端不存在，静默返回
		return
	}

	// 构建消息
	message := map[string]interface{}{
		"type":   "status",
		"status": status,
	}

	// 添加附加数据
	if data != nil {
		for k, v := range data {
			message[k] = v
		}
	}

	// 序列化消息
	jsonData, err := json.Marshal(message)
	if err != nil {
		log.Printf("[WS] ERROR: Failed to marshal message: %v", err)
		return
	}

	// 发送消息
	if err := ws.sendToClient(client, jsonData); err != nil {
		log.Printf("[WS] WARN: Failed to send message: token=%s, error=%v", token, err)
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
func (ws *WebSocketService) Shutdown() {
	ws.mu.Lock()
	if ws.isShutdown {
		ws.mu.Unlock()
		return
	}
	ws.isShutdown = true
	ws.mu.Unlock()

	// 发送关闭信号
	close(ws.shutdown)

	// 关闭所有连接
	ws.closeAllConnections()

	// 等待清理协程结束
	ws.wg.Wait()

	log.Println("[WS] WebSocket service shutdown complete")
}

// GetStats 获取服务统计信息
// 返回：
//   - map[string]interface{}: 统计信息
func (ws *WebSocketService) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"connectionCount": ws.GetConnectionCount(),
		"maxConnections":  maxConnections,
		"shardCount":      wsShardCount,
		"isShutdown":      ws.IsShutdown(),
	}

	// 统计每个分片的连接数
	shardStats := make([]int, wsShardCount)
	for i := 0; i < wsShardCount; i++ {
		shard := ws.shards[i]
		shard.mu.RLock()
		shardStats[i] = len(shard.clients)
		shard.mu.RUnlock()
	}
	stats["shardStats"] = shardStats

	return stats
}

// ====================  私有方法 ====================

// getShard 获取 token 对应的分片
// 参数：
//   - token: 客户端 Token
//
// 返回：
//   - *wsClientShard: 对应的分片
func (ws *WebSocketService) getShard(token string) *wsClientShard {
	if token == "" {
		return ws.shards[0]
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	return ws.shards[h.Sum32()%wsShardCount]
}

// register 注册客户端
// 参数：
//   - client: 客户端
//
// 返回：
//   - bool: 是否注册成功
func (ws *WebSocketService) register(client *WSClient) bool {
	// 检查连接数限制
	if atomic.LoadInt32(&ws.connCount) >= maxConnections {
		log.Printf("[WS] WARN: Max connections reached, rejecting new client")
		return false
	}

	shard := ws.getShard(client.token)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// 检查是否已存在同 token 的连接
	if existingClient, ok := shard.clients[client.token]; ok {
		// 关闭旧连接
		ws.closeClient(existingClient)
		atomic.AddInt32(&ws.connCount, -1)
		log.Printf("[WS] Replaced existing client: token=%s", client.token)
	}

	shard.clients[client.token] = client
	atomic.AddInt32(&ws.connCount, 1)

	log.Printf("[WS] Client registered: token=%s, total=%d", client.token, atomic.LoadInt32(&ws.connCount))
	return true
}

// unregister 注销客户端
// 参数：
//   - client: 客户端
func (ws *WebSocketService) unregister(client *WSClient) {
	shard := ws.getShard(client.token)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, ok := shard.clients[client.token]; ok {
		delete(shard.clients, client.token)
		ws.closeClient(client)
		atomic.AddInt32(&ws.connCount, -1)
		log.Printf("[WS] Client unregistered: token=%s, total=%d", client.token, atomic.LoadInt32(&ws.connCount))
	}
}

// closeClient 关闭客户端连接
// 参数：
//   - client: 客户端
func (ws *WebSocketService) closeClient(client *WSClient) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closed {
		return
	}
	client.closed = true

	// 关闭发送通道
	close(client.send)

	// 关闭连接
	if client.conn != nil {
		client.conn.Close()
	}
}

// sendToClient 发送消息到客户端
// 参数：
//   - client: 客户端
//   - message: 消息
//
// 返回：
//   - error: 错误信息
func (ws *WebSocketService) sendToClient(client *WSClient, message []byte) error {
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		return ErrWSClientNotFound
	}
	client.mu.Unlock()

	select {
	case client.send <- message:
		return nil
	default:
		// 发送缓冲区满，移除客户端
		ws.unregister(client)
		return ErrWSSendBufferFull
	}
}

// writePump 写入协程
// 参数：
//   - client: 客户端
func (ws *WebSocketService) writePump(client *WSClient) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			if err := client.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("[WS] WARN: Failed to set write deadline: %v", err)
				return
			}

			if !ok {
				// 通道已关闭
				if err := client.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					log.Printf("[WS] DEBUG: Failed to write close message: %v", err)
				}
				return
			}

			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[WS] DEBUG: Failed to write message: %v", err)
				return
			}

		case <-ticker.C:
			if err := client.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("[WS] WARN: Failed to set write deadline for ping: %v", err)
				return
			}

			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[WS] DEBUG: Failed to write ping: %v", err)
				return
			}
		}
	}
}

// readPump 读取协程
// 参数：
//   - client: 客户端
func (ws *WebSocketService) readPump(client *WSClient) {
	defer func() {
		ws.unregister(client)
		client.conn.Close()
	}()

	// 设置读取限制
	client.conn.SetReadLimit(maxMessageSize)

	// 设置读取超时
	if err := client.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Printf("[WS] WARN: Failed to set read deadline: %v", err)
		return
	}

	// 设置 Pong 处理器
	client.conn.SetPongHandler(func(string) error {
		if err := client.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			log.Printf("[WS] WARN: Failed to set read deadline in pong handler: %v", err)
			return err
		}
		return nil
	})

	// 读取消息循环
	for {
		_, _, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("[WS] DEBUG: Unexpected close error: %v", err)
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
			log.Println("[WS] Cleanup goroutine stopped")
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

	for i := 0; i < wsShardCount; i++ {
		shard := ws.shards[i]

		// 收集过期的客户端
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

		// 关闭过期的客户端（在锁外执行）
		for _, client := range expiredClients {
			ws.closeClient(client)
		}
	}

	if expired > 0 {
		log.Printf("[WS] Cleaned up %d expired connections, remaining: %d", expired, atomic.LoadInt32(&ws.connCount))
	}
}

// closeAllConnections 关闭所有连接
func (ws *WebSocketService) closeAllConnections() {
	for i := 0; i < wsShardCount; i++ {
		shard := ws.shards[i]

		shard.mu.Lock()
		for _, client := range shard.clients {
			// 发送关闭消息
			if err := client.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutdown")); err != nil {
				log.Printf("[WS] DEBUG: Failed to write close message: %v", err)
			}
			ws.closeClient(client)
		}
		shard.clients = make(map[string]*WSClient)
		shard.mu.Unlock()
	}

	atomic.StoreInt32(&ws.connCount, 0)
	log.Println("[WS] All connections closed")
}
