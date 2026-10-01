#!/bin/bash

source "$CONFIG_DIR/plugins/hover.sh"
source "$CONFIG_DIR/colors.sh"
source "$CONFIG_DIR/icons.sh"

# $INFO holds the new volume on volume_change; read it directly otherwise.
if [ "$SENDER" = "volume_change" ]; then
  VOLUME="$INFO"
else
  VOLUME="$(osascript -e 'output volume of (get volume settings)')"
fi
MUTED="$(osascript -e 'output muted of (get volume settings)')"

if [ "$MUTED" = "true" ] || [ "$VOLUME" = "0" ]; then
  sketchybar --set "$NAME" icon="$ICON_VOL_MUTE" icon.color=$OVERLAY0 \
                           label="muted" label.color=$OVERLAY0
  exit 0
fi

case "$VOLUME" in
  [6-9][0-9]|100) ICON=$ICON_VOL_HIGH ;;
  [3-5][0-9])     ICON=$ICON_VOL_MED ;;
  *)              ICON=$ICON_VOL_LOW ;;
esac

sketchybar --set "$NAME" icon="$ICON" icon.color=$YELLOW \
                         label="${VOLUME}%" label.color=$SUBTEXT0
