-- OAuth2 Provider（ADR-0002）：客户端与授权码
CREATE TABLE oauth_clients (
    id           TEXT PRIMARY KEY NOT NULL,
    name         TEXT NOT NULL,
    secret_hash  TEXT,
    redirect_uri TEXT NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL
);

CREATE TABLE oauth_authorization_codes (
    id             TEXT PRIMARY KEY NOT NULL,
    code_hash      TEXT NOT NULL UNIQUE,
    client_id      TEXT NOT NULL REFERENCES oauth_clients (id),
    user_id        TEXT NOT NULL REFERENCES users (id),
    redirect_uri   TEXT NOT NULL,
    scope          TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    expires_at     TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    consumed_at    TEXT
);

-- 家族归属：NULL 为第一方用户会话，非空为第三方客户端会话（同一轮转/重放机制）
ALTER TABLE refresh_token_families ADD COLUMN client_id TEXT REFERENCES oauth_clients (id);
