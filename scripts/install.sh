#!/usr/bin/env bash
set -euo pipefail

release_base="${TB_RELEASE_BASE:-https://github.com/HughWang-wzy/taskbridge/releases/latest/download}"
install_dir="${TB_INSTALL_DIR:-$HOME/.local/bin}"
if [[ "$(uname -s)" == Darwin ]]; then
  config_dir="$HOME/Library/Application Support/taskbridge"
else
  config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/taskbridge"
fi
config_file="$config_dir/config.json"

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) platform=linux-amd64 ;;
  Darwin-x86_64) platform=darwin-amd64 ;;
  Darwin-arm64) platform=darwin-arm64 ;;
  *) echo "Unsupported platform. Build from source with scripts/build.sh." >&2; exit 2 ;;
esac

install_mode="${TB_INSTALL_MODE:-}"
if [[ -z "$install_mode" ]]; then
  if [[ -n "${TB_WORKER_URL:-}" || -n "${TB_CLIENT_TOKEN:-}" ]]; then
    install_mode=client
  elif [[ -t 0 ]]; then
    echo "TaskBridge installation:"
    echo "  1) Create a new Cloudflare Worker and D1 database"
    echo "  2) Connect this computer to an existing Worker"
    default_choice=1
    [[ -f "$config_file" ]] && default_choice=2
    read -r -p "Choose 1 or 2 [$default_choice]: " install_mode </dev/tty
    install_mode="${install_mode:-$default_choice}"
  elif [[ -f "$config_file" ]]; then
    install_mode=client
  else
    echo "Choose TB_INSTALL_MODE=deploy or TB_INSTALL_MODE=client for noninteractive setup." >&2
    exit 2
  fi
fi
case "$install_mode" in
  1|deploy)
    command -v curl >/dev/null 2>&1 || { echo "curl is required for first deployment" >&2; exit 2; }
    bootstrap="$(mktemp)"
    trap 'rm -f "$bootstrap"' EXIT
    curl -fsSL "${TB_FIRST_RUN_SCRIPT_URL:-https://github.com/HughWang-wzy/taskbridge/releases/latest/download/first-run.sh}" -o "$bootstrap"
    bash "$bootstrap"
    exit
    ;;
  2|client) ;;
  *) echo "Choose 1 (new deployment) or 2 (existing Worker)." >&2; exit 2 ;;
esac

missing=()
for program in curl tar awk; do
  command -v "$program" >/dev/null 2>&1 || missing+=("$program")
done
if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  missing+=("SHA-256 utility")
fi
if ((${#missing[@]})); then
  echo "Missing installation tools: ${missing[*]}" >&2
  if [[ "$(uname -s)" == Linux && -t 0 ]]; then
    read -r -p 'Install required tools using this system package manager? [y/N] ' repair </dev/tty
    if [[ "$repair" =~ ^[Yy]([Ee][Ss])?$ ]]; then
      if command -v apt-get >/dev/null 2>&1; then
        sudo apt-get update && sudo apt-get install -y curl tar gawk coreutils
      elif command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y curl tar gawk coreutils
      fi
    fi
  fi
  for program in curl tar awk; do
    command -v "$program" >/dev/null 2>&1 || { echo "Still missing: $program. See https://github.com/HughWang-wzy/taskbridge/blob/main/docs/deployment.zh-CN.md" >&2; exit 2; }
  done
  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    echo "Still missing: SHA-256 utility. See the deployment guide." >&2
    exit 2
  fi
fi

archive="taskbridge-$platform.tar.gz"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
curl -fsSL "$release_base/$archive" -o "$work_dir/$archive"
curl -fsSL "$release_base/SHA256SUMS" -o "$work_dir/SHA256SUMS"
expected="$(awk -v name="./$archive" '$2 == name {print $1}' "$work_dir/SHA256SUMS")"
[[ -n "$expected" ]] || { echo "Release checksum is missing for $archive" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$work_dir/$archive" | awk '{print $1}')"
else
  command -v shasum >/dev/null 2>&1 || { echo "A SHA-256 utility is required" >&2; exit 2; }
  actual="$(shasum -a 256 "$work_dir/$archive" | awk '{print $1}')"
fi
[[ "$actual" == "$expected" ]] || { echo "Release checksum mismatch" >&2; exit 1; }

tar -xzf "$work_dir/$archive" -C "$work_dir"
mkdir -p "$install_dir"
tb_path="$install_dir/tb"
install -m 755 "$work_dir/taskbridge-$platform/tb" "$tb_path"
echo "Installed $tb_path"

prompt() {
  local label="$1" result
  [[ -t 0 ]] || { echo "Interactive terminal required for $label" >&2; return 1; }
  read -r -p "$label" result </dev/tty
  printf '%s' "$result"
}

if [[ ! -f "$config_file" || "${TB_RECONFIGURE:-0}" == 1 ]]; then
  worker_url="${TB_WORKER_URL:-}"
  topic="${TB_NTFY_TOPIC:-}"
  token="${TB_CLIENT_TOKEN:-}"
  [[ -n "$worker_url" ]] || worker_url="$(prompt 'Worker URL: ')"
  [[ -n "$topic" ]] || topic="$(prompt 'ntfy topic: ')"
  if [[ -z "$token" ]]; then
    [[ -t 0 ]] || { echo "Interactive terminal required for client token" >&2; exit 1; }
    read -r -s -p 'Device client token: ' token </dev/tty
    printf '\n' >/dev/tty
  fi
  TB_TOKEN="$token" TB_NTFY_TOPIC="$topic" "$tb_path" init --url "$worker_url"
  unset token
else
  echo "Keeping existing $config_file (set TB_RECONFIGURE=1 to replace it)."
fi

"$tb_path" doctor

enable_codex="${TB_ENABLE_CODEX:-}"
if [[ -z "$enable_codex" && -t 0 ]]; then
  enable_codex="$(prompt 'Install Codex Hooks and MCP? [y/N] ')"
fi
if [[ "$enable_codex" =~ ^[Yy]([Ee][Ss])?$|^1$ ]]; then
  command -v codex >/dev/null 2>&1 || { echo "Codex CLI not found; skipping Codex integration" >&2; exit 1; }
  if [[ ! -f "$config_dir/codex.json" || "${TB_RECONFIGURE:-0}" == 1 ]]; then
    install -m 600 "$config_file" "$config_dir/codex.json"
  fi
  customize="${TB_CUSTOMIZE_CODEX:-}"
  if [[ -z "$customize" && -t 0 ]]; then
    customize="$(prompt 'Customize Codex completion notifications? [y/N] ')"
  fi
  if [[ "$customize" =~ ^[Yy]([Ee][Ss])?$|^1$ ]]; then
    hook_topic="${TB_CODEX_TOPIC:-}"
    hook_title="${TB_CODEX_TITLE:-}"
    hook_body="${TB_CODEX_BODY:-}"
    hook_output="${TB_CODEX_FINAL_OUTPUT:-}"
    [[ -n "$hook_topic" ]] || hook_topic="$(prompt 'Topic name (e.g. Training): ')"
    [[ -n "$hook_title" ]] || hook_title="$(prompt 'Title template [{topic} finished]: ')"
    [[ -n "$hook_body" ]] || hook_body="$(prompt 'Body template [{topic} task finished; Duration: {duration}]: ')"
    [[ -n "$hook_output" ]] || hook_output="$(prompt 'Include final Codex answer on phone? [y/N] ')"
    [[ "$hook_output" =~ ^[Yy]([Ee][Ss])?$|^1$ ]] && hook_output=on || hook_output=off
    "$tb_path" hook codex --topic "$hook_topic" --title "$hook_title" --body "$hook_body" --final-output "$hook_output"
  else
    "$tb_path" hook codex
  fi
  if codex mcp get taskbridge >/dev/null 2>&1; then
    echo "TaskBridge MCP already exists; check its binary path with: codex mcp get taskbridge"
  else
    codex mcp add taskbridge -- "$tb_path" mcp
  fi
  echo "Restart Codex and review the new Hooks in /hooks."
fi

enable_relay="${TB_ENABLE_RELAY:-}"
if [[ -z "$enable_relay" && -t 0 ]]; then
  enable_relay="$(prompt 'Start a background relay on this computer? [Y/n] ')"
fi
start_user_systemd_relay() {
  local unit_dir unit_file
  unit_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
  mkdir -p "$unit_dir" || return 1
  unit_file="$unit_dir/taskbridge-relay.service"
  if [[ ! -e "$unit_file" || "${TB_RECONFIGURE:-0}" == 1 ]]; then
    cat > "$unit_file" <<EOF
[Unit]
Description=TaskBridge pending notification relay
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$tb_path relay --interval=20s
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
EOF
  fi
  systemctl --user daemon-reload &&
    systemctl --user enable --now taskbridge-relay.service &&
    systemctl --user restart taskbridge-relay.service &&
    systemctl --user is-active --quiet taskbridge-relay.service || return 1
  echo "Relay started with systemd user service."
}

start_systemd_root_relay() {
  local unit_dir unit_file
  unit_dir="${TB_SYSTEMD_UNIT_DIR:-/etc/systemd/system}"
  mkdir -p "$unit_dir" || return 1
  unit_file="$unit_dir/taskbridge-relay.service"
  if [[ -e "$unit_file" && "${TB_RECONFIGURE:-0}" != 1 ]] && ! grep -Fq "ExecStart=$tb_path relay" "$unit_file"; then
    echo "Existing $unit_file belongs to another relay; leaving it untouched." >&2
    return 1
  fi
  if [[ ! -e "$unit_file" || "${TB_RECONFIGURE:-0}" == 1 ]]; then
    cat > "$unit_file" <<EOF
[Unit]
Description=TaskBridge pending notification relay
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Environment=HOME=$HOME
Environment=TB_CONFIG=$config_file
ExecStart=$tb_path relay --interval=20s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  fi
  systemctl --system daemon-reload &&
    systemctl --system enable --now taskbridge-relay.service &&
    systemctl --system restart taskbridge-relay.service &&
    systemctl --system is-active --quiet taskbridge-relay.service || return 1
  echo "Relay status: $(systemctl --system is-active taskbridge-relay.service) (system service)"
}

start_temporary_relay() {
  local pid_file pid
  command -v nohup >/dev/null 2>&1 || { echo "nohup is unavailable; cannot start a temporary relay." >&2; return 1; }
  mkdir -p "$config_dir" || return 1
  pid_file="$config_dir/relay.pid"
  if [[ -f "$pid_file" ]]; then
    pid="$(cat "$pid_file")"
    if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null &&
       ps -p "$pid" -o args= 2>/dev/null | grep -Fq "$tb_path relay"; then
      echo "Temporary relay is already running (PID $pid)."
      return 0
    fi
  fi
  nohup "$tb_path" relay --interval=20s >> "$config_dir/relay.log" 2>&1 </dev/null &
  pid=$!
  sleep 1
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "Temporary relay exited; inspect $config_dir/relay.log" >&2
    return 1
  fi
  printf '%s\n' "$pid" > "$pid_file"
  echo "Temporary relay started (PID $pid). It will stop after this host reboots."
}

if [[ ! "$enable_relay" =~ ^[Nn]([Oo])?$|^0$ ]]; then
  relay_started=0
  if [[ "$(uname -s)" == Linux ]] && command -v systemctl >/dev/null 2>&1; then
    if [[ "$(id -u)" == 0 ]] &&
       systemctl --system show-environment >/dev/null 2>&1 && start_systemd_root_relay; then
      relay_started=1
    fi
    if [[ "$relay_started" == 0 ]] &&
       systemctl --user show-environment >/dev/null 2>&1 && start_user_systemd_relay; then
      relay_started=1
    fi
  elif [[ "$(uname -s)" == Darwin ]] && command -v launchctl >/dev/null 2>&1; then
    plist_dir="$HOME/Library/LaunchAgents"
    mkdir -p "$plist_dir"
    escaped_path="$(printf '%s' "$tb_path" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g')"
    plist="$plist_dir/com.taskbridge.relay.plist"
    cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.taskbridge.relay</string>
  <key>ProgramArguments</key><array><string>$escaped_path</string><string>relay</string><string>--interval=20s</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
EOF
    launchctl bootout "gui/$(id -u)" "$plist" >/dev/null 2>&1 || true
    if launchctl bootstrap "gui/$(id -u)" "$plist"; then
      echo "Relay started with launchd."
      relay_started=1
    else
      echo "launchd could not start relay now; trying a temporary process." >&2
    fi
  fi
  if [[ "$relay_started" == 0 ]]; then
    echo "No persistent relay service is available; starting a temporary background relay." >&2
    start_temporary_relay || echo "Run '$tb_path relay' under your own startup manager." >&2
  fi
fi

echo "TaskBridge setup complete. Test notifications with: $tb_path notify --title Test 'New computer connected'"
