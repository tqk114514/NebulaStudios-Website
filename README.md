# Nebula Studios Website

这是 Nebula Studios 网站的前后端源码。

**此README仅介绍后端架构，前端请自行查看源代码。**

主要是我自己用来管理网站用户、处理登录认证以及提供一些后台服务的系统。代码开源主要是为了备份和审查，如果你刚好需要一个基于 Go 的身份认证或简单的后端框架，可以参考看看。

## 项目简介

这是一个基于 **Go (Gin)** 编写的应用，集成了用户认证、OAuth 2.0 (Provider & Client)以及一个用 **Zig** 编写的高性能图片处理微服务。

这不是什么企业级的通用解决方案，很多设计都是为了满足我们的需求。

## 技术栈

* **语言**: Go 1.26.1 (主逻辑), Zig 0.15+ (图片处理), TypeScript (部分前端逻辑)
* **Web 框架**: Gin
* **数据库**: PostgreSQL (使用 `pgx` 驱动)
* **缓存/限流**: 内存 LRU 缓存 (基于 `hashicorp/golang-lru`)
* **图片处理**: Zig 调用 `libwebp` 与 `stb_image` (通过 Unix Domain Socket 通信)

## 功能模块

### 1. 身份认证系统 (Auth System)
位于 `internal/handlers/auth.go`。
* **基础功能**: 注册、登录、密码重置、邮箱验证。
* **安全机制**:
    * 基于分片 LRU 的限流器 (`internal/middleware/ratelimit.go`)，防止暴力破解。
    * 支持封禁系统 (Ban System)。
    * WebSocket 扫码登录。

### 2. OAuth 2.0 集成
位于 `internal/handlers/oauth/` 和 `docs/oauth-integration.md`。
* **作为客户端**: 支持使用 Microsoft 账号登录。
* **作为提供者 (Provider)**: 允许第三方应用通过本站账号登录，支持标准的 `authorization_code` 流程和 `refresh_token`。

### 3. 图片处理服务 (Image Processor)
位于 `img-processor/` 目录。
* 这是一个独立的 **Zig** 程序。
* 通过 Unix Socket (`/tmp/img-processor.sock`) 与 Go 主程序通信。
* 功能很简单：接收图片数据，转码为 WebP 格式返回，主打一个省内存。

## 目录结构

```text
.
├── cmd/
│   ├── server/       # Go 后端入口
│   └── build/        # 前端构建工具
├── img-processor/    # Zig 图片处理服务源码
├── internal/
│   ├── cache/        # 用户数据 LRU 缓存
│   ├── config/       # 应用配置加载
│   ├── middleware/   # Gin 中间件 (限流, CORS, Auth)
│   ├── handlers/     # 业务逻辑控制层
│   ├── models/       # 数据库模型
│   ├── services/     # 业务服务 (Token, Email, Session)
│   └── utils/        # 工具函数
├── modules/          # 前端模块
├── shared/           # 前端共享资源 (i18n, types)
└── docs/             # 相关文档
```

## 快速开始

前置要求

* Go 1.26.1
* Zig 0.15.2+ (如果不需要图片处理可跳过，但这部分功能将不可用)
* PostgreSQL 14+

### 1.编译图片处理服务
进入`img-processor`目录并编译Zig服务：
```Zig
cd img-processor
zig build -Doptimize=ReleaseFast -Dtarget=x86_64-linux-musl
# 编译产物在 zig-out/bin/img-processor
```

### 2.前端编译
```Bash
go run ./cmd/build/
```
**注意**；此编译指令会将所有前端资源处理并输出到`dist/`目录，且是**预压缩`.br`文件**，某些老浏览器可能会无法访问，但这能极大降低流量

### 3.运行后端
回到项目根目录：
```Bash
# 设置环境变量 (参考 internal/config 或代码中的 env 读取)
export DB_DSN="postgres://user:pass@localhost:5432/dbname"
export AI_API_KEY="your_key"
...

# 运行（需要dist/目录）
go run cmd/server/main.go
```

## 注意事项

1.  **环境依赖**: 图片处理服务默认监听 `/tmp/img-processor.sock`，请确保运行环境支持 Unix Socket 且有权限读写该路径。
2.  **安全性**: 项目中虽然包含限流和简单的防护，但作为个人项目，未经过专业的安全审计。在生产环境使用请自行评估风险。
3.  **配置**: 大部分配置通过环境变量加载，具体请查看 `internal/config`。
