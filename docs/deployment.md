# TaskBridge deployment

[README](../README.md) · [简体中文](deployment.zh-CN.md)

## Check your environment

The first deployment needs Git, Node.js 22+ with npm, network access to Cloudflare, and an ntfy topic subscribed on your phone. Linux/macOS client installation also needs `curl`, `tar`, and a SHA-256 utility. Prebuilt clients support Linux x86-64, macOS Intel/Apple Silicon, and Windows x86-64; Linux ARM64 requires a source build.

The first-run script checks Git, Node.js, and npm. If something is missing or outdated, it offers a repair using an available manager: Homebrew on macOS; apt/dnf for Git and existing nvm for Node.js on Linux; winget on Windows. The client installer also checks download, archive, and SHA-256 tools and offers an apt/dnf repair on supported Linux systems. Otherwise, use the [Node.js installer](https://nodejs.org/en/download) and [Git downloads](https://git-scm.com/downloads), open a new terminal, and retry. No system packages are changed silently.

## Create a Cloudflare account

1. Open the [Cloudflare signup page](https://dash.cloudflare.com/sign-up). Enter your email and password and create the account.
2. Verify your email using Cloudflare's message.
3. Return to the wizard and press Enter. Wrangler opens a browser to authorize this computer.
4. Wait for `whoami` to succeed. D1 and the Worker will be created in the signed-in Cloudflare account.

You complete signup and verification on Cloudflare's site; TaskBridge does not collect the password. See [Cloudflare's D1 prerequisites](https://developers.cloudflare.com/d1/get-started/).

## Deploy for the first time

Linux/macOS:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.0/scripts/first-run.sh)
```

Windows PowerShell:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.0/scripts/first-run.ps1 -OutFile first-run.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\first-run.ps1
```

The wrapper clones the repository into `~/taskbridge` by default, installs dependencies, and runs the wizard. Supply the ntfy topic already subscribed on your phone. The wizard creates a separate D1 and Worker, applies migrations, sets a random admin secret, checks `/health`, creates this computer's client token, and starts the client installer. The installer then offers Codex Hook/MCP integration and a local relay.

Keep the deployment checkout's `.local/` directory. It contains the admin configuration and recovery state, is ignored by Git, and should not be copied to another computer. If setup stops, retry in the same checkout:

```bash
cd ~/taskbridge
npm run setup
```

A retry reuses the D1, admin secret, and local client token. The wizard stops if the signed-in account cannot see the saved D1, or if an existing hand-written `wrangler.jsonc` has no wizard state.

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

Transfer the displayed token privately. The new computer needs the Worker URL, this device token, and the same ntfy topic; it does not need the admin secret.

Linux/macOS client installer:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.0/scripts/install.sh)
```

Windows PowerShell client installer:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/HughWang-wzy/taskbridge/v0.4.0/scripts/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

The installer checks the release SHA-256 and runs `tb doctor`. It keeps an existing configuration by default. At least one computer must run `tb relay` to deliver queued notices. An interrupted Codex turn first spools locally, so that computer also needs a relay.

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
- `wrangler whoami` fails: finish Cloudflare signup and email verification, check network access, run `npx wrangler login`, then retry `npm run setup`.
- D1 name conflict: choose a different `TB_SETUP_DB_NAME` before the first create. Do not delete a live database.
- `tb doctor` returns 401: verify the Worker URL and this device's client token. Do not enter an ntfy token or the Cloudflare admin secret as the client token.
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
