# wimy for SketchyBar (macOS)

`wimy.sh` shows wimy's views and the focused view's column mode in
[SketchyBar](https://felixkratz.github.io/SketchyBar/), like the waybar
modules do on Linux. It streams `wimyctl subscribe` and keeps these items
in sync:

| item | shows | click |
|---|---|---|
| `wimy.view.<name>` | one per view: focused (accent block), shown on another screen (lavender block), occupied, empty (dimmed) | `wimyctl run view <name>` |
| `wimy.mode` | column mode of the focused view | cycle default → stack → max |
| `wimy.sep` | hairline separator | — |

## Install

1. Copy `wimy.sh` into your SketchyBar plugin directory and make it
   executable.
2. In `sketchybarrc`, after the item the views should follow (the items
   are inserted right before `front_app` by default):

   ```sh
   export WIMYCTL=wimyctl          # or the full path, see below
   "$PLUGIN_DIR/wimy.sh" &
   ```

   A `sketchybar --reload` starts a fresh copy, which replaces the old one.
3. Reserve the bar's height in `~/.config/wimy/config.kdl`, so tiles don't
   go under the bar (use the `height` from `sketchybarrc`):

   ```kdl
   bar-gap 37
   ```

## Notes

- SketchyBar runs as a launchd agent with a minimal `PATH`. If `wimyctl`
  isn't on it (a development build, say), set `WIMYCTL` to its full path.
  `jq` comes with macOS (`/usr/bin/jq`).
- Colors default to Catppuccin Macchiato; the plugin uses `BLUE`,
  `LAVENDER`, `TEXT`, `OVERLAY0`, `MANTLE` and `SURFACE1` (0xAARRGGBB)
  when `sketchybarrc` exports them. `WIMY_ANCHOR` changes the item the
  views are inserted before.
- While wimy isn't running the items disappear; the plugin reconnects
  every 2 seconds.
- Debug the rendering without wimy:
  `wimyctl state | jq -c '{params: .}' | ./wimy.sh render`.
