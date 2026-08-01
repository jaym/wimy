#!/bin/sh
# Streams wimy's views as waybar JSON lines (pango markup).
# Requires: wimyctl, jq. Put this script somewhere in your PATH
# (or reference it by absolute path from your waybar config).
#
# Selected views (visible on an output) are highlighted, occupied
# views underlined, empty views dimmed.
exec wimyctl subscribe | jq -c --unbuffered '
  def esc: gsub("&";"&amp;") | gsub("<";"&lt;") | gsub(">";"&gt;");
  def tagview:
    .name as $n |
    if .output != "" then
      "<span weight=\"bold\" foreground=\"#1e2030\" background=\"#8aadf4\"> \($n|esc) </span>"
    elif .occupied then
      "<span underline=\"single\"> \($n|esc) </span>"
    else
      "<span alpha=\"60%\"> \($n|esc) </span>"
    end;
  .params | {
    text: ([.views[] | tagview] | join("")),
    tooltip: ([.views[] | select(.occupied) | .name] | join(", ")),
    class: "tags"
  }'
