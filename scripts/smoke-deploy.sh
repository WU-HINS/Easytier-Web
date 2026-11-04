#!/usr/bin/env bash
# Zero-to-deploy walk used by review role R-B.
# Builds the daemon, starts it on a scratch SQLite database, runs the setup
# wizard, logs in, registers a user and probes the admin surface plus headers.
set -eo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
BIN="$WORK/easytier-webd"
DB="$WORK/et.db"
PORT="$SMOKE_PORT"
if [ -z "$PORT" ]; then PORT=18211; fi
RPC_PORT="$SMOKE_RPC_PORT"
if [ -z "$RPC_PORT" ]; then RPC_PORT=18221; fi
BASE="http://127.0.0.1:$PORT"
COOKIE="$WORK/cookie.txt"
PID=""

cleanup() {
  if [ -n "$PID" ]; then kill "$PID" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT

STEPS_FILE="$WORK/steps.txt"
step() {
  printf '%-30s %s\n' "$1" "$2"
  printf '%s\n' "$1" >> "$STEPS_FILE"
}


# step() prints and records an observation; require() fails the run when the
# observation is empty. Without it a vanished security header only printed a blank
# column and the walk still reported success (found in r96).
# require_code() is require() for HTTP status codes: recording "member renamed 404"
# and moving on would hide a missing route (found in r98).
require_code() {
  local name="$1" want="$2" got="$3"
  step "$name" "$got"
  if [ "$got" != "$want" ]; then
    echo "FAIL: $name returned $got, expected $want"
    exit 1
  fi
}

require() {
  local name="$1" value="$2" min="$3"
  step "$name" "$value"
  if [ -z "$min" ]; then min=1; fi
  if [ "${#value}" -lt "$min" ]; then
    echo "FAIL: $name is empty (or shorter than $min chars): ${value}"
    exit 1
  fi
}

echo "== build =="
(cd "$ROOT" && go build -o "$BIN" ./cmd/easytier-webd)

echo "== start on an empty database =="
DRIVER="$SMOKE_DB_DRIVER"
if [ -z "$DRIVER" ]; then DRIVER=sqlite; fi
DSN="$SMOKE_DB_DSN"
if [ -z "$DSN" ]; then DSN="$DB"; fi
printf "%-30s %s\n" "database" "$DRIVER"
if [ "$DRIVER" = "postgres" ] && [ "$SMOKE_PG_RESET" = "1" ]; then
  psql -q "$DSN" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" > /dev/null 2>&1
fi
EASYTIER_DB_DRIVER="$DRIVER" EASYTIER_DB_DSN="$DSN" EASYTIER_DB_BACKUP=0 \
  "$BIN" --http "127.0.0.1:$PORT" --config-server "127.0.0.1:$RPC_PORT" \
         --secure-cookies=false --web-dir "$ROOT/client/dist" > "$WORK/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 60); do
  if curl -sf "$BASE/healthz" > /dev/null 2>&1; then break; fi
  sleep 0.2
done

require "setup status (before)" "$(curl -s "$BASE/api/v1/setup")"
step "gate before setup" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/machines")"
step "spa /setup" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/setup")"
step "setup admin" "$(curl -s -X POST "$BASE/api/v1/setup" -H 'Content-Type: application/json' -d '{"username":"root-owner","password":"Sup3r-Secret-Pass!"}')"
step "setup again" "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/setup" -H 'Content-Type: application/json' -d '{"username":"x","password":"Sup3r-Secret-Pass!"}')"
curl -s -c "$COOKIE" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"username":"root-owner","password":"Sup3r-Secret-Pass!"}' > /dev/null
require "admin users" "$(curl -s -b "$COOKIE" "$BASE/api/v1/admin/users" | head -c 110)"
step "register user" "$(curl -s -X POST "$BASE/api/v1/auth/register" -H 'Content-Type: application/json' -d '{"username":"alice","password":"An0ther-Strong-Pass"}')"
require "csp header" "$(curl -s -D - -o /dev/null "$BASE/healthz" | grep -i '^content-security-policy' | head -c 70)"
require "x-frame-options" "$(curl -s -D - -o /dev/null "$BASE/healthz" | grep -i '^x-frame-options' | tr -d '\r' | head -c 40)"
step "ws route (no upgrade)" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/@up/alice")"
require "boot js served" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/boot.js")"

echo "== device -> config push =="
MACHINE="9a7c0000-0000-4000-8000-000000000001"
(cd "$ROOT" && go build -o "$WORK/smokedevice" ./cmd/smokedevice)
"$WORK/smokedevice" --endpoint "127.0.0.1:$RPC_PORT" --token root-owner --machine "$MACHINE" \
  --expect-network smoke-net --expect-ipv4 10.99.0.1 --expect-count 2 --timeout 60s > "$WORK/device.log" 2>&1 &
DEVICE_PID=$!
for _ in $(seq 1 60); do
  if curl -s -b "$COOKIE" "$BASE/api/v1/machines" | grep -q "$MACHINE"; then break; fi
  sleep 0.2
done
require "device registered" "$(curl -s -b "$COOKIE" "$BASE/api/v1/machines" | head -c 90)"
NETWORK_JSON=$(curl -s -b "$COOKIE" -X POST "$BASE/api/v1/networks" -H 'Content-Type: application/json' -d '{"settings":{"display_name":"smoke-net","networking_method":"Standalone","virtual_cidr":"10.99.0.0/29"}}')
NETWORK_ID=$(printf %s "$NETWORK_JSON" | grep -o '"network_id":"[^"]*"' | cut -d'"' -f4)
step "network created" "$NETWORK_ID"
MEMBER_BODY=$(printf '{"device_ids":["%s"]}' "$MACHINE")
MEMBER_RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE" -X POST "$BASE/api/v1/networks/$NETWORK_ID/members" -H 'Content-Type: application/json' -d "$MEMBER_BODY")
require_code "member added" 204 "$MEMBER_RESPONSE"
for _ in $(seq 1 150); do
  if grep -q CONFIG_RECEIVED "$WORK/device.log" 2>/dev/null; then break; fi
  sleep 0.2
done
RENAME_CODE=$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE" -X PATCH "$BASE/api/v1/networks/$NETWORK_ID/members/$MACHINE" -H 'Content-Type: application/json' -d '{"hostname_override":"smoke-renamed"}')
require_code "member renamed" 204 "$RENAME_CODE"
cat > "$WORK/override.json" <<'JSON'
{"override_config":"{\"mtu\":1300}"}
JSON
OVERRIDE_CODE=$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE" -X PUT "$BASE/api/v1/networks/$NETWORK_ID/members/$MACHINE/config" -H 'Content-Type: application/json' -d @"$WORK/override.json")
require_code "member override set" 204 "$OVERRIDE_CODE"
OVERRIDE_READ=$(curl -s -b "$COOKIE" "$BASE/api/v1/networks/$NETWORK_ID/members/$MACHINE/config")
step "member override read" "$OVERRIDE_READ"
DEVICE_RC=0
wait "$DEVICE_PID" || DEVICE_RC=$?
step "configs received" "$(grep CONFIG_RECEIVED "$WORK/device.log" | tr '\n' '|')"
if ! grep -q "mtu=1300" "$WORK/device.log"; then
  echo "FAIL: the member override never reached the device"
  grep CONFIG_RECEIVED "$WORK/device.log" || true
  exit 1
fi
step "override applied on device" "mtu=1300"
if [ "$DEVICE_RC" -ne 0 ]; then
  echo "--- device log ---"; cat "$WORK/device.log"
  echo "--- daemon log ---"; tail -n 12 "$WORK/server.log"
  echo "FAIL: device never received a configuration"
  exit 1
fi


echo "== extra listener setting =="
EXTRA_PORT=$((PORT + 100))
SETTINGS_BODY=$(printf '{"extra_tcp_enabled":true,"extra_udp_enabled":true,"extra_port":%d}' "$EXTRA_PORT")
SETTINGS_CODE=$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE" -X PUT "$BASE/api/v1/admin/settings" -H 'Content-Type: application/json' -d "$SETTINGS_BODY")
require_code "extra listener enabled" 200 "$SETTINGS_CODE"

echo "== graceful shutdown =="
kill -TERM "$PID" 2> /dev/null
SHUTDOWN_OK=0
for _ in $(seq 1 100); do
  if ! kill -0 "$PID" 2> /dev/null; then SHUTDOWN_OK=1; break; fi
  sleep 0.1
done
if [ "$SHUTDOWN_OK" -ne 1 ]; then
  echo "FAIL: process did not exit within 10s of SIGTERM"
  exit 1
fi
step "process exited" "SIGTERM honoured"
if ! grep -qE '^[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} shutting down$' "$WORK/server.log"; then
  echo "FAIL: no graceful shutdown marker in the log"
  exit 1
fi
if ! grep -qE '^[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} stopped$' "$WORK/server.log"; then
  echo "FAIL: the process did not log a clean stop"
  exit 1
fi

echo "== restart on the same database =="
EASYTIER_DB_DRIVER="$DRIVER" EASYTIER_DB_DSN="$DSN" EASYTIER_DB_BACKUP=0   "$BIN" --http "127.0.0.1:$PORT" --config-server "127.0.0.1:$RPC_PORT"          --secure-cookies=false --web-dir "$ROOT/client/dist" >> "$WORK/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 60); do
  if curl -sf "$BASE/healthz" > /dev/null 2>&1; then break; fi
  sleep 0.2
done
RESTART_STATUS=$(curl -s "$BASE/api/v1/setup")
step "setup status after restart" "$RESTART_STATUS"

# The UDP transport landed in r145; prove the datagram socket is bound too.
if grep -qE "extra udp listener on .*:$EXTRA_PORT" "$WORK/server.log"; then
  step "extra udp listener open" "$EXTRA_PORT"
else
  echo "FAIL: the extra UDP listener did not announce itself on port $EXTRA_PORT"
  exit 1
fi
if timeout 2 bash -c ": > /dev/tcp/127.0.0.1/$EXTRA_PORT" 2>/dev/null; then
  step "extra listener open" "$EXTRA_PORT"
else
  echo "FAIL: the extra listener did not come up on $EXTRA_PORT"
  exit 1
fi

if ! printf %s "$RESTART_STATUS" | grep -q '"initialized":true'; then
  echo "FAIL: database did not survive the restart"
  exit 1
fi
kill -TERM "$PID" 2> /dev/null

# wait for the previous process to release the ports
for _ in $(seq 1 100); do
  if ! kill -0 "$PID" 2> /dev/null; then break; fi
  sleep 0.1
done
echo "== standalone ws listener =="
WS_PORT=$((PORT + 200))
EASYTIER_DB_DRIVER="$DRIVER" EASYTIER_DB_DSN="$DSN" EASYTIER_DB_BACKUP=0 "$BIN" --http "127.0.0.1:$PORT" --config-server "127.0.0.1:$RPC_PORT" --config-server-ws "127.0.0.1:$WS_PORT" --secure-cookies=false --web-dir "$ROOT/client/dist" >> "$WORK/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 60); do
  if curl -sf "$BASE/healthz" > /dev/null 2>&1; then break; fi
  sleep 0.2
done
if ! grep -q "config server listening on ws://" "$WORK/server.log"; then
  echo "FAIL: the standalone ws listener did not start"
  exit 1
fi
step "ws listener started" "ws://127.0.0.1:$WS_PORT"
WS_RENAME=$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE" -X PATCH "$BASE/api/v1/networks/$NETWORK_ID/members/$MACHINE" -H 'Content-Type: application/json' -d '{"hostname_override":"smoke-ws"}')
require_code "member renamed for ws" 204 "$WS_RENAME"

timeout 60 "$WORK/smokedevice" --transport ws --endpoint "ws://127.0.0.1:$WS_PORT/" --token root-owner --machine "$MACHINE" --expect-network smoke-net --expect-count 1 --timeout 45s > "$WORK/ws-device.log" 2>&1 || true
if ! grep -q "CONFIG_RECEIVED" "$WORK/ws-device.log"; then
  echo "FAIL: no configuration arrived over the standalone ws listener"
  cat "$WORK/ws-device.log"
  exit 1
fi
step "ws device config" "$(grep -c CONFIG_RECEIVED "$WORK/ws-device.log") revision(s) received"

# The ws device has exited, but its console session lingers until the idle
# watchdog fires. Pushing the next revision into that stale session would
# record it as applied while nobody is listening, so wait for offline first.
OFFLINE=0
for _ in $(seq 1 200); do
  if curl -s -b "$COOKIE" "$BASE/api/v1/networks/$NETWORK_ID/members" | python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if any(m.get("online") is False for m in data.get("members", [])) else 1)'; then OFFLINE=1; break; fi
  sleep 0.2
done
if [ "$OFFLINE" -ne 1 ]; then
  echo "FAIL: the ws device never went offline, so the udp push would hit a stale session"
  exit 1
fi
step "ws session closed" "device seen offline before the udp run"
# The reconciler only pushes when the stored revision differs from the compiled
# one, so the udp session needs its own edit just like the ws session above.
UDP_RENAME="$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE" -X PATCH "$BASE/api/v1/networks/$NETWORK_ID/members/$MACHINE" -H "Content-Type: application/json" -d '{"hostname_override":"smoke-udp"}')"
require_code "member renamed for udp" 204 "$UDP_RENAME"
# The datagram transport gets the same treatment: a real device session over
# UDP on the extra listener, which is enabled a few steps above (r157).
timeout 60 "$WORK/smokedevice" --transport udp --endpoint "127.0.0.1:$EXTRA_PORT" --token root-owner --machine "$MACHINE" --expect-network smoke-net --expect-count 1 --timeout 45s > "$WORK/udp-device.log" 2>&1 || true
if ! grep -q "CONFIG_RECEIVED" "$WORK/udp-device.log"; then
  echo "FAIL: no configuration arrived over the udp listener"
  cat "$WORK/udp-device.log"
  exit 1
fi
step "udp device config" "$(grep -c CONFIG_RECEIVED "$WORK/udp-device.log") revision(s) received"
kill -TERM "$PID" 2> /dev/null
for _ in $(seq 1 100); do
  if ! kill -0 "$PID" 2> /dev/null; then break; fi
  sleep 0.1
done
echo "== assertion coverage =="
REQUIRED_STEPS="setup admin|gate before setup|ws route (no upgrade)|configs received|member override set|override applied on device|extra listener open|ws listener started|ws device config|udp device config|process exited|setup status after restart"
missing=0
IFS="|"
for name in $REQUIRED_STEPS; do
  if ! grep -qxF "$name" "$STEPS_FILE" 2> /dev/null; then
    echo "FAIL: step never ran: ""$name"
    missing=1
  fi
done
IFS=$(printf " \t\n")
if [ "$missing" -ne 0 ]; then
  exit 1
fi
step "assertions ran" "$(wc -l < "$STEPS_FILE") steps"

echo "== server log tail =="
tail -n 5 "$WORK/server.log"
echo "== done =="
