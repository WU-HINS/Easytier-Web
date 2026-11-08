#!/usr/bin/env bash
# Every test the ledger or the charter cites has to run and pass, in one sweep.
# A cited test that is skipped is worse than a missing one: the gate stays green
# while the evidence evaporates (found by review in r130).
set -uo pipefail
cd "$(dirname "$0")/.."
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

python3 - <<'PY' > "$WORK/names.txt"
import glob, re
NL = chr(10)
text = open('ai-censorship/ledger.md').read() + NL + open('docs/GOALS.md').read() + NL + open('root.md').read()
for path in glob.glob('ai-censorship/review-*.md'):
    lines = [l for l in open(path, encoding='utf-8').read().split(NL) if not l.startswith(('    ', chr(9)))]
    text += NL + NL.join(lines)
skip = {'TestThisTestDoesNotExist', 'TestThisChecklistEntryIsStale', 'TestGuardThenInsertIsNeverChecked', 'TestGateAuditMissingTestProbe'}
names = sorted({t for t in re.split('[^A-Za-z0-9_]', text) if len(t) > 6 and t.startswith('Test')} - skip)
print(NL.join(names))
PY

COUNT=$(wc -l < "$WORK/names.txt")
if [ "$COUNT" -lt 100 ]; then
  echo "citations: only $COUNT names harvested; the scan is not reading the documents"
  exit 1
fi
PATTERN="^($(paste -sd '|' "$WORK/names.txt"))$"

go test -count=1 -run "$PATTERN" -v ./internal/... > "$WORK/out.log" 2>&1
STATUS=$?

RAN=$(grep -c '^=== RUN   Test' "$WORK/out.log" || true)
SKIPPED=$(grep -c '^--- SKIP' "$WORK/out.log" || true)
FAILED=$(grep -c '^--- FAIL' "$WORK/out.log" || true)

fail=0
if [ "$STATUS" -ne 0 ] || [ "$FAILED" -ne 0 ]; then
  echo "citations: $FAILED cited test(s) failed"
  grep "^--- FAIL" "$WORK/out.log" | head -n 10 | sed "s/^/  /"
  fail=1
fi

# A cited test may only skip when it is declared environment gated and the
# dependency it names is genuinely absent. Anything else is a test that was turned
# off while the ledger still cites it as evidence.
DECLARED=0
if [ "$SKIPPED" -ne 0 ]; then
  UNDECLARED=0
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    dependency=$(awk -F'|' -v n="$name" '$1 == n { print $2 }' scripts/env-gated-tests.txt)
    if [ -z "$dependency" ]; then
      echo "  undeclared skip: $name (not in scripts/env-gated-tests.txt)"
      UNDECLARED=$((UNDECLARED + 1))
      continue
    fi
    if [ -n "$(printenv "$dependency" || true)" ]; then
      echo "  undeclared skip: $name ($dependency is set, so it should have run)"
      UNDECLARED=$((UNDECLARED + 1))
    else
      DECLARED=$((DECLARED + 1))
    fi
  done <<EOF_SKIPS
$(grep "^--- SKIP: Test" "$WORK/out.log" | sed "s/^--- SKIP: //; s/ (.*//")
EOF_SKIPS
  if [ "$UNDECLARED" -ne 0 ]; then
    echo "citations: $UNDECLARED cited test(s) skipped without a declared dependency"
    fail=1
  fi
fi
if [ "$RAN" -lt 100 ]; then
  echo "citations: only $RAN tests ran; the -run pattern is not matching"
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "citations: $RAN cited test(s) ran, $DECLARED skipped for a declared dependency, none failed"
