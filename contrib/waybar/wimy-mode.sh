#!/bin/sh
# wimy-mode.sh — the column mode of the focused view, for waybar.
#
# Two roles, one file:
#
#   wimy-mode.sh            stream JSON lines for a custom/* module
#   wimy-mode.sh --cycle    advance default -> stack -> max -> default
#                           (bound to the module's left click)
#
# Requires: wimyctl, jq.

# jq filter picking the mode of the view shown on the focused output.
focused_mode='
  .outputs as $o
  | (($o // []) | map(select(.focused)) | first) as $f
  | ((.views // []) | map(select(.name == ($f.view // ""))) | first) as $v
  | (if (($v.mode // "") == "") then "default" else $v.mode end)
'

if [ "${1:-}" = "--cycle" ]; then
	cur=$(wimyctl state | jq -r "$focused_mode")
	case "$cur" in
	default) next=stack ;;
	stack) next=max ;;
	*) next=default ;;
	esac
	exec wimyctl run mode "$next"
fi

exec wimyctl subscribe | jq -c --unbuffered --argjson icons '{
	"default": "󰕰",
	"stack":   "󰓩",
	"max":     "󰊓"
}' '
  .params
  | ((.outputs // []) | map(select(.focused)) | first) as $f
  | ((.views // []) | map(select(.name == ($f.view // ""))) | first) as $v
  | (if (($v.mode // "") == "") then "default" else $v.mode end) as $m
  | ($v.columns // 0) as $cols
  | {
      text: "\($icons[$m] // $icons["default"]) \($m)",
      alt: $m,
      class: $m,
      tooltip: "column mode: \($m)\ncolumns: \($cols)\n\nclick cycle · right default · middle max"
    }'
