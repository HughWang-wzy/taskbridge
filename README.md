# TaskBridge

TaskBridge tracks long-running commands and Codex sessions, then sends notifications through [ntfy](https://ntfy.sh/). It uses a Cloudflare Worker with D1 for state and a small Go client (`tb`) on each computer. The Worker never publishes to ntfy; a client sends ordinary notifications directly, while an online `tb relay` delivers queued notifications.

[简体中文](README.zh-CN.md) · [Architecture](docs/architecture.md) · [License](LICENSE)

## What it does

- `tb run` records a command's start, heartbeats, duration, exit code, and final state. Failures always notify; successful commands notify after 60 seconds by default.
- Codex Hooks report completion, interruption, and heartbeats. A supported native question can trigger a phone reminder.
- The TaskBridge MCP server provides phone-answerable questions (`ask_user`) and tools to coordinate a question across phone and Codex (`begin_question`, `question_status`, `answer_question`). Registering MCP does **not** automatically route every native Codex prompt through it.
- A D1 queue stores LOST, recovery, and question notifications. Relays claim messages with a lease and ACK successful publication. Delivery is at least once.

## Requirements

- A Cloudflare account with Workers and D1
- A subscribed ntfy topic (use a long random topic name unless you have access controls)
- Node.js 22+ for deploying and testing the Worker
- Go 1.22+ to build `tb`, or a prebuilt release archive
- Codex CLI or a compatible Codex app only for the optional Codex integration

## Deploy the Worker

```bash
npm ci
cp wrangler.example.jsonc wrangler.jsonc
npx wrangler login
npx wrangler d1 create taskbridge-db
```

Paste the returned D1 `database_id` into the local `wrangler.jsonc`. Then apply every migration, set a long random admin secret, and deploy:

```bash
npx wrangler d1 migrations apply taskbridge-db --remote
npx wrangler secret put TB_ADMIN_TOKEN
npm run deploy
```

Save the admin secret securely. `wrangler.jsonc`, `.dev.vars`, and local credentials are excluded from Git. For local Worker development, copy `.dev.vars.example` to `.dev.vars` and replace its example value.

## Install a client

Download the archive for your platform from a release, or run `npm run build:cli`. The supported targets are Linux x86-64, Windows x86-64, macOS Intel, and macOS Apple Silicon. Put `tb` or `tb.exe` at a stable path; the examples below assume `tb` is on `PATH`.

Create a distinct client token for each computer. On your admin machine, initialize a separate admin config with the same `TB_ADMIN_TOKEN` you set in Cloudflare:

```bash
TB_CONFIG="$HOME/.config/taskbridge/admin.json" tb init --url https://YOUR_WORKER.YOUR_SUBDOMAIN.workers.dev
TB_CONFIG="$HOME/.config/taskbridge/admin.json" tb clients create workstation --scopes=tasks:write,tasks:read,notify:write,notifications:relay,codex:write,questions:write,questions:read
```

`tb init` prompts for the token and ntfy topic. The second command prints the new client token once; transfer it privately to that computer. Do not copy the admin config or reuse one client token across computers.

On the client computer:

```bash
tb init --url https://YOUR_WORKER.YOUR_SUBDOMAIN.workers.dev
tb doctor
tb notify --title "TaskBridge test" "This computer is connected"
tb run -n "Training" -- python train.py
```

`tb init` prompts for the client token and the same ntfy topic subscribed on your phone. The config is written with restricted permissions to the OS user config directory (`~/.config/taskbridge/config.json` on Linux). `TB_CONFIG` overrides its path. A successful command shorter than 60 seconds is recorded without a completion notification; a failure notifies at any duration.

## Keep a relay online

Run `tb relay` on at least one configured computer. D1 keeps queued notifications while all relays are offline. To deliver a Codex **Interrupt** notification, a relay must run on the computer where that interrupt occurred because the three-second Hook first writes a local spool.

On Linux, install `tb` as `~/.local/bin/tb`, copy [the systemd user service](deploy/taskbridge-relay.service) to `~/.config/systemd/user/taskbridge-relay.service`, and run:

```bash
systemctl --user daemon-reload
systemctl --user enable --now taskbridge-relay.service
systemctl --user status taskbridge-relay.service
```

On macOS or Windows, run `tb relay` with the platform's startup service manager. Multiple relays are safe: D1 grants each claim a lease token, and only a matching ACK marks it sent. A publish that succeeds before its ACK is lost may be retried, so ntfy delivery is at least once.

## Optional Codex integration

Give the Codex host a client token with `codex:write`, `questions:write`, `questions:read`, `notify:write`, and `notifications:relay`. Copy its normal TaskBridge config to `codex.json` in the same user config directory. On Linux:

```bash
cp ~/.config/taskbridge/config.json ~/.config/taskbridge/codex.json
chmod 600 ~/.config/taskbridge/codex.json
tb hook codex
codex mcp add taskbridge -- "$(command -v tb)" mcp
codex mcp list
```

Restart Codex, then review and trust the TaskBridge Hooks in `/hooks`. Hook commands run outside the Codex sandbox. `PreToolUse`, `PostToolUse`, and `Stop` have 30-second limits and run in the background; `Interrupt` has Codex's three-second maximum and only writes to the local spool. The relay later submits that notification through D1. No global `AGENTS.md` changes are required.

`PreToolUse` attempts a phone **reminder** for native prompts that use a supported local tool path. The reminder cannot answer the native prompt. Phone answers work when Codex explicitly calls a TaskBridge MCP question tool. If the phone answers first, the native desktop prompt may remain visible and need manual dismissal. Codex permission approvals are not routed through TaskBridge.

## Development and packaging

```bash
go test ./...
npm ci
cp wrangler.example.jsonc wrangler.jsonc
npm test
npm run typecheck
npm run package
```

`npm run package` creates four archives and `release/SHA256SUMS`. CI runs the same checks without deployment secrets. See [architecture and API notes](docs/architecture.md) for the delivery paths and trust boundaries.
