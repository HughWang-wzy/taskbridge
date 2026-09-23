#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist/linux-amd64 dist/windows-amd64 dist/darwin-amd64 dist/darwin-arm64
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/linux-amd64/tb ./cmd/tb
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/windows-amd64/tb.exe ./cmd/tb
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o dist/darwin-amd64/tb ./cmd/tb
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o dist/darwin-arm64/tb ./cmd/tb
