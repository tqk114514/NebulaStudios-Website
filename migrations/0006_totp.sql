-- TOTP 两步验证（§2 状态机：disabled → pending → enabled；pending 密钥入库，红线 #4）
ALTER TABLE users ADD COLUMN totp_state TEXT NOT NULL DEFAULT 'disabled';
ALTER TABLE users ADD COLUMN totp_secret TEXT;

-- 一次性恢复码；只存 SHA-256，明文仅在启用响应中出现一次
CREATE TABLE totp_recovery_codes (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id),
    code_hash   TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT
);
CREATE INDEX idx_totp_recovery_user ON totp_recovery_codes (user_id);

-- 登录二段挑战：密码通过后签发，单次消费、5 分钟有效、限次失败作废
CREATE TABLE totp_login_challenges (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id),
    token_hash  TEXT NOT NULL UNIQUE,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT
);
