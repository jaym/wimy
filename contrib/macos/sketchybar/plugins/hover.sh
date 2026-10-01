#!/bin/bash

# Sourced by item scripts (or used directly as one): highlights the item on
# hover, then exits so the caller doesn't run its normal update.
case "$SENDER" in
  mouse.entered) sketchybar --set "$NAME" background.drawing=on; exit 0 ;;
  mouse.exited)  sketchybar --set "$NAME" background.drawing=off; exit 0 ;;
esac
