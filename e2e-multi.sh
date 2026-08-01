#!/usr/bin/env bash
set -u
cd "$(dirname "$0")" 2>/dev/null || cd /home/jaym/workspace/river-mywm
RT=/tmp/wimy-multi-rt
rm -rf "$RT"; mkdir -p "$RT"; chmod 700 "$RT"
export XDG_RUNTIME_DIR="$RT"
export WLR_BACKENDS=headless
export WLR_RENDERER=pixman
export WLR_HEADLESS_OUTPUTS=2

cleanup() { [ -n "${RIVER_PID:-}" ] && kill "$RIVER_PID" 2>/dev/null; wait "$RIVER_PID" 2>/dev/null; }
trap cleanup EXIT

printf 'terminal "foot"\n' > "$RT/config.kdl"
river -log-level warning -c "./bin/wimy -config $RT/config.kdl" >"$RT/river.log" 2>&1 &
RIVER_PID=$!
SOCK=""
for i in $(seq 1 50); do
  SOCK=$(ls "$RT"/wimy-*.sock 2>/dev/null | head -1); [ -n "$SOCK" ] && break; sleep 0.2
done
[ -n "$SOCK" ] || { echo "FAIL: no socket"; cat "$RT/river.log"; exit 1; }
export WAYLAND_DISPLAY=$(basename "$SOCK" | sed 's/wimy-\(.*\)\.sock/\1/')
ctl() { ./bin/wimyctl -socket "$SOCK" "$@"; }
sleep 1

PASS=0; FAIL=0
check() { if [ "$2" = "$3" ]; then PASS=$((PASS+1)); echo "ok: $1"; else FAIL=$((FAIL+1)); echo "FAIL: $1 (got '$2', want '$3')"; fi; }

NOUT=$(ctl state | jq '.outputs | length')
check "two outputs detected" "$NOUT" 2
VIEWS=$(ctl state | jq -r '[.outputs[].view] | join(",")')
check "outputs have distinct views" "$VIEWS" "1,2"

ctl run 'spawn foot' >/dev/null; sleep 1.5
NWIN=$(ctl state | jq '.windows | length')
check "window spawned" "$NWIN" 1

# select the view shown on the other output -> swap
ctl run 'view 2' >/dev/null; sleep 0.3
VIEWS=$(ctl state | jq -r '[.outputs[].view] | join(",")')
check "swap on select" "$VIEWS" "2,1"

# output rects should be side by side
X1=$(ctl state | jq '.outputs[1].rect.X')
check "second output positioned right of first" "$([ "$X1" -gt 0 ] && echo yes)" yes

# focus-output switches active output
FOC0=$(ctl state | jq -r '[.outputs[].focused] | join(",")')
ctl run 'focus-output next' >/dev/null; sleep 0.3
FOC1=$(ctl state | jq -r '[.outputs[].focused] | join(",")')
check "focus-output next toggles active output" "$FOC0,$FOC1" "true,false,false,true"

# cycle view skips view shown on other output
ctl run 'focus-output next' >/dev/null; sleep 0.2  # back to output 0 (view 2)
ctl run 'view-next' >/dev/null; sleep 0.3
V0=$(ctl state | jq -r '.outputs[0].view')
check "view-next wraps skipping shown view (lands on fresh/none)" "$([ "$V0" != 1 ] && echo yes)" yes

echo "== multi PASS=$PASS FAIL=$FAIL =="
grep -i 'error\|protocol' "$RT/river.log" | grep -v xkbcomp | head -5
[ "$FAIL" = 0 ]
