#!/usr/bin/env bash
# End-to-end smoke test for reqmin: builds the binary and the loopback demo
# API, then walks the real workflow — dry-run enumeration, minimization of a
# fat browser-copied request, raw-HTTP input on stdin, JSON reports, oracle
# mismatches, and the exit-code contract. No external network, idempotent,
# finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
SRV_PID=""
cleanup() {
  [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/reqmin"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/reqmin) || fail "go build failed"
(cd "$ROOT" && go build -o "$WORKDIR/demo-server" ./examples/demo-server) \
  || fail "demo-server build failed"

echo "2. version matches manifest"
"$BIN" --version | grep -qx "reqmin 0.1.0" || fail "--version mismatch"

echo "3. start the loopback demo API"
"$WORKDIR/demo-server" -addr 127.0.0.1:0 > "$WORKDIR/server.log" &
SRV_PID=$!
for _ in $(seq 1 50); do
  grep -q "listening on" "$WORKDIR/server.log" 2>/dev/null && break
  sleep 0.1
done
URL="$(sed -n 's#.*listening on \(http://[^ ]*\).*#\1#p' "$WORKDIR/server.log")"
[ -n "$URL" ] || fail "server never reported its address"
sed "s#http://127.0.0.1:8641#$URL#" "$ROOT/examples/copied.curl" > "$WORKDIR/copied.curl"

echo "4. dry run enumerates every removable item without sending"
DRY="$("$BIN" --dry-run "$WORKDIR/copied.curl")" || fail "dry-run failed"
echo "$DRY" | grep -q "22 removable (14 headers, 4 query params, 4 cookies)" \
  || fail "dry-run summary wrong: $(echo "$DRY" | tail -1)"

echo "5. minimize the fat capture down to token + user"
OUT="$("$BIN" "$WORKDIR/copied.curl" 2> "$WORKDIR/report.txt")" || fail "minimize failed"
echo "$OUT" | grep -q "Authorization: Bearer demo-token" || fail "token dropped: $OUT"
echo "$OUT" | grep -q "user=42" || fail "user param dropped: $OUT"
for junk in Cookie Referer Sec-Fetch session=9f3ab1 tab=recent; do
  echo "$OUT" | grep -q "$junk" && fail "junk survived: $junk"
done
grep -q "result: kept 2 of 22 items" "$WORKDIR/report.txt" \
  || fail "report should say kept 2 of 22: $(cat "$WORKDIR/report.txt")"

echo "6. the minimized command still reproduces via real curl"
if command -v curl >/dev/null 2>&1; then
  # --noproxy keeps loopback traffic away from any configured proxy.
  eval "${OUT/curl /curl -s --noproxy '*' }" | grep -q '"ok":true' \
    || fail "minimized curl no longer reproduces"
else
  echo "   (curl not found — skipping replay)"
fi

echo "7. raw HTTP on stdin, raw output"
HOSTPORT="${URL#http://}"
RAW="$(printf 'GET /api/orders?user=42&junk=1 HTTP/1.1\nHost: %s\nAuthorization: Bearer demo-token\nX-Extra: x\n' "$HOSTPORT" \
  | "$BIN" -q --format raw -)" || fail "raw stdin run failed"
echo "$RAW" | head -1 | grep -qx "GET /api/orders?user=42 HTTP/1.1" \
  || fail "raw output wrong: $(echo "$RAW" | head -1)"
echo "$RAW" | grep -q "X-Extra" && fail "raw output kept junk header"

echo "8. JSON report is machine-readable"
"$BIN" -q --format json "$WORKDIR/copied.curl" > "$WORKDIR/report.json" \
  || fail "json run failed"
grep -q '"baseline_status": 200' "$WORKDIR/report.json" || fail "json baseline missing"
grep -q '"minimal_curl"' "$WORKDIR/report.json" || fail "json minimal_curl missing"

echo "9. --keep pins an otherwise-removable header"
"$BIN" -q --keep accept-language "$WORKDIR/copied.curl" \
  | grep -q "Accept-Language" || fail "--keep did not pin the header"

echo "10. oracle mismatch exits 1"
set +e
"$BIN" --expect-status 418 "$WORKDIR/copied.curl" >/dev/null 2>&1
CODE=$?
set -e
[ "$CODE" -eq 1 ] || fail "oracle mismatch should exit 1, got $CODE"

echo "11. usage errors exit 2"
set +e
"$BIN" >/dev/null 2>&1; C1=$?
"$BIN" --format yaml "$WORKDIR/copied.curl" >/dev/null 2>&1; C2=$?
set -e
[ "$C1" -eq 2 ] && [ "$C2" -eq 2 ] || fail "usage errors should exit 2, got $C1/$C2"

echo "12. unreachable target exits 3"
set +e
"$BIN" 'curl http://127.0.0.1:1/' >/dev/null 2>&1
CODE=$?
set -e
[ "$CODE" -eq 3 ] || fail "network failure should exit 3, got $CODE"

echo "SMOKE OK"
