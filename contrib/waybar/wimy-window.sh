#!/bin/sh
# wimy-window.sh — the focused window's title, for waybar.
#
# Emits plain text (no markup): pair it with "escape": true in the module
# so waybar escapes titles containing & < > for us.
#
# The module class is "floating" or "tiled" so style.css can distinguish
# them; the module is empty (and therefore hidden) when nothing is focused.
#
# Requires: wimyctl, jq.

exec wimyctl subscribe | jq -c --unbuffered '
  # app_id -> glyph. Fallback is a generic window icon.
  def icon:
    (. // "") | ascii_downcase
    | if   test("alacritty|foot|kitty|wezterm|ghostty|xterm|term")  then ""
      elif test("firefox|zen|librewolf|chromium|chrome|brave")      then "󰈹"
      elif test("code|codium|nvim|neovim|vim|emacs|jetbrains|idea") then "󰨞"
      elif test("thunar|nautilus|dolphin|nemo|file")                then "󰉋"
      elif test("slack")                                            then "󰏯"
      elif test("discord")                                          then "󰙯"
      elif test("steam")                                            then "󰓁"
      elif test("pavucontrol|mpv|vlc|spotify")                      then "󰝚"
      elif . == ""                                                  then "󰖸"
      else "󰖸"
      end;

  .params
  | ((.windows // []) | map(select(.focused)) | first) as $w
  | if $w == null then
      { text: "", tooltip: "", class: "empty" }
    else
      {
        text: "\($w.app_id | icon)  \($w.title // "")",
        alt: ($w.app_id // ""),
        class: (if $w.floating then "floating" else "tiled" end),
        tooltip: "\($w.app_id // "?")\n\($w.title // "")"
                 + (if $w.floating then "\n\nfloating" else "" end)
      }
    end'
