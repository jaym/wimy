#!/bin/bash

source "$CONFIG_DIR/plugins/hover.sh"
source "$CONFIG_DIR/colors.sh"
source "$CONFIG_DIR/icons.sh"

BATT="$(pmset -g batt)"
PERCENTAGE="$(echo "$BATT" | grep -Eo '[0-9]+%' | head -1 | tr -d '%')"

# Desktops (no battery) hide the item.
if [ -z "$PERCENTAGE" ]; then
  sketchybar --set "$NAME" drawing=off
  exit 0
fi

COLOR=$SUBTEXT0
case "$PERCENTAGE" in
  9[0-9]|100) ICON=$ICON_BAT_100 ;;
  [7-8][0-9]) ICON=$ICON_BAT_80 ;;
  [5-6][0-9]) ICON=$ICON_BAT_60 ;;
  [3-4][0-9]) ICON=$ICON_BAT_40 ;;
  [1-2][0-9]) ICON=$ICON_BAT_20; COLOR=$YELLOW ;;
  *)          ICON=$ICON_BAT_0;  COLOR=$RED ;;
esac

if echo "$BATT" | grep -q "AC Power"; then
  ICON=$ICON_BAT_CHARGING
  COLOR=$SUBTEXT0
fi

sketchybar --set "$NAME" drawing=on icon="$ICON" label="${PERCENTAGE}%" label.color=$COLOR
