#!/usr/bin/env bash
# End-to-end keybinding test: injects real key events via a virtual
# keyboard and checks they trigger wimy commands.
set -u
cd "$(dirname "$0")"
go build -o bin/wimy ./cmd/wimy && go build -o bin/keyinject ./cmd/keyinject || exit 1
RT=/tmp/wimy-keys-rt
rm -rf "$RT"; mkdir -p "$RT"; chmod 700 "$RT"
export XDG_RUNTIME_DIR="$RT" WLR_BACKENDS=headless WLR_RENDERER=pixman

# test config: foot (headless-capable) instead of alacritty
cat > "$RT/config.kdl" <<'KDL'
terminal "foot"
KDL

cleanup() { [ -n "${RIVER_PID:-}" ] && kill "$RIVER_PID" 2>/dev/null; wait "$RIVER_PID" 2>/dev/null; }
trap cleanup EXIT

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

echo "== injecting super+enter =="
./bin/keyinject "logo+enter"
sleep 2
NWIN=$(ctl state | jq '.windows | length')
check "super+enter spawned a window" "$NWIN" 1

echo "== injecting super+shift+c (kill) =="
./bin/keyinject "logo+shift+c"
sleep 2
NWIN=$(ctl state | jq '.windows | length')
check "super+shift+c killed the window" "$NWIN" 0

echo "== injecting super+enter, super+2 =="
./bin/keyinject "logo+enter" "logo+2"
sleep 2
OUTVIEW=$(ctl state | jq -r '.outputs[0].view')
check "super+2 selected view 2" "$OUTVIEW" 2

echo "== injecting super+1, super+d/s/m modes =="
./bin/keyinject "logo+1" "logo+s"
sleep 1.5
MODE=$(ctl state | jq -r '.views[0].mode')
check "super+s set stack mode" "$MODE" stack
./bin/keyinject "logo+d"
sleep 1
MODE=$(ctl state | jq -r '.views[0].mode')
check "super+d set default mode" "$MODE" default

echo "== injecting super+enter, super+shift+l (move right = new column) =="
./bin/keyinject "logo+enter" "logo+shift+l"
sleep 2
COLS=$(ctl state | jq '.views[0].columns')
check "super+shift+l created second column" "$COLS" 2

grep -i 'error' "$RT/river.log" | grep -v xkbcomp | grep -v 'Broken pipe' | head -5
echo "== keys PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" = 0 ]
