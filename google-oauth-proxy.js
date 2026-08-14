// Google OAuth Proxy Worker
// 部署到 Cloudflare Workers，用于代理 Google OAuth API 请求（解决国内网络问题）
//
// 代理端点：
//   POST /token    → https://oauth2.googleapis.com/token
//     成功响应在返回前先验 id_token 的 RS256 签名（JWKS 取自 Google /certs，
//     按响应 Cache-Control 缓存，防"Google→代理"传输段被替换），验证通过后
//     用 Worker 私钥（env.WORKER_SIGNING_PRIVATE_KEY，Ed25519 PEM）对响应签名背书：
//       M = timestamp + "\n" + data   （data 为 Google 原始响应体逐字节原文）
//     返回 { data, timestamp, signature }。
//     服务器用预置公钥验签（见 internal/handlers/oauth/google/proxyverify.go），
//     不再需要预置 Google JWKS，轮换由本 Worker 自动消化。
//     任一验证失败 fail-closed：返回 401，不产生签名响应。
//   GET  /userinfo → https://www.googleapis.com/oauth2/v2/userinfo（透传，无签名：
//     仅含头像/昵称等展示数据，非身份依据）
//
// 部署：
//   1. Worker URL 配置为服务器环境变量 GOOGLE_PROXY_URL
//   2. wrangler secret put WORKER_SIGNING_PRIVATE_KEY  （Ed25519 私钥 PEM 全文）
//      对应公钥（PKIX DER base64）配置为服务器 WORKER_SIGNING_PUBLIC_KEY

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    if (url.pathname === '/token' && request.method === 'POST') {
      const body = await request.text();
      const googleResp = await fetch('https://oauth2.googleapis.com/token', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body,
      });
      const data = await googleResp.text();

      // Google 侧错误（如 invalid_grant）原样透传，不签名
      if (!googleResp.ok) {
        return new Response(data, {
          status: googleResp.status,
          headers: { 'Content-Type': 'application/json' },
        });
      }

      // 成功响应：验 id_token 签名 → 签名背书
      try {
        const { id_token: idToken } = JSON.parse(data);
        if (!idToken) throw new Error('no id_token in token response');
        await verifyIDToken(idToken);
        const timestamp = Math.floor(Date.now() / 1000);
        const signature = await signPayload(env, timestamp + '\n' + data);
        return Response.json({ data, timestamp, signature });
      } catch (e) {
        return Response.json(
          { error: 'ID_TOKEN_VERIFY_FAILED', detail: String((e && e.message) || e) },
          { status: 401 }
        );
      }
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

// ---- id_token RS256 验签（JWKS 内存缓存，kid 未命中时强制刷新一次）----

let jwksCache = { keys: null, expiry: 0 };

async function fetchJwks(force) {
  const now = Date.now();
  if (!force && jwksCache.keys && now < jwksCache.expiry) return jwksCache.keys;
  const resp = await fetch('https://www.googleapis.com/oauth2/v3/certs');
  if (!resp.ok) throw new Error('jwks fetch failed: ' + resp.status);
  const doc = await resp.json();
  const cc = resp.headers.get('cache-control') || '';
  const m = cc.match(/max-age=(\d+)/);
  const ttl = (m ? parseInt(m[1], 10) : 3600) * 1000;
  jwksCache = { keys: doc.keys, expiry: now + ttl };
  return doc.keys;
}

function b64urlToBytes(s) {
  s = s.replace(/-/g, '+').replace(/_/g, '/');
  while (s.length % 4) s += '=';
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

async function verifyIDToken(idToken) {
  const parts = idToken.split('.');
  if (parts.length !== 3) throw new Error('malformed id_token');
  const header = JSON.parse(new TextDecoder().decode(b64urlToBytes(parts[0])));
  if (header.alg !== 'RS256' || !header.kid) throw new Error('unsupported alg or missing kid');

  let keys = await fetchJwks(false);
  let jwk = keys.find((k) => k.kid === header.kid && k.use === 'sig');
  if (!jwk) {
    // kid 未命中：可能 Google 刚轮换密钥，强制刷新一次再找
    keys = await fetchJwks(true);
    jwk = keys.find((k) => k.kid === header.kid && k.use === 'sig');
  }
  if (!jwk) throw new Error('kid not found: ' + header.kid);

  const key = await crypto.subtle.importKey(
    'jwk',
    jwk,
    { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-256' },
    false,
    ['verify']
  );
  const ok = await crypto.subtle.verify(
    'RSASSA-PKCS1-v1_5',
    key,
    b64urlToBytes(parts[2]),
    new TextEncoder().encode(parts[0] + '.' + parts[1])
  );
  if (!ok) throw new Error('id_token signature mismatch');
}

// ---- Ed25519 签名背书 ----

function pemToDer(pem) {
  const body = pem
    .replace(/-----BEGIN PRIVATE KEY-----/, '')
    .replace(/-----END PRIVATE KEY-----/, '')
    .replace(/\s+/g, '');
  const bin = atob(body);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

async function signPayload(env, message) {
  if (!env.WORKER_SIGNING_PRIVATE_KEY) throw new Error('WORKER_SIGNING_PRIVATE_KEY not set');
  const key = await crypto.subtle.importKey(
    'pkcs8',
    pemToDer(env.WORKER_SIGNING_PRIVATE_KEY),
    { name: 'Ed25519' },
    false,
    ['sign']
  );
  const sig = await crypto.subtle.sign('Ed25519', key, new TextEncoder().encode(message));
  const bytes = new Uint8Array(sig);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
}
