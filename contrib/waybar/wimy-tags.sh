#!/bin/sh
# wimy-tags.sh — stream wimy's views to waybar as JSON lines.
#
# Waybar's built-in river/* modules speak river-status-unstable-v1, the
# river-classic (0.3.x) status protocol. river 0.4 removed it: the window
# manager owns tags now. So we read state from wimy's JSON-RPC socket
# (`wimyctl subscribe`) instead, which emits an immediate snapshot and then
# one line per model change.
#
# Rendering uses pango markup, so the waybar module must NOT set "escape"
# (this script escapes tag names itself):
#
#   selected on the focused output  solid accent block, dark text, bold
#   selected on another output      solid lavender block (multi-head)
#   occupied, not selected          bright text with an accent underline
#   empty                           dimmed
#
# wimy garbage-collects empty views, so by default only tags in use show up
# — that is the wmii behaviour. Set WIMY_TAGS_PAD to a comma-separated list
# to always render those tags, e.g. WIMY_TAGS_PAD=1,2,3,4,5
#
# Requires: wimyctl, jq.

exec wimyctl subscribe | jq -c --unbuffered --arg pad "${WIMY_TAGS_PAD:-}" '
  # Catppuccin Macchiato — keep in sync with style.css.
  def c_on_accent: "#1e2030";
  def c_accent:    "#8aadf4";
  def c_alt:       "#b7bdf8";
  def c_text:      "#cad3f5";
  def c_dim:       "#6e738d";

  def esc: tostring | gsub("&"; "&amp;") | gsub("<"; "&lt;") | gsub(">"; "&gt;");

  def span($fg; $bg; $extra; $t):
    "<span foreground=\"\($fg)\""
    + (if $bg    != "" then " background=\"\($bg)\"" else "" end)
    + (if $extra != "" then " \($extra)"             else "" end)
    + "> \($t) </span>";

  # numbered tags first (numerically), then named tags alphabetically
  def sortkey:
    if (.name | test("^[0-9]+$"))
    then [0, (.name | tonumber), ""]
    else [1, 0, .name]
    end;

  .params as $s

  # name of the output that currently has focus ("" if none)
  | ((($s.outputs // []) | map(select(.focused)) | first).name // "") as $focus

  # tag name -> number of windows carrying it
  | (reduce ($s.windows // [])[] as $w ({};
      reduce ($w.tags // [])[] as $t (.; .[$t] = (.[$t] // 0) + 1))) as $count

  # union of live views and the WIMY_TAGS_PAD placeholders
  | (($s.views // []) | map(.name)) as $live
  | (($pad | split(",") | map(select(length > 0))) - $live
      | map({name: ., output: "", mode: "", columns: 0, occupied: false})) as $extra
  | (($s.views // []) + $extra | sort_by(sortkey)) as $views

  | {
      text: ([ $views[]
        | (.name | esc) as $n
        | if .output != "" and .output == $focus then
            span(c_on_accent; c_accent; "weight=\"bold\""; $n)
          elif .output != "" then
            span(c_on_accent; c_alt; ""; $n)
          elif .occupied then
            span(c_text; ""; "underline=\"single\" underline_color=\"\(c_accent)\""; $n)
          else
            span(c_dim; ""; ""; $n)
          end
      ] | join(" ")),

      tooltip: ([ $views[]
        | (.name | esc) as $n
        | ($count[.name] // 0) as $k
        | "\($n)  "
          + (if $k == 1 then "1 window" elif $k > 0 then "\($k) windows" else "empty" end)
          + (if .output != "" then "  ·  \(.output | esc)" else "" end)
      ] | join("\n")),

      class: "tags"
    }'
