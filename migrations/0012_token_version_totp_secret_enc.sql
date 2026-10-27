-- access token 版本：撤销会话/封禁时递增，旧 token 立即失效（ADR-0010）。
ALTER TABLE users ADD COLUMN token_version INTEGER NOT NULL DEFAULT 0;

-- TOTP 密钥改为 AES-GCM 密文列。v4 未上线，旧明文密钥不迁移，
-- 强制重新启用 TOTP，避免遗留明文继续可用。
ALTER TABLE users ADD COLUMN totp_secret_enc TEXT;
DELETE FROM totp_recovery_codes;
DELETE FROM totp_login_challenges;
UPDATE users SET totp_state = 'disabled', totp_secret_enc = NULL;
ALTER TABLE users DROP COLUMN totp_secret;
