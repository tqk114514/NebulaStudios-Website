-- refresh 家族是会话聚合（ADR-0006）：创建时固定 client_id 与 scope，
-- 之后只轮转 token。第一方家族的 scope 为空串。
ALTER TABLE refresh_token_families ADD COLUMN scope TEXT NOT NULL DEFAULT '';
