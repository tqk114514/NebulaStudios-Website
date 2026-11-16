//! SQLite schema 初始化。
//!
//! v4 没有真实业务数据，schema 由本模块在启动时幂等创建（ADR-0003 修订）；
//! 不再维护版本化迁移。schema 变更需要删除数据库文件后重新创建。

use sqlx::SqlitePool;

use crate::storage::StorageError;

/// 创建缺失的表、索引与触发器；已存在的对象保持不变。
///
/// # Errors
/// 任一 DDL 语句执行失败时返回存储故障。
pub async fn ensure_schema(pool: &SqlitePool) -> Result<(), StorageError> {
    sqlx::raw_sql(SCHEMA).execute(pool).await?;
    Ok(())
}

/// 当前 schema 的完整定义；所有语句均为 `IF NOT EXISTS`，可重复执行。
const SCHEMA: &str = r"
CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY NOT NULL,
    username        TEXT NOT NULL COLLATE NOCASE,
    email           TEXT NOT NULL COLLATE NOCASE,
    created_at      TEXT NOT NULL,
    password_hash   TEXT,
    email_verified  INTEGER NOT NULL DEFAULT 0,
    totp_state      TEXT NOT NULL DEFAULT 'disabled',
    totp_secret_enc TEXT,
    token_version   INTEGER NOT NULL DEFAULT 0,
    avatar_version  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (username),
    UNIQUE (email)
);

CREATE TABLE IF NOT EXISTS oauth_clients (
    id           TEXT PRIMARY KEY NOT NULL,
    name         TEXT NOT NULL,
    secret_hash  TEXT,
    redirect_uri TEXT NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS refresh_token_families (
    id         TEXT PRIMARY KEY NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    state      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    revoked_at TEXT,
    client_id  TEXT REFERENCES oauth_clients (id),
    scope      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         TEXT PRIMARY KEY NOT NULL,
    family_id  TEXT NOT NULL REFERENCES refresh_token_families (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    state      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    rotated_at TEXT
);

CREATE TABLE IF NOT EXISTS email_verifications (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT,
    purpose     TEXT NOT NULL DEFAULT 'email_verify'
);

CREATE TABLE IF NOT EXISTS totp_recovery_codes (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT
);

CREATE TABLE IF NOT EXISTS totp_login_challenges (
    id          TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT
);

CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
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

CREATE TABLE IF NOT EXISTS user_bans (
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

CREATE TABLE IF NOT EXISTS audit_logs (
    id         TEXT PRIMARY KEY NOT NULL,
    actor_id   TEXT NOT NULL,
    action     TEXT NOT NULL,
    target_id  TEXT,
    details    TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_social_identities (
    id               TEXT PRIMARY KEY NOT NULL,
    user_id          TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider         TEXT NOT NULL CHECK (provider IN ('google', 'microsoft')),
    provider_user_id TEXT NOT NULL,
    display_name     TEXT,
    created_at       TEXT NOT NULL,
    UNIQUE (provider, provider_user_id)
);

CREATE TABLE IF NOT EXISTS social_login_states (
    state         TEXT PRIMARY KEY NOT NULL,
    provider      TEXT NOT NULL CHECK (provider IN ('google', 'microsoft')),
    code_verifier TEXT NOT NULL,
    action        TEXT NOT NULL CHECK (action IN ('login', 'link')),
    user_id       TEXT REFERENCES users (id) ON DELETE CASCADE,
    expires_at    TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    CHECK (
        (action = 'login' AND user_id IS NULL)
        OR (action = 'link' AND user_id IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS data_export_tokens (
    token_hash  TEXT PRIMARY KEY NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    consumed_at TEXT
);

CREATE TABLE IF NOT EXISTS policy_consents (
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    policy       TEXT NOT NULL,
    version      TEXT NOT NULL,
    consented_at TEXT NOT NULL,
    PRIMARY KEY (user_id, policy, version)
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family ON refresh_tokens (family_id);
CREATE INDEX IF NOT EXISTS idx_refresh_families_user ON refresh_token_families (user_id);
CREATE INDEX IF NOT EXISTS idx_email_verifications_user ON email_verifications (user_id);
CREATE INDEX IF NOT EXISTS idx_totp_recovery_user ON totp_recovery_codes (user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor ON audit_logs (actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs (action);
CREATE INDEX IF NOT EXISTS idx_social_identities_user ON user_social_identities (user_id);
CREATE INDEX IF NOT EXISTS idx_social_login_states_expires ON social_login_states (expires_at);
CREATE INDEX IF NOT EXISTS idx_data_export_tokens_user ON data_export_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_data_export_tokens_expires ON data_export_tokens (expires_at);

CREATE TRIGGER IF NOT EXISTS users_username_chars_insert
BEFORE INSERT ON users
FOR EACH ROW
WHEN NEW.username GLOB '*[^A-Za-z0-9_-]*'
BEGIN
    SELECT RAISE(ABORT, 'username contains invalid characters');
END;

CREATE TRIGGER IF NOT EXISTS users_username_chars_update
BEFORE UPDATE OF username ON users
FOR EACH ROW
WHEN NEW.username GLOB '*[^A-Za-z0-9_-]*'
BEGIN
    SELECT RAISE(ABORT, 'username contains invalid characters');
END;
";
