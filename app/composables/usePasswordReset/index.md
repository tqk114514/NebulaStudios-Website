# usePasswordReset

忘记密码两步：发码 → 验证码 + 新口令，已接入 `/api/auth/password/reset/request` 与 `/confirm`。

```ts
const {
  email,
  code,
  password,
  step,
  pending,
  completed,
  formError,
  emailError,
  codeError,
  passwordError,
  submitEmail,
  submitReset,
  backToEmail,
} = usePasswordReset()
```

- `submitEmail(token?)`：Turnstile 启用时传当前 token；发码成功（含未注册与冷却中的 204）都进入 `reset`
- `submitReset`：验码并设置新口令；成功后 `completed` 为真，全部会话已被服务端撤销
- 口令规则与注册一致（`app/utils/password.ts`）；服务端字段错误按 `details.fields[]` 落回对应输入
- 发码端点不泄露账户是否存在，界面只提示「如果该邮箱已注册，验证码已发送」
