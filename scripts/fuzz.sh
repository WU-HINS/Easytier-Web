#!/usr/bin/env bash
# Extended fuzzing for the protocol layer. Not part of gate.sh: the seed corpus
# already runs inside "go test", this script explores for new inputs.
set -eo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
DURATION="$1"
if [ -z "$DURATION" ]; then DURATION=30; fi

echo "== FuzzDecodePacket ($DURATION s) =="
go test -run='^$' -fuzz=FuzzDecodePacket -fuzztime="$DURATION"s ./internal/wire/
echo "== FuzzFeedPacket ($DURATION s) =="
go test -run='^$' -fuzz=FuzzFeedPacket -fuzztime="$DURATION"s ./internal/rpc/
echo "== fuzz complete =="
