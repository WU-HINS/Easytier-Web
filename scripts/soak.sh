#!/usr/bin/env bash
# Long run of the device churn scenario. The round count in the test itself is
# tiny so the gate stays fast; this script is how the long version gets executed
# (review team, round 159).
#
#   ./scripts/soak.sh              # 1000 rounds, SQLite
#   ./scripts/soak.sh 5000         # 5000 rounds
#   EASYTIER_E2E_PG=... ./scripts/soak.sh
set -uo pipefail
cd "$(dirname "$0")/.."
ROUNDS="$1"
if [ -z "$ROUNDS" ]; then ROUNDS=1000; fi
case "$ROUNDS" in
  '*'|*[!0-9]*)
    echo "soak: the round count must be a positive integer" >&2
    exit 2
    ;;
esac
if [ "$ROUNDS" -lt 1 ]; then
  echo "soak: the round count must be at least 1" >&2
  exit 2
fi

start=$(date +%s)
if EASYTIER_SOAK_ROUNDS="$ROUNDS" go test -count=1 -timeout 30m -run TestSoakDeviceChurn ./internal/e2e/; then
  echo "soak: $ROUNDS round(s) converged in $(( $(date +%s) - start ))s"
else
  echo "soak: FAILED after $(( $(date +%s) - start ))s ($ROUNDS rounds)"
  exit 1
fi
