# Linux 服务器配置

## 服务器信息

- 用户: `tqk114514`
- SSH: `ssh.nebulastudios.top` (通过 Cloudflare Tunnel)

## 数据库

- PostgreSQL 16
- 数据库: `nebula_account`
- 用户: `postgres`
- 密码: `114514`

```bash
# 连接数据库
sudo -u postgres psql -d nebula_account

# 查看状态
sudo systemctl status postgresql
```

## 服务列表

### 1. Nebula 主服务

- 路径: `/var/www/nebula/`
- 服务文件: `/etc/systemd/system/nebula.service`
- 端口: 见 `.env` 配置

```bash
# 管理命令
sudo systemctl status nebula
sudo systemctl restart nebula
sudo journalctl -u nebula -f  # 查看日志
```

### 2. Webhook 部署服务

- 路径: `/var/www/webhook/`
- 服务文件: `/etc/systemd/system/webhook.service`
- 端口: 3004
- 域名: `linux-webhook.nebulastudios.dpdns.org`

```bash
# 管理命令
sudo systemctl status webhook
sudo systemctl restart webhook
sudo journalctl -u webhook -f  # 查看日志
```

## Cloudflare Tunnel 配置

| 域名 | 目标 |
|------|------|
| `ssh.nebulastudios.top` | `ssh://localhost:22` |
| `linux-webhook.nebulastudios.dpdns.org` | `http://localhost:3004` |

## Cloudflare R2

- 存储桶: `nebula-deploy`
- 公开 URL: `https://pub-ce313d59ead54456af3c16eaf60b2e79.r2.dev`

## GitHub Secrets

| 名称 | 值 |
|------|------|
| `R2_ACCESS_KEY_ID` | `eb0b3941f136b5c1f71db1acdca8375f` |
| `R2_SECRET_ACCESS_KEY` | `fdfeeb02a59b1a9ae2868e65643125db245a17db88c1fd79236d11a3b4c63bda` |
| `R2_ENDPOINT` | `https://24008de77e39dec32fd5570eb76a38f3.r2.cloudflarestorage.com` |
| `DEPLOY_TOKEN` | 13a844912703813a451d372138905c38a55ec8063bf83128d0135171436b9463 |

## 部署流程

1. 推送代码到 `main` 分支
2. GitHub Actions 编译并上传到 R2
3. 发送 webhook 通知服务器
4. 服务器下载并解压文件
5. 重启 nebula 服务
