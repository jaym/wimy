#!/usr/bin/env bash
# End-to-end test: headless river + wimy, driven via wimyctl.
# Uses foot for client windows (shm rendering works headless;
# alacritty requires GL).
set -u
cd "$(dirname "$0")"

RT=/tmp/wimy-test-rt
rm -rf "$RT"; mkdir -p "$RT"; chmod 700 "$RT"
export XDG_RUNTIME_DIR="$RT"
export WLR_BACKENDS=headless
export WLR_RENDERER=pixman

PASS=0; FAIL=0
check() { # check <desc> <actual> <expected>
  if [ "$2" = "$3" ]; then PASS=$((PASS+1)); echo "ok: $1"
  else FAIL=$((FAIL+1)); echo "FAIL: $1 (got '$2', want '$3')"; fi
}

cleanup() {
  [ -n "${RIVER_PID:-}" ] && kill "$RIVER_PID" 2>/dev/null
  wait "$RIVER_PID" 2>/dev/null
}
trap cleanup EXIT

printf 'terminal "foot"\n' > "$RT/config.kdl"
river -log-level warning -c "./bin/wimy -config $RT/config.kdl" >"$RT/river.log" 2>&1 &
RIVER_PID=$!

SOCK=""
for i in $(seq 1 50); do
  SOCK=$(ls "$RT"/wimy-*.sock 2>/dev/null | head -1)
  [ -n "$SOCK" ] && break
  sleep 0.2
done
[ -n "$SOCK" ] || { echo "FAIL: no socket"; tail -30 "$RT/river.log"; exit 1; }
export WAYLAND_DISPLAY=$(basename "$SOCK" | sed 's/wimy-\(.*\)\.sock/\1/')
echo "socket: $SOCK (display $WAYLAND_DISPLAY)"

ctl() { ./bin/wimyctl -socket "$SOCK" "$@"; }
sleep 0.5

# --- view-n through empty views (regression: GC compaction) -------
ctl run 'view-n 2' >/dev/null
check "view-n 2 on empty session" "$(ctl state | jq -r '.outputs[0].view')" 2
ctl run 'view-n 1' >/dev/null
check "view-n 1 back from empty view 2" "$(ctl state | jq -r '.outputs[0].view')" 1

# --- client spawn -------------------------------------------------
ctl run 'spawn foot' >/dev/null
sleep 1.5
check "one window after spawn" "$(ctl state | jq '.windows | length')" 1
TAGS1=$(ctl state | jq -r '.windows[0].tags | join(",")')
check "window tagged 1" "$TAGS1" 1

ctl run 'spawn foot' >/dev/null
sleep 1.5
check "two windows" "$(ctl state | jq '.windows | length')" 2
check "new window focused" "$(ctl state | jq '[.windows[] | select(.focused)] | length')" 1

# --- modes --------------------------------------------------------
ctl run 'mode stack' >/dev/null
check "stack mode set" "$(ctl state | jq -r '.views[0].mode')" stack
ctl run 'mode max' >/dev/null
check "max mode set" "$(ctl state | jq -r '.views[0].mode')" max
ctl run 'mode default' >/dev/null
check "default mode set" "$(ctl state | jq -r '.views[0].mode')" default

# --- columns ------------------------------------------------------
ctl run 'move right' >/dev/null
check "two columns after move right" "$(ctl state | jq '.views[0].columns')" 2

# --- views ----------------------------------------------------------
ctl run 'view-n 2' >/dev/null
OUTVIEW=$(ctl state | jq -r '.outputs[0].view')
check "view-n 2 selects view 2" "$OUTVIEW" 2
NWIN=$(ctl state | jq '.windows | length')
check "window count unchanged on other view" "$NWIN" 2
VIEWS=$(ctl state | jq -r '[.views[].name] | join(",")')
check "view 1 kept (occupied)" "$VIEWS" "1,2"

ctl run 'view-n 1' >/dev/null
OUTVIEW=$(ctl state | jq -r '.outputs[0].view')
check "back on view 1" "$OUTVIEW" 1

# view 2 is destroyed when left empty; view-n 2 must recreate it
ctl run 'view-n 2' >/dev/null
ctl run 'view-n 3' >/dev/null
VIEWS=$(ctl state | jq -r '[.views[].name] | join(",")')
check "empty view 2 GCd (1 kept, occupied)" "$VIEWS" "1,3"
ctl run 'view-n 2' >/dev/null
check "view-n 2 after middle view GC" "$(ctl state | jq -r '.outputs[0].view')" 2
ctl run 'view-n 1' >/dev/null

# --- tags ---------------------------------------------------------
ctl run 'tag +web' >/dev/null
FOCUSED_TAGS=$(ctl state | jq -r '[.windows[] | select(.focused)][0].tags | join(",")')
check "tag +web applied" "$FOCUSED_TAGS" "1,web"
VIEWS=$(ctl state | jq -r '[.views[].name] | join(",")')
check "view web created (2 GCd when left empty)" "$VIEWS" "1,web"
ctl run 'view web' >/dev/null
NWEB=$(ctl state | jq '[.windows[] | select(.tags | index("web"))] | length')
check "view web shows multi-tagged window" "$NWEB" 1
ctl run 'view-n 1' >/dev/null
ctl run 'tag -web' >/dev/null
TAGS_NOW=$(ctl state | jq -r '[.windows[] | select(.focused)][0].tags | join(",")')
check "tag -web removed" "$TAGS_NOW" 1

# --- floating -----------------------------------------------------
ctl run 'toggle-float' >/dev/null
check "window floats" "$(ctl state | jq '[.windows[] | select(.floating)] | length')" 1
ctl run 'toggle-float' >/dev/null
check "window back to tiled" "$(ctl state | jq '[.windows[] | select(.floating)] | length')" 0

# --- subscribe ----------------------------------------------------
timeout 2 ./bin/wimyctl -socket "$SOCK" subscribe > "$RT/sub.log" 2>/dev/null &
SUBPID=$!
sleep 0.5
ctl run 'view-n 5' >/dev/null
wait $SUBPID 2>/dev/null
NSUB=$(grep -c '"method":"state"' "$RT/sub.log")
if [ "$NSUB" -ge 2 ]; then NSUB=yes; else NSUB=no; fi
check "subscribe got notifications" "$NSUB" yes
ctl run 'view-n 1' >/dev/null

# --- kill ---------------------------------------------------------
FOCUSED_BEFORE=$(ctl state | jq -r '[.windows[] | select(.focused)][0].id')
ctl run 'kill' >/dev/null
for i in 1 2 3 4 5; do
  sleep 0.5
  NOW=$(ctl state | jq '.windows | length')
  [ "$NOW" = 1 ] && break
done
check "window closed after kill" "$(ctl state | jq '.windows | length')" 1
check "killed window gone" "$(ctl state | jq --argjson id "$FOCUSED_BEFORE" '[.windows[] | select(.id == $id)] | length')" 0

# --- alacritty smoke test (expected to fail headless: needs GL) ---
ALAC_OUT=$(timeout 3 alacritty 2>&1 | head -2 || true)
echo "alacritty headless: ${ALAC_OUT:-(no output)}"

# --- quit ends the whole session -------------------------------------
ctl quit >/dev/null
for i in 1 2 3 4 5 6 7 8 9 10; do
  sleep 0.5
  kill -0 "$RIVER_PID" 2>/dev/null || break
done
if kill -0 "$RIVER_PID" 2>/dev/null; then GONE=no; else GONE=yes; fi
check "wimyctl quit exits river" "$GONE" yes

echo
echo "== PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" = 0 ]
