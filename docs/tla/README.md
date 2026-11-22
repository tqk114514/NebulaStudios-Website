# TLA+：refresh token 家族模型

本目录是设计层模型，不是 Rust/SQLite 实现的精化证明。它检查协议动作在所有可达状态下是否保持以下不变量：

- 同一家族最多一个 `active` token；
- 已撤销家族不得留下 `active` token；
- 撤销是吸收态（`everRevoked` 中的家族永远保持 revoked）；
- 已轮转 token 不再回到 `active`；
- 触发过重放的家族必须已撤销。

`RefreshFamily.tla` 的动作与实现对应关系：

| TLA+ 动作 | 实现 |
|---|---|
| `CreateFamily` | 登录/授权码兑换创建 refresh 家族 |
| `Rotate` | 正常 refresh 轮转 |
| `Replay` | 呈递已轮转 token，整族撤销 |
| `Revoke` | 登出、改密、重置、封禁等撤销路径 |
| `Expire` | token 自然过期 |

## 运行

需要 Java 11+：

```bash
bash scripts/check-tla.sh
```

脚本会下载 `tla2tools 1.7.4` 到用户缓存目录；可用 `TLA2TOOLS_JAR` 指定已有 JAR。
当前配置为 `MaxFamilies = 3`、`MaxTokens = 5`，TLC 穷举该边界内的全部状态：
61,426 个不同状态，未发现不变量违反。

## 边界

TLA+ 模型抽象掉了 SQL 事务、实际时间、HTTP 与并发调度；模型中的 `expired` / `revoked`
token 状态也是对 SQL 的 `expires_at` 与家族状态的抽象，不是逐列映射。它证明的是状态
转移设计，而不是 Rust 实现。实现层仍需 `storage` 集成测试与 property-based 测试；
只有把实现与模型对照，才能谈精化。
