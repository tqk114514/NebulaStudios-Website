-- no-transaction
-- 用户从属表统一 ON DELETE CASCADE（ADR-0007）：注销退化为 DELETE FROM users。
-- SQLite 无法给已有表追加外键动作，故重建子表并复制数据；外键必须在事务外关闭。
PRAGMA foreign_keys = OFF;

BEGIN;

-- refresh 家族：user_id 级联；client_id 仍引用客户端（不随用户删除）
CREATE TABLE refresh_token_families_new (
    id         TEXT PRIMARY KEY NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    state      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    revoked_at TEXT,
    client_id  TEXT REFERENCES oauth_clients (id),
    scope      TEXT NOT NULL DEFAULT ''
);
INSERT INTO refresh_token_families_new (id, user_id, state, created_at, revoked_at, client_id, scope)
SELECT id, user_id, state, created_at, revoked_at, client_id, scope FROM refresh_token_families;
DROP TABLE refresh_token_families;
ALTER TABLE refresh_token_families_new RENAME TO refresh_token_families;
CREATE INDEX idx_refresh_families_user ON refresh_token_families (user_id);

-- refresh token 随家族级联；token 本身不直接引用 users
CREATE TABLE refresh_tokens_new (
    id         TEXT PRIMARY KEY NOT NULL,
    family_id  TEXT NOT NULL REFERENCES refresh_token_families (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    state      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    rotated_at TEXT
);
INSERT INTO refresh_tokens_new (id, family_id, token_hash, state, created_at, expires_at, rotated_at)
SELECT id, family_id, token_hash, state, created_at, expires_at, rotated_at FROM refresh_tokens;
DROP TABLE refresh_tokens;
ALTER TABLE refresh_tokens_new RENAME TO refresh_tokens;
CREATE INDEX idx_refresh_tokens_family ON refresh_tokens (family_id);

-- 邮箱验证码
CREATE TABLE email_verifications_new (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT,
    purpose     TEXT NOT NULL DEFAULT 'email_verify'
);
INSERT INTO email_verifications_new (id, user_id, code_hash, attempts, expires_at, created_at, consumed_at, purpose)
SELECT id, user_id, code_hash, attempts, expires_at, created_at, consumed_at, purpose FROM email_verifications;
DROP TABLE email_verifications;
ALTER TABLE email_verifications_new RENAME TO email_verifications;
CREATE INDEX idx_email_verifications_user ON email_verifications (user_id);

-- TOTP 恢复码
CREATE TABLE totp_recovery_codes_new (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT
);
INSERT INTO totp_recovery_codes_new (id, user_id, code_hash, created_at, consumed_at)
SELECT id, user_id, code_hash, created_at, consumed_at FROM totp_recovery_codes;
DROP TABLE totp_recovery_codes;
ALTER TABLE totp_recovery_codes_new RENAME TO totp_recovery_codes;
CREATE INDEX idx_totp_recovery_user ON totp_recovery_codes (user_id);

-- TOTP 登录挑战
CREATE TABLE totp_login_challenges_new (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT
);
INSERT INTO totp_login_challenges_new (id, user_id, token_hash, attempts, expires_at, created_at, consumed_at)
SELECT id, user_id, token_hash, attempts, expires_at, created_at, consumed_at FROM totp_login_challenges;
DROP TABLE totp_login_challenges;
ALTER TABLE totp_login_challenges_new RENAME TO totp_login_challenges;

-- OAuth 授权码
CREATE TABLE oauth_authorization_codes_new (
    id             TEXT PRIMARY KEY NOT NULL,
    code_hash      TEXT NOT NULL UNIQUE,
    client_id      TEXT NOT NULL REFERENCES oauth_clients (id),
    user_id        TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    redirect_uri   TEXT NOT NULL,
    scope          TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    expires_at     TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    consumed_at    TEXT
);
INSERT INTO oauth_authorization_codes_new (id, code_hash, client_id, user_id, redirect_uri, scope, code_challenge, expires_at, created_at, consumed_at)
SELECT id, code_hash, client_id, user_id, redirect_uri, scope, code_challenge, expires_at, created_at, consumed_at FROM oauth_authorization_codes;
DROP TABLE oauth_authorization_codes;
ALTER TABLE oauth_authorization_codes_new RENAME TO oauth_authorization_codes;

COMMIT;

PRAGMA foreign_keys = ON;
