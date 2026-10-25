-- 用户表（ADR-0003）：时间戳 RFC3339 TEXT；用户名与邮箱大小写不敏感唯一，
-- v3 的 LOWER(username) 查找语义由列级 COLLATE NOCASE 承担
CREATE TABLE users (
    id         TEXT PRIMARY KEY NOT NULL,
    username   TEXT NOT NULL COLLATE NOCASE,
    email      TEXT NOT NULL COLLATE NOCASE,
    created_at TEXT NOT NULL,
    UNIQUE (username),
    UNIQUE (email)
);
