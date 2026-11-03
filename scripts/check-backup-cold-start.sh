#!/usr/bin/env bash
# The deploy path with snapshots ON. Every other script sets EASYTIER_DB_BACKUP=0,
# so "first start writes a snapshot before touching the schema" was never executed
# anywhere (review team, r140).
set -uo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"
WORK="$(mktemp -d)"
PID=""
cleanup() {
  if [ -n "$PID" ]; then kill "$PID" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

if ! go build -o "$WORK/webd" ./cmd/easytier-webd; then
  echo "backup cold start: build failed"
  exit 1
fi

PORT=25011
RPC=25012
BACKUPS="$WORK/does/not/exist/yet"
EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="file:$WORK/et.db?_pragma=busy_timeout(5000)" EASYTIER_DB_BACKUP=1 \
  EASYTIER_DB_BACKUP_DIR="$BACKUPS" \
  "$WORK/webd" --http "127.0.0.1:$PORT" --config-server "127.0.0.1:$RPC" > "$WORK/run.log" 2>&1 &
PID=$!
for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$PORT/healthz" > /dev/null 2>&1; then break; fi
  sleep 0.2
done

fail=0
if ! curl -sf "http://127.0.0.1:$PORT/healthz" > /dev/null 2>&1; then
  echo "  the daemon did not come up with snapshots enabled"
  tail -n 3 "$WORK/run.log" | sed 's/^/    /'
  fail=1
fi
SNAPSHOTS=$(find "$WORK" -maxdepth 6 -name 'et-backup-*' 2>/dev/null | wc -l)
if [ "$SNAPSHOTS" -eq 0 ]; then
  echo "  no snapshot was written before the first schema change"
  fail=1
fi
if [ ! -d "$BACKUPS" ]; then
  echo "  the backup directory was not created"
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo "backup cold start: FAIL"
  exit 1
fi
echo "backup cold start: snapshots are written on a first start with a missing directory"
