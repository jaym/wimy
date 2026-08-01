#!/usr/bin/env bash
# End-to-end layer shell test: fuzzel must survive and receive focus.
set -u
cd "$(dirname "$0")"
go build -o bin/wimy ./cmd/wimy || exit 1
RT=/tmp/wimy-layer-rt
rm -rf "$RT"; mkdir -p "$RT"; chmod 700 "$RT"
export XDG_RUNTIME_DIR="$RT" WLR_BACKENDS=headless WLR_RENDERER=pixman
cat > "$RT/config.kdl" <<'KDL'
terminal "foot"
KDL
river -log-level warning -c "WAYLAND_DEBUG=1 ./bin/wimy -config $RT/config.kdl -log $RT/wimy.log" >"$RT/river.log" 2>&1 &
RIVER_PID=$!
SOCK=""
for i in $(seq 1 50); do
  SOCK=$(ls "$RT"/wimy-*.sock 2>/dev/null | head -1); [ -n "$SOCK" ] && break; sleep 0.2
done
[ -n "$SOCK" ] || { echo "FAIL: no socket"; cat "$RT/river.log"; exit 1; }
export WAYLAND_DISPLAY=$(basename "$SOCK" | sed 's/wimy-\(.*\)\.sock/\1/')
sleep 1

PASS=0; FAIL=0
check() { if [ "$2" = "$3" ]; then PASS=$((PASS+1)); echo "ok: $1"; else FAIL=$((FAIL+1)); echo "FAIL: $1 (got '$2', want '$3')"; fi; }

# fuzzel with exclusive keyboard focus, like Mod-p launches it
echo "foo" | timeout 5 fuzzel --dmenu > "$RT/fuzzel.out" 2>"$RT/fuzzel.err" &
FZ=$!
sleep 2.5
if kill -0 $FZ 2>/dev/null; then ALIVE=yes; else ALIVE=no; fi
check "fuzzel layer surface survives" "$ALIVE" yes
CLOSED=$(grep -c 'closing layer surface' "$RT/river.log" || true)
check "river did not close layer surfaces" "$CLOSED" 0
NEXA=$(grep -c 'non_exclusive_area' "$RT/wimy.log" || true)
check "non_exclusive_area received" "$([ "$NEXA" -gt 0 ] && echo yes)" yes
DEF=$(grep -c 'set_default' "$RT/wimy.log" || true)
check "set_default sent" "$([ "$DEF" -gt 0 ] && echo yes)" yes
FOC=$(grep -cE 'focus_(exclusive|non_exclusive)' "$RT/wimy.log" || true)
check "layer focus event received" "$([ "$FOC" -gt 0 ] && echo yes)" yes
kill $FZ 2>/dev/null; wait $FZ 2>/dev/null
kill $RIVER_PID 2>/dev/null; wait $RIVER_PID 2>/dev/null
echo "== layer PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" = 0 ]
