# useSignUp

注册两步：建号发码 → 邮箱验证码确认，已接入 `/api/auth/register`、`/verify`、`/resend`。

```ts
const {
  email,
  username,
  password,
  code,
  step,
  pending,
  verified,
  notice,
  formErrorText,
  emailError,
  usernameError,
  passwordError,
  codeError,
  submit,
  verifyCode,
  resend,
  backToRegister,
} = useSignUp()
```

- `step`：`'register' | 'verify'`；`submit` 成功进入验证码步骤，`verifyCode` 成功后 `verified` 为真
- `submit(token?)` / `resend(token?)`：Turnstile 启用时必须传当前 token；token 单次使用，调用方随后 `reset()`
- 一次只报一个本地 `SignUpIssue`；服务端字段错误按 `details.fields[]` 落到同一字段
- 用户名与口令规则与后端一致：用户名 3–15 个 ASCII 字母/数字/`_`/`-`，口令 16–64 位且含大小写、数字与标点
- 重发对未注册邮箱与冷却中同样返回 204，不能据此判断账户是否存在
