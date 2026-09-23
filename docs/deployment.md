# TaskBridge deployment

[README](../README.md) · [简体中文](deployment.zh-CN.md)

## Check your environment

The first deployment needs Git, network access to Cloudflare, and the ntfy app on your phone. The script checks and can install Node.js 22+ with npm. Linux/macOS client installation also needs `curl`, `tar`, and a SHA-256 utility. Prebuilt clients support Linux x86-64, macOS Intel/Apple Silicon, and Windows x86-64; Linux ARM64 requires a source build.

The first-run script checks Git, Node.js, and npm. If something is missing or outdated, it offers repair: Homebrew on macOS; existing nvm on Linux, or a SHA-256-verified download from the [official Node.js 22 release directory](https://nodejs.org/download/release/latest-v22.x/) into `~/.local/share/taskbridge/node-22`; winget on Windows. Linux can install Git through apt/dnf, running directly as root when appropriate. The client installer also checks download, archive, and SHA-256 tools. Worker checks during first deployment use existing HTTP/HTTPS proxy environment variables. No system packages are changed silently.

## Create a Cloudflare account

1. Open the [Cloudflare signup page](https://dash.cloudflare.com/sign-up). Enter your email and password and create the account.
2. Verify your email using Cloudflare's message.
3. Return to the wizard and press Enter. Wrangler prints a device verification URL and short code. Open the URL in a browser on your computer or phone, enter the code, and approve access.
4. Wait for `whoami` to succeed. [Cloudflare's device authorization flow](https://developers.cloudflare.com/workers/wrangler/commands/general/) does not use `localhost:8976`, so it works over remote SSH. D1 and the Worker will be created in the signed-in Cloudflare account.

You complete signup and verification on Cloudflare's site; TaskBridge does not collect the password. See [Cloudflare's D1 prerequisites](https://developers.cloudflare.com/d1/get-started/).

## Deploy for the first time

Use the same installer on every computer. Choose **1: create a new Cloudflare Worker and D1** for a first deployment, or **2: connect to an existing Worker** on another computer. The Worker runs the TaskBridge server; D1 is Cloudflare’s database for tasks, questions, and pending notifications; the ntfy topic is the channel your phone subscribes to.

Linux/macOS:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.7/scripts/install.sh)
```

Windows PowerShell:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.7/scripts/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

The wrapper clones the repository into `~/taskbridge` by default, installs dependencies, and runs the wizard. Enter an easy name such as `Cospeak3`; the wizard appends 32 random hexadecimal characters, displays the full topic, and waits while you subscribe to it on your phone. Then it creates a separate D1 and Worker, applies migrations, sets a random admin secret, checks `/health`, creates this computer's client token, and starts the client installer. The installer offers Codex Hook/MCP integration and a local relay.

Keep the deployment checkout's `.local/` directory. It contains the admin configuration and recovery state, is ignored by Git, and should not be copied to another computer. If setup stops, retry in the same checkout:

```bash
bash ~/taskbridge/scripts/first-run.sh
```

On Windows, rerun `powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\first-run.ps1` from the deployment checkout.

Rerunning the installer and choosing 1 updates an existing clean source checkout while preserving `.local/` deployment state. A retry reuses the D1, admin secret, and local client token. An unfinished v0.4.1 state gets a random topic suffix; a completed deployment keeps its topic. The wizard stops if the signed-in account cannot see the saved D1, or if an existing hand-written `wrangler.jsonc` has no wizard state.

## Add another computer

On the deployment computer, create a distinct client token. Linux/macOS example:

```bash
cd ~/taskbridge
TB_CONFIG="$PWD/.local/admin.json" "$HOME/.local/bin/tb" clients create laptop --scopes=tasks:write,tasks:read,notify:write,notifications:relay,codex:write,questions:write,questions:read
```

Windows PowerShell:

```powershell
Set-Location (Join-Path $HOME 'taskbridge')
$env:TB_CONFIG = (Join-Path (Get-Location) '.local/admin.json')
& "$env:LOCALAPPDATA\Programs\TaskBridge\tb.exe" clients create laptop --scopes=tasks:write,tasks:read,notify:write,notifications:relay,codex:write,questions:write,questions:read
Remove-Item Env:TB_CONFIG
```

Transfer the displayed token privately. Run the same installer on the new computer and choose **2**. Enter the Worker URL, this device token, and the same ntfy topic. It needs no admin secret and creates no D1 database.

Linux/macOS installer:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.7/scripts/install.sh)
```

Windows PowerShell installer:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.7/scripts/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

The installer checks the release SHA-256 and runs `tb doctor`. It keeps an existing configuration by default. At least one computer must run `tb relay` to deliver queued notices. An interrupted Codex turn first spools locally, so that computer also needs a relay. On Linux, ordinary users get a systemd user service and root prefers a system service. If no service manager is available, the installer starts a temporary background relay and warns that it will not restart after a reboot; use the host's startup manager for persistence.

## Customize Codex alerts

The installer offers completion notification settings when you enable Codex integration. You can also change them later:

```bash
"$HOME/.local/bin/tb" hook codex --topic "Training" --title "{topic} finished" --body "{topic} task ended after {duration}" --final-output off
```

On Windows, replace the executable with `& "$env:LOCALAPPDATA\Programs\TaskBridge\tb.exe"`. Set `--final-output on` to append the final assistant reply, or place `{output}` in the body template. This is off by default because the reply may contain sensitive content. At most the first 2,000 characters are sent. If Codex does not provide `last_assistant_message`, the template is sent without it.

| Variable | Meaning |
| --- | --- |
| `{topic}` | Configured name, or the project directory name |
| `{duration}` | Time from `UserPromptSubmit` to `Stop`; `unknown` when no start event was received |
| `{output}` | Final reply, only when `--final-output on` |
| `{session}`, `{turn}` | Codex session and turn IDs |

Restart Codex and review/trust the Hooks in `/hooks` after installation or modification. `UserPromptSubmit` records the start; `Stop` sends the completion notice. `Interrupt` remains limited to three seconds by Codex. These events and `last_assistant_message` are described in the [official Codex Hooks documentation](https://learn.chatgpt.com/docs/hooks). Hooks can remind you of supported native questions but cannot route every native prompt to the phone. Phone-answerable questions require an explicit TaskBridge MCP call. No global `AGENTS.md` change is needed.

## Troubleshoot

- Node.js below 22 or missing npm: use the environment repair guidance, open a new terminal, and rerun first-run.
- `wrangler whoami` fails: finish Cloudflare signup and email verification, check network access, then rerun `bash scripts/first-run.sh` from the deployment checkout. The wizard prints a new device verification URL and code. If the code expires, rerun it; do not copy an OAuth callback URL containing `code=`.
- D1 name conflict: choose a different `TB_SETUP_DB_NAME` before the first create. Do not delete a live database.
- `tb doctor` returns 401: verify the Worker URL and this device's client token. Do not enter an ntfy token or the Cloudflare admin secret as the client token.
- `Worker health check request ... failed`: the Worker was deployed, but the server could not reach its URL. Check that URL, server egress, and `HTTP_PROXY`/`HTTPS_PROXY`. The wizard uses those proxy variables and bypasses the proxy for local addresses. Do not share the complete `.local/setup.json`; it contains the admin secret.
- `Worker health check failed`: the wizard retries for about 20 seconds after deployment and includes the HTTP and D1 status if it still fails. Check that the printed `/health` URL returns `"db":"ok"`, then rerun the installer on the same computer and choose 1 to reuse the existing D1 and token.
- No phone notice: check the subscribed topic, trusted Codex Hooks, and running relay. Try `tb notify --title Test "TaskBridge connected"`.
- Existing manual `wrangler.jsonc`: keep it and use the manual steps below; the wizard will not overwrite it.

## Deploy the Worker manually

To customize the Worker configuration, from a source checkout run:

```bash
npm ci
cp wrangler.example.jsonc wrangler.jsonc
npx wrangler login
npx wrangler d1 create taskbridge-db
```

Put the returned `database_id` into the local `wrangler.jsonc`, then run:

```bash
npx wrangler d1 migrations apply taskbridge-db --remote
npx wrangler secret put TB_ADMIN_TOKEN
npm run deploy
```

Use a long random admin secret and keep it private. `wrangler.jsonc`, `.local/`, and `.dev.vars` are ignored by Git.
