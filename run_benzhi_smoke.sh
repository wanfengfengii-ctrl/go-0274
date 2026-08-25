#!/usr/bin/env bash
# Smoke test for the oyster-purification-release-gate service.
#
# Builds the server, starts it on a local loopback address with an ephemeral
# SQLite database, probes its health and API behaviour (create + lock a task),
# then cleans up every process and temporary file. It performs no external
# network access and never shells out to "go test".
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="$(mktemp -d)"
BIN="$WORK/server"
DB="$WORK/release.db"
PORT="${PORT:-18080}"
BASE="http://127.0.0.1:$PORT"
SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

# json_field extracts a single string field value from a JSON object.
json_field() {
  printf '%s' "$1" | sed -n "s/.*\"$2\":\"\([^\"]*\)\".*/\1/p" | head -n 1
}

echo "==> building server"
( cd "$ROOT" && go build -o "$BIN" ./cmd/server )

echo "==> resolving rule snapshot digest"
catalog_json="$("$BIN" -print-catalog)"
DIGEST="$(json_field "$catalog_json" digest)"
if [[ -z "$DIGEST" ]]; then
  echo "FAIL: could not resolve rule digest from catalog" >&2
  exit 1
fi
echo "    digest=$DIGEST"

echo "==> starting server on $BASE"
"$BIN" -addr "127.0.0.1:$PORT" -db "$DB" &
SERVER_PID=$!

# Wait for the health endpoint to come up.
for _ in $(seq 1 50); do
  if curl -fsS "$BASE/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

echo "==> probing /healthz"
health_resp="$(curl -fsS "$BASE/healthz")"
health_status="$(json_field "$health_resp" status)"
if [[ "$health_status" != "ok" ]]; then
  echo "FAIL: healthz status=$health_status" >&2
  exit 1
fi
echo "    healthz=$health_resp"

echo "==> creating release task"
create_resp="$(curl -fsS -X POST "$BASE/v1/tasks" \
  -H 'Content-Type: application/json' \
  -d "{\"area_id\":\"east\",\"rule_snapshot_id\":\"rules-east-1\",\"rule_digest\":\"$DIGEST\"}")"
TASK_ID="$(json_field "$create_resp" id)"
if [[ -z "$TASK_ID" ]]; then
  echo "FAIL: no task id in response: $create_resp" >&2
  exit 1
fi
echo "    task_id=$TASK_ID"

echo "==> locking task (atomic resource leases)"
lock_body='{"tide_batch":"T1","uv_lamp_batch":"UV1","locked_count":10,"cage_seals":["C1"],"timepoints":["TP1"],"observation_points":["OP1"],"blind_codes":["B1","B2","B3"],"pool":"P1","pump_branch":"PU1","probe_window":"PR1","holes":[{"id":"H1","kind":"toxin"},{"id":"H2","kind":"qpcr"},{"id":"H3","kind":"culture"}],"reviewers":["R1","R2"]}'
lock_resp="$(curl -fsS -X POST "$BASE/v1/tasks/$TASK_ID/lock" \
  -H 'Content-Type: application/json' \
  -H 'Operation-Id: smoke-lock' \
  -H 'If-Task-Generation: 1' \
  -d "$lock_body")"
lock_status="$(json_field "$lock_resp" status)"
if [[ "$lock_status" != "pending_intake" ]]; then
  echo "FAIL: lock status=$lock_status response=$lock_resp" >&2
  exit 1
fi
echo "    lock status=$lock_status"

echo "==> verifying task snapshot via GET"
task_resp="$(curl -fsS "$BASE/v1/tasks/$TASK_ID")"
task_status="$(json_field "$task_resp" status)"
if [[ "$task_status" != "pending_intake" ]]; then
  echo "FAIL: task status=$task_status" >&2
  exit 1
fi
echo "    task status=$task_status"

echo "==> smoke test passed"
