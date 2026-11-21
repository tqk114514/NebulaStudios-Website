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
- `submitReset` 只校验验证码与新密码非空；密码强度与白名单由服务端判定
- 后端接入前，两个提交都不发请求；`backToEmail` 清空验证码与密码
