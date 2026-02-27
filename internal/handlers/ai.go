/**
 * internal/handlers/ai.go
 * AI 聊天 API Handler
 *
 * 功能：
 * - 接收用户消息并转发至 AI API
 * - 注入系统提示词（政策助手角色）
 * - 返回 AI 生成的回复
 *
 * 依赖：
 * - internal/config (AI API 配置)
 * - internal/utils (日志工具、响应工具)
 */

package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  常量定义 ====================

const (
	// aiRequestTimeout AI API 请求超时时间
	aiRequestTimeout = 30 * time.Second

	// aiPromptFile 系统提示词文件路径
	aiPromptFile = "dist/data/ai-prompt.txt"
)

// ====================  系统提示词 ====================

// systemPrompt 缓存的系统提示词
var systemPrompt string

// loadSystemPrompt 从文件加载系统提示词
// 启动时调用一次，后续使用缓存
func loadSystemPrompt() error {
	data, err := os.ReadFile(aiPromptFile)
	if err != nil {
		return err
	}
	systemPrompt = string(data)
	return nil
}

// InitAI 初始化 AI 模块
// 在服务启动时调用
func InitAI() error {
	if err := loadSystemPrompt(); err != nil {
		utils.LogPrintf("[AI] Failed to load system prompt: %v", err)
		return err
	}
	utils.LogPrintf("[AI] System prompt loaded from %s", aiPromptFile)
	return nil
}

// ====================  请求/响应结构 ====================

// aiMessage AI 消息结构
// 用于与 AI API 通信的消息格式
type aiMessage struct {
	Role    string `json:"role"`    // 角色：system/user/assistant
	Content string `json:"content"` // 消息内容
}

// aiChatRequest 客户端聊天请求
// 前端发送的聊天请求格式
type aiChatRequest struct {
	Messages []aiMessage `json:"messages"` // 对话历史
}

// aiAPIRequest AI API 请求结构
// 发送给 AI 服务商的请求格式
type aiAPIRequest struct {
	Model    string      `json:"model"`    // 模型名称
	Messages []aiMessage `json:"messages"` // 消息列表（含系统提示词）
}

// aiAPIChoice AI API 响应选项
// AI 服务商返回的单个回复选项
type aiAPIChoice struct {
	Message      aiMessage `json:"message"`       // 回复消息
	FinishReason string    `json:"finish_reason"` // 结束原因
}

// aiAPIResponse AI API 响应结构
// AI 服务商返回的完整响应
type aiAPIResponse struct {
	Choices []aiAPIChoice `json:"choices"` // 回复选项列表
	Error   *struct {
		Message string `json:"message"` // 错误信息
	} `json:"error,omitempty"` // 错误信息（可选）
}

// ====================  Handler 函数 ====================

// HandleAIChat 处理 AI 聊天请求
//
// 请求方法：POST
// 请求路径：/api/ai/chat
//
// 请求体：
//
//	{
//	  "messages": [
//	    {"role": "user", "content": "我的数据会被泄露吗？"}
//	  ]
//	}
//
// 成功响应 (200)：
//
//	{
//	  "success": true,
//	  "content": "根据我们的隐私政策..."
//	}
//
// 错误响应 (400/500/503)：
//
//	{
//	  "success": false,
//	  "errorCode": "INVALID_REQUEST"
//	}
func HandleAIChat(c *gin.Context) {
	// 1. 解析请求
	var req aiChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[AI] Invalid request format: %v", err)
		utils.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	// 2. 验证消息不为空
	if len(req.Messages) == 0 {
		utils.LogPrintf("[AI] Empty messages in request")
		utils.RespondError(c, http.StatusBadRequest, "EMPTY_MESSAGES")
		return
	}

	// 3. 检查 AI 配置
	cfg := config.Get()
	if cfg.AIAPIKey == "" || cfg.AIBaseURL == "" {
		utils.LogPrintf("[AI] AI service not configured")
		utils.RespondError(c, http.StatusServiceUnavailable, "AI_NOT_CONFIGURED")
		return
	}

	// 4. 检查系统提示词是否已加载
	if systemPrompt == "" {
		utils.LogPrintf("[AI] System prompt not loaded")
		utils.RespondError(c, http.StatusServiceUnavailable, "AI_NOT_READY")
		return
	}

	// 5. 构建 API 请求（注入系统提示词）
	messages := []aiMessage{{Role: "system", Content: systemPrompt}}
	messages = append(messages, req.Messages...)

	apiReq := aiAPIRequest{
		Model:    cfg.AIModel,
		Messages: messages,
	}

	jsonData, err := json.Marshal(apiReq)
	if err != nil {
		utils.LogPrintf("[AI] Failed to marshal request: %v", err)
		utils.RespondError(c, http.StatusInternalServerError, "REQUEST_BUILD_FAILED")
		return
	}

	// 6. 调用 AI API
	client := &http.Client{Timeout: aiRequestTimeout}
	httpReq, err := http.NewRequest("POST", cfg.AIBaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		utils.LogPrintf("[AI] Failed to create HTTP request: %v", err)
		utils.RespondError(c, http.StatusInternalServerError, "REQUEST_CREATE_FAILED")
		return
	}

	httpReq.Header.Set("Authorization", "Bearer "+cfg.AIAPIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		utils.LogPrintf("[AI] API request failed: %v", err)
		utils.RespondError(c, http.StatusServiceUnavailable, "AI_SERVICE_UNAVAILABLE")
		return
	}
	defer resp.Body.Close()

	// 7. 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.LogPrintf("[AI] Failed to read response: %v", err)
		utils.RespondError(c, http.StatusInternalServerError, "RESPONSE_READ_FAILED")
		return
	}

	// 8. 解析响应
	var apiResp aiAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		utils.LogPrintf("[AI] Failed to parse response: %v", err)
		utils.RespondError(c, http.StatusInternalServerError, "RESPONSE_PARSE_FAILED")
		return
	}

	// 9. 检查 API 错误
	if apiResp.Error != nil {
		utils.LogPrintf("[AI] API returned error: %s", apiResp.Error.Message)
		utils.RespondError(c, http.StatusServiceUnavailable, "AI_SERVICE_ERROR")
		return
	}

	// 10. 检查响应内容
	if len(apiResp.Choices) == 0 {
		utils.LogPrintf("[AI] API returned no choices")
		utils.RespondError(c, http.StatusInternalServerError, "AI_NO_RESPONSE")
		return
	}

	// 11. 返回成功响应
	utils.LogPrintf("[AI] Chat completed successfully")
	utils.RespondSuccess(c, gin.H{
		"content": apiResp.Choices[0].Message.Content,
	})
}
