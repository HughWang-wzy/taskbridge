# TaskBridge

**30 秒部署，电脑跑任务，手机看结果。** 运行一个安装脚本，就能用 TaskBridge 把 Codex 任务、模型训练和其他长时间运行的命令接入手机通知。任务完成、失败或失联时，手机会收到提醒；离开电脑后，也能及时知道任务结果，不必反复回来查看终端。

安装器会引导你在**自己的 Cloudflare 账户**部署服务，并连接手机 ntfy。环境就绪时约 30 秒即可完成；首次注册 Cloudflare 或修复缺失工具会花更久。

[English](README.md) · [部署与故障处理](docs/deployment.zh-CN.md) · [架构](docs/architecture.md) · [MIT 许可证](LICENSE)

例如，安装完成后运行模型训练：

```bash
tb run python train.py
```

命令结束时，TaskBridge 会把结果推送到手机，并自动用 `train` 作为任务名。安装 Codex Hook 后，也可以接收 Codex 任务提醒。

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

## 日常使用

在终端输入 `tb`，用方向键选择运行任务、常用任务、提醒设置、测试提醒或检查连接。长命令可直接粘贴，运行前会显示完整命令和工作目录；确认后回到正常终端，训练日志和进度条照常显示。

```bash
tb                 # 打开主菜单
tb settings        # 调整提醒等级，无需记参数
tb run python train.py  # 直接运行，自动命名为 train
```

常跑的命令可以收藏，之后从菜单选择、检查输出目录并运行，也能直接用短命令启动：

```bash
tb tasks save training -n "模型训练" -- python train.py
tb tasks run training
```

菜单只在交互终端中打开；`tb --help` 随时查看命令帮助，脚本和自动化继续使用明确的子命令。详见[运行与收藏任务](docs/deployment.zh-CN.md#运行与收藏任务)。

## 这些名称是什么

| 名称 | 作用 |
| --- | --- |
| Cloudflare Worker | 运行 TaskBridge 服务端，管理任务状态、提问和待发送通知队列；它不是 ntfy topic。 |
| Cloudflare D1 | Cloudflare 提供的数据库，持久保存上述状态和队列。 |
| ntfy topic | 手机订阅的通知频道；各台电脑向它发送通知。 |
| `tb` | 安装在每台电脑上的客户端。 |

## 自定义 Codex 通知

安装时可设置任务名称、完成标题和正文，还可选择是否把最终回复发到手机。支持 `{topic}`、`{duration}`、`{output}` 等变量；最终回复默认关闭。示例和限制见 [Hook 通知配置](docs/deployment.zh-CN.md#自定义-codex-通知)。

## 设置提醒等级

运行 `tb settings` 打开提醒设置，按 ↑↓ 选择事件、←→ 调整等级，Enter 保存全部修改；Esc 放弃本次修改。也可选“低打扰 / 标准 / 重要任务”预设，或直接保存预设：

```bash
tb alerts --preset low-noise
tb alerts                    # 查看当前等级
```

低打扰预设把正常完成、Codex 中断和手动通知设为 2，失败和提问设为 4，失联设为 5。等级为 `min`（1）到 `urgent`（5）。Android 可在 ntfy 的 **Channel settings（通知渠道设置）** 中为各等级设置声音和振动；iPhone 的声音还受 iOS 权限、静音和专注模式控制，`urgent` 不保证强制响铃。详见[预设、单次任务覆盖与手机设置](docs/deployment.zh-CN.md#设置提醒等级)。

## 文档

- [完整部署、注册与环境修复](docs/deployment.zh-CN.md)
- [架构与通知流程](docs/architecture.md)
- [发布包](https://github.com/HughWang-wzy/taskbridge/releases)

Codex MCP 仅提供手机答题能力，注册 MCP 不会自动接管每个原生提问框；Hook 可提醒受支持的原生提问。首次运行 Hook 时，需在 Codex 中查看并信任它们。无需修改全局 `AGENTS.md`。
