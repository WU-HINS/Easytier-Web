#!/usr/bin/env bash
# A port that is already taken must stop the process. Before this check existed the
# daemon logged "console listening", kept running with no HTTP server and exited 0,
# which a supervisor reads as a clean stop (found by review in r126).
set -uo pipefail
cd "$(dirname "$0")/.."
WORK="$(mktemp -d)"
FIRST_PID=""
cleanup() {
  if [ -n "$FIRST_PID" ]; then kill "$FIRST_PID" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

if ! go build -o "$WORK/webd" ./cmd/easytier-webd; then
  echo "port conflict: build failed"
  exit 1
fi

PORT=24911
RPC=24912
RPC_OTHER=24913
EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$WORK/first.db" EASYTIER_DB_BACKUP=0   "$WORK/webd" --http "127.0.0.1:$PORT" --config-server "127.0.0.1:$RPC" > "$WORK/first.log" 2>&1 &
FIRST_PID=$!
for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$PORT/healthz" > /dev/null 2>&1; then break; fi
  sleep 0.2
done
if ! curl -sf "http://127.0.0.1:$PORT/healthz" > /dev/null 2>&1; then
  echo "port conflict: the first instance never came up"
  exit 1
fi

EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$WORK/second.db" EASYTIER_DB_BACKUP=0   timeout 15 "$WORK/webd" --http "127.0.0.1:$PORT" --config-server "127.0.0.1:$RPC_OTHER" > "$WORK/second.log" 2>&1
STATUS=$?

fail=0
if [ "$STATUS" -eq 0 ]; then
  echo "  the second instance exited 0; a taken port has to be a failure"
  fail=1
fi
if [ "$STATUS" -eq 124 ]; then
  echo "  the second instance kept running with no HTTP server"
  fail=1
fi
if grep -q "console listening" "$WORK/second.log"; then
  echo "  the second instance claimed to be listening"
  fail=1
fi
if ! grep -q "address already in use" "$WORK/second.log"; then
  echo "  the failure was not reported as a bind conflict"
  fail=1
fi



# Second scenario: the extra TCP listener is configured in the console settings and
# its port is taken. Enabling it and then serving without it leaves devices pointed
# at a dead port, so startup has to fail (r127).
EXTRA_PORT=24922
SECOND_HTTP=24931
SECOND_RPC=24932
EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$WORK/extra.db" EASYTIER_DB_BACKUP=0   "$WORK/webd" --http "127.0.0.1:$SECOND_HTTP" --config-server "127.0.0.1:$SECOND_RPC" > "$WORK/extra-setup.log" 2>&1 &
FIRST_PID=$!
for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$SECOND_HTTP/healthz" > /dev/null 2>&1; then break; fi
  sleep 0.2
done
curl -s -X POST "http://127.0.0.1:$SECOND_HTTP/api/v1/setup" -H 'Content-Type: application/json'   -d '{"username":"root-owner","password":"Sup3r-Secret-Pass!"}' > /dev/null
curl -s -c "$WORK/extra.jar" -X POST "http://127.0.0.1:$SECOND_HTTP/api/v1/auth/login"   -H 'Content-Type: application/json' -d '{"username":"root-owner","password":"Sup3r-Secret-Pass!"}' > /dev/null
SETTINGS_CODE=$(curl -s -o /dev/null -w '%{http_code}' -b "$WORK/extra.jar" -X PUT   "http://127.0.0.1:$SECOND_HTTP/api/v1/admin/settings" -H 'Content-Type: application/json'   -d "{\"extra_tcp_enabled\":true,\"extra_port\":$EXTRA_PORT}")
kill "$FIRST_PID" 2>/dev/null || true
wait "$FIRST_PID" 2>/dev/null || true
FIRST_PID=""

python3 - "$EXTRA_PORT" <<'PY' &
import socket, sys, time
sock = socket.socket()
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(('0.0.0.0', int(sys.argv[1])))
sock.listen(1)
time.sleep(20)
PY
OCCUPY_PID=$!
sleep 1

EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$WORK/extra.db" EASYTIER_DB_BACKUP=0   timeout 15 "$WORK/webd" --http "127.0.0.1:$SECOND_HTTP" --config-server "127.0.0.1:$SECOND_RPC" > "$WORK/extra-run.log" 2>&1
EXTRA_STATUS=$?
kill "$OCCUPY_PID" 2>/dev/null || true

if [ "$SETTINGS_CODE" != "200" ]; then
  echo "  the settings update for the extra listener returned $SETTINGS_CODE"
  fail=1
fi
if [ "$EXTRA_STATUS" -eq 0 ] || [ "$EXTRA_STATUS" -eq 124 ]; then
  echo "  the daemon started without the extra listener it was told to open"
  fail=1
fi
if grep -q "console listening" "$WORK/extra-run.log"; then
  echo "  it still claimed to listen while the extra port was dead"
  fail=1
fi
if ! grep -q "extra tcp listener" "$WORK/extra-run.log"; then
  echo "  the extra listener failure was not reported"
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo "port conflict: FAIL"
  exit 1
fi
echo "port conflict: both the console port and a configured extra listener stop startup"
