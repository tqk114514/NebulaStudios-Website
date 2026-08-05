/**
 * Account 模块 API 请求工具
 *
 * 原实现已合并至 shared/js/utils/api.ts（account / admin 共用），此处仅 re-export，
 * 以保持各调用方 import 路径不变。
 */

export {
  fetchApi,
  type FetchResult,
  type FetchOptions,
} from '../../../../../../shared/js/utils/api.ts';
