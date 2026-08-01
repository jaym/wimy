#!/usr/bin/env bash
# End-to-end mouse test: sloppy focus (hover), Mod+drag column resize,
# floating move and corner resize via virtual pointer, plus the grow
# keybinding.
set -u
cd "$(dirname "$0")"
go build -o bin/wimy ./cmd/wimy && go build -o bin/ptrinject ./cmd/ptrinject && go build -o bin/keyinject ./cmd/keyinject || exit 1
RT=/tmp/wimy-mouse-rt
rm -rf "$RT"; mkdir -p "$RT"; chmod 700 "$RT"
export XDG_RUNTIME_DIR="$RT" WLR_BACKENDS=headless WLR_RENDERER=pixman
cat > "$RT/config.kdl" <<'KDL'
terminal "foot"
KDL
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
winrect() { ctl state | jq -r --argjson id "$1" '[.windows[] | select(.id==$id)][0].rect | "\(.X),\(.Y),\(.W),\(.H)"'; }

# two windows, two columns (boundary at x=640)
ctl run 'spawn foot' >/dev/null; sleep 1.2
ctl run 'spawn foot' >/dev/null; sleep 1.2
ctl run 'move right' >/dev/null; sleep 0.3
check "setup: 2 columns" "$(ctl state | jq '.views[0].columns')" 2
check "setup: window 2 in column 2" "$(winrect 2 | cut -d, -f1)" 640

# --- Mod+right-drag the boundary 200px right ---
./bin/ptrinject -from 700,300 -to 900,300 -button right -mod
sleep 0.5
NEWX=$(winrect 2 | cut -d, -f1)
check "column boundary dragged right" "$([ "$NEWX" -gt 800 ] && echo yes)" yes

# --- grow keybinding (Mod-Ctrl-l) ---
ctl run 'focus left' >/dev/null; sleep 0.3  # focus column 1 (tiled)
W_BEFORE=$(winrect 1 | cut -d, -f3)
./bin/keyinject "logo+ctrl+l"
sleep 0.8
W_AFTER=$(winrect 1 | cut -d, -f3)
check "grow right keybinding widens column" "$([ "$W_AFTER" -gt "$W_BEFORE" ] && echo yes)" yes

# --- Mod+left-drag a floating window ---
ctl run 'focus right' >/dev/null; sleep 0.2  # focus window 2
ctl run 'toggle-float' >/dev/null; sleep 0.3
BEFORE=$(winrect 2)
X0=$(echo "$BEFORE" | cut -d, -f1); Y0=$(echo "$BEFORE" | cut -d, -f2)
CX=$((X0 + 100)); CY=$((Y0 + 20))
./bin/ptrinject -from "$CX,$CY" -to "$((CX+150)),$((CY+90))" -button left -mod
sleep 0.5
AFTER=$(winrect 2)
X1=$(echo "$AFTER" | cut -d, -f1); Y1=$(echo "$AFTER" | cut -d, -f2)
check "float window moved by drag" "$([ "$X1" -gt "$X0" ] && [ "$Y1" -gt "$Y0" ] && echo yes)" yes

# --- Mod+right-drag bottom-right corner resizes ---
W0=$(echo "$AFTER" | cut -d, -f3); H0=$(echo "$AFTER" | cut -d, -f4)
BX=$((X1 + W0 - 5)); BY=$((Y1 + H0 - 5))
./bin/ptrinject -from "$BX,$BY" -to "$((BX+120)),$((BY+80))" -button right -mod
sleep 0.5
AFTER2=$(winrect 2)
W1=$(echo "$AFTER2" | cut -d, -f3); H1=$(echo "$AFTER2" | cut -d, -f4)
check "float window resized by corner drag" "$([ "$W1" -gt "$W0" ] && [ "$H1" -gt "$H0" ] && echo yes)" yes

# --- sloppy focus: hover focuses without clicking ---
# window 1 is tiled (full width), window 2 floats on top; hover a
# point over window 1 that the floating window does not cover.
wincenter() { ctl state | jq -r --argjson id "$1" '[.windows[] | select(.id==$id)][0].rect | "\(.X + (.W/2|floor)),\(.Y + (.H/2|floor))"'; }
focused() { ctl state | jq '[.windows[] | select(.focused)][0].id'; }
R2X=$(ctl state | jq -r '[.windows[] | select(.id==2)][0].rect.X')
P1="$((R2X - 40)),600"
P2=$(wincenter 2)
./bin/ptrinject -from "$P1" -to "${P1%,*},$(( ${P1#*,} + 15 ))" -button none
sleep 0.5
check "hover focuses tiled window" "$(focused)" 1
./bin/ptrinject -from "$P2" -to "${P2%,*},$(( ${P2#*,} + 15 ))" -button none
sleep 0.5
check "hover focuses floating window" "$(focused)" 2
# pointer rests over window 2; a keyboard focus command must not be
# overridden by the resting pointer
ctl run 'focus-window 1' >/dev/null; sleep 0.3
check "keyboard focus wins over resting pointer" "$(focused)" 1

kill $RIVER_PID 2>/dev/null; wait $RIVER_PID 2>/dev/null
echo "== mouse PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" = 0 ]
