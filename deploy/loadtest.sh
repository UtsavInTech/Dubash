#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
scratch="$(mktemp -d)"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  rm -rf "$scratch"
}
trap cleanup EXIT
# Inherited live provider and probe settings cannot spend credits.
unset SARVAM_API_KEY SARVAM_PROBE GEMINI_API_KEY DATABASE_URL REDIS_URL ROOM_IDS NODE_URL
export CORRECTION=rules ADMIN_TOKEN=offline-load-test
port="${LOADTEST_PORT:-18080}"
go build -o "$scratch/dubash" ./cmd/dubash
go build -o "$scratch/loadtest" ./cmd/loadtest
"$scratch/dubash" -fake -addr "127.0.0.1:$port" -data "$scratch/data" >"$scratch/server.log" 2>&1 &
server_pid=$!
for attempt in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$port/readyz" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$server_pid" 2>/dev/null; then cat "$scratch/server.log"; exit 1; fi
  sleep 0.1
done
"$scratch/loadtest" -url "http://127.0.0.1:$port" -viewers "${VIEWERS:-200}" -duration "${DURATION:-15s}"
