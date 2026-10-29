-- 社交登录（ADR-0012）：provider 身份绑定与 OAuth 中转态。
-- 只存 provider 用户 id 与展示昵称；不存头像、不存邮箱。

-- 一个 (provider, provider_user_id) 只能属于一个用户；一个用户可绑多个 provider。
-- provider 标识入库即字符串（与 SocialProvider::as_str 一致）。
CREATE TABLE user_social_identities (
    id               TEXT PRIMARY KEY NOT NULL,
    user_id          TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider         TEXT NOT NULL CHECK (provider IN ('google', 'microsoft')),
    provider_user_id TEXT NOT NULL,
    display_name     TEXT,
    created_at       TEXT NOT NULL,
    UNIQUE (provider, provider_user_id)
);

CREATE INDEX idx_social_identities_user ON user_social_identities (user_id);

-- OAuth 中转态（红线 #4：不得只存进程内存）：state + PKCE verifier，
-- 单次消费、过期作废，由清理任务删除。login 时 user_id 为空，link 时非空。
CREATE TABLE social_login_states (
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

CREATE INDEX idx_social_login_states_expires ON social_login_states (expires_at);
