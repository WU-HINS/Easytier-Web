#!/usr/bin/env bash
# Prints the outcome counts for the gate summary.
#
# Counting lives here rather than inline so the step list in gate.sh stays readable
# and parseable: a summary that only says "result: PASS" invites the reader to round
# it up to "everything verified" (review team, r253). A skipped check is not a pass.
set -uo pipefail
summary=$(cat)
MARK_PASS='^  PASS  '
MARK_FAIL='^  FAIL  '
MARK_OMITTED='^  SKIP  '

pass=$(printf '%s
' "$summary" | grep -c "$MARK_PASS" || true)
fail=$(printf '%s
' "$summary" | grep -c "$MARK_FAIL" || true)
omitted=$(printf '%s
' "$summary" | grep -c "$MARK_OMITTED" || true)

printf '
  totals: PASS %s / FAIL %s / SKIP %s
' "$pass" "$fail" "$omitted"
if [ "$omitted" -ne 0 ]; then
  printf '  skip list: '
  printf '%s
' "$summary" | grep "$MARK_OMITTED" | sed 's/^  SKIP  //' | paste -sd '; ' -
  printf '
'
fi
