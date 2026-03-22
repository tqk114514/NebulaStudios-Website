# Nebula Account OAuth 2.0 接入指南

本文档介绍如何将第三方应用接入 Nebula Account 进行用户授权登录。

## 概述

Nebula Account 实现了标准的 OAuth 2.0 Authorization Code 授权流程，**强制要求使用 PKCE (Proof Key for Code Exchange)** 增强安全性，支持第三方应用安全地获取用户授权并访问用户信息。

### 支持的授权类型

- `authorization_code` - 授权码模式（强制要求 PKCE）
- `refresh_token` - 刷新令牌

### Token 有效期

| Token 类型 | 有效期 |
|-----------|--------|
| Authorization Code | 10 分钟 |
| Access Token | 1 小时 |
| Refresh Token | 30 天 |

---

## PKCE 说明

**PKCE (Proof Key for Code Exchange)** 是 OAuth 2.0 的安全扩展，用于防止授权码拦截攻击。Nebula Account **强制要求所有客户端使用 PKCE**。

### PKCE 流程

1. 客户端生成随机字符串 `code_verifier`（43-128 字符）
2. 使用 `code_verifier` 生成 `code_challenge`：
   - 方法 `S256`（推荐）：`Base64URL(SHA256(code_verifier))`
   - 方法 `plain`（不推荐，仅用于测试）：直接使用 `code_verifier`
3. 在授权请求时传递 `code_challenge` 和 `code_challenge_method`
4. 在 Token 交换时传递 `code_verifier`
5. 服务器验证 `code_verifier` 与 `code_challenge` 是否匹配

### 代码示例

```javascript
// 生成 code_verifier
function generateCodeVerifier() {
  const array = new Uint8Array(32);
  crypto.getRandomValues(array);
  return base64urlEncode(array);
}

// 生成 code_challenge (S256)
async function generateCodeChallenge(codeVerifier) {
  const encoder = new TextEncoder();
  const data = encoder.encode(codeVerifier);
  const digest = await crypto.subtle.digest('SHA-256', data);
  return base64urlEncode(new Uint8Array(digest));
}

// Base64URL 编码
function base64urlEncode(buffer) {
  return btoa(String.fromCharCode(...buffer))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=/g, '');
}
```

---

## 快速开始

### 1. 注册应用

联系管理员在后台创建 OAuth 应用，获取以下凭证：

- `client_id` - 客户端标识（32 字符）
- `client_secret` - 客户端密钥（64 字符，仅创建时显示一次，请妥善保管）

同时需要提供：

- 应用名称
- 应用描述（可选）
- 回调地址（redirect_uri）

> **重要**：回调地址必须精确匹配，不支持通配符。

### 2. 授权流程

1. 用户在第三方应用点击"使用 Nebula 登录"
2. 第三方应用生成 `code_verifier` 和 `code_challenge`
3. 第三方应用将用户重定向到 Nebula 授权页面，附带 `code_challenge`
4. 用户在 Nebula 登录并同意授权
5. Nebula 将用户重定向回第三方应用的回调地址，附带 `code`
6. 第三方应用使用 `code` 和 `code_verifier` 向 Nebula 请求 Token
7. Nebula 验证 PKCE 并返回 `access_token` 和 `refresh_token`
8. 第三方应用使用 `access_token` 获取用户信息
9. 用户登录成功

---

## API 参考

### 授权端点

#### 请求授权

引导用户访问此地址进行授权：

```
GET /oauth/authorize
```

**查询参数：**

| 参数 | 必需 | 说明 |
|-----|------|-----|
| `client_id` | 是 | 客户端 ID |
| `redirect_uri` | 是 | 回调地址，必须与注册时一致 |
| `response_type` | 是 | 固定为 `code` |
| `scope` | 是 | 请求的权限范围，空格分隔 |
| `state` | 推荐 | 随机字符串，用于防止 CSRF 攻击 |
| `code_challenge` | 是 | PKCE code_challenge |
| `code_challenge_method` | 是 | code_challenge 方法，必须为 `S256` 或 `plain` |

**示例：**

```
https://www.nebulastudios.top/oauth/authorize?
  client_id=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6&
  redirect_uri=https://www.123.xyz/callback&
  response_type=code&
  scope=openid%20profile%20email&
  state=xyz123&
  code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&
  code_challenge_method=S256
```

**授权成功响应：**

用户同意授权后，将重定向到回调地址：

```
https://www.123.xyz/callback?code=abc123def456&state=xyz123
```

**授权失败响应：**

```
https://www.123.xyz/callback?error=access_denied&error_description=User%20denied%20authorization&state=xyz123
```

---

### Token 端点

#### 用授权码换取 Token

```
POST /oauth/token
Content-Type: application/x-www-form-urlencoded
```

**请求参数：**

| 参数 | 必需 | 说明 |
|-----|------|-----|
| `grant_type` | 是 | 固定为 `authorization_code` |
| `client_id` | 是 | 客户端 ID |
| `client_secret` | 是 | 客户端密钥 |
| `code` | 是 | 授权码 |
| `redirect_uri` | 是 | 回调地址，必须与授权请求一致 |
| `code_verifier` | 是 | PKCE code_verifier |

**示例请求：**

```bash
curl -X POST https://www.nebulastudios.top/oauth/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=authorization_code" \
  -d "client_id=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6" \
  -d "client_secret=your_client_secret" \
  -d "code=abc123def456" \
  -d "redirect_uri=https://www.123.xyz/callback" \
  -d "code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
```

**成功响应：**

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4...",
  "scope": "openid profile email"
}
```

**错误响应：**

```json
{
  "error": "invalid_grant",
  "error_description": "Invalid authorization code"
}
```

---

#### 刷新 Access Token

```
POST /oauth/token
Content-Type: application/x-www-form-urlencoded
```

**请求参数：**

| 参数 | 必需 | 说明 |
|-----|------|-----|
| `grant_type` | 是 | 固定为 `refresh_token` |
| `client_id` | 是 | 客户端 ID |
| `client_secret` | 是 | 客户端密钥 |
| `refresh_token` | 是 | 刷新令牌 |

**示例请求：**

```bash
curl -X POST https://www.nebulastudios.top/oauth/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=refresh_token" \
  -d "client_id=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6" \
  -d "client_secret=your_client_secret" \
  -d "refresh_token=dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4..."
```

**成功响应：**

```json
{
  "access_token": "new_access_token...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "new_refresh_token...",
  "scope": "openid profile email"
}
```

> 注意：刷新后会返回新的 refresh_token，旧的 refresh_token 将失效。

---

### 用户信息端点

#### 获取用户信息

```
GET /oauth/userinfo
Authorization: Bearer <access_token>
```

**示例请求：**

```bash
curl https://www.nebulastudios.top/oauth/userinfo \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

**成功响应：**

返回的字段取决于授权时请求的 scope：

```json
{
  "sub": "u123abc456def",
  "username": "example_user",
  "avatar_url": "https://www.nebulastudios.top/avatars/u123abc456def.jpg",
  "email": "user@example.com"
}
```

**错误响应：**

```
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer error="invalid_token", error_description="Invalid or expired access token"

{
  "error": "invalid_token",
  "error_description": "Invalid or expired access token"
}
```

---

### Token 撤销端点

#### 撤销 Token

```
POST /oauth/revoke
Content-Type: application/x-www-form-urlencoded
```

**请求参数：**

| 参数 | 必需 | 说明 |
|-----|------|-----|
| `token` | 是 | 要撤销的 token（access_token 或 refresh_token） |
| `token_type_hint` | 否 | token 类型提示：`access_token` 或 `refresh_token` |

**示例请求：**

```bash
curl -X POST https://www.nebulastudios.top/oauth/revoke \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

**响应：**

无论 token 是否有效，始终返回 `200 OK`（符合 RFC 7009 规范）。

---

## Scope 说明

| Scope | 说明 | 返回字段 |
|-------|------|---------|
| `openid` | 用户标识 | `sub`（用户 UID） |
| `profile` | 用户基本信息 | `username`、`avatar_url` |
| `email` | 用户邮箱 | `email` |

请求多个 scope 时用空格分隔，例如：`openid profile email`

---

## 错误码

### 授权端点错误

| 错误码 | 说明 |
|-------|------|
| `invalid_request` | 请求参数缺失或无效（如缺少 code_challenge） |
| `invalid_client` | 无效的 client_id |
| `invalid_scope` | 无效的 scope |
| `unsupported_response_type` | 不支持的 response_type（仅支持 code） |
| `access_denied` | 用户拒绝授权或用户被封禁 |
| `server_error` | 服务器内部错误 |

### Token 端点错误

| 错误码 | 说明 |
|-------|------|
| `invalid_request` | 请求参数缺失或无效（如缺少 code_verifier） |
| `invalid_client` | 客户端认证失败（client_id 或 client_secret 错误） |
| `invalid_grant` | 授权码无效、已过期、已使用，或 redirect_uri 不匹配，或 PKCE 验证失败 |
| `unsupported_grant_type` | 不支持的 grant_type |

### UserInfo 端点错误

| 错误码 | 说明 |
|-------|------|
| `invalid_token` | access_token 无效或已过期 |
| `access_denied` | 用户被封禁 |
| `server_error` | 服务器内部错误 |

---

## 代码示例

### Node.js (Express)

```javascript
const express = require('express');
const axios = require('axios');
const crypto = require('crypto');

const app = express();

const CLIENT_ID = 'your_client_id';
const CLIENT_SECRET = 'your_client_secret';
const REDIRECT_URI = 'https://www.123.xyz/callback';
const NEBULA_BASE_URL = 'https://www.nebulastudios.top';

// 生成 code_verifier
function generateCodeVerifier() {
  return crypto.randomBytes(32).toString('base64url');
}

// 生成 code_challenge (S256)
function generateCodeChallenge(codeVerifier) {
  return crypto
    .createHash('sha256')
    .update(codeVerifier)
    .digest('base64url');
}

// 发起授权
app.get('/login', (req, res) => {
  const state = crypto.randomBytes(16).toString('hex');
  const codeVerifier = generateCodeVerifier();
  const codeChallenge = generateCodeChallenge(codeVerifier);
  
  req.session.oauthState = state;
  req.session.codeVerifier = codeVerifier;
  
  const authUrl = new URL(`${NEBULA_BASE_URL}/oauth/authorize`);
  authUrl.searchParams.set('client_id', CLIENT_ID);
  authUrl.searchParams.set('redirect_uri', REDIRECT_URI);
  authUrl.searchParams.set('response_type', 'code');
  authUrl.searchParams.set('scope', 'openid profile email');
  authUrl.searchParams.set('state', state);
  authUrl.searchParams.set('code_challenge', codeChallenge);
  authUrl.searchParams.set('code_challenge_method', 'S256');
  
  res.redirect(authUrl.toString());
});

// 处理回调
app.get('/callback', async (req, res) => {
  const { code, state, error } = req.query;
  
  // 检查错误
  if (error) {
    return res.status(400).send(`授权失败: ${error}`);
  }
  
  // 验证 state
  if (state !== req.session.oauthState) {
    return res.status(400).send('State 验证失败');
  }
  
  try {
    // 换取 token
    const tokenRes = await axios.post(`${NEBULA_BASE_URL}/oauth/token`, 
      new URLSearchParams({
        grant_type: 'authorization_code',
        client_id: CLIENT_ID,
        client_secret: CLIENT_SECRET,
        code,
        redirect_uri: REDIRECT_URI,
        code_verifier: req.session.codeVerifier
      }),
      { headers: { 'Content-Type': 'application/x-www-form-urlencoded' } }
    );
    
    const { access_token, refresh_token } = tokenRes.data;
    
    // 获取用户信息
    const userRes = await axios.get(`${NEBULA_BASE_URL}/oauth/userinfo`, {
      headers: { Authorization: `Bearer ${access_token}` }
    });
    
    const user = userRes.data;
    
    // 登录成功，创建会话
    req.session.user = user;
    req.session.accessToken = access_token;
    req.session.refreshToken = refresh_token;
    
    res.redirect('/dashboard');
  } catch (err) {
    console.error('OAuth error:', err.response?.data || err.message);
    res.status(500).send('登录失败');
  }
});
```

### Python (Flask)

```python
import secrets
import hashlib
import base64
import requests
from flask import Flask, redirect, request, session, url_for

app = Flask(__name__)
app.secret_key = 'your-secret-key'

CLIENT_ID = 'your_client_id'
CLIENT_SECRET = 'your_client_secret'
REDIRECT_URI = 'https://www.123.xyz/callback'
NEBULA_BASE_URL = 'https://www.nebulastudios.top'

# 生成 code_verifier
def generate_code_verifier():
    return secrets.token_urlsafe(32)

# 生成 code_challenge (S256)
def generate_code_challenge(code_verifier):
    sha256 = hashlib.sha256(code_verifier.encode('ascii')).digest()
    return base64.urlsafe_b64encode(sha256).rstrip(b'=').decode('ascii')

@app.route('/login')
def login():
    state = secrets.token_hex(16)
    code_verifier = generate_code_verifier()
    code_challenge = generate_code_challenge(code_verifier)
    
    session['oauth_state'] = state
    session['code_verifier'] = code_verifier
    
    auth_url = (
        f"{NEBULA_BASE_URL}/oauth/authorize?"
        f"client_id={CLIENT_ID}&"
        f"redirect_uri={REDIRECT_URI}&"
        f"response_type=code&"
        f"scope=openid%20profile%20email&"
        f"state={state}&"
        f"code_challenge={code_challenge}&"
        f"code_challenge_method=S256"
    )
    return redirect(auth_url)

@app.route('/callback')
def callback():
    error = request.args.get('error')
    if error:
        return f"授权失败: {error}", 400
    
    state = request.args.get('state')
    if state != session.get('oauth_state'):
        return "State 验证失败", 400
    
    code = request.args.get('code')
    
    # 换取 token
    token_res = requests.post(f"{NEBULA_BASE_URL}/oauth/token", data={
        'grant_type': 'authorization_code',
        'client_id': CLIENT_ID,
        'client_secret': CLIENT_SECRET,
        'code': code,
        'redirect_uri': REDIRECT_URI,
        'code_verifier': session.get('code_verifier')
    })
    
    if token_res.status_code != 200:
        return "获取 token 失败", 500
    
    tokens = token_res.json()
    access_token = tokens['access_token']
    
    # 获取用户信息
    user_res = requests.get(f"{NEBULA_BASE_URL}/oauth/userinfo", headers={
        'Authorization': f"Bearer {access_token}"
    })
    
    if user_res.status_code != 200:
        return "获取用户信息失败", 500
    
    user = user_res.json()
    session['user'] = user
    session['access_token'] = access_token
    session['refresh_token'] = tokens.get('refresh_token')
    
    return redirect('/dashboard')
```

---

## 安全建议

1. **保护 client_secret**：永远不要在前端代码或公开仓库中暴露 client_secret
2. **使用 state 参数**：始终使用随机生成的 state 参数防止 CSRF 攻击
3. **强制使用 PKCE**：必须使用 PKCE，推荐使用 `S256` 方法
4. **验证 redirect_uri**：确保回调地址使用 HTTPS
5. **安全存储 Token**：
   - 服务端：存储在安全的会话或数据库中
   - 客户端：避免存储在 localStorage，推荐使用 httpOnly cookie
6. **及时刷新 Token**：在 access_token 过期前使用 refresh_token 获取新 token
7. **最小权限原则**：只请求应用实际需要的 scope

---

## 常见问题

### Q: redirect_uri 可以使用通配符吗？

不可以。出于安全考虑，redirect_uri 必须精确匹配注册时填写的地址。

### Q: 授权码可以使用多次吗？

不可以。授权码是一次性的，使用后立即失效。

### Q: 用户被封禁后会怎样？

- 已登录用户的授权请求会被拒绝
- 已颁发的 Token 在调用 userinfo 时会返回 `access_denied` 错误
- 建议应用在收到此错误时清除本地会话

### Q: PKCE 是可选的吗？

不是。Nebula Account **强制要求**所有客户端使用 PKCE，不提供向后兼容。

### Q: code_challenge_method 可以使用 plain 吗？

可以，但强烈建议使用 `S256` 方法以获得更高的安全性。`plain` 方法仅用于测试或兼容性场景。

---

## 联系支持

如有问题，请联系系统管理员。
