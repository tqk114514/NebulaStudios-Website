-- 验证码用途列：邮箱验证与账户注销确认共用生命周期语义，但互不相通
ALTER TABLE email_verifications ADD COLUMN purpose TEXT NOT NULL DEFAULT 'email_verify';
