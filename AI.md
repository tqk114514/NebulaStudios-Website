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

## AI 工具系统

### 概述

AI 在回复中可以使用特定标记语法，前端解析后自动执行对应操作。标记会从显示文本中移除，用户只看到自然语言回复。

### 工具列表

| 工具 | 语法 | 行为 | 用户控制 |
|------|------|------|----------|
| 高亮章节 | `<highlight:section_id,policy>` | 跳转到对应政策页 + 滚动到章节 + 边框闪烁高亮 | 自动执行，无需确认 |
| 页面跳转 | `<goto:url>` | 倒计时 3 秒后跳转 | 可取消 |
| 发送邮件 | `<mail:email>` | 倒计时 3 秒后调起邮箱 | 可取消 |

### 工具详细说明

#### 1. 高亮章节 `<highlight:section_id,policy>`

- **用途**：引导用户查看政策中的特定章节
- **参数**：
  - `section_id` - 章节的 HTML id（如 `storage`、`rights`）
  - `policy` - 政策类型（`privacy`、`terms`、`cookies`）
- **行为**：
  1. 如果不在目标政策页，先跳转（修改 hash）
  2. 等待页面渲染后滚动到目标章节
  3. 滚动完成后，章节边框闪烁高亮 2-3 秒
- **示例**：`<highlight:storage,privacy>`

#### 2. 页面跳转 `<goto:url>`

- **用途**：跳转到站内其他页面
- **参数**：`url` - 完整 URL（必须是本域名，如 `https://example.com/account/login`）
- **行为**：
  1. 显示倒计时提示："即将跳转到 xxx (3秒) [取消]"
  2. 用户可点击取消
  3. 倒计时结束后执行跳转
- **限制**：仅允许本域名 URL，站内相对路径由 `<highlight>` 处理
- **示例**：`<goto:https://example.com/account/dashboard>`

#### 3. 发送邮件 `<mail:email>`

- **用途**：帮助用户快速联系客服
- **参数**：`email` - 邮箱地址
- **行为**：
  1. 显示倒计时提示："即将打开邮箱客户端 (3秒) [取消]"
  2. 用户可点击取消
  3. 倒计时结束后调用 `mailto:` 链接
- **示例**：`<mail:nebulastudios@163.com>`

### 多工具处理

当 AI 回复中包含多个工具标记时，按顺序依次执行：
- `<highlight>` 立即执行
- `<goto>` 和 `<mail>` 显示倒计时，等待用户确认或超时

### 前端解析示例

AI 原始回复：
```
您可以查看隐私政策的「数据安全」章节了解详情。<highlight:storage,privacy>
如有其他疑问，欢迎联系我们。<mail:nebulastudios@163.com>
```

用户看到的文本：
```
您可以查看隐私政策的「数据安全」章节了解详情。
如有其他疑问，欢迎联系我们。
```

执行的操作：
1. 如果不在隐私政策页，先跳转到 `#privacy`
2. 页面滚动到 `#storage`，滚动完成后边框闪烁
3. 显示倒计时提示框："即将打开邮箱客户端 (3秒) [取消]"

## 系统提示词

提示词文件位置：`data/ai-prompt.txt`

服务启动时加载，修改后需重启服务生效。

## 文件结构

```
data/
└── ai-prompt.txt    # AI 系统提示词

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
