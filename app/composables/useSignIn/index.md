# useSignIn

登录两步流程的表单状态：口令步骤与动态码步骤。

```ts
const {
  identifier,
  password,
  code,
  step,
  challengeToken,
  identifierError,
  passwordError,
  codeError,
  submitCredentials,
  submitCode,
  backToCredentials,
  startTotpChallenge,
} = useSignIn()
```

- `identifier`：邮箱或用户名；服务端只对含 `@` 的标识做邮箱归一化（ADR-0015）
- `step`：`'credentials' | 'totp'`
- `challengeToken`：密码登录返回 `401 totp_required` 或社交回调带 `totp_required` 时的一次性挑战令牌；`/api/auth/login/totp` 必须同时提交它与动态码
- `startTotpChallenge`：由登录响应或社交回调调用，不自行切换步骤
- 每个错误落在它所属的字段上，一次只报一个
- 错误只在提交后出现，之后随输入更新
- 校验规则在 `validation.ts`，社交回调解析在 `social.ts`，均为纯函数
- 后端接入前 `submitCode` 只走前端校验，不发请求
