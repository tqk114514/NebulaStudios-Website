import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from '@/App.vue'
import { registerGlobal } from '@/plugins'
import router from '@/router'
import { i18n } from '@/i18n'
// 字体走 CDN（index.html 引入 {{CDN_URL}}/fonts/fonts.css，构建期由 vite 插件替换占位符），不打包进产物
import '@/styles/main.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)
registerGlobal(app)
app.mount('#app')
