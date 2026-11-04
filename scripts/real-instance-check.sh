#!/usr/bin/env bash
# Exercises the console against a REAL EasyTier client. The simulated device in
# cmd/smokedevice cannot prove what an upstream instance actually sends, and it
# cannot prove the datagram session either, so this runs the real binary.
#
#   ET_BIN=/path/to/easytier-core             one binary
#   ET_CLIENTS="2.4.0:/path/a 2.7.0:/path/b"  a version matrix (see the matrix script)
#   ET_TRANSPORT=tcp|ws|udp                   which control plane to speak (default tcp)
#
# Every step is asserted. Nothing is reported as ok without evidence in a log or an
# API response, and a missing client is a failure of this check, not a pass.
set -uo pipefail
cd "$(dirname "$0")/.."

MATRIX=$(printenv ET_CLIENTS || true)
SINGLE=$(printenv ET_BIN || true)
if [ -z "$MATRIX" ]; then
  if [ -n "$SINGLE" ]; then MATRIX="client:$SINGLE"; else MATRIX="client:easytier-core"; fi
fi
ENTRY=${MATRIX%% *}
ET_BIN=${ENTRY#*:}
VERSION=${ENTRY%%:*}
if [ ! -x "$ET_BIN" ] && ! command -v "$ET_BIN" >/dev/null 2>&1; then
  echo "real instance check: no EasyTier client found ($ET_BIN)" >&2
  echo "  this check needs the upstream binary; the console on its own proves nothing" >&2
  exit 2
fi

TRANSPORT=$(printenv ET_TRANSPORT || true)
if [ -z "$TRANSPORT" ]; then TRANSPORT=tcp; fi
case "$TRANSPORT" in
  tcp|ws|udp) ;;
  *) echo "real instance check: unknown transport $TRANSPORT" >&2; exit 2 ;;
esac

WORK=$(mktemp -d)
KEEP=$(printenv EASYTIER_KEEP_WORK || true)
CONSOLE_PID=
CLIENT_PID=
cleanup() {
  if [ -n "$CLIENT_PID" ]; then kill "$CLIENT_PID" 2>/dev/null; fi
  if [ -n "$CONSOLE_PID" ]; then kill "$CONSOLE_PID" 2>/dev/null; fi
  if [ -n "$KEEP" ]; then echo "real instance check: kept $WORK" >&2; else rm -rf "$WORK"; fi
}
trap cleanup EXIT INT TERM

# Free ports, not a random range: two runs in a row picked the same numbers and the
# second one silently talked to the first one still shutting down.
pick_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
HTTP_PORT=$(pick_port)
RPC_PORT=$(pick_port)
WS_PORT=$(pick_port)
UDP_PORT=$(pick_port)
BASE="http://127.0.0.1:$HTTP_PORT"
COOKIE="$WORK/cookie"
PASSWORD=Real-Client-Pass-1

fail() { echo "real instance check: FAIL: $*" >&2; exit 1; }

start_console() {
  EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$WORK/et.db" EASYTIER_DB_BACKUP=0 "$WORK/webd" --http "127.0.0.1:$HTTP_PORT" --config-server "127.0.0.1:$RPC_PORT" --config-server-ws "127.0.0.1:$WS_PORT" > "$1" 2>&1 &
  CONSOLE_PID=$!
  for _ in $(seq 1 150); do
    curl -sf "$BASE/healthz" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  return 1
}

echo "== build and start a throwaway console =="
go build -o "$WORK/webd" ./cmd/easytier-webd || fail build
start_console "$WORK/console.log" || fail "the console never came up"

echo "== initialise the super admin and log in =="
printf '{"username":"root-owner","password":"%s"}' "$PASSWORD" > "$WORK/admin.json"
curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/setup" -H 'Content-Type: application/json' -d @"$WORK/admin.json" || fail setup
curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' -d @"$WORK/admin.json" || fail login

if [ "$TRANSPORT" = udp ]; then
  echo "== turn on the udp listener and restart =="
  curl -sf -o /dev/null -b "$COOKIE" -X PUT "$BASE/api/v1/admin/settings" -H 'Content-Type: application/json' -d '{"extra_udp_enabled":true,"extra_port":'$UDP_PORT'}' || fail "udp setting"
  kill "$CONSOLE_PID" 2>/dev/null; sleep 1
  start_console "$WORK/console2.log" || fail "the console did not come back"
  grep -q "extra udp listener" "$WORK/console2.log" || fail "the udp listener never bound"
fi

echo "== create a network that admits every device =="
curl -sf -b "$COOKIE" -X POST "$BASE/api/v1/networks" -H 'Content-Type: application/json' -d '{"settings":{"display_name":"real","networking_method":"Standalone","virtual_cidr":"10.144.0.0/29"}}' > "$WORK/network.json" || fail "create network"
NETWORK_ID=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["network_id"])' "$WORK/network.json") || fail "parse network id"
python3 -c 'import json;print(json.dumps({"advanced": json.dumps({"whitelist": ["*"]})}))' > "$WORK/advanced.json"
curl -sf -o /dev/null -b "$COOKIE" -X PUT "$BASE/api/v1/networks/$NETWORK_ID/advanced" -H 'Content-Type: application/json' -d @"$WORK/advanced.json" || fail "whitelist"

case "$TRANSPORT" in
  tcp) CONFIG_SERVER="tcp://127.0.0.1:$RPC_PORT/root-owner" ;;
  ws)  CONFIG_SERVER="ws://127.0.0.1:$WS_PORT/@up/root-owner" ;;
  udp) CONFIG_SERVER="udp://127.0.0.1:$UDP_PORT/root-owner" ;;
esac

echo "== start the real client over $TRANSPORT =="
"$ET_BIN" --config-server "$CONFIG_SERVER" --machine-id 11111111-2222-3333-4444-555555555501 --hostname real-et > "$WORK/client.log" 2>&1 &
CLIENT_PID=$!

echo "== wait for the device to report itself =="
MACHINE_ID=
for _ in $(seq 1 200); do
  MACHINE_ID=$(curl -sf -b "$COOKIE" "$BASE/api/v1/machines" 2>/dev/null | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["machines"][0]["machine_id"] if d.get("machines") else "")' 2>/dev/null || true)
  if [ -n "$MACHINE_ID" ]; then break; fi
  sleep 0.25
done
if [ -z "$MACHINE_ID" ]; then cat "$WORK/client.log"; fail "the console never saw the real client over $TRANSPORT"; fi

echo "== wait for the whitelist to enrol it =="
JOINED=
for _ in $(seq 1 200); do
  MEMBERS=$(curl -sf -b "$COOKIE" "$BASE/api/v1/networks/$NETWORK_ID/members" 2>/dev/null || true)
  case "$MEMBERS" in
    *"$MACHINE_ID"*) JOINED=1; break ;;
  esac
  sleep 0.25
done
[ -n "$JOINED" ] || fail "the whitelisted real client was never enrolled over $TRANSPORT"

PUSHED=
LOG="$WORK/console.log"
if [ -f "$WORK/console2.log" ]; then LOG="$WORK/console2.log"; fi
for _ in $(seq 1 160); do
  if grep -q "pushed=" "$LOG"; then PUSHED=1; break; fi
  sleep 0.25
done
[ -n "$PUSHED" ] || fail "the console never reported a push over $TRANSPORT; see $LOG"

echo "real instance check: $VERSION over $TRANSPORT reported, enrolled and was configured"
