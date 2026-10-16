#!/usr/bin/env bash
# Real PostgreSQL + Redis; fake providers only. Dedicated test database required.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${TEST_DATABASE_URL:?set TEST_DATABASE_URL to a dedicated test database}"
: "${TEST_REDIS_URL:?set TEST_REDIS_URL}"
scratch="$(mktemp -d)"
a_pid=""; b_pid=""
cleanup() {
  for pid in "$a_pid" "$b_pid"; do
    if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
  done
  rm -rf "$scratch"
}
trap cleanup EXIT
export DATABASE_URL="$TEST_DATABASE_URL" REDIS_URL="$TEST_REDIS_URL"
export SARVAM_API_KEY= SARVAM_PROBE= GEMINI_API_KEY= CORRECTION=rules
export ADMIN_TOKEN=offline-cluster-test CLUSTER_NAMESPACE="offline-test-$$"
go build -o "$scratch/dubash" ./cmd/dubash
go build -o "$scratch/loadtest" ./cmd/loadtest
start_a() {
  NODE_URL=http://127.0.0.1:18081 ROOM_IDS=main-hall \
    "$scratch/dubash" -fake -addr 127.0.0.1:18081 -data "$scratch/a" >>"$scratch/a.log" 2>&1 &
  a_pid=$!
}
wait_ready() {
  local port="$1"
  for attempt in $(seq 1 100); do
    if curl -fsS "http://127.0.0.1:$port/readyz" >/dev/null 2>&1; then return; fi
    sleep 0.1
  done
  cat "$scratch/a.log" "$scratch/b.log"
  return 1
}
start_a
NODE_URL=http://127.0.0.1:18082 ROOM_IDS=hall-b \
  "$scratch/dubash" -fake -addr 127.0.0.1:18082 -data "$scratch/b" >"$scratch/b.log" 2>&1 &
b_pid=$!
wait_ready 18081
wait_ready 18082
curl -fsS http://127.0.0.1:18081/api/rooms | python3 -c 'import sys,json; assert {r["id"] for r in json.load(sys.stdin)} == {"main-hall","hall-b"}'
# Connect to the NON-owner for both rooms: tests HTTP, SSE and WebSocket proxying.
"$scratch/loadtest" -url http://127.0.0.1:18082 -room main-hall -lang en -viewers 100 -duration 9s >"$scratch/main-report.json" &
main_load=$!
"$scratch/loadtest" -url http://127.0.0.1:18081 -room hall-b -lang hi -viewers 100 -duration 9s >"$scratch/b-report.json" &
b_load=$!
wait "$main_load"
wait "$b_load"
cat "$scratch/main-report.json" "$scratch/b-report.json"
# Drain and restart an owner, then read its durable transcript through the other node.
kill "$a_pid"
wait "$a_pid"
a_pid=""
status="$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:18082/events/main-hall)"
[[ "$status" == 503 ]]
start_a
wait_ready 18081
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" http://127.0.0.1:18082/export/main-hall/txt | python3 -c 'import sys; assert sys.stdin.read().strip(), "transcript lost on owner restart"'
echo "Two-node routing, load and durable owner restart passed (fake providers only)."
