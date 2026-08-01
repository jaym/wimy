#!/bin/sh
# Streams the focused window's title/app_id as waybar JSON lines.
# Requires: wimyctl, jq. Use with "escape": true in the waybar module.
exec wimyctl subscribe | jq -c --unbuffered '
  .params | ([.windows[] | select(.focused)] | first) // {} | {
    text: (.title // ""),
    tooltip: (.app_id // ""),
    class: "window"
  }'
