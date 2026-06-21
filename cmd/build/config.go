// 构建期配置。构建系统独立于服务端运行，无法读取后端 .env，
// 故在此集中维护构建期常量。修改 CDN 域名时只需改这一处，
// 重新执行 go run ./cmd/build/ 即可。
package main

// cdnURL 构建时注入的 CDN 地址，替换源码中的 {{CDN_URL}} 占位符。
// 与后端 R2_URL 保持一致。
const cdnURL = "https://fast-cdn01.nebulastudios.top"

// turnstileSDKURL Cloudflare Turnstile 人机验证 SDK 地址，
// 替换源码中的 {{TURNSTILE_SDK_URL}} 占位符。
const turnstileSDKURL = "https://challenges.cloudflare.com/turnstile/v0/api.js"
