# ChmlFrp 固定 HTTPS 入口

2026-10-22。访问地址：**https://team-aiorigin.owo.lat**。平台、数据库、原始文件和模型继续运行在本机 Mac；ChmlFrp 提供公网中转，Mac 不需要公网 IP。

## 已部署配置

| 项目 | 配置 |
| --- | --- |
| 服务商节点 | 中国香港 VIP，节点 #9，`vip.xg.frp.one` |
| 固定域名 | `team-aiorigin.owo.lat`，CNAME 指向上述节点，TTL 10 分钟 |
| HTTPS 隧道 | `research_platform_https`，ID 365528，本机 `127.0.0.1:8092` |
| HTTP 隧道 | `research_platform_http`，ID 365529，本机 `127.0.0.1:8093` |
| 客户端 | 官方 `ChmlFrp-0.70.1_261019`，Darwin arm64 |
| 证书网关 | Caddy 2.11.7，Let's Encrypt 证书自动申请及续期 |
| 平台网关 | Nginx，本机 `127.0.0.1:8091`，限制公开注册、支持流式传输 |
| 原平台入口 | `http://localhost:8090` |

HTTP 通道仅做证书校验和跳转到 HTTPS。浏览器与本机 Caddy 之间使用 HTTPS，Frpc 控制连接启用 TLS，两条代理也启用了数据加密。客户端只启动上述两条平台隧道，账号原有的 SSH、Ollama、PPT 等隧道未加入配置。

域名使用用户指定的 `team-aiorigin` 名称，已按服务商的[映射教程](https://docs.chmlfrp.net/docs/use/mapping)配置。该教程要求 HTTPS 证书安装在本地网站；续期由 [Caddy Automatic HTTPS](https://caddyserver.com/docs/automatic-https)处理。证书和 ACME 账号保存在 Docker 持久卷 `context-workspace_research-public-tls-data`，不要清理该卷。

## 日常操作

- 开启：双击项目目录的 `启动公网试用.command`。
- 查看固定地址及实际 HTTPS 连通状态：双击 `查看公网地址.command`。
- 关闭所有本项目公网入口：双击 `停止公网试用.command`。本机平台和文件保留。
- 为同事创建账号：管理员登录后，点击左下角头像 → 系统管理 → 用户管理 → 创建用户；填写同事姓名和邮箱，可以自动生成初始密码。本机也可双击 `开通同事账号.command`。

当前 macOS 用户登录后会自动启动隧道，断网后客户端会重新连接，进程退出后由 launchd 重启。隧道运行期间使用 `caffeinate -i` 防止空闲睡眠。**Mac 必须开机、联网、用户已登录，且 Docker/Colima 和平台保持运行**；关机、退出登录、手动睡眠或合盖会影响访问。

启动/停止/查看脚本已切换到 ChmlFrp。原 localhost.run 临时入口已停止，旧链接不再作为试用地址。

## 系统管理员

按用户要求，`ui-qa@example.invalid` 已于 2026-10-22 从验收账号转为系统管理员，并重新生成密码。旧密码和旧会话已失效；新密码只在交付会话中提供，不写入本说明。公网登录、管理员用户列表和创建用户表单已验证可用。

首次管理员通过 `.env` 的 `WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL` 和平台原生启动初始化流程设置，权限保存在数据库中。该配置在已有系统管理员时不会再次授予其他账号权限。原自动验收状态中的该账号密码和登录令牌已移除，避免自动测试继续使用管理员凭据。

## 配置与排查

- 编排：`compose.research.chmlfrp.yaml` 与 `compose.research.public.yaml`，需和 `compose.research.yaml` 一起使用。
- HTTPS 配置：`deploy/research-public/Caddyfile`。
- 隧道管理：`scripts/research-chmlfrp-tunnel.py`。
- 私密配置与官方客户端：`.research-private/chmlfrp/`，已被 Git 忽略；不要分享其中的 Token 或 `frpc.toml`。
- 运行目录：`~/Library/Application Support/ResearchChmlFrp/`，权限 700；包含 `status.json`、`frpc.log` 和私密配置。
- 自动启动：`~/Library/LaunchAgents/local.research.chmlfrp.plist`。

公网无法访问时，先运行 `查看公网地址.command`，再检查平台是否能通过本机 8090 访问。服务商面板应显示两条 `research_platform_...` 隧道在线。证书故障查看 `docker logs context-workspace-public-tls`；中转连接故障查看运行目录中的 `frpc.log`。更改客户端配置后通过停止、启动命令重新加载。若服务商更换节点，需要同步更新 DNS、面板隧道和本机配置。

## 验收证据

`artifacts/research/chmlfrp/` 保存本轮证据：

- `sharing-public/report.json`：两个独立合成账号的 7 组检查全部通过，涵盖 TXT/DOCX/PDF 上传解析、私有权限、共享后读取同一 ID、原件字节一致、替换后 6 次并发读取最新版、旧版保留、只读限制和撤权。
- `sse-public.json`：实际模型响应 51 个事件，约 4.751 秒完成，收到结束事件。
- `tls-and-auth.json`：证书链及域名验证成功；登录配置 200，公开注册 403，匿名原件预览 401。
- `before.json`、`after-rollout.json`：部署前后 23 张业务表、250 个原件/导出文件、原有配置与持久卷保持一致。之后验收只新增独立合成测试资料。
- `restart-check.json`：主动停止并重启隧道后的 HTTPS 恢复结果。
- `browser-verification.json`、`public-pdf-preview.png`：同事账号公网登录、无需手动刷新显示更新后的名称、PDF 原件中文正文和表格的浏览器验证。
- `domain-created.png`、`tunnels-online.png`：固定域名和两条在线隧道的面板记录。
- `admin-setup.json`、`system-admin-ready.png`：管理员初始化、旧凭据失效、新密码登录以及网页开户入口的验证记录（不含密码）。

共享列表在页面可见时约每 5 秒检查更新；已打开的原件需重新打开才能加载新版本。此处验证的是文件共享和版本同步，不是多人同时编辑同一文档。测试使用小型合成文件，不代表大文件吞吐、全天稳定性或任意并发人数已通过；实际速度受 Mac 上行网络和中转节点负载影响。
