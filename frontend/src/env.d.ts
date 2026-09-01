/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const component: DefineComponent<object, object, any>
  export default component
}

interface ImportMetaEnv {
  /** Turnstile SDK 地址，可用 .env 覆盖（默认 challenges.cloudflare.com） */
  readonly VITE_TURNSTILE_SDK_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}