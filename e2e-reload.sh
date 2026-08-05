#!/usr/bin/env bash
# End-to-end test: hot config reload (`wimyctl run reload`).
# Keybinding swap proven with real injected keys, border re-apply via
# WAYLAND_DEBUG, titlebar off/on via geometry, autostart reconcile via
# pgrep, invalid config keeps the old one.
set -u
cd "$(dirname "$0")"
go build -o bin/wimy ./cmd/wimy && go build -o bin/wimyctl ./cmd/wimyctl && go build -o bin/keyinject ./cmd/keyinject || exit 1

RT=/tmp/wimy-reload-rt
rm -rf "$RT"; mkdir -p "$RT"; chmod 700 "$RT"
export XDG_RUNTIME_DIR="$RT" WLR_BACKENDS=headless WLR_RENDERER=pixman

CFG="$RT/config.kdl"
cat > "$CFG" <<'KDL'
terminal "foot"
bind "Mod-x" { spawn "foot"; }
KDL

cleanup() {
  [ -n "${RIVER_PID:-}" ] && kill "$RIVER_PID" 2>/dev/null
  wait "$RIVER_PID" 2>/dev/null
  pkill -f "sleep 100[01]" 2>/dev/null
}
trap cleanup EXIT

river -log-level warning -c "WAYLAND_DEBUG=1 ./bin/wimy -config $CFG -log $RT/wimy.log" >"$RT/river.log" 2>&1 &
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
nwin() { ctl state | jq '.windows | length'; }
focused_h() { ctl state | jq '[.windows[] | select(.focused)][0].rect.H'; }
# log assertions must only see lines written since MARK
MARK=1
marklog() { MARK=$(wc -l < "$RT/wimy.log"); }
newlog() { tail -n +$((MARK+1)) "$RT/wimy.log"; }

# --- 1: custom bind fires before reload ---------------------------
./bin/keyinject "logo+x"
sleep 2
check "Mod-x spawned a window" "$(nwin)" 1

# --- 2: reload swaps keybindings and border ------------------------
SB_BEFORE=$(grep -c 'set_borders(' "$RT/wimy.log")
marklog
cat > "$CFG" <<'KDL'
terminal "foot"
border width=3
bind "Mod-y" { spawn "foot"; }
KDL
ctl run reload >/dev/null
sleep 1
check "reload report mentions keybindings" "$(newlog | grep -c 'config reloaded:.*keybindings')" 1
SB_AFTER=$(grep -c 'set_borders(' "$RT/wimy.log")
if [ "$SB_AFTER" -gt "$SB_BEFORE" ]; then SB_RE=yes; else SB_RE=no; fi
check "borders re-applied after reload" "$SB_RE" yes
if [ "$(newlog | grep -cE 'set_borders\([0-9]+, ?3,')" -ge 1 ]; then W3=yes; else W3=no; fi
check "new border width 3 applied" "$W3" yes

./bin/keyinject "logo+y"
sleep 2
check "new bind Mod-y spawns" "$(nwin)" 2
./bin/keyinject "logo+x"
sleep 1.5
check "old bind Mod-x is dead" "$(nwin)" 2

# --- 3: titlebar off ------------------------------------------------
H_BEFORE=$(focused_h)
marklog
cat > "$CFG" <<'KDL'
terminal "foot"
border width=3
titlebar "off"
bind "Mod-y" { spawn "foot"; }
KDL
ctl run reload >/dev/null
sleep 1
check "reload report mentions titlebar" "$(newlog | grep -c 'config reloaded:.*titlebar')" 1
H_AFTER=$(focused_h)
check "content grew by titlebar height" "$((H_AFTER - H_BEFORE))" 22
if [ "$(newlog | grep -cE 'set_borders\(15,')" -ge 1 ]; then TOP=yes; else TOP=no; fi
check "borders regained top edge" "$TOP" yes

# --- 4: titlebar back on ---------------------------------------------
DECO_BEFORE=$(grep -c 'get_decoration_above' "$RT/wimy.log")
SYNC_BEFORE=$(grep -c 'sync_next_commit' "$RT/wimy.log")
cat > "$CFG" <<'KDL'
terminal "foot"
border width=3
bind "Mod-y" { spawn "foot"; }
KDL
ctl run reload >/dev/null
sleep 1
if [ "$(grep -c 'get_decoration_above' "$RT/wimy.log")" -gt "$DECO_BEFORE" ]; then DECO=yes; else DECO=no; fi
check "decorations recreated" "$DECO" yes
if [ "$(grep -c 'sync_next_commit' "$RT/wimy.log")" -gt "$SYNC_BEFORE" ]; then SYNC=yes; else SYNC=no; fi
check "titlebars re-rendered (no blank bars)" "$SYNC" yes
check "content shrank back" "$((H_AFTER - $(focused_h)))" 22

# --- 5: autostart added -----------------------------------------------
cat > "$CFG" <<'KDL'
terminal "foot"
border width=3
bind "Mod-y" { spawn "foot"; }
autostart {
	exec "sleep 1000"
	exec "sleep 1001"
}
KDL
ctl run reload >/dev/null
sleep 1
if [ "$(pgrep -f 'sleep 1000' | wc -l)" -ge 1 ]; then S1=yes; else S1=no; fi
check "added autostart entry spawned" "$S1" yes
KEEP_PID=$(pgrep -f 'sleep 1001' | head -1)
check "second entry running" "$([ -n "$KEEP_PID" ] && echo yes)" yes

# --- 6: autostart removed, unchanged keeps its PID --------------------
marklog
cat > "$CFG" <<'KDL'
terminal "foot"
border width=3
bind "Mod-y" { spawn "foot"; }
autostart {
	exec "sleep 1001"
}
KDL
ctl run reload >/dev/null
sleep 1.5
check "removed entry killed" "$(pgrep -f 'sleep 1000' | wc -l)" 0
check "unchanged entry kept same PID" "$(pgrep -f 'sleep 1001' | head -1)" "$KEEP_PID"
check "reload report mentions autostart" "$(newlog | grep -c 'config reloaded:.*autostart')" 1

# --- 7: invalid config keeps the old one ------------------------------
WINS_BEFORE=$(nwin)
marklog
printf 'this is not { valid kdl' > "$CFG"
ctl run reload >/dev/null
sleep 1
check "reload failure logged" "$(newlog | grep -c 'keeping old config')" 1
./bin/keyinject "logo+y"
sleep 2
check "wimy alive, old binds still work" "$(( $(nwin) - WINS_BEFORE ))" 1

# --- 8: mod swap re-resolves bindings and pointer grabs ---------------
marklog
WINS_BEFORE=$(nwin)
cat > "$CFG" <<'KDL'
terminal "foot"
mod "Mod1"
bind "Mod-z" { spawn "foot"; }
KDL
ctl run reload >/dev/null
sleep 1
check "reload report mentions modifier" "$(newlog | grep -c 'config reloaded:.*modifier')" 1
./bin/keyinject "alt+z"
sleep 2
check "alt+z (new mod) spawns" "$(( $(nwin) - WINS_BEFORE ))" 1
./bin/keyinject "logo+z"
sleep 1.5
check "logo+z (old mod) is dead" "$(( $(nwin) - WINS_BEFORE ))" 1

# restore a valid file so teardown is clean
cat > "$CFG" <<'KDL'
terminal "foot"
KDL

echo
echo "== PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" = 0 ]
