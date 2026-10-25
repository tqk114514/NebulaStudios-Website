# Nebula v4 — 工程约束

条款优先于个人习惯与 AI 默认行为。条款与工具配置冲突时，二者必须改成一致，不得并存。无来源 URL 的条款不生效。

`Reference/` 是 v3 归档目录，不参与构建、测试与代码检索。

---

## 0. 范围闸门

功能清单锁定后，新增功能必须先写 `docs/adr/NNNN-*.md`，说明下表中哪一项判断错了。

| 功能 | 处置 | 理由 |
|---|---|---|
| 邮箱注册 / 域名白名单 | 保留 | 认证内核 |
| 密码登录 + 邮箱验证码 | 保留 | 认证内核；登录标识为邮箱或用户名（ADR-0002） |
| 账户注销 | 保留 | 邮件验证码确认；与登出、改密同走撤销路径（§9.1，ADR-0002） |
| JWT Access + 轮转 Refresh（家族 + 重放检测） | 保留 | 重写价值最高的一块 |
| TOTP + 恢复码 | 保留 | 用 `totp-rs`，不手写 |
| OAuth2 Provider（Auth Code + PKCE） | 保留 | 只能手写，见 §1；已知工作量最大项 |
| Microsoft / Google 社交登录 | 保留（简化） | 保留 Worker 代理；仅已绑定账号可登录，未绑定一律报错引导，不比对邮箱、不自动建号；绑定只在设置页主动发起（ADR-0002） |
| 头像自动同步 | 砍 | 头像仅由用户在信息页手动上传（ADR-0002） |
| 头像上传 | 保留但简化 | 只留「上传 → 缩放 → 转 WebP」；去掉同步、hash、哨兵 |
| 管理后台 | 保留最小集 | 统计 / 用户列表 / 封禁；角色管理、客户端管理、白名单改配置文件 |
| 数据导出导入 | 保留导出，砍导入 | 自助 JSON 下载；一次性下载令牌入库（§9.4），不做合并 / 覆盖（ADR-0002） |
| 政策中心 + 同意记录 + Cookie 同意 | 保留 | 政策文档不进仓库，随部署提供（manifest 描述语言与版本）；同意记录入库（ADR-0002） |
| Turnstile 人机验证 | 保留但受限 | 可配置开关；每处必须有替代路径（§9.12，ADR-0002） |
| 5 语言 i18n | 降为 2 语言 | 文案维护成本不会因工具而降低 |
| 用户日志 + 管理日志 | 合并为一张审计表 | |
| Zig 图片处理 sidecar | 砍 | `image` + `spawn_blocking` 足够 |
| pprof | 砍 | `tracing` + `/healthz` + 指标替代 |

---

## 1. 栈

后端：Rust（edition 2024）、`axum 0.8` + `tower-http`、`tokio 1`（`macros`/`rt-multi-thread`/`signal`/`time`）、`sqlx` + SQLite（bundled C，ADR-0003）、`argon2`、`jsonwebtoken`、`totp-rs`、`oauth2` + `openidconnect` + `reqwest`、`lettre`、`rand 0.10`、`validator`、`config`、`uuid`(v7)、`chrono`、`tracing` + `tracing-subscriber`、`metrics` + `axum-prometheus`、`tower_governor`、`image` + `webp`；测试用 `tempfile`、`insta`、`rstest`、`cargo-llvm-cov`。

前端：Nuxt 4、Node 22+、`@nuxtjs/i18n`、`@nuxt/test-utils`、`@nuxt/eslint`、`knip`、`lychee`、`renovate`；组件库 `@nuxt/ui`，用于替代手写 Modal/Toast。

`Cargo.toml` / `package.json` 里的版本号写死前必须经 `crates.io/api/v1/crates/<name>` 与 GitHub releases 查询确认。

**axum 0.8 的三处破坏性变更**：路径参数写 `/{name}` 不是 `/:name`；`State` layer 最后应用；消费 body 的抽取器必须是最后一个参数。

**API 约束**：`jsonwebtoken` 的 ES256 `EncodingKey::from_ec_pem` 只接受 PKCS#8，SEC1 `BEGIN EC PRIVATE KEY` 需转换；`rand 0.10` 改名 `thread_rng()`→`rng()`、`Standard`→`StandardUniform`；`lettre` 连接池绑定当前 runtime；SQLite 默认不启用外键与 WAL，连接须显式设 `foreign_keys=ON`、`journal_mode=WAL`、`busy_timeout`，且 sqlx 下 `:memory:` 每连接各一份，测试用临时文件库（ADR-0003）。

**硬约束**：Rust 生态没有可用的 OAuth 2.0 授权服务器 crate，Provider 必须手写。计划阶段不得假设存在此类库。

**sqlx 三项强制**：`query!` 编译期校验；CI 用 `SQLX_OFFLINE=true` 且 `.sqlx/` 必须提交；迁移用 `sqlx migrate` 作为独立部署步骤，启动路径不执行 DDL。

---

## 2. 后端结构

单一 crate。目录边界命名取自 MAS，粒度不取自——不照搬其多 crate workspace。

```
Cargo.toml  rustfmt.toml  migrations/  .sqlx/  docs/adr/
src/
  main.rs          装配与监听，无业务逻辑
  config.rs        env → 类型化结构体，启动即校验
  error.rs         thiserror 枚举 + impl IntoResponse
  context.rs       request_id、client ip、时钟
  model/           纯领域，零 I/O：user / password / tokens / grant / totp
  oauth2/          协议类型，与 HTTP 无关：requests / responses / errors / pkce / scope
  storage/mod.rs   trait 定义（repository seam）
  storage/sqlite/  sqlx 实现
  api/             一个端点一个文件（vertical slice）：auth/ user/ admin/ oauth/
  extractors.rs    AuthUser 等 FromRequestParts
  jobs/            过期清理
  telemetry.rs
tests/             HTTP 层集成测试
```

**依赖方向，违反则不合并：**

1. `model/` 与 `oauth2/` 不依赖 axum、sqlx、lettre、文件系统；只依赖 std 与加密/协议 crate。
2. `storage/mod.rs` 拥有 trait，`storage/pg/` 拥有实现；`api/` 只依赖 trait。
3. `api/` 下模块不导入另一个 `api/` 模块的内部项；跨端点复用下沉到 `model/` 或 `storage/`。
4. 不用 `mod.rs`，用 `foo.rs` + `foo/`。`unsafe_code = "forbid"`。

**状态建模**：授权码、refresh token 家族、TOTP 待验证态、封禁态一律建模为状态机，非法转移返回类型化错误；handler 内不用 `if` 分支表达状态。参照 MAS 的 `AuthorizationGrantStage: Pending → Fulfilled → Exchanged` + `exchange()/fulfill()/cancel()`。

**时钟注入**：过期判断一律经 `context.rs` 的时钟抽象；`Utc::now()` 散落会使 §8 的不变量测试无法编写。

**密码哈希升级**：登录时哈希参数落后于当前配置则重算写回，计算放 `spawn_blocking`。参照 MAS 的 `verify_and_upgrade`。

---

## 3. 前端结构

```
app/{pages,layouts,middleware,composables,types}
app/components/base/     通用控件，自动导入
app/components/patterns/ 领域重复件，自动导入
app/components/pages/    页面私有件，关自动导入、一律显式 import
shared/                  app 与 server 共用；仅 shared/utils 与 shared/types 自动导入，其余走 #shared
server/api/              文件式路由，方法后缀 .get.ts / .post.ts
server/middleware/
features/                FSD-lite：一个功能一个目录，对外经 index 暴露
test/{unit,nuxt,e2e}
```

1. `app/pages/` 只包含三类代码：`definePageMeta`、数据获取、组件编排。
2. Composable 一个功能一个目录：`index.ts` + `index.md`（测试位置见 §8）；命名导出；只读状态返回 `shallowReadonly`，表单类 composable 因 `v-model` 需可写 ref；每个公开成员带 JSDoc。
3. 组件内不手写 `fetch('/api/…')`。API client 生成为独立 workspace 包，照 `halo-dev/halo` 的 `ui/packages/api-client`——接口形状一致性由生成器保证。
4. i18n 用 `@nuxtjs/i18n`，locale 按功能域一份，key 命名空间 = 组件/功能名，CI 跑缺项检查。不使用手写 codegen。
5. 自动导入：composables/utils 开启；页面私有组件对该目录设 `components: false`，显式导入。
6. 组件分三层：`base/` 通用控件（按钮、字段、标签、分页），`patterns/` 领域重复件（详情行、带图标的列表项、状态徽标），`pages/` 页面私有件。Modal/Toast 由 `@nuxt/ui` 提供，不在 `base/` 重复实现。同一视觉结构第三次出现时必须提为组件，不再复制。

---

## 4. 命名

**Rust**（C-CASE / C-CONV / C-GETTER / C-ITER / C-WORD-ORDER）：

- 模块 `snake_case`；类型/trait/枚举变体 `UpperCamelCase`；函数/方法/局部/字段 `snake_case`；`const` 与不可变 `static` `SCREAMING_SNAKE_CASE`；类型参数简短大写；生命周期短小写。
- 转换前缀按成本：`as_` 免费且借用→借用；`to_` 昂贵；`into_` 消耗所有权。
- getter 不加 `get_`（`first()`）；setter 用 `set_x`；迭代器 `iter` / `iter_mut` / `into_iter`。
- 错误类型 `VerbObjectError`（`ParseScopeError`）。
- 保留字冲突用 `r#crate` 或 `crate_`，不拼错成 `krate`。
- **C-WORD-ORDER：同一概念在整个 crate 里词序一致。**

**前端**：`useXxx`；组件 SFC 文件名 PascalCase，页面文件跟随路由路径（`app/pages/account/login.vue`）；通用控件 `Base*`；紧耦合子件加父名前缀（`TodoList` → `TodoListItem`）；props camelCase；emit 事件 kebab-case；写完整单词，不用 `Mgr` / `Utils2` / `HomeNew` / `DashboardV3`。

**同一意图只允许一个键名。** v3 里"登录后回跳目标"在后端是 `?return=`、在路由守卫是 `query.redirect`、在登录页读的是 `return`。

**取不出名字说明抽象不清楚**（Ousterhout「Hard to Pick Name」）。此时拆分、重写、或写 ADR，不改名了事。

---

## 5. 注释与文档

**内容范围**：注释说明代码做什么，以及在什么条件下成立——约束、前提、不变量、与直觉相反的行为。不写面向假想读者的导读，不叙述调用链是谁在用、某次改动服务于哪个需求。

- 摘要首行用陈述式（对齐 std：`Constructs a new, empty Vec<T>`）；命令式不是官方规则。
- 每个 `pub` 项有 `///` 摘要；可调用的一律给 doctest；doctest 内不用 `unwrap()`，用 `?`。
- 可能 panic → `# Panics`；返回 `Err` → `# Errors`；`unsafe` → `# Safety` 并列出调用方义务。
- `missing_docs = "warn"`。
- 不变量写在声明处（结构体字段、trait 方法），不写在使用处。
- TODO：`TODO(@user): 上下文，附 issue; 截止日期或触发条件`。

---

## 6. 机器门禁 · Rust

`[workspace.lints]` 见仓库根的 `workspace-lints.toml`（复制进 `Cargo.toml`；成员 crate 写 `[lints] workspace = true`），含 `unsafe_code = "forbid"`、`missing_docs`、clippy `unwrap_used`/`expect_used`/`missing_errors_doc`/`missing_panics_doc`、rustdoc `broken_intra_doc_links = "deny"`。`rustfmt.toml` 用仓库根的 `rustfmt.toml`，只含 stable 选项；`group_imports`/`imports_granularity` 为 nightly-only，启用则 CI 必须改用 `cargo +nightly fmt`。

**错误处理**（C-GOOD-ERR）：库侧 thiserror 派生专用枚举，anyhow 只出现在 `main.rs` 与边界；公开 `Result` 的错误类型必须 `impl Error + Send + Sync + 'static'` 且 `impl Debug`；`()` 不作错误类型；`Display` 小写开头、结尾无标点；不用已废弃的 `description()`。

**CI 必跑**：`cargo fmt --all -- --check`、`cargo clippy -- -D warnings`、`cargo test`、`cargo doc --no-deps`（`RUSTDOCFLAGS` 含 `-D rustdoc::broken_intra_doc_links`）、`cargo deny check`、`cargo machete`、`typos`。

---

## 7. 机器门禁 · 前端

**规范权威只有一个：`@nuxt/eslint`**。没有 preset 可选：TS 侧恒叠加 `flat/recommended`，只有配了 `eslint.config.typescript.tsconfigPath` 才追加 `recommended-type-checked-only`；同处 `strict` 默认为 `true`（会再叠 strict 层），要 recommended-type-checked 的强度须显式 `strict: false`。

不叠 `@antfu/eslint-config`，不装 Prettier。两者与官方 style guide 在四条上直接冲突（多词组件名、`require-prop-types`、`require-default-prop`、`max-attributes-per-line`：antfu 关闭，官方要求），且两边都属权威，只能二选一。

**必须启用的 lint**：`vue/block-order`、`vue/define-macros-order`、`vue/component-name-in-template-casing`、`vue/prop-name-casing`、`vue/custom-event-name-casing`、`vue/require-v-for-key`、`vue/no-use-v-if-with-v-for`、`vue/html-self-closing`、`vue/html-indent`；`@typescript-eslint/`（无 `ts/` 别名）下的 `consistent-type-imports`（`fixStyle: separate-type-imports`）、`no-import-type-side-effects`、`ban-ts-comment`（`ts-expect-error` 带描述）；`app/**` 禁 `as any`（生成代码目录例外）；`vitest/no-focused-tests` 设 `error`。`vue/max-attributes-per-line` 取 `singleline: 3`：官方默认 1 会把短标签全部炸成多行，而 stylistic 未开时该规则的 fixer 只换行不缩进。layouts 与 pages 必须单根节点（`@nuxt/eslint-config` 的 `nuxt/vue/single-root`）。

**组件写法**：props 一律 `withDefaults(defineProps<Props>(), { list: () => [] })`，对象/数组默认值用工厂函数，不用数组形式声明；双向绑定用 `defineModel()`；模板不写复杂表达式（超过一个三元或链式数组方法）则提取具名 `computed`；`v-for` 带 `:key`，同节点不并存 `v-if` 与 `v-for`；无子元素组件自闭合；SFC 顶层顺序 `<script setup lang="ts">` → `<template>` → `<style scoped>`。

**行数上限**（项目自定，非外部标准；v3 的 `DashboardPage.vue` 2186 行、`HomePage.vue` 1465 行）：SFC ≤ 200、`app/composables/**/index.ts` ≤ 80、其余模块 ≤ 120，用 `max-lines` 强制。

**辅助门禁**：`knip`（死导出/死文件即失败）、`stylelint` 的 `declaration-property-value-disallowed-list` 与 `scale-unlimited/declaration-strict-value`（禁字面色值、字体族、`cubic-bezier`，只允许引用令牌；`tokens.css` 作为令牌定义处豁免）、`lychee`、`renovate`、`commitlint`。`nano-staged` 预提交只跑 `eslint --fix` 与 `stylelint --fix`；`vue-tsc` 不支持只查改动文件，类型检查与测试挂在 pre-push，全量门禁在 CI。命令：`nuxt typecheck`。

---

## 8. 测试

布局：`test/unit`（纯 TS）、`test/nuxt`（`mountSuspended` / `renderPage`）、`test/e2e`（Playwright）。Rust 单元测试写在 `src/` 内 `#[cfg(test)] mod tests`；HTTP 层集成测试写在 `tests/`，用 `tower::ServiceExt::oneshot` 直接打 `Router`，不绑端口（照 axum 仓库 `examples/testing`）。

**测试规模 ≠ 测试层次**：small = 单进程、无 I/O、不 sleep；medium = 多进程、仅 localhost；large = 任意机器。不以"金字塔三层"作为覆盖率指标。

1. `storage` 实现在真实 SQLite 上测（每用例临时文件库，ADR-0003）。v3 把 `internal/models`（全部手写 SQL）排除在覆盖率之外，是最大风险敞口。
2. 覆盖率排除必须逐条附理由；新增排除项需 ADR。
3. 协议纯逻辑写不变量测试（property-based）：PKCE 校验、token 轮转与家族撤销、恢复码原子消费、封禁判定。
4. given/when/then，一个用例一个行为；DAMP 优先于 DRY——关心的具体值写进断言，不放共享 setup。
5. 错误响应 JSON 与 JWT claims 用 `insta` 快照（照 MAS 的 `snapshots/`）。
6. fake 优先手写；`mockall` + `#[async_trait]` 可用但通常更啰嗦。
7. e2e 覆盖主流程：注册 → 验证 → 登录 → 会话 → 登出；断言用 `getByRole` / `getByPlaceholder` 与 web-first `expect(...).toHaveText`。
8. 并发代码在竞态检测下测（loom，或多线程 runtime 的并发用例），且进 PR 门禁，不只挂定时任务。

---

## 9. 红线

v3 中确实存在、重写时最容易复刻的缺陷。

1. **登出必须真的撤销会话。** v3 的登出路由没挂鉴权中间件，而 handler 依赖中间件写入 context 的 UID，撤销逻辑在生产路径永不执行；其单测因测试助手手工注入 UID 而假绿。→ 登出、改密、重置、关闭 TOTP 走同一条撤销路径，且路由表与 handler 前置条件之间有测试覆盖。
2. **鉴权与 CSRF 不依赖单一 `SameSite` 属性。** v3 的管理 API 与 `confirm-link` 无 CSRF 中间件，安全性全压在 `SameSite=Lax`。→ 所有 cookie 鉴权的写接口过 CSRF 门禁，含后台。
3. **DDL 不进启动路径。** v3 在启动事务里执行 ALTER：大表锁表，任一破坏性操作未开闸即拒绝启动，rename 不可识别，无 revert，只 introspect `public`。
4. **进程内状态有边界声明。** v3 把 OAuth state、待绑定、TOTP 中转态与锁、验证码失败锁、限流桶、管理员导出的一次性授权码（全局单槽位，两个超管并发互相覆盖）放进程内存。→ 入数据库，或 README 声明单实例并列全状态清单。
5. **头像有尺寸上限。** v3 全链路无 resize：4096×4096 原图转出同尺寸 WebP 并直接分发。
6. **降级他人 2FA 必须通知。** v3 超管重置 TOTP 只写审计日志。→ 任何由第三方发起的认证强度降级都发信。
7. **配置项不得存在而不生效。** v3 的 `JWT_EXPIRES_IN` 被解析却无人读取；`session_tokens.banned` 恒写 false 使一条分支不可达；响应中的有效期与库里实际值不一致。
8. **有 `/healthz` 与基础指标。** v3 两者皆无，限流命中、爆破尝试、重放检测只能 grep 日志。
9. **PKCE 不接受 `plain`。** v3 接受 `plain`（无客户端使用）而文档称强制 PKCE。降级路径存在则降级迟早发生。
10. **`openid` 命名需有依据。** v3 有 `openid` scope 而无 OIDC discovery/JWKS 端点。补齐，或不叫 `openid`。
11. **语言覆盖范围与实际一致。** v3 的 `i18n/sources/admin/` 只有 `zh-CN`，对外称五语言。
12. **人机验证不得是唯一步骤。** WCAG 2.2 SC 3.3.8 禁止把认知功能测试作为认证的必要环节；v3 把 Turnstile 挂在登录、注册发码、改密、注销、开关 TOTP 上且无替代路径。→ 每处验证码都提供替代（邮件链接、TOTP、passkey）。

---

## 10. 前端渲染与安全模型

**SSR 只用于公开页面**（首页、静态政策页）。`/account/*` 与 `/admin` 客户端渲染：`definePageMeta({ ssr: false })` 或 `routeRules`。

依据：

1. Nuxt 修复过两个问题：认证路由上应用 `cache`/`swr`/`isr` 时缓存的 SSR payload 泄漏用户数据；`routeRules` 大小写不敏感可绕过鉴权。这两类攻击面在纯 SPA 中不存在。
2. Nuxt 内联输出 `<script type="importmap">` 与 `__NUXT_DATA` payload，与「nonce + 仅外链脚本」的 CSP 姿态冲突；CSP 支持在 `@nuxt-security` 模块，不在 Nuxt 核心。
3. `useFetch` 打相对 URL 时，SSR 阶段经 `useRequestFetch` 代理客户端 header，包括 cookie；官方文档要求只代理确实需要的 header。盲转 `cookie` 是主要泄漏向量。
4. Nuxt 路由中间件运行在应用的 Vue 部分，不在 Nitro 服务器部分，因此不是服务端鉴权边界，不作为唯一权限检查。
5. Nuxt 文档对重度交互型应用（管理面板）的建议是 `ssr: false`。

**SSR 取数不是浏览器发起的请求**，Double Submit Cookie 对它不成立。前端经 Nuxt server routes 代理到后端时，后端无法区分跨站浏览器请求与 Nuxt 自身代理调用，除非引入显式内部标记。该机制需在 `docs/adr/` 中记录，不依赖默认行为。

**保留 v3 的有效姿态**：HttpOnly + `Secure` + `SameSite` 的 access/refresh 分离 cookie；refresh 只存哈希；检测重放即撤销整个家族；写操作恒定时间比较 CSRF 令牌；CSRF 判定按请求方法，不按是否有 body（v3 曾按后者出 bug）。

---

## 11. 流程纪律

- 一个 commit 一顶帽子：`feat:` 不夹重构，`refactor:` 不加行为。重构 commit 由「改动前后同一批测试全绿」证明可观察行为未变。
- 格式改动与功能改动分属不同 commit。
- **PR 尺寸闸**：目标 100–200 行；>1000 行或改动摊到 >50 个文件则 CI 拒绝，除非附 ADR。200 行集中在一个文件可接受，摊到 50 个文件不行。
- **提交信息**：Conventional Commits；必需类型只有 `feat` / `fix`，破坏性用 `!` 或 `BREAKING CHANGE:` 脚注；CI 跑 commitlint。
- **CHANGELOG**：Keep a Changelog 1.1.0；分组固定为 Added / Changed / Deprecated / Removed / Fixed / Security；最新在上；每个版本留链接。
- **版本**：SemVer 2.0。`0.y.z` 阶段规范允许任意变更，初期不走正式发版流程。
- **ADR**：每个不可逆决策一份 `docs/adr/NNNN-标题.md`，章节 Title / Status / Context / Decision / Consequences。

---

## 12. 只能靠评审的四条

1. **深模块优先**：接口简单、实现复杂。不做只做转发或重命名的浅模块，不扩散信息泄漏。
2. **非法状态不可表示**：能用状态机就不用布尔字段组合。补丁通常是往非法状态上再叠一个分支。
3. **注释只写做什么与在什么条件下成立**（§5）。
4. **不能解释的 diff 不合**，无论是否测过。

---

## 13. 不予合并的写法

- 为绕过某个失败而加的特判分支。
- 为未来可能的用途而建的通用 repository / generic helper 等投机抽象。
- 复制第二套同类实现而不复用第一套。
- 静默丢弃错误。被吞掉的错误必须在注释中写明后果。
- 无来源 URL 的条款进入本文件。

---

## 14. 未定项：采用本文件前需查证

1. `shared/` 的 `srcDir`/`dir` 语义；`components/` 的 `pathPrefix` 与 `global` 前缀规则；`layouts/` 命名规则。
2. `headers: 'forward'` 是否作为 `useAsyncData` 选项存在。
3. Nuxt 官方 auth 指南与 `routeMiddleware` 的服务端语义——§10 的鉴权边界设计依赖它。
4. `routeRules` 中 `cache` 键的现行定义（安全公告提到，`define-route-rules` 页列出的键不含它）。

---

## 15. 不存在的条目：不得引用

- Alice Ryhl / Tokio《Common Rust Layout》
- API Guidelines 的 prepositions 规则
- 「注释首行必须命令式」（不是官方规则，std 用陈述式）
- stable 的 `group_imports` / `imports_granularity`
- `Austern` GitHub 仓库（实为 `element-hq/matrix-authentication-service`）
- `barruchen/rust-auth`（仓库已删除；网传的 `api/domain/{entities,usecases}` 目录树不予采信）
- crates.io 上的 `oauthier` / `pjwt` / `inclane` / `carbon`
- Google 评审的「NO / NO\*」响应类型、「non-American English」拼写规则（实际约定是 `Nit:` 前缀）
- Nygard 的 ADR 原帖（已 404）
- `vuejs/style-guide` 独立仓库（内容在 vuejs.org 站点仓库）
- Nuxt 的 Configuration / Data Fetching / Pooling / SEO / MODULES 最佳实践系列（实际存在：accessibility、devcontainers、hydration、performance、plugins）
- `csr: true` 配置项。`routeRules` 可用键：`ssr`、`isr`、`prerender`、`cors`、`headers`、`proxy`、`redirect`
- 不带版本段的 Nuxt 文档路径（`/docs/...` 404，需用 `/docs/4.x/...`）
- `nuxt-labs` org 的公开仓库（`venia` 已没）；`planka` 的 Vue 身份（它是 React）；`antfu/vite-ssg` 旧路径（迁至 `antfu-collective`）；`feature-sliced.dev` 域名（`.design`）；FSD 的 `processes` 层（已废弃）；VueUse 的 `_fn` reactive-pointer 约定

---

## 16. 维护规则

新条款必须有来源 URL。文件超过 300 行时先删后加：论证进 `docs/adr/`，本文件只留结论。

---

## 17. 来源

**Rust 规范与工具链**：[API Guidelines · Naming](https://rust-lang.github.io/api-guidelines/naming.html) · [Documentation](https://rust-lang.github.io/api-guidelines/documentation.html) · [Interoperability](https://rust-lang.github.io/api-guidelines/interoperability.html) · [Rust Style Guide](https://doc.rust-lang.org/style-guide/) · [rustdoc](https://doc.rust-lang.org/rustdoc/write-documentation/the-doc-attribute.html) · [Cargo Book 布局](https://doc.rust-lang.org/cargo/guide/project-layout.html) · [Rust Book 7.5](https://doc.rust-lang.org/book/ch07-05-separating-modules-into-different-files.html) · [Edition 2024](https://doc.rust-lang.org/edition-guide/rust-2024/index.html) · [rustfmt 选项](https://raw.githubusercontent.com/rust-lang/rustfmt/main/src/config/options.rs) · [thiserror](https://raw.githubusercontent.com/dtolnay/thiserror/master/README.md) / [anyhow](https://raw.githubusercontent.com/dtolnay/anyhow/master/README.md)

**Rust 参考代码库与 crate**：[MAS](https://github.com/element-hq/matrix-authentication-service)（`docs/development/architecture.md`）· [axum examples](https://github.com/tokio-rs/axum/tree/main/examples) · [rustls](https://github.com/rustls/rustls) · [tokio](https://github.com/tokio-rs/tokio) · [jsonwebtoken](https://docs.rs/jsonwebtoken/latest/jsonwebtoken/struct.EncodingKey.html) · [sqlx](https://github.com/launchbadge/sqlx/blob/main/CHANGELOG.md) · [rand](https://github.com/rust-random/rand/blob/master/CHANGELOG.md) · [lettre](https://github.com/lettre/lettre) · [axum-prometheus](https://github.com/Ptrskay3/axum-prometheus) · [cargo-llvm-cov](https://github.com/taiki-e/cargo-llvm-cov) · [12factor](https://12factor.net/)

**前端规范**：[Vue Style Guide](https://vuejs.org/style-guide/) · [SFC script-setup](https://vuejs.org/api/sfc-script-setup.html) · [VueUse CONTRIBUTING](https://vueuse.org/contributing) · [antfu/eslint-config](https://github.com/antfu/eslint-config) · [@nuxt/eslint](https://eslint.nuxt.com/packages/module) · [FSD](https://feature-sliced.design/docs/get-started/overview) · [nuxt/ui](https://github.com/nuxt/ui) · [halo](https://github.com/halo-dev/halo) · [openobserve](https://github.com/openobserve/openobserve) · [nuxt/nuxt 根配置](https://github.com/nuxt/nuxt)

**Nuxt 文档**：[入口](https://nuxt.com/docs/4.x/getting-started/introduction) · [shared](https://nuxt.com/docs/4.x/directory-structure/shared) · [server](https://nuxt.com/docs/4.x/directory-structure/server) · [routing](https://nuxt.com/docs/4.x/getting-started/routing/) · [data fetching](https://nuxt.com/docs/4.x/getting-started/data-fetching) · [rendering](https://nuxt.com/docs/4.x/guide/concepts/rendering) · [testing](https://nuxt.com/docs/4.x/getting-started/testing) · [typecheck](https://nuxt.com/docs/4.x/api/commands/typecheck) · [i18n releases](https://github.com/nuxt-modules/i18n/releases) · [v4.5 安全公告](https://nuxt.com/blog/v4-5-security) · [nuxt/nuxt#33686](https://github.com/nuxt/nuxt/issues/33686)

**架构与流程**：[Clean Architecture](https://blog.cleancoder.com/uncle-bob/2012-08-13/the-clean-architecture.html) · [Hexagonal](https://alistair.cockburn.us/hexagonal-architecture/) · [Vertical Slice](https://www.jimmybogard.com/vertical-slice-architecture/) · [Test Pyramid](https://martinfowler.com/bliki/TestPyramid.html) · [Testing Trophy](https://kentcdodds.com/blog/the-testing-trophy-and-testing-classifications) · [Given/When/Then](https://dannorth.net/introducing-bdd/) · [SWE at Google ch.11](https://abseil.io/resources/swe-book/html/ch11.html) / [ch.12](https://abseil.io/resources/swe-book/html/ch12.html) · [refactoring.com](https://refactoring.com/) · [eng-practices](https://google.github.io/eng-practices/review/reviewer/standard.html) / [looking-for](https://google.github.io/eng-practices/review/reviewer/looking-for.html) / [small-cls](https://google.github.io/eng-practices/review/developer/small-cls.html) · [Google Python Guide](https://google.github.io/styleguide/pyguide.html) · [Playwright best practices](https://playwright.dev/docs/best-practices) · [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) · [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) · [SemVer](https://semver.org/spec/v2.0.0.html) · [ADR 模板](https://github.com/joelparkerhenderson/architecture-decision-record) · [AI 时代的维护要求](https://kubernetes.io/blog/2026-06-26/open-source-maintainership-in-the-age-of-ai/) · Ousterhout《A Philosophy of Software Design》（措辞需对照纸质书）
