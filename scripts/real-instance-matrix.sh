#!/usr/bin/env bash
# Runs real-instance-check.sh once per client release, so "2.4.0+" is a claim with
# a line per version instead of one happy path.
#
#   ET_CLIENTS="2.4.0:/path/a 2.5.0:/path/b 2.7.0:/path/c" ./scripts/real-instance-matrix.sh
#
# Each entry is version:path. A missing binary fails the matrix: a version nobody
# ran must not look supported.
set -uo pipefail
cd "$(dirname "$0")/.."

CLIENTS=$(printenv ET_CLIENTS || true)
if [ -z "$CLIENTS" ]; then
  echo "real instance matrix: set ET_CLIENTS="2.4.0:/path/a 2.7.0:/path/b"" >&2
  exit 2
fi

failed=0
ran=0
for entry in $CLIENTS; do
  version=${entry%%:*}
  binary=${entry#*:}
  ran=$((ran + 1))
  echo "== $version =="
  if [ ! -x "$binary" ]; then
    echo "  $version: no executable at $binary" >&2
    failed=$((failed + 1))
    continue
  fi
  if ET_BIN="$binary" ./scripts/real-instance-check.sh; then
    echo "  $version: PASS"
  else
    echo "  $version: FAIL" >&2
    failed=$((failed + 1))
  fi
done

if [ "$ran" -eq 0 ]; then
  echo "real instance matrix: no clients listed" >&2
  exit 2
fi
if [ "$failed" -ne 0 ]; then
  echo "real instance matrix: $failed of $ran release(s) failed"
  exit 1
fi
echo "real instance matrix: $ran release(s) accepted the console"
