-- refresh token 家族（§2 状态机：家族 active/revoked，token active/rotated/revoked）
-- 家族：登录创建，重放或撤销性事件（登出/改密/关闭 TOTP/注销）整族撤销（红线 #1）
CREATE TABLE refresh_token_families (
    id         TEXT PRIMARY KEY NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id),
    state      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    revoked_at TEXT
);

-- token 明文永不入库，只存 SHA-256 十六进制摘要（§10 保留 v3 姿态）
CREATE TABLE refresh_tokens (
    id         TEXT PRIMARY KEY NOT NULL,
    family_id  TEXT NOT NULL REFERENCES refresh_token_families (id),
    token_hash TEXT NOT NULL UNIQUE,
    state      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    rotated_at TEXT
);

CREATE INDEX idx_refresh_tokens_family ON refresh_tokens (family_id);
CREATE INDEX idx_refresh_families_user ON refresh_token_families (user_id);
