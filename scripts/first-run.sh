#!/usr/bin/env bash
set -euo pipefail

git_ready() { command -v git >/dev/null 2>&1 && git --version >/dev/null 2>&1; }

managed_node_bin="$HOME/.local/share/taskbridge/node-22/bin"
if [[ -x "$managed_node_bin/node" ]]; then
  export PATH="$managed_node_bin:$PATH"
fi

node_ready() {
  command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1 &&
    [[ "$(node -p 'Number(process.versions.node.split(".")[0])' 2>/dev/null)" -ge 22 ]] && npm --version >/dev/null 2>&1
}

as_admin() {
  if [[ "$(id -u)" -eq 0 ]]; then "$@"
  elif command -v sudo >/dev/null 2>&1; then sudo "$@"
  else echo "Administrator access is needed to install system packages." >&2; return 1
  fi
}

install_official_node_linux() {
  local arch filename digest work_dir install_parent stage version program
  case "$(uname -m)" in
    x86_64) arch=x64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "No Node.js 22 binary installer for $(uname -m). See https://nodejs.org/en/download" >&2; return 1 ;;
  esac
  if ! command -v tar >/dev/null 2>&1 || ! command -v gzip >/dev/null 2>&1 || ! command -v sha256sum >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      as_admin apt-get update && as_admin apt-get install -y tar gzip coreutils
    elif command -v dnf >/dev/null 2>&1; then
      as_admin dnf install -y tar gzip coreutils
    fi
  fi
  for program in curl tar gzip sha256sum; do
    command -v "$program" >/dev/null 2>&1 || { echo "Missing $program for Node.js installation" >&2; return 1; }
  done
  work_dir="$(mktemp -d)"
  local release_base="${TB_NODE_RELEASE_BASE:-https://nodejs.org/download/release/latest-v22.x}"
  if ! curl -fsSL --retry 3 "$release_base/SHASUMS256.txt" -o "$work_dir/SHASUMS256.txt"; then
    rm -rf "$work_dir"
    echo "Could not download Node.js checksums from $release_base" >&2
    return 1
  fi
  filename=""
  digest=""
  while read -r checksum name; do
    if [[ "$name" =~ ^node-v22\.[0-9.]+-linux-${arch}\.tar\.gz$ ]]; then
      filename="$name"
      digest="$checksum"
      break
    fi
  done < "$work_dir/SHASUMS256.txt"
  if [[ -z "$filename" || ! "$digest" =~ ^[0-9a-f]{64}$ ]]; then
    rm -rf "$work_dir"
    echo "Node.js release checksum for linux-$arch was not found" >&2
    return 1
  fi
  echo "Downloading $filename from the official Node.js release..."
  if ! curl -fsSL --retry 3 "$release_base/$filename" -o "$work_dir/$filename" ||
     ! (cd "$work_dir" && printf '%s  %s\n' "$digest" "$filename" | sha256sum -c -); then
    rm -rf "$work_dir"
    echo "Node.js download or SHA-256 verification failed" >&2
    return 1
  fi
  install_parent="$HOME/.local/share/taskbridge"
  mkdir -p "$install_parent"
  stage="$(mktemp -d "$install_parent/.node-22.XXXXXX")"
  if ! tar -xzf "$work_dir/$filename" -C "$stage" --strip-components=1; then
    rm -rf "$stage" "$work_dir"
    echo "Could not unpack the Node.js release" >&2
    return 1
  fi
  version="$("$stage/bin/node" -p 'Number(process.versions.node.split(".")[0])' 2>/dev/null || true)"
  if [[ "$version" != 22 || ! -x "$stage/bin/npm" ]]; then
    rm -rf "$stage" "$work_dir"
    echo "The downloaded Node.js release cannot run on this Linux host" >&2
    return 1
  fi
  if [[ -e "$install_parent/node-22" ]]; then
    mv "$install_parent/node-22" "$install_parent/node-22.backup.$(date +%s)"
  fi
  mv "$stage" "$install_parent/node-22"
  rm -rf "$work_dir"
  export PATH="$managed_node_bin:$PATH"
  mkdir -p "$HOME/.local/bin"
  for program in node npm npx; do
    if [[ -x "$managed_node_bin/$program" && ! -e "$HOME/.local/bin/$program" && ! -L "$HOME/.local/bin/$program" ]]; then
      ln -s "$managed_node_bin/$program" "$HOME/.local/bin/$program"
    fi
  done
  echo "Installed $(node --version) in $install_parent/node-22"
}

if ! git_ready || ! node_ready; then
  echo "TaskBridge needs Git and Node.js 22+ with npm."
  git_ready || echo "  Missing or broken: git"
  node_ready || echo "  Missing or outdated: Node.js/npm"
  echo "Install guide: https://nodejs.org/en/download"
  if [[ "${TB_AUTO_REPAIR:-0}" == 1 ]]; then
    repair=y
  elif [[ -t 0 ]]; then
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
        if command -v apt-get >/dev/null 2>&1; then as_admin apt-get update && as_admin apt-get install -y git
        elif command -v dnf >/dev/null 2>&1; then as_admin dnf install -y git
        fi
      fi
      if ! node_ready && [[ -f "${NVM_DIR:-$HOME/.nvm}/nvm.sh" ]]; then
        # shellcheck disable=SC1091
        source "${NVM_DIR:-$HOME/.nvm}/nvm.sh"
        nvm install 22
        nvm use 22
      fi
      if ! node_ready; then install_official_node_linux; fi
    fi
  fi
  if ! git_ready || ! node_ready; then
    echo "Environment is still incomplete. Install Node.js 22+ and Git, open a new terminal, then rerun this command." >&2
    exit 2
  fi
fi

release_tag=v0.4.2
setup_dir="${TB_SETUP_DIR:-$HOME/taskbridge}"
if [[ ! -e "$setup_dir" ]]; then
  git clone --branch "$release_tag" --depth 1 https://github.com/HughWang-wzy/taskbridge.git "$setup_dir"
elif [[ ! -f "$setup_dir/scripts/setup.mjs" ]]; then
  echo "$setup_dir exists and is not a TaskBridge checkout; set TB_SETUP_DIR to a new path" >&2
  exit 2
else
  [[ -d "$setup_dir/.git" ]] || { echo "$setup_dir is not a Git checkout; cannot update it safely" >&2; exit 2; }
  if [[ -n "$(git -C "$setup_dir" status --porcelain --untracked-files=no)" ]]; then
    echo "$setup_dir has local source edits; keep them and update the checkout manually" >&2
    exit 2
  fi
  git -C "$setup_dir" fetch --depth 1 origin "refs/tags/$release_tag:refs/tags/$release_tag"
  git -C "$setup_dir" checkout --detach -q "$release_tag"
fi

cd "$setup_dir"
npm ci || { echo "npm ci failed. Check Node.js version, network access, and the deployment guide." >&2; exit 1; }
node scripts/setup.mjs
