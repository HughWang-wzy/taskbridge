# TaskBridge

**30 秒部署，电脑跑任务，手机看结果。** 运行一个安装脚本，就能用 TaskBridge 把 Codex 任务、模型训练和其他长时间运行的命令接入手机通知。任务完成、失败或失联时，手机会收到提醒；离开电脑后，也能及时知道任务结果，不必反复回来查看终端。

安装器会引导你在**自己的 Cloudflare 账户**部署服务，并连接手机 ntfy。环境就绪时约 30 秒即可完成；首次注册 Cloudflare 或修复缺失工具会花更久。

[English](README.md) · [部署与故障处理](docs/deployment.zh-CN.md) · [架构](docs/architecture.md) · [MIT 许可证](LICENSE)

例如，安装完成后运行模型训练：

```bash
tb run -n "模型训练" -- python train.py
```

命令结束时，TaskBridge 会把结果推送到手机。安装 Codex Hook 后，也可以接收 Codex 任务提醒。

## 安装

首次使用时准备 Cloudflare 账户、Git 和手机 ntfy 应用。运行安装器后选择：

1. **新建服务**：在你自己的 Cloudflare 账户中创建 Worker 和 D1，再安装本机客户端。向导可协助安装 Node.js 22+；ntfy topic 只需输入易记前缀，向导会追加随机后缀。
2. **连接已有服务**：在另一台电脑安装客户端。先从原部署电脑创建该设备的客户端令牌，再输入已有 Worker URL、令牌和相同的 ntfy topic。详见[添加电脑](docs/deployment.zh-CN.md#添加另一台电脑)。

Linux/macOS：

```bash
bash <(curl -fsSL https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.sh)
```

Windows PowerShell：

```powershell
Invoke-WebRequest https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

## 这些名称是什么

| 名称 | 作用 |
| --- | --- |
| Cloudflare Worker | 运行 TaskBridge 服务端，管理任务状态、提问和待发送通知队列；它不是 ntfy topic。 |
| Cloudflare D1 | Cloudflare 提供的数据库，持久保存上述状态和队列。 |
| ntfy topic | 手机订阅的通知频道；各台电脑向它发送通知。 |
| `tb` | 安装在每台电脑上的客户端。 |

## 自定义 Codex 通知

安装时可设置任务名称、完成标题和正文，还可选择是否把最终回复发到手机。支持 `{topic}`、`{duration}`、`{output}` 等变量；最终回复默认关闭。示例和限制见 [Hook 通知配置](docs/deployment.zh-CN.md#自定义-codex-通知)。

## 文档

- [完整部署、注册与环境修复](docs/deployment.zh-CN.md)
- [架构与通知流程](docs/architecture.md)
- [发布包](https://github.com/HughWang-wzy/taskbridge/releases)

Codex MCP 仅提供手机答题能力，注册 MCP 不会自动接管每个原生提问框；Hook 可提醒受支持的原生提问。首次运行 Hook 时，需在 Codex 中查看并信任它们。无需修改全局 `AGENTS.md`。
