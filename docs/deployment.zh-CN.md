# TaskBridge 部署指南

[返回 README](../README.zh-CN.md) · [English](deployment.md)

## 准备环境

首次部署需要 Git、可访问 Cloudflare 的网络和手机 ntfy 应用。脚本会检查并辅助安装 Node.js 22+（含 npm）。Linux/macOS 客户端安装还需要 `curl`、`tar` 和 SHA-256 工具；Windows 使用 PowerShell。当前发布包支持 Linux x86-64、macOS Intel/Apple Silicon 和 Windows x86-64。Linux ARM64 需要自行从源码构建。

首次部署脚本会检查 Git、Node.js 和 npm。缺少或版本过旧时，会显示问题并询问是否修复：macOS 使用已有 Homebrew；Linux 有 nvm 时使用 nvm，否则从 [Node.js 官方发布目录](https://nodejs.org/download/release/latest-v22.x/)下载并校验 SHA-256 后安装到 `~/.local/share/taskbridge/node-22`；Windows 使用已有 winget。Linux 缺 Git 时使用 apt/dnf；root 用户直接执行包管理器，不要求 sudo。客户端安装器也会检查下载、解压和 SHA-256 工具。首次部署的 Worker 检查会使用系统已有的 HTTP/HTTPS 代理环境变量。脚本不会静默修改系统软件。

## 注册 Cloudflare 账户

1. 打开 [Cloudflare 注册页](https://dash.cloudflare.com/sign-up)，填写邮箱和密码，创建账户。
2. 打开 Cloudflare 发送的验证邮件，完成邮箱验证。
3. 返回首次部署向导，按 Enter。Wrangler 会在终端显示设备验证网址和短代码；用电脑或手机浏览器打开该网址，输入代码并授权。
4. 回到终端，等待 `whoami` 验证通过。[Cloudflare 的设备授权流程](https://developers.cloudflare.com/workers/wrangler/commands/general/)不使用 `localhost:8976`，适用于远程 SSH 服务器。Worker 和 D1 会建立在当前登录的 Cloudflare 账户中。

注册和邮箱验证必须由账户本人在 Cloudflare 页面完成；TaskBridge 不收集 Cloudflare 密码。[Cloudflare 的 D1 入门文档](https://developers.cloudflare.com/d1/get-started/)也把账户注册列为前提。

## 首次部署

所有电脑都使用同一个安装入口。首次部署选择 **1：新建 Cloudflare Worker 和 D1**；添加电脑选择 **2：连接已有 Worker**。Worker 是运行 TaskBridge 服务端的 Cloudflare 程序，D1 是保存任务、提问和待发送通知的 Cloudflare 数据库；ntfy topic 是手机订阅的通知频道，三者各不相同。

Linux/macOS：

```bash
bash <(curl -fsSL https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.sh)
```

Windows PowerShell：

```powershell
Invoke-WebRequest https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

脚本默认把源码放在 `~/taskbridge`，安装依赖后启动向导。你输入易记的名称，例如 `Cospeak3`；向导自动追加 32 位随机十六进制后缀，显示完整 topic，并等你在手机 ntfy 中订阅这个完整名称后继续。随后它创建独立命名的 D1 和 Worker、执行迁移、设置随机管理员密钥、验证 `/health`、为本机创建客户端令牌，并调用客户端安装器。客户端安装器再询问是否安装 Codex Hook/MCP、是否启动 relay。

管理员密钥和恢复状态保存在部署目录的 `.local/` 中，已被 Git 忽略；请保留该目录，不要把它传给其他电脑。首次部署中断后，在同一目录运行：

```bash
bash ~/taskbridge/scripts/first-run.sh
```

Windows 在部署目录重运行 `powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\first-run.ps1`。

重跑安装器并选择 1 时，脚本会在没有本地源码改动的情况下更新已有源码目录，同时保留 `.local/` 部署状态。向导会复用已创建的 D1、管理员密钥和本机令牌。v0.4.1 尚未完成客户端配置的旧状态会为原 topic 补随机后缀；已完成部署的 topic 保持不变。若当前登录账户找不到原 D1，它会停止，避免在另一个账户创建同名数据库。已有手工配置的 `wrangler.jsonc` 也不会被向导接管。

## 添加另一台电脑

在部署电脑创建新设备专用令牌。Linux/macOS 示例：

```bash
cd ~/taskbridge
TB_CONFIG="$PWD/.local/admin.json" "$HOME/.local/bin/tb" clients create laptop --scopes=tasks:write,tasks:read,notify:write,notifications:relay,codex:write,questions:write,questions:read
```

Windows PowerShell：

```powershell
Set-Location (Join-Path $HOME 'taskbridge')
$env:TB_CONFIG = (Join-Path (Get-Location) '.local/admin.json')
& "$env:LOCALAPPDATA\Programs\TaskBridge\tb.exe" clients create laptop --scopes=tasks:write,tasks:read,notify:write,notifications:relay,codex:write,questions:write,questions:read
Remove-Item Env:TB_CONFIG
```

令牌只显示一次，私下传给对应电脑。新电脑运行同一安装器并选择 **2**，输入 Worker URL、该令牌和相同的 ntfy topic；不需要管理员密钥，也不会新建 D1。

Linux/macOS 安装入口：

```bash
bash <(curl -fsSL https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.sh)
```

Windows PowerShell 安装入口：

```powershell
Invoke-WebRequest https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

安装器会校验发布包的 SHA-256、保存用户配置并运行 `tb doctor`。已有客户端配置默认保留。至少一台电脑需要运行 `tb relay`，失联等队列通知才能送达；中断通知先保存在发生中断的电脑上，该电脑也需运行 relay。选后台 relay 时，Linux 普通用户优先使用 systemd 用户服务；root 优先使用系统级 systemd 服务。若无可用服务管理器，安装器会启动临时后台进程，并明确提示**重启后不会自动恢复**；持久运行需配置该主机自己的启动管理器。

## 设置提醒等级

TaskBridge 会为通知附上 ntfy 优先级。你可以按事件设置不同等级，再配合手机端决定声音和振动。运行 `tb alerts` 可查看当前生效的设置。例如，让正常完成和 Codex 中断使用较低等级：

```bash
tb alerts --success low --failure high --question high --interrupt low --lost urgent --manual normal
tb alerts
```

命令会保存本机客户端 `config.json`，并同步更新已有的 Codex `codex.json`。若显式设置了 `TB_CONFIG`，则只更新该文件。配置属于本机；其他电脑需要分别设置。已在运行的 Codex MCP 会在启动时读取配置，修改提问等级后需重启 Codex/MCP；完成通知的 Hook 会在下次执行时读取新值。

| 等级 | 可用参数值 | Android 默认表现 |
| --- | --- | --- |
| 最低 | `1`、`min`、`silent` | 无声音、无振动，进入其他通知 |
| 较低 | `2`、`low` | 无声音、无振动 |
| 普通 | `3`、`normal`、`default` | 普通提示音、短振动 |
| 较高 | `4`、`high` | 提示音、较长振动、横幅 |
| 紧急 | `5`、`urgent`、`max` | 提示音、更长振动、横幅 |

名称不区分大小写。以上是 [ntfy 的优先级默认行为](https://docs.ntfy.sh/publish/#message-priority)，手机设置可改变实际表现。`silent` 只是优先级 1 的别名，不能强制手机静音。

未自行修改时，保留原有默认等级：

| 事件 | 默认等级 |
| --- | --- |
| 正常完成、失联后恢复、手动通知 | `normal`（3） |
| 失败、等待回答的问题、Codex 中断 | `high`（4） |
| 任务失联（`LOST`） | `urgent`（5） |

单次手动通知可覆盖等级：

```bash
tb notify -p low --title 测试 '低等级通知'
```

运行训练时，可分别指定正常完成、失败和失联的等级：

```bash
tb run -n training --priority high --failure-priority urgent --lost-priority urgent -- python train.py
```

`tb run` 的 `--priority`（或 `-p`）只影响正常完成通知；它不会跳过“成功任务运行满 60 秒才通知”的原有门槛。失败和失联使用各自等级，恢复通知跟随正常完成等级。relay 会保留通知入队时的等级，因此修改默认值不会改变已入队的通知。

Codex 可分别设置完成、提问和中断等级：

```bash
tb hook codex --priority low --question-priority high --interrupt-priority low
```

这些参数可以与下节的标题、正文设置一起使用。更改 Hook 后，重启 Codex，并查看、信任更新后的 Hook。各台发送通知的电脑都需更新 `tb`；自定义失联等级和 MCP 提问等级还需要部署更新后的 Worker，无需迁移 D1。在保留原 `wrangler.jsonc` 的部署源码目录更新代码后，执行 `npm run deploy`。

### 配合手机设置声音

**Android：** 打开 ntfy → Settings → **Channel settings（通知渠道设置）**，或进入 Android 的 ntfy 通知设置。每个优先级都有独立渠道，可分别选择声音、振动、横幅及是否绕过勿扰模式。等级高并不代表一定能绕过系统设置。参见 [ntfy 手机设置文档](https://docs.ntfy.sh/subscribe/phone/#message-priority)。

**iPhone：** 在 iOS 设置中允许 ntfy 通知和声音，并检查专注模式配置。截至 2026 年 10 月 18 日，ntfy iOS 当前稳定版为 v1.7.0；优先级 5 穿透静音、勿扰的“关键警报”功能仍列在未发布的 v1.8.0 中。因此，当前稳定版的 `urgent` 不能保证强制响铃，也没有 Android 那样逐等级配置通知渠道的界面。参见[官方发布说明](https://docs.ntfy.sh/releases/#ntfy-ios-app-v180-unreleased)和[当前 iOS 设置实现](https://github.com/binwiederhier/ntfy-ios/blob/v1.7.0/ntfy/Views/Settings/SettingsView.swift)。

## 自定义 Codex 通知

安装时选择 Codex 集成后，可以选择自定义完成通知。也可稍后在已安装的电脑上运行：

```bash
"$HOME/.local/bin/tb" hook codex --topic "训练" --title "{topic} 已完成" --body "{topic} 任务结束，耗时 {duration}" --final-output off
```

Windows 将命令开头替换为 `& "$env:LOCALAPPDATA\Programs\TaskBridge\tb.exe"`。要把 Codex 的最终回复附在正文后面，把 `--final-output off` 改为 `on`。最终回复可能包含敏感内容，因此默认关闭。开启后最多发送前 2000 个字符；若 Codex 没有提供 `last_assistant_message`，只发送模板正文。模板中也可放 `{output}`，自行决定最终回复的位置。

| 变量 | 内容 |
| --- | --- |
| `{topic}` | 配置的名称；未配置时用项目目录名 |
| `{duration}` | 本轮从 `UserPromptSubmit` 到 `Stop` 的时间；缺起始事件时为 `unknown` |
| `{output}` | 最终回复；只有 `--final-output on` 才有值 |
| `{session}`、`{turn}` | Codex 会话和轮次 ID |

安装或更改 Hook 后，重启 Codex，在 `/hooks` 中查看并信任新的 Hook。`UserPromptSubmit` 记录起始时间，`Stop` 发完成通知；`Interrupt` 仍受 Codex 的 3 秒上限约束。这些事件及 `last_assistant_message` 字段见 [Codex Hook 官方文档](https://learn.chatgpt.com/docs/hooks)。Hook 只能提醒受支持的原生提问，不能自动让手机回答所有原生提问；手机答题需由 Codex 调用 TaskBridge MCP。无需修改全局 `AGENTS.md`。

## 故障处理

- `node` 版本低于 22 或 `npm` 缺失：使用上面的环境修复提示，打开新终端后重新运行首次部署脚本。
- `wrangler whoami` 失败：检查网络，完成注册和邮箱验证；在部署目录重跑 `bash scripts/first-run.sh`，向导会重新显示设备验证网址和短代码。若代码过期，重新运行即可。不要复制带 `code=` 的 OAuth 回调地址。
- D1 名称冲突：首次创建前设置其他 `TB_SETUP_DB_NAME`，或清理本次尚未部署的 `.local/setup.json` 后重试。不要删除已投入使用的 D1。
- `tb doctor` 返回 401：核对 Worker URL 和**该设备**的客户端令牌，不要把 ntfy 令牌或 Cloudflare 管理员密钥填入客户端令牌字段。
- `Worker health check request ... failed`：Worker 已部署，但服务器访问其 URL 失败。核对该 URL、服务器出站网络和 `HTTP_PROXY`/`HTTPS_PROXY`；向导会遵循这些代理变量，也会绕过代理访问本机地址。不要把 `.local/setup.json` 整份发给别人，其中有管理员密钥。
- `Worker health check failed`：部署刚完成时向导会重试约 20 秒；若仍失败，错误会附 HTTP 状态和 D1 状态。先访问显示的 `/health` 地址确认返回 `"db":"ok"`，然后在同一台电脑重跑安装器并选择 1，继续使用原有 D1 和令牌。
- 手机没有完成通知：确认 ntfy topic 已订阅、Codex Hook 已信任、`tb relay` 正在运行；可用 `tb notify --title 测试 "TaskBridge 已连接"` 验证客户端发送。
- 首次部署目录已有手工 `wrangler.jsonc`：保留它，继续按下节手动部署或更新；向导不会覆盖它。

## 手动部署 Worker

如果需要自定义 Worker 配置，可在克隆的源码目录执行：

```bash
npm ci
cp wrangler.example.jsonc wrangler.jsonc
npx wrangler login
npx wrangler d1 create taskbridge-db
```

把返回的 `database_id` 填入本机 `wrangler.jsonc`，然后执行：

```bash
npx wrangler d1 migrations apply taskbridge-db --remote
npx wrangler secret put TB_ADMIN_TOKEN
npm run deploy
```

管理员密钥请使用足够长的随机值并安全保管。`wrangler.jsonc`、`.local/`、`.dev.vars` 均被 Git 忽略。
