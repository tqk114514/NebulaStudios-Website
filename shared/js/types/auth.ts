/**
 * 认证相关类型定义
 */

// ==================== API 响应 ====================

/** API 成功响应 */
export interface ApiSuccessResponse<T = unknown> {
  success: true;
  data: T;
  message?: string;
}

/** API 失败响应 */
export interface ApiErrorResponse {
  success: false;
  errorCode: string;
  message?: string;
}

/** API 响应联合类型 */
export type ApiResponse<T = unknown> = ApiSuccessResponse<T> | ApiErrorResponse;

/** 验证结果 */
export interface ValidationResult {
  valid: boolean;
  errorKey?: string;
}

/** 加载结果 */
export interface LoadResult {
  success: boolean;
  error?: string;
}

// ==================== 用户相关 ====================

/** 用户数据 */
export interface User {
  id?: string;
  username: string;
  email: string;
  avatar?: string;
  avatar_url?: string | null;
  microsoft_id?: string | null;
  microsoft_name?: string | null;
  microsoft_avatar_url?: string | null;
  is_banned?: boolean;
  ban_reason?: string | null;
  banned_at?: string | null;
  unban_at?: string | null;
}

// ==================== 表单数据 ====================

/** 注册表单数据 */
export interface RegisterFormData {
  username: string;
  email: string;
  verificationCode: string;
  password: string;
  passwordConfirm?: string;  // 前端验证用，不发送到后端
}

/** 登录表单数据 */
export interface LoginFormData {
  email: string;
  password: string;
  captchaToken?: string;
  captchaType?: string;
}

// ==================== 认证响应 ====================

/** 认证响应 */
export interface AuthResponse {
  success: boolean;
  data?: User;
  errorCode?: string;
}

/** 发送验证码响应 */
export interface SendCodeResponse {
  success: boolean;
  message?: string;
  expireTime?: number;
  email?: string;
  errorCode?: string;
}

// ==================== 其他 ====================

/** 邮箱服务商信息 */
export type EmailProviders = Record<string, string>;

/** PC 端信息（扫码登录） */
export interface PcInfo {
  ip: string;
  browser: string;
  os: string;
}
