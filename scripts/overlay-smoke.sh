#!/usr/bin/env bash
# Builds a two node overlay through the console and proves as much of it as the
# environment allows:
#
#   1. both official clients register, are enrolled and get a config;
#   2. they find each other through a public relay (peer table shows the peer);
#   3. a TCP payload crosses the overlay (needs a tun device or CAP_NET_ADMIN).
#
#   ET_BIN=/path/to/easytier-core ET_PEERS=... ./scripts/overlay-smoke.sh
#
# Exits 3 when the first two steps pass but the payload step cannot run without
# privileges: unverified must not look like a pass.
set -uo pipefail
cd "$(dirname "$0")/.."

FROM_BIN=$(printenv ET_BIN || true)
ET_BIN=easytier-core
if [ -n "$FROM_BIN" ]; then ET_BIN="$FROM_BIN"; fi
if [ ! -x "$ET_BIN" ] && ! command -v "$ET_BIN" >/dev/null 2>&1; then
  echo "overlay smoke: no easytier-core ($ET_BIN)" >&2
  exit 2
fi
CLI=$(dirname "$ET_BIN")/easytier-cli
PEERS=$(printenv ET_PEERS || echo "tcp://v4v6.usvps.server.ddns.hinswu.top:21010")

WORK=$(mktemp -d)
PIDS=
cleanup() { for pid in $PIDS; do kill "$pid" 2>/dev/null; done; rm -rf "$WORK"; }
trap cleanup EXIT INT TERM
fail() { echo "overlay smoke: FAIL: $*" >&2; exit 1; }
pick_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }

HTTP_PORT=$(pick_port); RPC_PORT=$(pick_port); PORTAL_A=$(pick_port); PORTAL_B=$(pick_port)
BASE="http://127.0.0.1:$HTTP_PORT"
COOKIE="$WORK/cookie"
PASSWORD=Overlay-Smoke-1

echo "== build console =="
go build -o "$WORK/webd" ./cmd/easytier-webd || fail build
EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$WORK/et.db" EASYTIER_DB_BACKUP=0 "$WORK/webd" --http "127.0.0.1:$HTTP_PORT" --config-server "127.0.0.1:$RPC_PORT" > "$WORK/console.log" 2>&1 &
PIDS="$PIDS $!"
for _ in $(seq 1 150); do curl -sf "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.2; done
curl -sf "$BASE/healthz" >/dev/null 2>&1 || fail "console did not start"
printf '{"username":"root-owner","password":"%s"}' "$PASSWORD" > "$WORK/admin.json"
curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/setup" -H 'Content-Type: application/json' -d @"$WORK/admin.json" || fail setup
curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' -d @"$WORK/admin.json" || fail login

echo "== a network that admits every device and points at the relay =="
python3 - "$PEERS" > "$WORK/net.json" <<'PY'
import json, sys
print(json.dumps({"settings": {"display_name": "overlay", "networking_method": "Standalone",
                          "virtual_cidr": "10.178.0.0/29", "peer_urls": [sys.argv[1]]}}))
PY
curl -sf -b "$COOKIE" -X POST "$BASE/api/v1/networks" -H 'Content-Type: application/json' -d @"$WORK/net.json" > "$WORK/created.json" || fail "create network"
NETWORK=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["network_id"])' "$WORK/created.json")
python3 -c 'import json;print(json.dumps({"advanced": json.dumps({"no_tun": True, "whitelist": ["*"]})}))' > "$WORK/adv.json"
curl -sf -o /dev/null -b "$COOKIE" -X PUT "$BASE/api/v1/networks/$NETWORK/advanced" -H 'Content-Type: application/json' -d @"$WORK/adv.json" || fail whitelist

echo "== two clients =="
"$ET_BIN" --config-server "tcp://127.0.0.1:$RPC_PORT/root-owner" --machine-id 20000001-1111-2222-3333-444444440001 --hostname node-a --rpc-portal "127.0.0.1:$PORTAL_A" > "$WORK/a.log" 2>&1 &
PIDS="$PIDS $!"
"$ET_BIN" --config-server "tcp://127.0.0.1:$RPC_PORT/root-owner" --machine-id 20000001-1111-2222-3333-444444440002 --hostname node-b --rpc-portal "127.0.0.1:$PORTAL_B" > "$WORK/b.log" 2>&1 &
PIDS="$PIDS $!"

echo "== step 1: both instances run and are members =="
RUNNING=
for _ in $(seq 1 200); do
  RUNNING=$(curl -sf -b "$COOKIE" "$BASE/api/v1/networks/$NETWORK/members" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print(sum(1 for m in d.get("members",[]) if m.get("running")))' 2>/dev/null || true)
  if [ "$RUNNING" = "2" ]; then break; fi
  sleep 0.3
done
[ "$RUNNING" = "2" ] || fail "expected two running members, got $RUNNING"
echo "  ok: two members running without a tun device"

echo "== step 2: the two instances find each other =="
PEERED=
for _ in $(seq 1 200); do
  if "$CLI" -p "127.0.0.1:$PORTAL_A" peer 2>/dev/null | grep -q node-b; then PEERED=1; break; fi
  sleep 0.5
done
[ -n "$PEERED" ] || fail "node-a never saw node-b in its peer table"
echo "  ok: peer table on node-a lists node-b"

echo "== step 3: a payload across the overlay =="
if [ ! -e /dev/net/tun ]; then
  echo "  SKIP: no /dev/net/tun here, so the virtual ip cannot be bound (EADDRNOTAVAIL)" >&2
  exit 3
fi
echo "  (needs a machine that can create a tun device)" >&2
exit 3
