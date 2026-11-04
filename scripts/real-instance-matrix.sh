#!/usr/bin/env bash
# Runs real-instance-check.sh once per client release (and transport), so "2.4.0+"
# is a claim with a line per combination instead of one happy path.
#
#   ET_CLIENTS="2.4.0:/path/a 2.7.0:/path/b"  ./scripts/real-instance-matrix.sh
#   ET_TRANSPORTS="tcp ws udp"               ./scripts/real-instance-matrix.sh
#
# Each client entry is version:path. A missing binary fails the matrix: a version
# nobody ran must not look supported.
set -uo pipefail
cd "$(dirname "$0")/.."

CLIENTS=$(printenv ET_CLIENTS || true)
TRANSPORTS=$(printenv ET_TRANSPORTS || true)
if [ -z "$TRANSPORTS" ]; then TRANSPORTS=tcp; fi
if [ -z "$CLIENTS" ]; then
  echo "real instance matrix: set ET_CLIENTS="2.4.0:/path/a 2.7.0:/path/b"" >&2
  exit 2
fi

failed=0
ran=0
for entry in $CLIENTS; do
  version=${entry%%:*}
  binary=${entry#*:}
  for transport in $TRANSPORTS; do
    ran=$((ran + 1))
    printf "%-8s %-4s " "$version" "$transport"
    if [ ! -x "$binary" ]; then
      echo "FAIL (no executable at $binary)" >&2
      failed=$((failed + 1))
      continue
    fi
    if env -u ET_CLIENTS ET_BIN="$binary" ET_TRANSPORT="$transport" ./scripts/real-instance-check.sh > "$(mktemp)" 2>&1; then
      echo PASS
    else
      echo FAIL
      tail -3 "$(mktemp)" | sed "s/^/    /"
      failed=$((failed + 1))
    fi
    sleep 2
  done
done

if [ "$ran" -eq 0 ]; then
  echo "real instance matrix: nothing to run" >&2
  exit 2
fi
if [ "$failed" -ne 0 ]; then
  echo "real instance matrix: $failed of $ran combination(s) failed"
  exit 1
fi
echo "real instance matrix: $ran combination(s) accepted the console"
