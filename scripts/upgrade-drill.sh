#!/usr/bin/env bash
# Walks a database forward across a real version boundary and back again.
#
#   1. build the console from an older commit (a worktree, not a stash);
#   2. start the old binary on an empty database and create an admin + a network;
#   3. stop it, start the current binary on the same database and check the data
#      survived (forward migration);
#   4. start the old binary again and record whether it can still read - a
#      forward-only migration is a legitimate policy, but the drill has to say
#      which one this project has.
#
#   ./scripts/upgrade-drill.sh [git-ref]      default: the 40th commit back
#
# Exit 0 when the forward step works and the rollback outcome is recorded.
# Exit 3 when the older commit cannot be built here (no network, missing deps).
set -uo pipefail
cd "$(dirname "$0")/.."

REF=${1:-$(git rev-list --max-count=1 --skip=40 HEAD)}
WORK=$(mktemp -d)
OLD_TREE="$WORK/old"
PIDS=""
cleanup() {
  for pid in $PIDS; do kill "$pid" 2>/dev/null; done
  git worktree remove --force "$OLD_TREE" >/dev/null 2>&1
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

fail() { echo "upgrade drill: FAIL: $*" >&2; exit 1; }
note() { echo "upgrade drill: $*"; }

pick_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }

note "building $REF"
git worktree add --detach "$OLD_TREE" "$REF" >/dev/null 2>&1 || fail "cannot create a worktree for $REF"
(cd "$OLD_TREE" && go build -o "$WORK/webd-old" ./cmd/easytier-webd) || { note "the old commit does not build here"; exit 3; }
go build -o "$WORK/webd-new" ./cmd/easytier-webd || fail "the current commit does not build"

# The drill runs on SQLite by default; set DRILL_DRIVER=postgres with
# EASYTIER_TEST_PG_DSN to walk the same path on PostgreSQL.
DRIVER=${DRILL_DRIVER:-sqlite}
if [ "$DRIVER" = "postgres" ]; then
  PG_DSN=${EASYTIER_TEST_PG_DSN:-}
  [ -n "$PG_DSN" ] || { note "DRILL_DRIVER=postgres needs EASYTIER_TEST_PG_DSN"; exit 3; }
  DB="$PG_DSN"
  note "target database: postgres ($PG_DSN)"
else
  DB="$WORK/et.db"
  note "target database: sqlite ($DB)"
fi
start() {
  local bin="$1" port="$2" log="$3"
  EASYTIER_DB_DRIVER="$DRIVER" EASYTIER_DB_DSN="$DB" EASYTIER_DB_BACKUP=0 \
    "$bin" --http "127.0.0.1:$port" --config-server "127.0.0.1:$((port + 1))" > "$log" 2>&1 &
  PIDS="$PIDS $!"
  for _ in $(seq 1 150); do curl -sf "http://127.0.0.1:$port/healthz" >/dev/null 2>&1 && return 0; sleep 0.2; done
  return 1
}
# A start failure without its log is a guessing game; print what the binary said.
start_or_fail() {
  local bin="$1" port="$2" log="$3" what="$4"
  if start "$bin" "$port" "$log"; then return 0; fi
  tail -n 10 "$log" >&2
  fail "$what"
}
stop_last() {
  local pid
  pid=$(echo "$PIDS" | awk '{print $NF}')
  kill "$pid" 2>/dev/null
  for _ in $(seq 1 100); do kill -0 "$pid" 2>/dev/null || return 0; sleep 0.1; done
  kill -9 "$pid" 2>/dev/null
  return 0
}

PORT=$(pick_port)
start_or_fail "$WORK/webd-old" "$PORT" "$WORK/old.log" "the old binary did not start"
BASE="http://127.0.0.1:$PORT"
COOKIE="$WORK/cookie"
curl -sf -o /dev/null -X POST "$BASE/api/v1/setup" -H 'Content-Type: application/json' \
  -d '{"username":"root-owner","password":"Upgrade-Drill-1"}' || fail "setup on the old binary"
curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"root-owner","password":"Upgrade-Drill-1"}' || fail "login on the old binary"
NETWORK=$(curl -sf -b "$COOKIE" -X POST "$BASE/api/v1/networks" -H 'Content-Type: application/json' \
  -d '{"settings":{"display_name":"drill-net","networking_method":"Standalone","virtual_cidr":"10.77.0.0/29"}}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["network_id"])') || fail "create a network on the old binary"
note "old binary created network $NETWORK"
stop_last

PORT=$(pick_port)
start_or_fail "$WORK/webd-new" "$PORT" "$WORK/new.log" "the current binary did not start on the old database"
BASE="http://127.0.0.1:$PORT"
curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"root-owner","password":"Upgrade-Drill-1"}' || fail "the admin created by the old binary cannot log in after the upgrade"
NETWORKS=$(curl -sf -b "$COOKIE" "$BASE/api/v1/networks") || fail "cannot list networks after the upgrade"
echo "$NETWORKS" | grep -q "$NETWORK" || fail "the network created before the upgrade is gone"
note "forward migration keeps the admin and the network"
stop_last

PORT=$(pick_port)
if start "$WORK/webd-old" "$PORT" "$WORK/rollback.log"; then
  BASE="http://127.0.0.1:$PORT"
  if curl -sf -o /dev/null -c "$COOKIE" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
      -d '{"username":"root-owner","password":"Upgrade-Drill-1"}'; then
    note "rollback: the old binary still reads the migrated database"
  else
    note "rollback: the old binary starts but cannot authenticate on the migrated database"
  fi
  stop_last
else
  note "rollback: the old binary does not start on the migrated database (forward-only migration)"
fi
note "done"
