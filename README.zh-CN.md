# TaskBridge

TaskBridge 记录长时间运行的命令和 Codex 任务，并通过 ntfy 向手机发送完成、失败、提问和失联提醒。Cloudflare Worker + D1 保存状态；每台电脑运行 `tb` 客户端。每位使用者在**自己的 Cloudflare 账户**部署服务端。

[English](README.md) · [部署与故障处理](docs/deployment.zh-CN.md) · [架构](docs/architecture.md) · [MIT 许可证](LICENSE)

## 首次部署

准备 Cloudflare 账户、Git 和手机 ntfy 应用。向导可安装 Node.js 22+；你只需输入喜欢的 topic 前缀，它会追加随机后缀，再提示你在手机订阅完整名称。随后向导会登录 Cloudflare、创建 D1 和 Worker，并安装本机客户端。

Linux/macOS：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.4/scripts/first-run.sh)
```

Windows PowerShell：

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.4/scripts/first-run.ps1 -OutFile first-run.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\first-run.ps1
```

## 添加电脑

先在部署电脑为新设备创建独立客户端令牌，再在新电脑运行 [客户端安装器](docs/deployment.zh-CN.md#添加另一台电脑)。新电脑无需 Cloudflare 管理员密钥。

## 自定义 Codex 通知

安装时可设置任务名称、完成标题和正文，还可选择是否把最终回复发到手机。支持 `{topic}`、`{duration}`、`{output}` 等变量；最终回复默认关闭。示例和限制见 [Hook 通知配置](docs/deployment.zh-CN.md#自定义-codex-通知)。

## 文档

- [完整部署、注册与环境修复](docs/deployment.zh-CN.md)
- [架构与通知流程](docs/architecture.md)
- [发布包](https://github.com/HughWang-wzy/taskbridge/releases)

Codex MCP 仅提供手机答题能力，注册 MCP 不会自动接管每个原生提问框；Hook 可提醒受支持的原生提问。首次运行 Hook 时，需在 Codex 中查看并信任它们。无需修改全局 `AGENTS.md`。
