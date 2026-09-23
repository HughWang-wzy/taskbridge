#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

./scripts/build.sh
mkdir -p release
rm -f release/*.tar.gz release/*.zip release/install.sh release/install.ps1 release/first-run.sh release/first-run.ps1 release/SHA256SUMS
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

cp scripts/install.sh scripts/install.ps1 scripts/first-run.sh scripts/first-run.ps1 release/
if command -v sha256sum >/dev/null 2>&1; then
  checksum_command=(sha256sum)
else
  checksum_command=(shasum -a 256)
fi
(cd release && "${checksum_command[@]}" ./*.tar.gz ./*.zip ./install.sh ./install.ps1 ./first-run.sh ./first-run.ps1 > SHA256SUMS)
echo "Release archives and SHA256SUMS written to release/"
