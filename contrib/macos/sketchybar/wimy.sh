#!/bin/bash
# wimy.sh — wimy's views and column mode for SketchyBar (macOS).
#
# SketchyBar has no idea of wimy's views, so this script streams wimy's
# state (`wimyctl subscribe`: an immediate snapshot, then one line per
# model change) and keeps a set of bar items in sync:
#
#   wimy.view.<name>   one per view, in wmii order (numbers first)
#                      focused output's view  solid accent block
#                      shown on another screen solid lavender block
#                      occupied               normal text
#                      empty                  dimmed
#                      click: `wimyctl run view <name>`
#   wimy.mode          column mode of the focused view; click cycles
#                      default -> stack -> max
#   wimy.sep           hairline separator after them
#
# The items go right before $WIMY_ANCHOR (default: front_app). When
# wimy isn't running they disappear, and the script retries every 2s.
#
# Start it from sketchybarrc (a reload starts a new one, which replaces
# the old):
#
#   "$PLUGIN_DIR/wimy.sh" &
#
# Environment:
#   WIMYCTL      wimyctl to use (default: wimyctl on PATH)
#   WIMY_ANCHOR  item to insert before (default: front_app)
#   BLUE LAVENDER TEXT OVERLAY0 MANTLE SURFACE1
#                colors (0xAARRGGBB), Catppuccin Macchiato by default —
#                sketchybarrc usually exports them from colors.sh
#
# Requires: jq (macOS ships /usr/bin/jq), sketchybar.
#
#   wimy.sh cycle-mode       advance the focused view's column mode
#   wimy.sh render < lines   render subscribe lines from stdin once (debug)

WIMYCTL=${WIMYCTL:-wimyctl}
ANCHOR=${WIMY_ANCHOR:-front_app}
: "${BLUE:=0xff8aadf4}" "${LAVENDER:=0xffb7bdf8}" "${TEXT:=0xffcad3f5}"
: "${OVERLAY0:=0xff6e738d}" "${MANTLE:=0xff1e2030}" "${SURFACE1:=0xff494d64}"

# The subscribe notification reduced to what the bar shows. Views are
# sorted like the waybar module: numbered first (numerically), then by
# name. state: focused | shown | occupied | empty.
SUMMARY='
  .params as $s
  | ((($s.outputs // []) | map(select(.focused)) | first) // {}) as $f
  | {
      views: [ ($s.views // [])
        | sort_by(if (.name | test("^[0-9]+$")) then [0, (.name | tonumber), ""] else [1, 0, .name] end)[]
        | { name,
            state: (if .output != "" and .output == ($f.name // "") then "focused"
                    elif .output != "" then "shown"
                    elif .occupied then "occupied"
                    else "empty" end) } ],
      mode: ((($s.views // []) | map(select(.name == ($f.view // ""))) | first | .mode) // ""
             | if . == "" then "default" else . end)
    }'

mode_icon() {
	case "$1" in
	stack) echo "󰓩" ;;
	max) echo "󰊓" ;;
	*) echo "󰕰" ;;
	esac
}

# item_id turns a view name into a SketchyBar item name.
item_id() {
	printf 'wimy.view.%s' "$(printf '%s' "$1" | tr -c 'A-Za-z0-9_-' '_')"
}

# existing_views lists the wimy.view.* items SketchyBar has.
existing_views() {
	sketchybar --query bar 2>/dev/null | jq -r '.items[] | select(startswith("wimy.view."))'
}

# ensure_fixed adds wimy.mode and wimy.sep if they are missing.
ensure_fixed() {
	local items
	items=$(sketchybar --query bar 2>/dev/null | jq -r '.items[]')
	if ! grep -qx wimy.mode <<<"$items"; then
		sketchybar --add item wimy.mode left \
			--set wimy.mode icon.color="$BLUE" label.color="$TEXT" \
			click_script="$(printf '%q' "$0") cycle-mode" \
			--move wimy.mode before "$ANCHOR"
	fi
	if ! grep -qx wimy.sep <<<"$items"; then
		sketchybar --add item wimy.sep left \
			--set wimy.sep width=1 icon.drawing=off label.drawing=off \
			background.drawing=on background.color="$SURFACE1" background.height=18 \
			--move wimy.sep before "$ANCHOR"
	fi
}

# render updates the items from one summary line.
render() {
	local line=$1 existing wanted="" name state id mode
	local args=()
	ensure_fixed
	existing=$(existing_views)
	while IFS=$'\t' read -r name state; do
		[ -n "$name" ] || continue
		id=$(item_id "$name")
		wanted="$wanted
$id"
		if ! grep -qx "$id" <<<"$existing"; then
			args+=(--add item "$id" left
				--set "$id" icon.drawing=off label.padding_left=10 label.padding_right=10
				click_script="$(printf '%q' "$WIMYCTL") run view $(printf '%q' "$name")")
		fi
		# moving every view, in order, right before wimy.mode keeps them sorted
		args+=(--move "$id" before wimy.mode --set "$id" label="$name")
		case "$state" in
		focused) args+=(--set "$id" background.drawing=on background.color="$BLUE" label.color="$MANTLE") ;;
		shown) args+=(--set "$id" background.drawing=on background.color="$LAVENDER" label.color="$MANTLE") ;;
		occupied) args+=(--set "$id" background.drawing=off label.color="$TEXT") ;;
		*) args+=(--set "$id" background.drawing=off label.color="$OVERLAY0") ;;
		esac
	done < <(jq -r '.views[] | "\(.name)\t\(.state)"' <<<"$line")
	while IFS= read -r id; do
		[ -n "$id" ] && ! grep -qx "$id" <<<"$wanted" && args+=(--remove "$id")
	done <<<"$existing"
	mode=$(jq -r '.mode' <<<"$line")
	args+=(--set wimy.mode drawing=on icon="$(mode_icon "$mode")" label="$mode"
		--set wimy.sep drawing=on)
	sketchybar "${args[@]}"
}

# clear hides everything while wimy isn't running.
clear_items() {
	local args=() id
	while IFS= read -r id; do
		[ -n "$id" ] && args+=(--remove "$id")
	done < <(existing_views)
	args+=(--set wimy.mode drawing=off --set wimy.sep drawing=off)
	sketchybar "${args[@]}" 2>/dev/null
}

stream() {
	local last="" line
	while IFS= read -r line; do
		[ "$line" = "$last" ] && continue
		last=$line
		render "$line"
	done
}

case "${1:-}" in
cycle-mode)
	cur=$("$WIMYCTL" state | jq -r "{params: .} | $SUMMARY | .mode")
	case "$cur" in
	default) next=stack ;;
	stack) next=max ;;
	*) next=default ;;
	esac
	exec "$WIMYCTL" run mode "$next"
	;;
render)
	jq --unbuffered -c "$SUMMARY" | stream
	exit
	;;
esac

# One instance: a SketchyBar reload starts a new one, which replaces the
# old one and its wimyctl/jq pipeline.
pidfile="${TMPDIR:-/tmp}/wimy-sketchybar.$(id -u).pid"
if [ -f "$pidfile" ]; then
	old=$(cat "$pidfile")
	pkill -P "$old" 2>/dev/null
	kill "$old" 2>/dev/null
fi
echo $$ >"$pidfile"
trap 'pkill -P $$ 2>/dev/null' EXIT

ensure_fixed
while :; do
	"$WIMYCTL" subscribe 2>/dev/null | jq --unbuffered -c "$SUMMARY" | stream
	clear_items
	sleep 2
done
