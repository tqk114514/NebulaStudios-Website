# usePasswordReset

忘记密码的两步表单状态。

```ts
const {
  email,
  code,
  password,
  step,
  emailError,
  codeError,
  passwordError,
  submitEmail,
  submitReset,
  backToEmail,
} = usePasswordReset()
```

- `submitEmail` 只校验邮箱非空，通过后切到 `reset` 步骤
- `submitReset` 校验验证码非空与新口令强度；密码强度与白名单由服务端最终判定
- 口令规则与注册一致：16–64 位且含大小写、数字与标点（`app/utils/password.ts`）
- 后端接入前，两个提交都不发请求；`backToEmail` 清空验证码与密码
- 发码端点对未注册邮箱与冷却中同样返回 204，界面只能提示「如果该邮箱已注册，验证码已发送」，不得据此判断账户是否存在
