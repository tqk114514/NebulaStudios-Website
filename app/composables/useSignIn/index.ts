/**
 * 登录两步流程的表单状态：口令步骤与动态码步骤，错误只在提交后出现。
 * 二段由服务端 401 `totp_required` 的挑战令牌驱动（ADR-0004、ADR-0016）。
 */
export { useSignInState as useSignIn } from './state'
