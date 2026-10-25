-- 邮箱验证码（只存 SHA-256；限次/时效/重发冷却由应用层执行，见 model/verification.rs）
CREATE TABLE email_verifications (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id),
    code_hash   TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT
);

CREATE INDEX idx_email_verifications_user ON email_verifications (user_id);
