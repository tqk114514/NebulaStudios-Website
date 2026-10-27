-- 管理员角色由配置提供（AGENTS §0）；这里只持久化封禁状态与审计记录。

-- 每用户至多一条封禁；解封删除该行。temporary 必须有到期时间，permanent 必须没有。
CREATE TABLE user_bans (
    user_id    TEXT PRIMARY KEY NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('temporary', 'permanent')),
    expires_at TEXT,
    reason     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    CHECK (
        (kind = 'permanent' AND expires_at IS NULL)
        OR (kind = 'temporary' AND expires_at IS NOT NULL)
    )
);

-- 审计日志不引用 users：用户删除后记录仍需保留；actor/target 存 UUID 文本。
CREATE TABLE audit_logs (
    id         TEXT PRIMARY KEY NOT NULL,
    actor_id   TEXT NOT NULL,
    action     TEXT NOT NULL,
    target_id  TEXT,
    details    TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at);
CREATE INDEX idx_audit_logs_actor ON audit_logs (actor_id);
CREATE INDEX idx_audit_logs_action ON audit_logs (action);
