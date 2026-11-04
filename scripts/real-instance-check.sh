#!/usr/bin/env bash
# Exercises the console against a REAL EasyTier client, which is the one thing the
# simulated device in cmd/smokedevice cannot prove: what an upstream instance
# actually reports in its heartbeat, and whether the console handles it.
#
#   ET_BIN=/path/to/easytier-core ./scripts/real-instance-check.sh
#
# Every step is asserted. Nothing is reported as ok without evidence in the log or
# in an API response, and a missing client is a failure of this check, not a pass.
set -uo pipefail
cd "$(dirname "$0")/.."

# Read the environment before assigning anything: assigning to an already exported
# variable rewrites the exported value too, which silently discarded ET_BIN and
# turned a runnable check into "no client found".
FROM_ENV=$(printenv ET_BIN || true)
ET_BIN=easytier-core
if [ -n "$FROM_ENV" ]; then ET_BIN="$FROM_ENV"; fi
if ! command -v "$ET_BIN" >/dev/null 2>&1; then
  echo "real instance check: no EasyTier client found (ET_BIN=$ET_BIN)" >&2
  echo "  this check needs the upstream binary; the console on its own proves nothing" >&2
  exit 2
fi

WORK=$(mktemp -d)
CONSOLE_PID=
CLIENT_PID=
cleanup() {
  if [ -n "$CLIENT_PID" ]; then kill "$CLIENT_PID" 2>/dev/null; fi
  if [ -n "$CONSOLE_PID" ]; then kill "$CONSOLE_PID" 2>/dev/null; fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

HTTP_PORT=$((26000 + RANDOM % 400))
RPC_PORT=$((HTTP_PORT + 500))
BASE="http://127.0.0.1:$HTTP_PORT"
COOKIE="$WORK/cookie"
PASSWORD=Real-Client-Pass-1

fail() { echo "real instance check: FAIL: $*" >&2; exit 1; }

json() {
  python3 -c 'import json,sys;print(json.dumps(json.loads(sys.argv[1])))' "$1"
}

echo "== build and start a throwaway console =="
go build -o "$WORK/webd" ./cmd/easytier-webd || fail build
EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$WORK/et.db" EASYTIER_DB_BACKUP=0 "$WORK/webd" --http "127.0.0.1:$HTTP_PORT" --config-server "127.0.0.1:$RPC_PORT" > "$WORK/console.log" 2>&1 &
CONSOLE_PID=$!
for _ in $(seq 1 100); do
  curl -sf "$BASE/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -sf "$BASE/healthz" >/dev/null 2>&1 || fail "the console never came up"

echo "== initialise the super admin and log in =="
printf '{"username":"root-owner","password":"%s"}' "$PASSWORD" > "$WORK/admin.json"
curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/setup" -H 'Content-Type: application/json' -d @"$WORK/admin.json" || fail setup
curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' -d @"$WORK/admin.json" || fail login

echo "== create a network that admits every device =="
curl -sf -b "$COOKIE" -X POST "$BASE/api/v1/networks" -H 'Content-Type: application/json' -d '{"settings":{"display_name":"real-client","networking_method":"Standalone","virtual_cidr":"10.144.0.0/29"}}' > "$WORK/network.json" || fail "create network"
NETWORK_ID=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["network_id"])' "$WORK/network.json") || fail "parse network id"
[ -n "$NETWORK_ID" ] || fail "empty network id"

python3 -c 'import json;print(json.dumps({"advanced": json.dumps({"whitelist": ["*"]})}))' > "$WORK/advanced.json"
curl -sf -o /dev/null -b "$COOKIE" -X PUT "$BASE/api/v1/networks/$NETWORK_ID/advanced" -H 'Content-Type: application/json' -d @"$WORK/advanced.json" || fail "whitelist"

echo "== start the real client against the control plane =="
# The upstream client carries the account name in the URL path and presents it as
# the heartbeat token, so that path is the console username.
"$ET_BIN" --config-server "tcp://127.0.0.1:$RPC_PORT/root-owner" --machine-id 11111111-2222-3333-4444-555555555501 --hostname real-et > "$WORK/client.log" 2>&1 &
CLIENT_PID=$!

echo "== wait for the device to report itself =="
SEEN=
for _ in $(seq 1 150); do
  MACHINES=$(curl -sf -b "$COOKIE" "$BASE/api/v1/machines" 2>/dev/null || true)
  case "$MACHINES" in
    *11111111-2222-3333-4444-555555555501*) SEEN=1; break ;;
  esac
  sleep 0.2
done
if [ -z "$SEEN" ]; then cat "$WORK/client.log"; fail "the console never saw the real client"; fi

echo "== wait for the whitelist to enrol it =="
JOINED=
for _ in $(seq 1 150); do
  MEMBERS=$(curl -sf -b "$COOKIE" "$BASE/api/v1/networks/$NETWORK_ID/members" 2>/dev/null || true)
  case "$MEMBERS" in
    *11111111-2222-3333-4444-555555555501*) JOINED=1; break ;;
  esac
  sleep 0.2
done
[ -n "$JOINED" ] || fail "the whitelisted real client was never enrolled"

if ! grep -q "pushed=" "$WORK/console.log"; then
  fail "the console never reported a push; see $WORK/console.log"
fi

echo "== the real client's heartbeat payload, for the record =="
grep -o "running[^,}]*" "$WORK/client.log" | head -5 || true
echo "real instance check: a real EasyTier client reported, enrolled and was configured"
