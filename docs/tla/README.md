# TLA+：refresh 家族与授权码模型

本目录是设计层模型，不是 Rust/SQLite 实现的精化证明。它检查协议动作在所有可达状态下是否保持各自的不变量。

## refresh token 家族：`RefreshFamily.tla`

检查以下性质：

- 同一家族最多一个 `active` token；
- 已撤销家族不得留下 `active` token；
- 撤销是吸收态（`everRevoked` 中的家族永远保持 revoked）；
- 已轮转 token 不再回到 `active`；
- 触发过重放的家族必须已撤销。

动作与实现的对应关系：

| TLA+ 动作 | 实现 |
|---|---|
| `CreateFamily` | 登录/授权码兑换创建 refresh 家族 |
| `Rotate` | 正常 refresh 轮转 |
| `Replay` | 呈递已轮转 token，整族撤销 |
| `Revoke` | 登出、改密、重置、封禁等撤销路径 |
| `Expire` | token 自然过期 |

## 授权码：`OAuthCode.tla`

检查授权码从 `Fulfilled` 到 `Exchanged` 的状态转移。错误凭证与过期只产生拒绝记录，不消费码；成功兑换是原子动作，与存储层 `consume_code` 的 `consumed_at IS NULL` 条件更新对应。

动作与实现的对应关系：

| TLA+ 动作 | 实现 |
|---|---|
| `Issue` | `POST /api/oauth/authorize/consent` 校验通过后 `create_code`，码为 `Fulfilled` |
| `ExchangeSuccess` | `authorization_code` 兑换：归属/时效/redirect/PKCE 均正确，`consume_code` 成功 |
| `ExchangeWrongClient` | `record.client_id != client.id`，`invalid_grant` |
| `ExchangeWrongRedirect` | `record.redirect_uri != req.redirect_uri`，`invalid_grant` |
| `ExchangeWrongPkce` | `verify_pkce_s256` 失败，`invalid_grant` |
| `ExchangeExpired` | `record.expires_at <= now`，`invalid_grant` |
| `Replay` | 码已消费，`consume_code` 返回 `false`，`invalid_grant` |
| `Expire` | 抽象时间推进越过 `expires_at`；实现中在下一次兑换时观察到 |

不变量：

- `TypeOK` / `WellFormed`：类型、绑定属性与未签发状态的形状；
- `FulfilledNeverUnissued`：签发只前进，码不会回到未签发；
- `StageTerminal`：进入 `Exchanged` 后永远保持 `Exchanged`；
- `AtMostOneSuccess`：同一码最多一次成功兑换；
- `SuccessImpliesExchanged`：成功计数为 1 蕴含 `Exchanged`；
- `ExpiredNoSuccess`：过期码不能成功，且停在 `Fulfilled`；
- `WrongAttemptsDoNotConsume`：错误 client/redirect/PKCE 只留下拒绝记录，码仍为 `Fulfilled` 且成功计数为 0；
- `ExpiredRejectionOnFulfilled`：过期拒绝只发生在 `Fulfilled` 码上；
- `ReplayOnlyOnExchanged`：重放拒绝只命中已成功兑换的码。

`successes` 的类型上界取 `MaxSuccesses = 2`，使 `AtMostOneSuccess` 成为可被 TLC 证伪的检查，而不是类型域蕴含的恒真式。

## 运行

需要 Java 11+：

```bash
bash scripts/check-tla.sh
```

脚本会下载 `tla2tools 1.7.4` 到用户缓存目录；可用 `TLA2TOOLS_JAR` 指定已有 JAR。两个配置依次运行，各自使用独立的状态缓存目录。

当前结果（TLC 2.19，`tla2tools 1.7.4`）：

| 模型 | 配置边界 | 生成状态 | 不同状态 | 深度 | 结果 |
|---|---|---:|---:|---:|---|
| `RefreshFamily` | `MaxFamilies = 3`、`MaxTokens = 5` | 243,676 | 61,426 | 9 | 无不变量违反 |
| `OAuthCode` | `MaxCodes = 3`、`MaxClients = 2`、`MaxRedirects = 2`、`MaxChallenges = 2`、`MaxSuccesses = 2` | 5,322,913 | 704,969 | 10 | 无不变量违反 |

`OAuthCode` 的 `-coverage 1` 运行显示全部 8 个动作（`Issue`、`ExchangeSuccess`、三个错误凭证动作、`ExchangeExpired`、`Replay`、`Expire`）均有实际执行。

## 边界

TLA+ 模型抽象掉了 SQL 事务、实际时间、HTTP 与并发调度；模型中的 `expired` / `revoked` 状态也是对 SQL 的 `expires_at` 与家族状态的抽象，不是逐列映射。

`OAuthCode` 还额外做了三项抽象：

1. `ExchangeSuccess` 把「校验 + 原子消费」建模为单步动作。实现中校验与 `consume_code` 之间虽跨 `await`，但消费本身由 `consumed_at IS NULL` 条件更新裁决，两个并发请求只有一个成功；模型不复现该竞态的交错。
2. `Expire` 只推进尚处于 `Fulfilled` 的码。已兑换码是终态，其 `expires_at` 越过与否不再影响任何行为，故不建模。
3. `lastRejection` 只是观察变量，不参与任何动作的使能条件；它让「拒绝不消费」成为可检查的状态不变量。

模型证明的是状态转移设计，而不是 Rust 实现。实现层仍需 `storage` 集成测试与 property-based 测试；只有把实现与模型对照，才能谈精化。

## 已知观察

TLC 未发现不变量违反。阅读实现时记录一处模型范围之外的观察：授权码重放只返回 `invalid_grant`，不撤销此前由该码签发的 refresh 家族；[RFC 6749 §4.1.2](https://www.rfc-editor.org/rfc/rfc6749#section-4.1.2) 对此使用 SHOULD 级建议。`OAuthCode.tla` 只覆盖授权码自身状态，`RefreshFamily.tla` 的家族撤销没有被连接到授权码重放上。本次不改 Rust 实现，故该行为按现状建模为「拒绝且不改变码状态」。
