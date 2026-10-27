-- 恢复码改为 HMAC-SHA256(pepper)；旧 SHA-256 记录无法再校验，
-- v4 未上线，直接清空，用户可在关闭/重新启用 TOTP 时生成新批次。
DELETE FROM totp_recovery_codes;
