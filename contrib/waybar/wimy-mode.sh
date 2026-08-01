#!/bin/sh
# Streams the mode (default/stack/max) of the focused view's focused
# column as waybar JSON lines. Requires: wimyctl, jq.
exec wimyctl subscribe | jq -c --unbuffered '
  .params | {
    text: (([.views[] | select(.output != "") | .mode] | first) // "default"),
    class: "mode"
  }'
