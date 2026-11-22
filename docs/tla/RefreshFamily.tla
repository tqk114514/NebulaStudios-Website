---- MODULE RefreshFamily ----
EXTENDS Naturals, FiniteSets

CONSTANTS MaxFamilies, MaxTokens
ASSUME MaxFamilies \in Nat /\ MaxFamilies >= 1
ASSUME MaxTokens \in Nat /\ MaxTokens >= 1

\* 家族与 token 的标识域固定为 1..Max；families/tokens 是其中“已创建”的子集。
\* 函数覆盖整个标识域，Create/Rotate 只改已有的键，避免空函数上做 EXCEPT。
VARIABLES
    families,
    tokens,
    familyState,
    tokenFamily,
    tokenState,
    everRevoked,
    everRotated,
    replayedFamilies

vars == <<families, tokens, familyState, tokenFamily, tokenState,
          everRevoked, everRotated, replayedFamilies>>

TypeOK ==
    /\ families \subseteq 1..MaxFamilies
    /\ tokens \subseteq 1..MaxTokens
    /\ familyState \in [1..MaxFamilies -> {"inactive", "active", "revoked"}]
    /\ tokenFamily \in [1..MaxTokens -> 0..MaxFamilies]
    /\ tokenState \in [1..MaxTokens -> {"unused", "active", "rotated", "revoked", "expired"}]
    /\ everRevoked \subseteq families
    /\ everRotated \subseteq tokens
    /\ replayedFamilies \subseteq families

Init ==
    /\ families = {}
    /\ tokens = {}
    /\ familyState = [f \in 1..MaxFamilies |-> "inactive"]
    /\ tokenFamily = [t \in 1..MaxTokens |-> 0]
    /\ tokenState = [t \in 1..MaxTokens |-> "unused"]
    /\ everRevoked = {}
    /\ everRotated = {}
    /\ replayedFamilies = {}

\* 新登录/新授权：创建一个家族及其首个 active token。
CreateFamily ==
    /\ Cardinality(families) < MaxFamilies
    /\ \E f \in (1..MaxFamilies) \ families:
        \E t \in (1..MaxTokens) \ tokens:
            /\ families' = families \cup {f}
            /\ tokens' = tokens \cup {t}
            /\ familyState' = [familyState EXCEPT ![f] = "active"]
            /\ tokenFamily' = [tokenFamily EXCEPT ![t] = f]
            /\ tokenState' = [tokenState EXCEPT ![t] = "active"]
            /\ UNCHANGED <<everRevoked, everRotated, replayedFamilies>>

\* 正常轮转：旧 token 变 rotated，同族新增一个 active token。
Rotate ==
    /\ \E f \in families:
        \E t \in tokens:
            /\ familyState[f] = "active"
            /\ tokenFamily[t] = f
            /\ tokenState[t] = "active"
            /\ Cardinality(tokens) < MaxTokens
            /\ \E nt \in (1..MaxTokens) \ tokens:
                /\ tokens' = tokens \cup {nt}
                /\ tokenFamily' = [tokenFamily EXCEPT ![nt] = f]
                /\ tokenState' = [tokenState EXCEPT ![t] = "rotated", ![nt] = "active"]
                /\ familyState' = familyState
                /\ families' = families
                /\ everRevoked' = everRevoked
                /\ everRotated' = everRotated \cup {t}
                /\ replayedFamilies' = replayedFamilies

\* 已轮转 token 再次呈递：整族撤销，所有 token 一并失效。
\* 这是重放检测的核心动作；已撤销家族的重复呈递在实现中只返回 Invalid。
Replay ==
    /\ \E t \in tokens:
        /\ tokenState[t] = "rotated"
        /\ \E f \in families:
            /\ tokenFamily[t] = f
            /\ familyState[f] = "active"
            /\ familyState' = [familyState EXCEPT ![f] = "revoked"]
            /\ tokenState' =
                [u \in 1..MaxTokens |->
                    IF u \in tokens /\ tokenFamily[u] = f THEN "revoked" ELSE tokenState[u]]
            /\ everRevoked' = everRevoked \cup {f}
            /\ replayedFamilies' = replayedFamilies \cup {f}
            /\ UNCHANGED <<families, tokens, tokenFamily, everRotated>>

\* 登出、改密、重置、封禁等撤销路径：整族撤销。
Revoke ==
    /\ \E f \in families:
        /\ familyState[f] = "active"
        /\ familyState' = [familyState EXCEPT ![f] = "revoked"]
        /\ tokenState' =
            [u \in 1..MaxTokens |->
                IF u \in tokens /\ tokenFamily[u] = f THEN "revoked" ELSE tokenState[u]]
        /\ everRevoked' = everRevoked \cup {f}
        /\ UNCHANGED <<families, tokens, tokenFamily, everRotated, replayedFamilies>>

\* token 自然过期：不再是 active，但仍保留在模型中供重放/撤销路径观察。
Expire ==
    /\ \E t \in tokens:
        /\ tokenState[t] = "active"
        /\ tokenState' = [tokenState EXCEPT ![t] = "expired"]
        /\ UNCHANGED <<families, tokens, familyState, tokenFamily,
                      everRevoked, everRotated, replayedFamilies>>

Next == CreateFamily \/ Rotate \/ Replay \/ Revoke \/ Expire

Spec == Init /\ [][Next]_vars

\* 同一家族最多一个可轮转 token。
AtMostOneActivePerFamily ==
    \A f \in families:
        Cardinality({t \in tokens : tokenFamily[t] = f /\ tokenState[t] = "active"}) <= 1

\* 已撤销家族不得留下任何 active token。
NoActiveInRevokedFamily ==
    \A f \in families:
        familyState[f] = "revoked" =>
            \A t \in tokens: tokenFamily[t] = f => tokenState[t] # "active"

\* 撤销是吸收态；everRevoked 记录曾经撤销的家族。
RevokedStaysRevoked ==
    everRevoked \subseteq {f \in families : familyState[f] = "revoked"}

\* 已轮转 token 永不再回到 active；它只能等待撤销或保持 rotated。
RotatedNeverActive ==
    \A t \in everRotated: tokenState[t] # "active"

\* 重放过的家族必须已经撤销。
ReplayedFamilyRevoked ==
    replayedFamilies \subseteq {f \in families : familyState[f] = "revoked"}

====
