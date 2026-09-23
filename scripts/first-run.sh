#!/usr/bin/env bash
set -euo pipefail

for program in git node npm; do
  command -v "$program" >/dev/null 2>&1 || { echo "Missing $program (Node.js 22+ and Git are required)" >&2; exit 2; }
done

setup_dir="${TB_SETUP_DIR:-$HOME/taskbridge}"
if [[ ! -e "$setup_dir" ]]; then
  git clone --branch v0.3.0 --depth 1 https://github.com/HughWang-wzy/taskbridge.git "$setup_dir"
elif [[ ! -f "$setup_dir/scripts/setup.mjs" ]]; then
  echo "$setup_dir exists and is not a TaskBridge checkout; set TB_SETUP_DIR to a new path" >&2
  exit 2
fi

cd "$setup_dir"
npm ci
node scripts/setup.mjs
