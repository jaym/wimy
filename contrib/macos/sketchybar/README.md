# wimy for SketchyBar (macOS)

A complete [SketchyBar](https://felixkratz.github.io/SketchyBar/) config
for wimy, styled after the waybar theme (Catppuccin Macchiato): wimy's
views and the focused view's column mode on the left, next to the
focused app; CPU, Wi-Fi, volume, battery and clock on the right.

With Nix, `programs.wimy` (the flake's home-manager module) installs it,
runs SketchyBar and keeps wimy's `bar-gap` in step with the bar; see
the main README. The rest of this file is for setting it up by hand.

| file | what |
|---|---|
| `sketchybarrc` | the bar; reads `settings.sh` and `extra.sh` if present |
| `colors.sh`, `icons.sh` | palette (0xAARRGGBB) and Nerd Font glyphs |
| `plugins/wimy.sh` | wimy's views and column mode (below) |
| `plugins/*.sh` | the status items and the hover highlight |

`plugins/wimy.sh` streams `wimyctl subscribe` and keeps these items in
sync:

| item | shows | click |
|---|---|---|
| `wimy.view.<name>` | one per view: focused (accent block), shown on another screen (lavender block), occupied, empty (dimmed) | `wimyctl run view <name>` |
| `wimy.mode` | column mode of the focused view | cycle default → stack → max |
| `wimy.sep` | hairline separator | — |

## Install by hand

1. Install a Nerd Font, by default MesloLGM Nerd Font
   (`brew install --cask font-meslo-lg-nerd-font`).
2. Copy this directory to `~/.config/sketchybar` and start SketchyBar
   (`brew services start sketchybar`).
3. Reserve the bar's height in `~/.config/wimy/config.kdl`, so tiles
   don't go under the bar:

   ```kdl
   bar-gap 37
   ```

4. Optionally auto-hide the macOS menu bar (System Settings → Control
   Center → Automatically hide and show the menu bar), which the bar
   then sits under.

Settings go in `settings.sh` next to `sketchybarrc` (`BAR_HEIGHT`,
`FONT`, `WIMYCTL`), your own items in `extra.sh`, which runs last.

To use only the wimy items in a bar of your own, copy `plugins/wimy.sh`
and start it from your `sketchybarrc`; a `sketchybar --reload` starts a
fresh copy, which replaces the old one:

```sh
export WIMYCTL=wimyctl          # or the full path, see below
"$PLUGIN_DIR/wimy.sh" &
```

## Notes

- SketchyBar runs as a launchd agent with a minimal `PATH`. If `wimyctl`
  isn't on it, set `WIMYCTL` to its full path (`sketchybarrc` looks in
  `~/.local/bin`, where `make mac-install` links it). `jq` comes with
  macOS (`/usr/bin/jq`).
- `plugins/wimy.sh` uses `BLUE`, `LAVENDER`, `TEXT`, `OVERLAY0`,
  `MANTLE` and `SURFACE1` when they are exported (Catppuccin Macchiato
  otherwise). `WIMY_ANCHOR` changes the item the views are inserted
  before (default `front_app`).
- While wimy isn't running the items disappear; the plugin reconnects
  every 2 seconds.
- Debug the rendering without wimy:
  `wimyctl state | jq -c '{params: .}' | plugins/wimy.sh render`.
