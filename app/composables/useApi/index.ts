import { createApiClient, type ApiClient } from '@nebula/api-client'
import { useRuntimeConfig } from '#imports'

let client: ApiClient | undefined

/**
 * 返回全站共享的 API client；同源部署 baseUrl 为空，前后端分离时由
 * `NUXT_PUBLIC_API_BASE` 指向后端来源（cookie 与 CSRF 回显由 client 内建）。
 */
export function useApi(): ApiClient {
  if (!client) {
    const config = useRuntimeConfig()
    client = createApiClient({ baseUrl: config.public.apiBase })
  }
  return client
}
