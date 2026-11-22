---- MODULE OAuthCode ----
EXTENDS Naturals, FiniteSets

\* 授权码设计层模型：Issue 签发 Fulfilled 码，ExchangeSuccess 原子消费并进入
\* Exchanged 终态；任何失败的兑换（错误 client/redirect/PKCE、过期、重放）
\* 只记录拒绝原因，不改变码的可兑换状态。
\*
\* 抽象掉了：HTTP 参数、哈希、SQL 事务与真实时间。`expired` 是“过期时刻已到”
\* 的抽象；Expire 只推进尚未兑换的码，因此已兑换码的过期不再参与行为。
CONSTANTS MaxCodes, MaxClients, MaxRedirects, MaxChallenges, MaxSuccesses
ASSUME MaxCodes \in Nat /\ MaxCodes >= 1
\* 错误 client/redirect/PKCE 动作需要至少一个不同的候选值，故下界为 2。
ASSUME MaxClients \in Nat /\ MaxClients >= 2
ASSUME MaxRedirects \in Nat /\ MaxRedirects >= 2
ASSUME MaxChallenges \in Nat /\ MaxChallenges >= 2
\* 成功计数的类型上界取 2：够 TLC 发现“同一码成功两次”，又不引入无界状态。
ASSUME MaxSuccesses \in Nat /\ MaxSuccesses >= 2

\* stage 的取值域包含“未签发”，使 MaxCodes 同时充当签发次数的上界。
\* bound* 是签发时绑定到码上的请求属性；0 表示未绑定。
VARIABLES
    stage,
    expired,
    boundClient,
    boundRedirect,
    boundChallenge,
    successes,
    lastRejection,
    everFulfilled,
    everExchanged

vars == <<stage, expired, boundClient, boundRedirect, boundChallenge,
          successes, lastRejection, everFulfilled, everExchanged>>

TypeOK ==
    /\ stage \in [1..MaxCodes -> {"unissued", "fulfilled", "exchanged"}]
    /\ expired \in [1..MaxCodes -> BOOLEAN]
    /\ boundClient \in [1..MaxCodes -> 0..MaxClients]
    /\ boundRedirect \in [1..MaxCodes -> 0..MaxRedirects]
    /\ boundChallenge \in [1..MaxCodes -> 0..MaxChallenges]
    /\ successes \in [1..MaxCodes -> 0..MaxSuccesses]
    /\ lastRejection \in
        [1..MaxCodes -> {"none", "client", "redirect", "pkce", "expired", "replay"}]
    /\ everFulfilled \subseteq 1..MaxCodes
    /\ everExchanged \subseteq 1..MaxCodes

\* 未签发的码没有绑定属性、没有过期、没有成功记录，也没有拒绝记录；
\* 已签发的码三项绑定均非 0。
WellFormed ==
    /\ \A c \in 1..MaxCodes:
        stage[c] = "unissued" =>
            /\ boundClient[c] = 0
            /\ boundRedirect[c] = 0
            /\ boundChallenge[c] = 0
            /\ ~expired[c]
            /\ successes[c] = 0
            /\ lastRejection[c] = "none"
    /\ \A c \in 1..MaxCodes:
        stage[c] # "unissued" =>
            /\ boundClient[c] # 0
            /\ boundRedirect[c] # 0
            /\ boundChallenge[c] # 0

Init ==
    /\ stage = [c \in 1..MaxCodes |-> "unissued"]
    /\ expired = [c \in 1..MaxCodes |-> FALSE]
    /\ boundClient = [c \in 1..MaxCodes |-> 0]
    /\ boundRedirect = [c \in 1..MaxCodes |-> 0]
    /\ boundChallenge = [c \in 1..MaxCodes |-> 0]
    /\ successes = [c \in 1..MaxCodes |-> 0]
    /\ lastRejection = [c \in 1..MaxCodes |-> "none"]
    /\ everFulfilled = {}
    /\ everExchanged = {}

\* 用户同意后签发一条 Fulfilled 码，并绑定 client、redirect_uri 与 PKCE challenge。
Issue ==
    /\ \E c \in 1..MaxCodes:
        /\ stage[c] = "unissued"
        /\ \E cl \in 1..MaxClients:
            \E rd \in 1..MaxRedirects:
                \E ch \in 1..MaxChallenges:
                    /\ stage' = [stage EXCEPT ![c] = "fulfilled"]
                    /\ expired' = [expired EXCEPT ![c] = FALSE]
                    /\ boundClient' = [boundClient EXCEPT ![c] = cl]
                    /\ boundRedirect' = [boundRedirect EXCEPT ![c] = rd]
                    /\ boundChallenge' = [boundChallenge EXCEPT ![c] = ch]
                    /\ successes' = [successes EXCEPT ![c] = 0]
                    /\ lastRejection' = [lastRejection EXCEPT ![c] = "none"]
                    /\ everFulfilled' = everFulfilled \cup {c}
                    /\ UNCHANGED everExchanged

\* 正确 client + redirect + PKCE、未过期、未消费：单步原子消费。
\* 存储层的 consume_code 以“consumed_at IS NULL”条件更新，等价于本动作的前置条件。
ExchangeSuccess ==
    /\ \E c \in 1..MaxCodes:
        /\ stage[c] = "fulfilled"
        /\ ~expired[c]
        /\ successes[c] = 0
        /\ stage' = [stage EXCEPT ![c] = "exchanged"]
        /\ successes' = [successes EXCEPT ![c] = successes[c] + 1]
        /\ lastRejection' = [lastRejection EXCEPT ![c] = "none"]
        /\ everExchanged' = everExchanged \cup {c}
        /\ UNCHANGED <<expired, boundClient, boundRedirect, boundChallenge,
                       everFulfilled>>

\* 以下三个动作模拟 token 端点的拒绝路径：码保持 Fulfilled 且未被消费。
ExchangeWrongClient ==
    /\ \E c \in 1..MaxCodes:
        /\ \E cl \in 1..MaxClients:
            /\ stage[c] = "fulfilled"
            /\ ~expired[c]
            /\ cl # boundClient[c]
            /\ lastRejection' = [lastRejection EXCEPT ![c] = "client"]
            /\ UNCHANGED <<stage, expired, boundClient, boundRedirect,
                           boundChallenge, successes, everFulfilled, everExchanged>>

ExchangeWrongRedirect ==
    /\ \E c \in 1..MaxCodes:
        /\ \E rd \in 1..MaxRedirects:
            /\ stage[c] = "fulfilled"
            /\ ~expired[c]
            /\ rd # boundRedirect[c]
            /\ lastRejection' = [lastRejection EXCEPT ![c] = "redirect"]
            /\ UNCHANGED <<stage, expired, boundClient, boundRedirect,
                           boundChallenge, successes, everFulfilled, everExchanged>>

\* 该动作抽象“client 与 redirect 均正确、只有 verifier 不匹配”的请求。
ExchangeWrongPkce ==
    /\ \E c \in 1..MaxCodes:
        /\ \E ch \in 1..MaxChallenges:
            /\ stage[c] = "fulfilled"
            /\ ~expired[c]
            /\ ch # boundChallenge[c]
            /\ lastRejection' = [lastRejection EXCEPT ![c] = "pkce"]
            /\ UNCHANGED <<stage, expired, boundClient, boundRedirect,
                           boundChallenge, successes, everFulfilled, everExchanged>>

\* 过期码兑换：拒绝，且不把码推进到 Exchanged。
ExchangeExpired ==
    /\ \E c \in 1..MaxCodes:
        /\ stage[c] = "fulfilled"
        /\ expired[c]
        /\ lastRejection' = [lastRejection EXCEPT ![c] = "expired"]
        /\ UNCHANGED <<stage, expired, boundClient, boundRedirect, boundChallenge,
                       successes, everFulfilled, everExchanged>>

\* 已消费码再次兑换：拒绝，successes 不增加，stage 保持 Exchanged。
Replay ==
    /\ \E c \in 1..MaxCodes:
        /\ stage[c] = "exchanged"
        /\ lastRejection' = [lastRejection EXCEPT ![c] = "replay"]
        /\ UNCHANGED <<stage, expired, boundClient, boundRedirect, boundChallenge,
                       successes, everFulfilled, everExchanged>>

\* 时间推进使一条仍可兑换的码过期；已兑换码是终态，不再参与过期。
Expire ==
    /\ \E c \in 1..MaxCodes:
        /\ stage[c] = "fulfilled"
        /\ ~expired[c]
        /\ expired' = [expired EXCEPT ![c] = TRUE]
        /\ UNCHANGED <<stage, boundClient, boundRedirect, boundChallenge,
                       successes, lastRejection, everFulfilled, everExchanged>>

Next == Issue \/ ExchangeSuccess
          \/ ExchangeWrongClient \/ ExchangeWrongRedirect \/ ExchangeWrongPkce
          \/ ExchangeExpired \/ Replay \/ Expire

Spec == Init /\ [][Next]_vars

\* 签发只前进：曾经 Fulfilled 的码不会回到未签发。
FulfilledNeverUnissued ==
    everFulfilled \subseteq {c \in 1..MaxCodes : stage[c] # "unissued"}

\* Exchanged 是终态：进入过 Exchanged 的码永远保持 Exchanged。
StageTerminal ==
    everExchanged \subseteq {c \in 1..MaxCodes : stage[c] = "exchanged"}

\* 同一码最多一次成功兑换。
AtMostOneSuccess ==
    \A c \in 1..MaxCodes: successes[c] <= 1

\* 成功兑换与 Exchanged 状态一一对应。
SuccessImpliesExchanged ==
    \A c \in 1..MaxCodes: successes[c] = 1 => stage[c] = "exchanged"

\* 过期码不能成功，且停在 Fulfilled。
ExpiredNoSuccess ==
    \A c \in 1..MaxCodes:
        expired[c] => /\ successes[c] = 0
                     /\ stage[c] = "fulfilled"

\* 错误 client/redirect/PKCE 只留下拒绝记录；码仍可兑换、未被消费。
WrongAttemptsDoNotConsume ==
    \A c \in 1..MaxCodes:
        lastRejection[c] \in {"client", "redirect", "pkce"} =>
            /\ stage[c] = "fulfilled"
            /\ successes[c] = 0

\* 过期拒绝只能发生在 Fulfilled 码上。
ExpiredRejectionOnFulfilled ==
    \A c \in 1..MaxCodes:
        lastRejection[c] = "expired" =>
            /\ stage[c] = "fulfilled"
            /\ successes[c] = 0

\* 重放只能命中已成功兑换的码。
ReplayOnlyOnExchanged ==
    \A c \in 1..MaxCodes:
        lastRejection[c] = "replay" =>
            /\ stage[c] = "exchanged"
            /\ successes[c] = 1

====
