# useTurnstile

读取 Turnstile 站点配置，并持有一次性的验证 token。

```ts
const { enabled, siteKey, token, version, reset } = useTurnstile()
```

- 页面挂载后请求 `/api/auth/turnstile`；未启用时不渲染组件、`token` 保持空串
- `BaseTurnstile` 以 `:key="version"` 重新渲染，`v-model="token"` 接收验证结果
- token 单次有效：每次提交后调用 `reset()`，让下一次提交拿到新 token
- 后端启用校验但 token 缺失时返回 422 `turnstile_invalid`，服务不可用返回 503
