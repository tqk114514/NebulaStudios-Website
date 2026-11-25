# useSignUp

注册表单的字段状态与本地校验。

```ts
const { email, username, password, formError, usernameError, passwordError, submit } = useSignUp()
```

- 错误只在提交后出现，之后随输入更新
- 一次只报一个 `SignUpIssue`，落在能解决它的那个字段上：`usernameTooShort` / `usernameTooLong` / `usernameInvalidCharacters` → `usernameError`；`passwordTooShort` 等 → `passwordError`；`fieldsRequired` 归不到单个字段，由 `formError` 承担
- 用户名与口令规则与后端一致：用户名 3–15 个 ASCII 字母/数字/`_`/`-`，口令 16–64 位且含大小写、数字与标点
- 用户名是否被占用、邮箱域名是否在白名单内只有服务端能判定，本地不猜
