#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

./scripts/build.sh
mkdir -p release
rm -f release/*.tar.gz release/*.zip release/SHA256SUMS
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

for target in linux-amd64 darwin-amd64 darwin-arm64 windows-amd64; do
  package_dir="$staging/taskbridge-$target"
  mkdir -p "$package_dir"
  if [[ "$target" == windows-* ]]; then
    cp "dist/$target/tb.exe" "$package_dir/"
  else
    cp "dist/$target/tb" "$package_dir/"
  fi
  cp README.md README.zh-CN.md LICENSE "$package_dir/"
  if [[ "$target" == windows-* ]]; then
    (cd "$staging" && zip -qr "$project_root/release/taskbridge-$target.zip" "taskbridge-$target")
  else
    tar -czf "release/taskbridge-$target.tar.gz" -C "$staging" "taskbridge-$target"
  fi
done

(cd release && sha256sum ./*.tar.gz ./*.zip > SHA256SUMS)
echo "Release archives and SHA256SUMS written to release/"
