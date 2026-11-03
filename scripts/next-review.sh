#!/usr/bin/env bash
# Prints the next free review-record number and its path.
#
# Record numbers must be consecutive and check-docs enforces it. A previous
# version silently skipped file names it could not parse, so it could hand out a
# number already taken. Unparseable names are reported on stderr now.
set -uo pipefail
cd "${0%/*}/.."
last=0
for path in ai-censorship/review-*.md; do
  [ -e "$path" ] || continue
  name=$(basename "$path")
  if [[ ! "$name" =~ ^review-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-r[0-9]+[.]md$ ]]; then
    echo "next-review: cannot parse $name" >&2
    continue
  fi
  n=${name##*-r}
  n=${n%.md}
  if [ "$n" -gt "$last" ]; then last="$n"; fi
done
next=$((last + 1))
today=$(date +%Y%m%d)
echo "$next ai-censorship/review-$today-r$next.md"
