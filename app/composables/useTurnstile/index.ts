import { onMounted, ref } from 'vue'
import { useApi } from '~/composables/useApi'

/**
 * Turnstile 站点配置与一次性 token。启用校验时注册/发码端点要求 token；
 * token 单次有效，提交后调用 `reset` 重新渲染组件。
 */
export function useTurnstile() {
  const api = useApi()
  const enabled = ref(false)
  const siteKey = ref('')
  const token = ref('')
  const version = ref(0)

  onMounted(async () => {
    try {
      const { data } = await api.GET('/api/auth/turnstile')
      if (data?.enabled && data.site_key) {
        enabled.value = true
        siteKey.value = data.site_key
      }
    } catch {
      // 配置读不到按未启用处理；真启用时后端会返回 503/422，由表单展示
    }
  })

  function reset() {
    token.value = ''
    version.value += 1
  }

  return { enabled, siteKey, token, version, reset }
}
