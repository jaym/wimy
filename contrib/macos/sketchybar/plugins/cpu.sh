#!/bin/bash

source "$CONFIG_DIR/plugins/hover.sh"
source "$CONFIG_DIR/colors.sh"

# Sum per-process CPU and normalize by core count; cheaper than `top -l 2`.
CORES="$(sysctl -n hw.ncpu)"
LOAD="$(ps -A -o %cpu= | awk -v cores="$CORES" '{ s += $1 } END { printf "%d", s / cores }')"

COLOR=$SUBTEXT0
if [ "$LOAD" -ge 90 ]; then
  COLOR=$RED
elif [ "$LOAD" -ge 70 ]; then
  COLOR=$YELLOW
fi

sketchybar --set "$NAME" label="${LOAD}%" label.color=$COLOR
