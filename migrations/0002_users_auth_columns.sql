-- 注册所需的口令与验证状态（0001 建表时未含，随注册端点补齐）
-- password_hash 允许 NULL：口令未设的预留位（当前不存在该路径，注册必写）
ALTER TABLE users ADD COLUMN password_hash TEXT;
ALTER TABLE users ADD COLUMN email_verified INTEGER NOT NULL DEFAULT 0;
