-- ADR-0006：旧 access token 因 audience 变更失效；旧 refresh 家族一并撤销，
-- 避免部署前留下的分叉链在部署后继续可用。所有既有会话重新登录。
UPDATE refresh_token_families
   SET state = 'revoked',
       revoked_at = strftime('%Y-%m-%dT%H:%M:%S+00:00', 'now')
 WHERE state = 'active';

UPDATE refresh_tokens
   SET state = 'revoked'
 WHERE state != 'revoked';
