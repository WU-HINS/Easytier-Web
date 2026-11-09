#!/usr/bin/env bash
# Proves the merged artifact works on its own.
#
# scripts/build.sh compiles the client into the daemon. This check builds that binary,
# starts it with no --web-dir and no client/dist on its path, and asks it for the page.
# Without it the merge could break silently: every other gate step serves the client
# from disk, which is the development path.
set -uo pipefail
cd "$(dirname "$0")/.."

if [ ! -f client/dist/index.html ]; then
  echo "embedded client: client/dist is missing; run the client build first" >&2
  exit 1
fi

WORK=${WORK:-$(mktemp -d)}
PID=""
cleanup() {
  [ -n "$PID" ] && kill "$PID" 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

if ! OUT="$WORK" ./scripts/build.sh --skip-client > "$WORK/build.log" 2>&1; then
  echo "embedded client: the merge build failed" >&2
  tail -5 "$WORK/build.log" >&2
  exit 1
fi

PORT=${PORT:-$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')}
EASYTIER_DB_DRIVER=sqlite EASYTIER_DB_DSN="$WORK/et.db" EASYTIER_DB_BACKUP=0 \
  "$WORK/easytier-webd" --http "127.0.0.1:$PORT" --config-server "127.0.0.1:$((PORT + 1))" > "$WORK/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 100); do curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break; sleep 0.2; done

BODY=$(curl -s "http://127.0.0.1:$PORT/" || true)
if ! printf '%s' "$BODY" | grep -q '<!doctype html'; then
  echo "embedded client: the binary did not serve the page (got $(printf '%s' "$BODY" | wc -c) bytes)" >&2
  tail -5 "$WORK/server.log" >&2
  exit 1
fi

DEEP=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/devices" || true)
if [ "$DEEP" != "200" ]; then
  echo "embedded client: a deep link answered $DEEP, want 200 from the client history fallback" >&2
  exit 1
fi

API=$(curl -s -o /dev/null -w '%{content_type}' "http://127.0.0.1:$PORT/api/v1/does-not-exist" || true)
case "$API" in
  application/json*) ;;
  *) echo "embedded client: an unknown API path answered $API, the SPA must not swallow it" >&2; exit 1 ;;
esac

echo "embedded client: the binary serves the page, deep links and leaves /api alone"
