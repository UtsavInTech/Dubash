#!/usr/bin/env bash
# Browser end-to-end suite: builds Dubash, runs it with -fake on :8080 and
# drives every page with Playwright. Setup once: cd e2e && npm install &&
# npx playwright install chromium.
set -euo pipefail
dir="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$dir/.." && pwd)"
data="$(mktemp -d)"
go build -C "$repo" -o "$dir/bin/" ./cmd/dubash ./cmd/dubash-ingest
cd "$repo"
ADMIN_TOKEN=secreto "$dir/bin/dubash" -fake -config "$dir/rooms.yaml" -addr 127.0.0.1:8080 -data "$data" 2>"$data/server.log" &
server=$!
trap 'kill $server 2>/dev/null; rm -rf "$data"' EXIT
for _ in $(seq 50); do
  curl -fs http://127.0.0.1:8080/healthz >/dev/null && break
  kill -0 $server 2>/dev/null || { echo "dubash did not start (is :8080 free?)" >&2; exit 1; }
  sleep 0.2
done
node "$dir/e2e.mjs"
