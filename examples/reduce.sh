#!/usr/bin/env bash
# Runnable demo: start the loopback demo API, then let reqmin shrink the
# fat browser-copied request in copied.curl down to what actually matters.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
SRV_PID=""
cleanup() {
  [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

echo "== building reqmin and the demo server"
(cd "$ROOT" && go build -o "$WORKDIR/reqmin" ./cmd/reqmin)
(cd "$ROOT" && go build -o "$WORKDIR/demo-server" ./examples/demo-server)

echo "== starting the demo API on a free loopback port"
"$WORKDIR/demo-server" -addr 127.0.0.1:0 > "$WORKDIR/server.log" &
SRV_PID=$!
for _ in $(seq 1 50); do
  grep -q "listening on" "$WORKDIR/server.log" 2>/dev/null && break
  sleep 0.1
done
URL="$(sed -n 's#.*listening on \(http://[^ ]*\).*#\1#p' "$WORKDIR/server.log")"
echo "   $URL"

echo "== rewriting copied.curl against that port"
sed "s#http://127.0.0.1:8641#$URL#" "$ROOT/examples/copied.curl" > "$WORKDIR/copied.curl"

echo "== what reqmin sees (dry run)"
"$WORKDIR/reqmin" --dry-run "$WORKDIR/copied.curl"

echo
echo "== minimizing (oracle: same status as baseline)"
"$WORKDIR/reqmin" "$WORKDIR/copied.curl"
