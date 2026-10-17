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
bash <(curl -fsSL https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.sh)
```

Windows PowerShell:

```powershell
Invoke-WebRequest https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.ps1 -OutFile install.ps1
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
bash <(curl -fsSL https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.sh)
```

Windows PowerShell installer:

```powershell
Invoke-WebRequest https://github.com/HughWang-wzy/taskbridge/releases/latest/download/install.ps1 -OutFile install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

The installer checks the release SHA-256 and runs `tb doctor`. It keeps an existing configuration by default. At least one computer must run `tb relay` to deliver queued notices. An interrupted Codex turn first spools locally, so that computer also needs a relay. On Linux, ordinary users get a systemd user service and root prefers a system service. If no service manager is available, the installer starts a temporary background relay and warns that it will not restart after a reboot; use the host's startup manager for persistence.

## Run and save tasks

After installation, enter `tb` or `tb menu` in an interactive terminal. Use ↑↓ to select and Enter to open, or press 1–9 to choose directly; Esc returns. The main menu offers task execution, saved tasks, reminder settings, test notifications, and connection checks. `tb --help` always displays help; without an interactive terminal, `tb` also prints help. `tb menu` and `tb settings` require an interactive terminal. Use explicit subcommands in scripts and automation.

### Run a command directly

Prefix your existing command with `tb run`. Reminder levels come from the defaults saved on this computer:

```bash
tb run .venv/bin/python scripts/run_mouth_parallel.py --help
```

The task is named `run_mouth_parallel` automatically. `python scripts/train.py` becomes `train`, and `python -m package` becomes `package`. Supply `-n` to choose another name. The standalone `--` separator is optional; keep it to mark the start of the command explicitly. TaskBridge flags go before the command being run:

```bash
tb run -n "Model training" -p high -- python train.py
```

Successful tasks notify after running for at least 60 seconds by default; failures notify regardless of duration. The command runs in the current terminal. TaskBridge waits for it and preserves its exit code.

### Paste a long command in the menu

Choose Run task and paste the command you would normally execute. Paste multiple lines directly or use Ctrl+J to insert a newline. Enter submits, unless the line ends with a continuation character (`\` on Linux/macOS, `^` on Windows). Edit with arrow keys, Home, End, and Backspace; Ctrl+U clears the field and Esc cancels. Leave the task name blank to infer it from the script.

Linux/macOS commands run through `/bin/sh -c`, with shell quoting and redirection but without your interactive shell aliases. Windows uses the command processor specified by `ComSpec` (usually `cmd.exe`) and cmd syntax. Invoke Bash or PowerShell explicitly if a command depends on their specific syntax.

Review the full command and working directory before confirming execution; use PgUp/PgDn to scroll through long details. The menu closes and restores the terminal before forwarding standard input, output, and error to the command. Training logs, progress bars, and interactive input keep working in the terminal; no full-screen dashboard remains open while the task runs.

### Save recurring commands

Use Saved tasks to save, edit, or delete a command. A favorite contains an identifier, display name, command, and working directory. Preview it before running, check parameters such as output directories, and edit them first when needed. CLI-created favorites edit arguments individually; favorites pasted through the menu edit the original shell text. Deletion requires confirmation.

Manage favorites from the command line as well:

```bash
tb tasks save mouth -n "Mouth training" -- .venv/bin/python scripts/run_mouth_parallel.py --help
tb tasks list
tb tasks run mouth
tb tasks remove mouth
```

`save` records the current working directory. `run` executes immediately from that directory without a confirmation menu, making it suitable for scripts; relative paths work even when you launch it elsewhere. CLI saves retain argument boundaries; menu saves retain explicit shell command text. Favorites live in `tasks.json` beside this computer's client configuration. They are not uploaded to Cloudflare or automatically synchronized to another computer.

## Choose reminder levels

TaskBridge attaches an ntfy priority to each notification. Configure different levels for different events, then choose their sound and vibration on the phone. Run `tb settings` or choose Reminder settings in the `tb` menu. Use ↑↓ to select the preset or an event and ←→ to change the preset or adjust a level from 1 to 5. Choose Low noise, Standard, or Important tasks as a starting point, then adjust individual values. Enter saves all changes from any row; Esc discards changes.

For fewer interruptions, save the Low noise preset once. Future `tb run` commands use it automatically:

```bash
tb alerts --preset low-noise
tb alerts
```

| Preset | Success / recovered | Failure | Question | Codex interruption | Lost contact | Manual |
| --- | --- | --- | --- | --- | --- | --- |
| Low noise `low-noise` | 2 | 4 | 4 | 2 | 5 | 2 |
| Standard `standard` | 3 | 4 | 4 | 4 | 5 | 3 |
| Important tasks `important` | 4 | 5 | 4 | 4 | 5 | 4 |

Use `standard` to restore the original defaults. Explicit flags remain available for precise settings:

```bash
tb alerts --success low --failure high --question high --interrupt low --lost urgent --manual normal
tb alerts
```

The command saves the defaults to the client `config.json` and, when present, the Codex `codex.json` on this computer. When `TB_CONFIG` is explicitly set, it updates only that file. Settings are local to each computer; apply them on other computers as needed. A running Codex MCP server reads settings at startup, so restart Codex/MCP after changing question defaults. Completion Hooks read the new values on their next invocation.

| Level | Accepted values | Android's default behavior |
| --- | --- | --- |
| Minimum | `1`, `min`, `silent` | No sound or vibration; placed in other notifications |
| Low | `2`, `low` | No sound or vibration |
| Normal | `3`, `normal`, `default` | Default sound and short vibration |
| High | `4`, `high` | Sound, longer vibration, and a pop-over |
| Urgent | `5`, `urgent`, `max` | Sound, longer vibration, and a pop-over |

Names are case-insensitive. These are [ntfy's priority defaults](https://docs.ntfy.sh/publish/#message-priority); phone settings can change the behavior. `silent` selects priority 1 and does not force the phone to obey a particular sound setting.

Existing defaults remain unchanged until you configure them:

| Event | Default |
| --- | --- |
| Successful completion, recovered task, manual notification | `normal` (3) |
| Failure, question awaiting an answer, interrupted Codex turn | `high` (4) |
| Task loses contact (`LOST`) | `urgent` (5) |

To override one manual notification:

```bash
tb notify -p low --title Test 'Quiet notification'
```

For a training job, choose separate successful-completion, failure, and lost-contact levels:

```bash
tb run -n training --priority high --failure-priority urgent --lost-priority urgent -- python train.py
```

For `tb run`, `--priority` (or `-p`) affects successful completion only. It does not bypass the existing 60-second threshold for successful-task notifications; failures and lost-contact events use their own levels. Recovered tasks use the successful-completion level. The relay sends the priority stored with each queued notification, so changing defaults does not change already queued messages.

For Codex, change completion, question, and interruption levels without changing the notification text:

```bash
tb hook codex --priority low --question-priority high --interrupt-priority low
```

You can combine these flags with the title and body flags below. After changing Hooks, restart Codex and review/trust any updated Hooks. Upgrade the `tb` client on each sending computer. Custom `LOST` levels and MCP question levels also need the updated Worker deployed; no D1 migration is needed. From the deployment checkout with its existing `wrangler.jsonc`, run `npm run deploy` after updating the source.

### Set up your phone

**Android:** Open ntfy → Settings → **Channel settings**, or open Android's notification settings for ntfy. Each priority has its own channel; choose sound, vibration, pop-over, and whether to override Do Not Disturb for that channel. A higher priority alone does not guarantee it bypasses your system settings. See [ntfy's phone configuration guide](https://docs.ntfy.sh/subscribe/phone/#message-priority).

**iPhone:** Enable ntfy notifications and sounds in iOS Settings, and check your Focus configuration. The current stable ntfy iOS app is v1.7.0 as of October 18, 2026; priority-5 critical alerts that bypass silent mode and Do Not Disturb are listed for the unreleased v1.8.0. Do not rely on `urgent` forcing an audible alert on the current stable app. It also does not offer Android's per-priority notification channels. See the [official release notes](https://docs.ntfy.sh/releases/#ntfy-ios-app-v180-unreleased) and [current iOS settings implementation](https://github.com/binwiederhier/ntfy-ios/blob/v1.7.0/ntfy/Views/Settings/SettingsView.swift).

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
