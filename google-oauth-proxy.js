// Google OAuth Proxy Worker
// 部署到 Cloudflare Workers，用于代理 Google OAuth API 请求（解决国内网络问题）
//
// 代理端点：
//   POST /token    → https://oauth2.googleapis.com/token
//   GET  /userinfo → https://www.googleapis.com/oauth2/v2/userinfo
//
// 部署后，将 Worker URL 配置为 GOOGLE_PROXY_URL 环境变量即可。

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);

    if (url.pathname === '/token' && request.method === 'POST') {
      const body = await request.text();
      const googleResp = await fetch('https://oauth2.googleapis.com/token', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body,
      });
      return googleResp;
    }

    if (url.pathname === '/userinfo') {
      const authHeader = request.headers.get('Authorization');
      if (!authHeader) {
        return new Response('Missing Authorization header', { status: 400 });
      }
      const googleResp = await fetch('https://www.googleapis.com/oauth2/v2/userinfo', {
        headers: { Authorization: authHeader },
      });
      return googleResp;
    }

    return new Response('Not Found', { status: 404 });
  },
};