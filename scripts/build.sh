#!/usr/bin/env bash
# Builds one artifact: the daemon with the client inside it.
#
# The frontend and the backend are separate projects - client/ builds with npm, the
# daemon builds with Go, and neither reads the other's source. They meet here once:
# the client is bundled, copied into internal/webui/dist, and compiled into the binary.
# The result runs anywhere on its own, with no adjacent dist directory to keep in sync.
#
#   ./scripts/build.sh                # client + daemon
#   ./scripts/build.sh --skip-client  # daemon only, reusing the last client build
#
# Development does not need this script: run the daemon with --web-dir ./client/dist
# and the client with npm run dev.
set -euo pipefail
cd "$(dirname "$0")/.."

SKIP_CLIENT=0
for arg in "$@"; do
  case "$arg" in
    --skip-client) SKIP_CLIENT=1 ;;
    *) echo "build: unknown argument $arg" >&2; exit 2 ;;
  esac
done

OUT=${OUT:-dist}
TARGET=internal/webui/dist

if [ "$SKIP_CLIENT" -eq 0 ]; then
  echo "build: client"
  ( cd client && npm run build )
fi

if [ ! -f client/dist/index.html ]; then
  echo "build: client/dist/index.html is missing; run without --skip-client first" >&2
  exit 1
fi

echo "build: embedding client/dist into $TARGET"
rm -rf "$TARGET"
mkdir -p "$TARGET"
cp -R client/dist/. "$TARGET/"

if [ ! -f "$TARGET/index.html" ]; then
  echo "build: the copy left no index.html behind" >&2
  exit 1
fi

echo "build: daemon"
mkdir -p "$OUT"
go build -trimpath -o "$OUT/easytier-webd" ./cmd/easytier-webd

size=$(du -h "$OUT/easytier-webd" | cut -f1)
echo "build: $OUT/easytier-webd ($size) carries the client; no --web-dir needed"
