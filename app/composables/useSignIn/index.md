# useSignIn

登录两步流程的表单状态：口令步骤与动态码步骤。

```ts
const { email, password, code, step, emailError, passwordError, codeError, submitCredentials, submitCode, backToCredentials } = useSignIn()
```

- `step`：`'credentials' | 'totp'`
- 每个错误落在它所属的字段上，一次只报一个
- 错误只在提交后出现，之后随输入更新
- 校验规则在 `validation.ts`，纯函数，单测在 `test/unit/auth-form-validation.test.ts`
- 后端接入前 `submitCode` 只走前端校验，不发请求
