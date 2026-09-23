#!/usr/bin/env bash
set -euo pipefail

release_base="${TB_RELEASE_BASE:-https://github.com/HughWang-wzy/taskbridge/releases/latest/download}"
install_dir="${TB_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) platform=linux-amd64 ;;
  Darwin-x86_64) platform=darwin-amd64 ;;
  Darwin-arm64) platform=darwin-arm64 ;;
  *) echo "Unsupported platform. Build from source with scripts/build.sh." >&2; exit 2 ;;
esac

for command in curl tar awk; do
  command -v "$command" >/dev/null 2>&1 || { echo "Missing command: $command" >&2; exit 2; }
done

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

if [[ "$(uname -s)" == Darwin ]]; then
  config_dir="$HOME/Library/Application Support/taskbridge"
else
  config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/taskbridge"
fi

prompt() {
  local label="$1" result
  [[ -r /dev/tty ]] || { echo "Interactive terminal required for $label" >&2; return 1; }
  read -r -p "$label" result </dev/tty
  printf '%s' "$result"
}

config_file="$config_dir/config.json"
if [[ ! -f "$config_file" || "${TB_RECONFIGURE:-0}" == 1 ]]; then
  worker_url="${TB_WORKER_URL:-}"
  topic="${TB_NTFY_TOPIC:-}"
  token="${TB_CLIENT_TOKEN:-}"
  [[ -n "$worker_url" ]] || worker_url="$(prompt 'Worker URL: ')"
  [[ -n "$topic" ]] || topic="$(prompt 'ntfy topic: ')"
  if [[ -z "$token" ]]; then
    [[ -r /dev/tty ]] || { echo "Interactive terminal required for client token" >&2; exit 1; }
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
if [[ -z "$enable_codex" && -r /dev/tty ]]; then
  enable_codex="$(prompt 'Install Codex Hooks and MCP? [y/N] ')"
fi
if [[ "$enable_codex" =~ ^[Yy]([Ee][Ss])?$|^1$ ]]; then
  command -v codex >/dev/null 2>&1 || { echo "Codex CLI not found; skipping Codex integration" >&2; exit 1; }
  if [[ ! -f "$config_dir/codex.json" || "${TB_RECONFIGURE:-0}" == 1 ]]; then
    install -m 600 "$config_file" "$config_dir/codex.json"
  fi
  "$tb_path" hook codex
  if codex mcp get taskbridge >/dev/null 2>&1; then
    echo "TaskBridge MCP already exists; check its binary path with: codex mcp get taskbridge"
  else
    codex mcp add taskbridge -- "$tb_path" mcp
  fi
  echo "Restart Codex and review the new Hooks in /hooks."
fi

enable_relay="${TB_ENABLE_RELAY:-}"
if [[ -z "$enable_relay" && -r /dev/tty ]]; then
  enable_relay="$(prompt 'Start a background relay on this computer? [Y/n] ')"
fi
if [[ ! "$enable_relay" =~ ^[Nn]([Oo])?$|^0$ ]]; then
  if [[ "$(uname -s)" == Linux ]] && command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    unit_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
    mkdir -p "$unit_dir"
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
    systemctl --user daemon-reload
    systemctl --user enable --now taskbridge-relay.service
    systemctl --user restart taskbridge-relay.service
    echo "Relay status: $(systemctl --user is-active taskbridge-relay.service)"
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
    else
      echo "launchd could not start relay now; it will be available at the next login." >&2
    fi
  else
    echo "No supported service manager was found. Run '$tb_path relay' under your startup manager." >&2
  fi
fi

echo "TaskBridge setup complete. Test notifications with: $tb_path notify --title Test 'New computer connected'"
