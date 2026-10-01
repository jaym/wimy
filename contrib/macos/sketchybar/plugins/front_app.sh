#!/bin/bash

# $INFO holds the app name on front_app_switched.
if [ "$SENDER" = "front_app_switched" ]; then
  sketchybar --set "$NAME" label="$INFO"
fi
