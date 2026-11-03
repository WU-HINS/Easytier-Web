#!/usr/bin/env bash
# Two console processes on one database is supported but degraded: device tunnels
# live in process memory, so control and online state are per process. That has to
# be announced, not discovered later (review team, r154). This runs two real
# daemons against one SQLite file and asserts the warning appears.
set -uo pipefail
cd "$(dirname "$0")/.."
WORK="$(mktemp -d)"
FIRST=""
SECOND=""
cleanup() {
  if [ -n "$FIRST" ]; then kill "$FIRST" 2>/dev/null || true; fi
  if [ -n "$SECOND" ]; then kill "$SECOND" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

if ! go build -o "$WORK/webd" ./cmd/easytier-webd; then
  echo "shared database: build failed"
  exit 1
fi

DSN="$WORK/shared.db"
EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$DSN" EASYTIER_DB_BACKUP=0   "$WORK/webd" --http 127.0.0.1:25111 --config-server 127.0.0.1:25112 > "$WORK/first.log" 2>&1 &
FIRST=$!
for _ in $(seq 1 60); do
  if curl -sf http://127.0.0.1:25111/healthz > /dev/null 2>&1; then break; fi
  sleep 0.2
done
if ! curl -sf http://127.0.0.1:25111/healthz > /dev/null 2>&1; then
  echo "shared database: the first instance never came up"
  exit 1
fi

EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$DSN" EASYTIER_DB_BACKUP=0   "$WORK/webd" --http 127.0.0.1:25121 --config-server 127.0.0.1:25122 > "$WORK/second.log" 2>&1 &
SECOND=$!
for _ in $(seq 1 80); do
  if grep -q 'share this database' "$WORK/second.log"; then break; fi
  sleep 0.2
done

if ! grep -q 'console instances share this database' "$WORK/second.log"; then
  echo "  the second instance never warned that the database is shared"
  tail -n 3 "$WORK/second.log" | sed 's/^/    /'
  echo "shared database: FAIL"
  exit 1
fi
if ! curl -sf http://127.0.0.1:25121/healthz > /dev/null 2>&1; then
  echo "  the second instance is not serving after the warning"
  echo "shared database: FAIL"
  exit 1
fi
echo "shared database: two instances on one database announce the degraded mode"
