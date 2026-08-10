#!/usr/bin/env python3
"""比对 Google 当前 JWKS 与仓库基准 .github/google-jwks.json。

Google 轮换签名密钥时（新 kid），本脚本退出码非 0，触发 CI 失败告警。
比较的是规范化后的 keys 集合（按 kid 排序、固定字段序），不受字段顺序变化影响。

用法: python check_google_jwks.py <baseline.json>
"""
import hashlib
import json
import sys
import urllib.request

GOOGLE_CERTS_URL = "https://www.googleapis.com/oauth2/v3/certs"

_FIELDS = ("kid", "n", "e", "kty", "use", "alg")


def normalize(data: bytes) -> bytes:
    doc = json.loads(data)
    keys = []
    for k in doc.get("keys", []):
        keys.append(tuple(k.get(f, "") for f in _FIELDS))
    keys.sort()
    return json.dumps(keys, ensure_ascii=False, sort_keys=True).encode()


def sha256_hex(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def main() -> int:
    if len(sys.argv) < 2:
        print("usage: check_google_jwks.py <baseline.json>")
        return 2

    with open(sys.argv[1], "rb") as f:
        baseline = normalize(f.read())

    try:
        with urllib.request.urlopen(GOOGLE_CERTS_URL, timeout=30) as resp:
            current = normalize(resp.read())
    except Exception as e:  # noqa: BLE001
        print(f"!! failed to fetch Google JWKS: {e}")
        return 1

    b, c = sha256_hex(baseline), sha256_hex(current)
    print(f"baseline sha256: {b}")
    print(f"current  sha256: {c}")

    if baseline == current:
        print("OK: Google JWKS unchanged")
        return 0

    print("!! Google JWKS ROTATED: update .github/google-jwks.json")
    print("   1. curl -s https://www.googleapis.com/oauth2/v3/certs > .github/google-jwks.json")
    print("   2. update GOOGLE_JWKS_SHA256 (base64) in server env and redeploy")
    return 1


if __name__ == "__main__":
    sys.exit(main())
