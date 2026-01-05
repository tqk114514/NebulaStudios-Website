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
 * - internal/utils (日志工具)
 */

package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  常量定义 ====================

const (
	// aiRequestTimeout AI API 请求超时时间
	aiRequestTimeout = 30 * time.Second
)

// ====================  系统提示词 ====================

// systemPrompt AI 助手的系统提示词
// 定义 AI 的角色、职责和行为规范
const systemPrompt = `你是 Nebula Studios 的政策助手，专门帮助用户理解我们的隐私政策、服务条款和 Cookie 政策。

你的职责：
1. 用简洁友好的语言解答用户关于政策的问题
2. 引导用户找到相关的政策章节
3. 如果问题超出政策范围，礼貌地说明并建议联系客服

注意事项：
- 回答要简洁明了，避免过于冗长
- 不要编造政策中没有的内容
- 如果不确定，建议用户查看原文或联系我们
- 保持友好专业的语气

联系邮箱：nebulastudios@163.com`

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

// aiChatResponse 客户端聊天响应
// 返回给前端的响应格式
type aiChatResponse struct {
	Content string `json:"content,omitempty"` // AI 回复内容
	Error   string `json:"error,omitempty"`   // 错误信息
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
//	  "content": "根据我们的隐私政策..."
//	}
//
// 错误响应 (400/500/503)：
//
//	{
//	  "error": "错误信息"
//	}
func HandleAIChat(c *gin.Context) {
	// 1. 解析请求
	var req aiChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[AI] Invalid request format: %v", err)
		c.JSON(http.StatusBadRequest, aiChatResponse{Error: "无效的请求格式"})
		return
	}

	// 2. 验证消息不为空
	if len(req.Messages) == 0 {
		utils.LogPrintf("[AI] Empty messages in request")
		c.JSON(http.StatusBadRequest, aiChatResponse{Error: "消息不能为空"})
		return
	}

	// 3. 检查 AI 配置
	cfg := config.Get()
	if cfg.AIAPIKey == "" || cfg.AIBaseURL == "" {
		utils.LogPrintf("[AI] AI service not configured")
		c.JSON(http.StatusServiceUnavailable, aiChatResponse{Error: "AI 服务未配置"})
		return
	}

	// 4. 构建 API 请求（注入系统提示词）
	messages := []aiMessage{{Role: "system", Content: systemPrompt}}
	messages = append(messages, req.Messages...)

	apiReq := aiAPIRequest{
		Model:    cfg.AIModel,
		Messages: messages,
	}

	jsonData, err := json.Marshal(apiReq)
	if err != nil {
		utils.LogPrintf("[AI] Failed to marshal request: %v", err)
		c.JSON(http.StatusInternalServerError, aiChatResponse{Error: "请求构建失败"})
		return
	}

	// 5. 调用 AI API
	client := &http.Client{Timeout: aiRequestTimeout}
	httpReq, err := http.NewRequest("POST", cfg.AIBaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		utils.LogPrintf("[AI] Failed to create HTTP request: %v", err)
		c.JSON(http.StatusInternalServerError, aiChatResponse{Error: "请求创建失败"})
		return
	}

	httpReq.Header.Set("Authorization", "Bearer "+cfg.AIAPIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		utils.LogPrintf("[AI] API request failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, aiChatResponse{Error: "AI 服务暂时不可用"})
		return
	}
	defer resp.Body.Close()

	// 6. 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.LogPrintf("[AI] Failed to read response: %v", err)
		c.JSON(http.StatusInternalServerError, aiChatResponse{Error: "响应读取失败"})
		return
	}

	// 7. 解析响应
	var apiResp aiAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		utils.LogPrintf("[AI] Failed to parse response: %v", err)
		c.JSON(http.StatusInternalServerError, aiChatResponse{Error: "响应解析失败"})
		return
	}

	// 8. 检查 API 错误
	if apiResp.Error != nil {
		utils.LogPrintf("[AI] API returned error: %s", apiResp.Error.Message)
		c.JSON(http.StatusServiceUnavailable, aiChatResponse{Error: "AI 服务错误"})
		return
	}

	// 9. 检查响应内容
	if len(apiResp.Choices) == 0 {
		utils.LogPrintf("[AI] API returned no choices")
		c.JSON(http.StatusInternalServerError, aiChatResponse{Error: "AI 未返回有效响应"})
		return
	}

	// 10. 返回成功响应
	utils.LogPrintf("[AI] Chat completed successfully")
	c.JSON(http.StatusOK, aiChatResponse{Content: apiResp.Choices[0].Message.Content})
}
