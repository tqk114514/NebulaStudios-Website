/**
 * 验证码页面服务模块
 * 
 * 功能：
 * - 验证邮件链接中的 token
 * - 获取验证码
 */

// ==================== 错误码映射 ====================

/**
 * 错误码到翻译键的映射
 */
export const errorCodeMap = {
  'INVALID_TOKEN': 'verify.errorInvalidToken',
  'TOKEN_EXPIRED': 'verify.errorTokenExpired',
  'TOKEN_USED': 'verify.errorTokenUsed',
  'NO_TOKEN': 'verify.errorNoToken',
  'NETWORK_ERROR': 'verify.errorNetwork',
  'VERIFY_FAILED': 'verify.errorDefault'
};

// ==================== API 调用 ====================

/**
 * 验证 token 并获取验证码
 * @param {string} token - 验证 token
 * @returns {Promise<Object>} 包含 success, code 或 errorCode 的对象
 */
export async function verifyToken(token) {
  try {
    const response = await fetch('/api/auth/verify-token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token })
    });

    const data = await response.json();
    
    if (response.ok && data.success) {
      return { success: true, code: data.code, email: data.email };
    } else {
      return { success: false, errorCode: data.errorCode || 'VERIFY_FAILED' };
    }
  } catch (error) {
    console.error('[VERIFY] ERROR: Token verification request failed:', error.message);
    return { success: false, errorCode: 'NETWORK_ERROR' };
  }
}
