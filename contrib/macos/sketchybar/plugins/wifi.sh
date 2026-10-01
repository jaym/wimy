#!/bin/bash

source "$CONFIG_DIR/plugins/hover.sh"
source "$CONFIG_DIR/colors.sh"
source "$CONFIG_DIR/icons.sh"

# macOS redacts the SSID from ipconfig unless verbose mode is on
# (`sudo ipconfig setverbose 1`); fall back to a generic label.
SSID="$(ipconfig getsummary en0 2>/dev/null | awk -F ' SSID : ' '/ SSID : / { print $2 }')"

if [ -z "$SSID" ]; then
  sketchybar --set "$NAME" icon="$ICON_WIFI_OFF" icon.color=$RED \
                           label="offline" label.color=$RED
elif [ "$SSID" = "<redacted>" ]; then
  sketchybar --set "$NAME" icon="$ICON_WIFI" icon.color=$TEAL \
                           label="connected" label.color=$SUBTEXT0
else
  sketchybar --set "$NAME" icon="$ICON_WIFI" icon.color=$TEAL \
                           label="$SSID" label.color=$SUBTEXT0
fi
