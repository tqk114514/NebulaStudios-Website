# useApi

返回 `@nebula/api-client` 创建的同源 client 单例。

```ts
const api = useApi()
const { data, error } = await api.POST('/api/auth/login', {
  body: { identifier, password },
})
```

- `baseUrl` 来自 `runtimeConfig.public.apiBase`，默认空（同源）
- 前后端分离开发时可设 `NUXT_PUBLIC_API_BASE=http://127.0.0.1:8080`
- client 内建 `credentials: 'include'` 与写请求的 `X-CSRF-Token` 回显
- 端点与类型由 `npm run codegen:api` 从后端 `ApiDoc` 生成，不手写请求路径
