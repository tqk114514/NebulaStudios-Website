-- 验证码哈希改为 HMAC-SHA256(pepper)；旧 SHA-256 记录无法再校验，
-- v4 未上线，直接清空待重发，避免留下不可用记录。
DELETE FROM email_verifications;
