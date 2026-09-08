#!/usr/bin/env bash
# Compiles cmd/dylwasm to web/public/, so `vite build` copies dyl.wasm and
# its loader into dist/ verbatim. Run before `npm run build` for the
# standalone (no backend) deployment.
set -euo pipefail
cd "$(dirname "$0")/.."

out=web/public
mkdir -p "$out"

GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o "$out/dyl.wasm" ./cmd/dylwasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"

printf 'built %s (%s) and %s\n' \
  "$out/dyl.wasm" "$(du -h "$out/dyl.wasm" | cut -f1)" "$out/wasm_exec.js"
