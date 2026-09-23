# TaskBridge

TaskBridge tracks long-running commands and Codex turns and sends completion, failure, question, and lost-task alerts to your phone through ntfy. Each user deploys the server in **their own Cloudflare account**.

[简体中文](README.zh-CN.md) · [Deployment guide](docs/deployment.md) · [Architecture](docs/architecture.md) · [MIT license](LICENSE)

## Install

For a first deployment, have a Cloudflare account, Git, and the ntfy app on your phone. The installer asks you to choose:

1. **Create a new service** in your Cloudflare account, then install this computer's client. The wizard can help install Node.js 22+ and adds a random suffix to your chosen ntfy topic prefix.
2. **Connect to an existing service** on another computer. First create a separate client token on the deployment computer, then enter its Worker URL, that token, and the same ntfy topic. See [Add another computer](docs/deployment.md#add-another-computer).

Linux/macOS:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.7/scripts/install.sh)
```

Windows PowerShell:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.7/scripts/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

## What the components do

| Component | Purpose |
| --- | --- |
| Cloudflare Worker | Runs the TaskBridge server, managing task state, questions, and the pending notification queue. It is not the ntfy topic. |
| Cloudflare D1 | Cloudflare database that persists that state and queue. |
| ntfy topic | Notification channel subscribed to on your phone; computers publish notices there. |
| `tb` | Client installed on each computer. |

## Customize Codex alerts

During installation you can set the task topic, completion title and body, and whether to send the final assistant reply to the phone. Templates support `{topic}`, `{duration}`, `{output}`, and other variables. Final-answer forwarding is off by default. See [Codex notification settings](docs/deployment.md#customize-codex-alerts).

## Documentation

- [Full deployment, signup, and environment repair](docs/deployment.md)
- [Architecture and notification flow](docs/architecture.md)
- [Releases](https://github.com/HughWang-wzy/taskbridge/releases)

The Codex MCP server enables phone-answerable questions, but registration does not route every native Codex prompt through it. Hooks can remind you of supported native prompts. Review and trust newly installed Hooks in Codex. No global `AGENTS.md` edits are needed.
