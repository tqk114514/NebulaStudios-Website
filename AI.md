# AI 服务配置文档

## API 提供商

智谱 AI (ZhipuAI / BigModel)

## 基本信息

| 项目 | 值 |
|------|-----|
| 模型 | glm-4-flash |
| API URL | https://open.bigmodel.cn/api/paas/v4/chat/completions |
| 认证方式 | Bearer Token |

## 环境变量配置

```env
AI_API_KEY=your_api_key_here
AI_BASE_URL=https://open.bigmodel.cn/api/paas/v4/chat/completions
AI_MODEL=glm-4-flash
```

## 请求格式

```json
{
  "model": "glm-4-flash",
  "messages": [
    {"role": "system", "content": "系统提示词"},
    {"role": "user", "content": "用户消息"}
  ]
}
```

## 响应格式

```json
{
  "choices": [
    {
      "finish_reason": "stop",
      "index": 0,
      "message": {
        "content": "AI 回复内容",
        "role": "assistant"
      }
    }
  ],
  "created": 1767610577,
  "id": "请求ID",
  "model": "glm-4-flash",
  "object": "chat.completion",
  "request_id": "请求ID",
  "usage": {
    "completion_tokens": 82,
    "prompt_tokens": 17,
    "total_tokens": 99
  }
}
```

## 后端 API

### POST /api/ai/chat

请求体：
```json
{
  "messages": [
    {"role": "user", "content": "我的数据会被泄露吗？"}
  ]
}
```

响应：
```json
{
  "content": "根据我们的隐私政策..."
}
```

错误响应：
```json
{
  "error": "错误信息"
}
```

## AI 可用工具

> 待实现

### 工具格式设计

AI 在回复中可以使用以下标记，前端解析后执行对应操作：

| 工具 | 格式 | 说明 |
|------|------|------|
| 高亮章节 | `<highlight:policy_type:section_id>` | 高亮指定政策的章节 |
| 页面跳转 | `<goto:relative_url>` | 跳转到站内页面 |
| 发送邮件 | `<mail:email_address>` | 调起邮件客户端 |
| 滚动定位 | `<scroll:section_id>` | 滚动到指定章节 |

### 示例

```
根据我们的隐私政策，您的数据不会被出售给第三方。<highlight:privacy:sharing>

如有疑问，请联系我们。<mail:nebulastudios@163.com>
```

## 文件结构

```
shared/i18n/policy/
├── policy.json      # 政策内容（仅简体中文）
├── zh-CN.json       # AI 聊天界面 i18n
├── zh-TW.json
├── en.json
├── ja.json
└── ko.json

modules/policy/assets/
├── css/
│   ├── policy.css
│   └── ai-chat.css  # AI 聊天组件样式
└── js/
    ├── policy.ts
    └── ai-chat.ts   # AI 聊天组件逻辑

internal/handlers/
└── ai.go            # AI 聊天 API 处理
```
