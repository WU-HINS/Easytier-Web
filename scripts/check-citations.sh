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
  grep '^--- FAIL' "$WORK/out.log" | head -n 10 | sed 's/^/  /'
  fail=1
fi
if [ "$SKIPPED" -ne 0 ]; then
  echo "citations: $SKIPPED cited test(s) skipped; the evidence is missing, not green"
  grep -B1 '^--- SKIP' "$WORK/out.log" | grep '^=== RUN' | head -n 10 | sed 's/^/  /'
  fail=1
fi
if [ "$RAN" -lt 100 ]; then
  echo "citations: only $RAN tests ran; the -run pattern is not matching"
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "citations: $RAN cited test(s) ran, none skipped, none failed"
