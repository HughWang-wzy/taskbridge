# TaskBridge

TaskBridge 用 Cloudflare Worker + D1 记录长时间运行的命令和 Codex 会话，并通过 [ntfy](https://ntfy.sh/) 发手机通知。每台电脑运行一个 Go 客户端 `tb`。Worker 只管理状态、Watchdog、问题和待发队列；普通通知由事件源直接发送，队列通知由在线 `tb relay` 领取、发送、ACK。

[English](README.md) · [架构](docs/architecture.md) · [许可证](LICENSE)

## 功能与限制

- `tb run` 记录任务开始、心跳、退出码和耗时。失败必通知；成功任务默认运行满 60 秒才通知。
- Codex Hook 记录结束、中断和心跳。符合 Hook 覆盖范围的原生提问可触发手机提醒。
- Codex MCP 支持手机答题；**只注册 MCP，不会自动接管所有 Codex 原生提问框**。手机先答后，电脑原生框可能仍需手动关闭。
- D1 保存 LOST、恢复、提问和待发通知。多个 relay 可并行运行；ntfy 交付语义为至少一次。

## 新电脑快速安装

先在已配置的管理电脑创建**新电脑专用的客户端令牌**（命令见下文）。新电脑只需该令牌、Worker URL 和手机已订阅的 ntfy topic，不需要 Cloudflare 管理员令牌。

Linux/macOS 在终端运行：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.2.0/scripts/install.sh)
```

Windows PowerShell 运行：

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.2.0/scripts/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

安装程序会选择平台发布包、核对 SHA-256、安装 `tb`、交互式配置客户端并运行 `tb doctor`，还会询问是否安装 Codex Hook/MCP 和本机 relay。已有配置默认保留。以上命令会运行从公开仓库下载的脚本，你也可以先打开脚本阅读。Linux ARM64 暂无预编译包，需要从源码构建。

## 部署 Worker

准备 Cloudflare Workers + D1、Node.js 22+ 和一个手机已订阅的 ntfy topic。建议使用足够长的随机 topic 名称。

```bash
npm ci
cp wrangler.example.jsonc wrangler.jsonc
npx wrangler login
npx wrangler d1 create taskbridge-db
```

把创建 D1 后返回的 `database_id` 填进本机 `wrangler.jsonc`，再运行：

```bash
npx wrangler d1 migrations apply taskbridge-db --remote
npx wrangler secret put TB_ADMIN_TOKEN
npm run deploy
```

请妥善保管 `TB_ADMIN_TOKEN`。本机 `wrangler.jsonc`、`.dev.vars` 和客户端凭据均被 Git 忽略；不要上传到公开仓库。

## 在电脑安装 `tb`

从发布包选对应平台的二进制，或安装 Go 1.22+ 后运行 `npm run build:cli`。支持 Linux x86-64、Windows x86-64、macOS Intel 和 Apple Silicon。

先在管理电脑创建一台设备专用的令牌：

```bash
TB_CONFIG="$HOME/.config/taskbridge/admin.json" tb init --url https://YOUR_WORKER.YOUR_SUBDOMAIN.workers.dev
TB_CONFIG="$HOME/.config/taskbridge/admin.json" tb clients create workstation --scopes=tasks:write,tasks:read,notify:write,notifications:relay,codex:write,questions:write,questions:read
```

第一次 `tb init` 输入 Cloudflare 中设置的管理员令牌和 ntfy topic；第二条命令会显示新设备令牌。每台电脑单独创建令牌，不要把管理员配置复制过去。

在新电脑运行：

```bash
tb init --url https://YOUR_WORKER.YOUR_SUBDOMAIN.workers.dev
tb doctor
tb notify --title "新电脑测试" "TaskBridge 已连接"
tb run -n "训练" -- python train.py
```

`tb init` 会提示输入该设备令牌和相同的 ntfy topic。Linux 配置保存在 `~/.config/taskbridge/config.json`；Windows/macOS 使用各自系统的用户配置目录。`TB_CONFIG` 可指定其他路径。

## relay 与 Codex

至少一台电脑需常驻 `tb relay` 才能发送 D1 待发通知。**Codex Interrupt** 有 3 秒 Hook 上限，因此先写入本机待发文件；发生中断的电脑也必须运行 relay，才能把该文件提交到 D1。Linux 可使用 [systemd 用户服务模板](deploy/taskbridge-relay.service)。

若需 Codex 集成，把本机 `config.json` 复制为同目录的 `codex.json`。Linux 示例：

```bash
cp ~/.config/taskbridge/config.json ~/.config/taskbridge/codex.json
chmod 600 ~/.config/taskbridge/codex.json
tb hook codex
codex mcp add taskbridge -- "$(command -v tb)" mcp
```

重启 Codex，在 `/hooks` 中检查并信任新 Hook。`PreToolUse`、`PostToolUse`、`Stop` 后台运行，上限 30 秒；`Interrupt` 快速落盘，上限仍为 Codex 规定的 3 秒。Hook 不会改变 Codex 的正常任务进度，但 relay 通知可能稍晚到达。无需修改全局 `AGENTS.md`。

原生提问的手机**提醒**不能直接回答；手机答题要求 Codex 明确调用 TaskBridge MCP。Codex 的权限审批不经 TaskBridge。

## 开发和打包

```bash
go test ./...
npm ci
cp wrangler.example.jsonc wrangler.jsonc
npm test
npm run typecheck
npm run package
```

发布文件写入 `release/`，包含四个平台的压缩包和 `SHA256SUMS`。详细数据流见[架构说明](docs/architecture.md)。
