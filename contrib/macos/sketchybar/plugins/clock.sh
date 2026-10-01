#!/bin/bash

source "$CONFIG_DIR/plugins/hover.sh"

sketchybar --set "$NAME" label="$(date '+%a %d %b  %H:%M')"
