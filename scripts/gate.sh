#!/usr/bin/env bash
# Single entry point for the review gate. Every round runs exactly this script
# so the charter and the practice cannot drift apart.
#
#   ./scripts/gate.sh            # sqlite only
#   EASYTIER_TEST_PG_DSN=... ./scripts/gate.sh   # also runs the postgres path
set -eo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FAILED=0
NL=$(printf '
')
summary=""

run() {
  name="$1"
  shift
  printf '== %-28s ' "$name"
  if output=$("$@" 2>&1); then
    printf 'PASS\n'
    summary="$summary\n  PASS  $name"
  else
    printf 'FAIL\n'
    printf '%s\n' "$output" | tail -n 20 | sed 's/^/    /'
    summary="$summary\n  FAIL  $name"
    FAILED=1
  fi
}

run "backend build"      go build ./...
run "backend vet"        go vet ./...
RACE_FLAG=""
if [ -n "$EASYTIER_RACE" ]; then RACE_FLAG="-race"; fi
run "backend tests"      go test $RACE_FLAG -count=1 ./...
run "client build"       bash -c "cd client && npm run build"
run "client bundle"      node scripts/check-bundle.mjs
run "design contrast"    node scripts/check-contrast.mjs
run "design tokens"      node scripts/check-design-tokens.mjs
run "api contract"       node scripts/check-contract.mjs
run "wire format"        node --experimental-strip-types scripts/check-wire.mjs
run "client origin"      node --experimental-transform-types scripts/check-client-origin.mjs
run "client request"     node --experimental-transform-types scripts/check-client-request.mjs
run "gate audit"       ./scripts/audit-gates.sh
run "docs consistency"   node scripts/check-docs.mjs
run "readme commands"    node scripts/check-readme.mjs
run "ui states"         node scripts/check-ui.mjs
run "i18n coverage"   node scripts/check-i18n.mjs
run "error contract"  node scripts/check-errors.mjs
run "deploy smoke"       bash scripts/smoke-deploy.sh
run "port conflict"     ./scripts/check-port-conflict.sh
run "backup cold start"  ./scripts/check-backup-cold-start.sh
run "shared database"   ./scripts/check-shared-database.sh
run "editor invariants" node scripts/check-editor-invariants.mjs

# A database that is not listening turns every postgres step red for a reason that
# has nothing to do with the code; say so instead of blaming the tests (r133).
pg_reachable() {
  local dsn="$1"
  local probe
  probe="$(command -v pg_isready || ls /usr/lib/postgresql/*/bin/pg_isready 2>/dev/null | head -1)"
  if [ -n "$probe" ] && [ -x "$probe" ]; then
    "$probe" -d "$dsn" > /dev/null 2>&1
    return $?
  fi
  python3 - "$dsn" <<'PY'
import socket, sys, urllib.parse as u
parsed = u.urlparse(sys.argv[1])
host = parsed.hostname or "127.0.0.1"
port = parsed.port or 5432
sock = socket.socket()
sock.settimeout(2)
try:
    sock.connect((host, port))
except OSError:
    sys.exit(1)
sys.exit(0)
PY
}

if [ -n "$EASYTIER_TEST_PG_DSN" ] && ! pg_reachable "$EASYTIER_TEST_PG_DSN"; then
  printf "== %-28s FAIL (database not reachable; check EASYTIER_TEST_PG_DSN)%s" "postgres steps" "$NL"
  summary="$summary
  FAIL  postgres steps (database not reachable)"
  FAILED=1
elif [ -n "$EASYTIER_TEST_PG_DSN" ]; then
  run "postgres tests"   go test $RACE_FLAG -count=1 ./internal/db/... ./internal/httpapi/...
  # The deploy walk above uses SQLite; repeat it against PostgreSQL so the schema
  # bootstrap, restart and reconciliation paths see the other dialect too.
  run "postgres deploy"  env SMOKE_DB_DRIVER=postgres SMOKE_DB_DSN="$EASYTIER_TEST_PG_DSN" SMOKE_PG_RESET=1 bash scripts/smoke-deploy.sh
  # The API suite above runs on SQLite; run it again against PostgreSQL in a
  # private schema so the two dialects cannot drift apart unnoticed.
  run "postgres api"     env EASYTIER_HTTPAPI_PG="$EASYTIER_TEST_PG_DSN" go test $RACE_FLAG -count=1 ./internal/httpapi/...
  # The reconcile, quota and session scenarios get the same treatment.
  run "postgres e2e"     env EASYTIER_E2E_PG="$EASYTIER_TEST_PG_DSN" go test $RACE_FLAG -count=1 ./internal/e2e/...
  # The citation sweep proves every test the ledger cites actually ran. It is
  # opt-in because it re-runs the whole cited set (a few minutes); leaving it out
  # silently is what let skipped evidence stay invisible (review team, r134).
  if [ -n "$EASYTIER_FULL_GATE" ]; then
    run "cited tests"      ./scripts/check-citations.sh
  else
    printf '== %-28s SKIP (set EASYTIER_FULL_GATE=1 to run ./scripts/check-citations.sh)%s' "cited tests" "$NL"
    summary="$summary
  SKIP  cited tests (set EASYTIER_FULL_GATE=1)"
  fi
  # Real EasyTier clients are the only proof the control plane speaks the upstream
  # protocol. Opt-in because it needs downloaded release binaries:
  #   ET_CLIENTS="2.4.0:/path/a 2.7.0:/path/b" EASYTIER_REAL_CLIENTS=1 ./scripts/gate.sh
  if [ -n "$EASYTIER_REAL_CLIENTS" ]; then
    run "real clients"    ./scripts/real-instance-matrix.sh
  else
    printf '== %-28s SKIP (set EASYTIER_REAL_CLIENTS=1 with ET_CLIENTS)%s' "real clients" "$NL"
  fi
  # The overlay smoke builds a two node network through the console. Exit 3 means
  # peering worked but the payload step needs a tun device: a skip, not a pass and
  # not a failure.
  # Ask the machine first instead of letting the payload step fail: the reason a
  # data-plane run cannot happen (no driver, no node, no CAP_NET_ADMIN) is worth
  # more than a timeout, and it has to be a SKIP rather than a pass.
  if ./scripts/tun-check.sh > /tmp/tun-check.log 2>&1; then tun_status=0; else tun_status=$?; fi
  if [ "$tun_status" -eq 3 ]; then
    reason=$(head -n 1 /tmp/tun-check.log)
    printf '== %-28s SKIP%s' "data plane" "$NL"
    echo "    "$reason
    summary="$summary
  SKIP  data plane ($reason)"
    EASYTIER_OVERLAY_SMOKE=""
  elif [ "$tun_status" -ne 0 ]; then
    printf '== %-28s FAIL%s' "data plane" "$NL"
    echo "    tun-check exited "$tun_status
    summary="$summary
  FAIL  data plane"
    FAILED=1
  else
    printf '== %-28s PASS%s' "data plane" "$NL"
    echo "    a tun interface can be created"
    summary="$summary
  PASS  data plane (tun available)"
  fi

  if [ -n "$EASYTIER_OVERLAY_SMOKE" ]; then
    if ./scripts/overlay-smoke.sh > /tmp/overlay-smoke.log 2>&1; then overlay_status=0; else overlay_status=$?; fi
    if [ "$overlay_status" -eq 0 ]; then
      printf "== %-28s PASS%s" "overlay smoke" "$NL"
      summary="$summary
  PASS  overlay smoke"
    elif [ "$overlay_status" -eq 3 ]; then
      printf '== %-28s SKIP (peering ok; payload needs a tun device)%s' "overlay smoke" "$NL"
      summary="$summary
  SKIP  overlay smoke (no tun device)"
    else
      printf "== %-28s FAIL%s" "overlay smoke" "$NL"
      tail -n 20 /tmp/overlay-smoke.log | sed "s/^/    /"
      summary="$summary
  FAIL  overlay smoke"
      FAILED=1
    fi
  else
    printf '== %-28s SKIP (set EASYTIER_OVERLAY_SMOKE=1 with ET_BIN)%s' "overlay smoke" "$NL"
  fi
else
  printf '== %-28s SKIP (set EASYTIER_TEST_PG_DSN to enable)\n' "postgres tests"
  summary="$summary\n  SKIP  postgres tests (no DSN)"
fi

printf '\n== gate summary ==%b\n' "$summary"
if [ "$FAILED" -ne 0 ]; then
  echo "
result: FAIL"
  exit 1
fi
echo "
result: PASS"
