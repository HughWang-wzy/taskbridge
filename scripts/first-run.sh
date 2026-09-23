#!/usr/bin/env bash
set -euo pipefail

git_ready() { command -v git >/dev/null 2>&1 && git --version >/dev/null 2>&1; }

node_ready() {
  command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1 &&
    [[ "$(node -p 'Number(process.versions.node.split(".")[0])' 2>/dev/null)" -ge 22 ]] && npm --version >/dev/null 2>&1
}

if ! git_ready || ! node_ready; then
  echo "TaskBridge needs Git and Node.js 22+ with npm."
  git_ready || echo "  Missing or broken: git"
  node_ready || echo "  Missing or outdated: Node.js/npm"
  echo "Install guide: https://nodejs.org/en/download"
  if [[ -t 0 ]]; then
    read -r -p 'Try to repair with an available package manager? [y/N] ' repair </dev/tty
  else
    repair=n
  fi
  if [[ "$repair" =~ ^[Yy]([Ee][Ss])?$ ]]; then
    if [[ "$(uname -s)" == Darwin ]] && command -v brew >/dev/null 2>&1; then
      git_ready || brew install git
      if ! node_ready; then brew upgrade node 2>/dev/null || brew install node; fi
    elif [[ "$(uname -s)" == Linux ]]; then
      if ! git_ready; then
        if command -v apt-get >/dev/null 2>&1; then sudo apt-get update && sudo apt-get install -y git
        elif command -v dnf >/dev/null 2>&1; then sudo dnf install -y git
        fi
      fi
      if ! node_ready && [[ -f "${NVM_DIR:-$HOME/.nvm}/nvm.sh" ]]; then
        # shellcheck disable=SC1091
        source "${NVM_DIR:-$HOME/.nvm}/nvm.sh"
        nvm install 22
        nvm use 22
      fi
    fi
  fi
  if ! git_ready || ! node_ready; then
    echo "Environment is still incomplete. Install Node.js 22+ and Git, open a new terminal, then rerun this command." >&2
    exit 2
  fi
fi

setup_dir="${TB_SETUP_DIR:-$HOME/taskbridge}"
if [[ ! -e "$setup_dir" ]]; then
  git clone --branch v0.4.0 --depth 1 https://github.com/HughWang-wzy/taskbridge.git "$setup_dir"
elif [[ ! -f "$setup_dir/scripts/setup.mjs" ]]; then
  echo "$setup_dir exists and is not a TaskBridge checkout; set TB_SETUP_DIR to a new path" >&2
  exit 2
fi

cd "$setup_dir"
npm ci || { echo "npm ci failed. Check Node.js version, network access, and the deployment guide." >&2; exit 1; }
node scripts/setup.mjs
