-- 用户名禁止邮箱字符，避免 login 的 email/username 两列出现跨列歧义。
-- 应用层已校验 3–15 位 ASCII 字母/数字/_/-；触发器作为数据库层兜底。
CREATE TRIGGER users_username_chars_insert
BEFORE INSERT ON users
FOR EACH ROW
WHEN NEW.username GLOB '*[^A-Za-z0-9_-]*'
BEGIN
    SELECT RAISE(ABORT, 'username contains invalid characters');
END;

CREATE TRIGGER users_username_chars_update
BEFORE UPDATE OF username ON users
FOR EACH ROW
WHEN NEW.username GLOB '*[^A-Za-z0-9_-]*'
BEGIN
    SELECT RAISE(ABORT, 'username contains invalid characters');
END;
