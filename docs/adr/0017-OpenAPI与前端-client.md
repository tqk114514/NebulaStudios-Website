# 0017 OpenAPI 与前端 client

## Status

Accepted

## Context

AGENTS §3.3 要求前端通过独立 workspace 包的生成式 API client 访问后端，组件与 composable 不手写
`fetch('/api/…')`。认证接线需要登录、注册、验证码、重置与资料读取，接口形状必须与 Rust DTO 单点一致；
后端此前没有 OpenAPI 文档，无法生成 client。

## Decision

1. 后端用 `utoipa` 在编译期登记 `ApiDoc`：认证与资料端点的 `#[utoipa::path]`、请求/响应
   `ToSchema` 与 `ErrorBody` 统一进文档。
2. `GET /api/openapi.json` 返回该文档；`cargo run --bin openapi` 输出同一份 JSON，供构建期使用。
   `Cargo.toml` 设 `default-run = "nebula-server"`，辅助 bin 不改变默认启动目标。
3. 前端新增 workspace 包 `packages/api-client`：`openapi-typescript` 生成 `src/schema.d.ts`，
   `openapi-fetch` 提供类型化调用。`npm run codegen:api` 运行 `cargo run --bin openapi` 并重新生成，
   生成物（`openapi.json`、`schema.d.ts`）入库。
4. client 固定 `credentials: 'include'`，写请求从 `nebula_csrf` cookie 读取并回显
   `X-CSRF-Token`（ADR-0005）；`baseUrl` 来自 `runtimeConfig.public.apiBase`，默认同源。
5. `paths` 是显式清单：只登记已进入前端范围的端点；新增端点时补注解与清单，不根据 axum 路由反射。

## Consequences

- 端点或 DTO 改动后必须重跑 `npm run codegen:api` 并提交生成物；未登记的端点不会出现在 client 类型中。
- `details` 在生成类型里是 `unknown`，client 用 `asApiErrorBody` 做结构收窄；字段级错误由前端按
  `field` + `code` 映射文案。
- 前后端分离开发需同时设置 `NUXT_PUBLIC_API_BASE` 与 `NEBULA_CORS__ALLOWED_ORIGIN`；同源部署保持
  `apiBase` 为空。
- 辅助 bin 使裸 `cargo run` 出现多个目标，已用 `default-run` 固定；CI 的 `cargo run --release` 行为不变。
