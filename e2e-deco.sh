#!/usr/bin/env bash
# End-to-end decoration test: SSD negotiation, titlebar creation,
# border edges, stack-mode content clipping, focus re-render.
set -u
cd "$(dirname "$0")"
go build -o bin/wimy ./cmd/wimy || exit 1
RT=/tmp/wimy-deco-rt
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
ctl() { ./bin/wimyctl -socket "$SOCK" "$@"; }
sleep 1
PASS=0; FAIL=0
check() { if [ "$2" = "$3" ]; then PASS=$((PASS+1)); echo "ok: $1"; else FAIL=$((FAIL+1)); echo "FAIL: $1 (got '$2', want '$3')"; fi; }

ctl run 'spawn foot' >/dev/null
sleep 2
check "window spawned" "$(ctl state | jq '.windows | length')" 1
check "use_ssd sent" "$(grep -c 'use_ssd' "$RT/wimy.log")" 1
check "decoration created" "$(grep -c 'get_decoration_above' "$RT/wimy.log")" 1
check "decoration synced+committed" "$([ "$(grep -c 'sync_next_commit' "$RT/wimy.log")" -ge 1 ] && echo yes)" yes
check "border has no top edge with titlebar" "$(grep -c 'set_borders(14,' "$RT/wimy.log")" 1

# stack mode: collapsed windows get content clip, titlebars stay
ctl run 'spawn foot' >/dev/null
sleep 1.5
ctl run 'mode stack' >/dev/null
sleep 0.5
check "collapsed window content-clipped" "$([ "$(grep -c 'set_content_clip_box' "$RT/wimy.log")" -ge 1 ] && echo yes)" yes

# focus change re-renders titlebars (new decoration commits)
BEFORE=$(grep -c 'sync_next_commit' "$RT/wimy.log")
ctl run 'focus up' >/dev/null
sleep 0.5
AFTER=$(grep -c 'sync_next_commit' "$RT/wimy.log")
check "focus change re-renders titlebars" "$([ "$AFTER" -gt "$BEFORE" ] && echo yes)" yes

grep -iE 'protocol.*error' "$RT/river.log" "$RT/wimy.log" | grep -v xkbcomp | head -3
kill $RIVER_PID 2>/dev/null; wait $RIVER_PID 2>/dev/null
echo "== deco PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" = 0 ]
