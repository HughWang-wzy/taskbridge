# TaskBridge

TaskBridge tracks long-running commands and Codex turns and sends completion, failure, question, and lost-task alerts to your phone through ntfy. A Cloudflare Worker with D1 stores state; a `tb` client runs on each computer. Each user deploys the Worker and D1 in **their own Cloudflare account**.

[简体中文](README.zh-CN.md) · [Deployment guide](docs/deployment.md) · [Architecture](docs/architecture.md) · [MIT license](LICENSE)

## First deployment

Have a Cloudflare account, Git, and the ntfy app on your phone. The wizard can install Node.js 22+, asks for a topic name, adds a random suffix, guides Cloudflare login, creates D1 and the Worker, and installs the local client. Subscribe to the generated topic on your phone when prompted.

Linux/macOS:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.4/scripts/first-run.sh)
```

Windows PowerShell:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.4/scripts/first-run.ps1 -OutFile first-run.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\first-run.ps1
```

## Add another computer

Create a separate client token on the deployment computer, then run the [client installer](docs/deployment.md#add-another-computer) on the new device. The new device does not need the Cloudflare admin secret.

## Customize Codex alerts

During installation you can set the task topic, completion title and body, and whether to send the final assistant reply to the phone. Templates support `{topic}`, `{duration}`, `{output}`, and other variables. Final-answer forwarding is off by default. See [Codex notification settings](docs/deployment.md#customize-codex-alerts).

## Documentation

- [Full deployment, signup, and environment repair](docs/deployment.md)
- [Architecture and notification flow](docs/architecture.md)
- [Releases](https://github.com/HughWang-wzy/taskbridge/releases)

The Codex MCP server enables phone-answerable questions, but registration does not route every native Codex prompt through it. Hooks can remind you of supported native prompts. Review and trust newly installed Hooks in Codex. No global `AGENTS.md` edits are needed.
