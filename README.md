# wimy

A [wmii](https://github.com/sunaku/wmii)-style window manager for the
[river](https://codeberg.org/river/river) Wayland compositor, written in Go.

river 0.4 splits the compositor and the window manager into two programs:
river provides rendering, input plumbing and frame-perfect commits, while
wimy — a separate client process speaking the stable
`river-window-management-v1` protocol — owns all window management policy.

## Features

- **Tag-based views**, wmii-style: views are named by arbitrary strings,
  windows carry one or more tags and appear on every matching view.
- **Columns** with wmii's three modes: *default* (equal split), *stack*
  (focused window plus title strips), *max* (only the focused window
  visible) — per column.
- **Floating layer** per view.
- **Multi-output**: one selected view per output; selecting a view shown
  elsewhere swaps the outputs' views.
- **JSON-RPC 2.0 control socket** (replacing wmii's 9P interface) with a
  `wimyctl` CLI — drive the WM from scripts, bars and key bindings.
- **Layer shell support**: launchers, bars and wallpapers (fuzzel,
  waybar, …) work; bars with exclusive zones reserve space, launchers
  with exclusive keyboard focus dim window borders until dismissed.
- **KDL configuration** with comments; external bar and launcher.
- No cgo: pure Go via [wlcl](https://codeberg.org/vyivel/wlcl).

## Installing on Arch Linux

[`contrib/arch/PKGBUILD`](contrib/arch/PKGBUILD) builds a pacman
package from this checkout (commit first — it packages the committed
state):

```sh
cd contrib/arch
makepkg -si          # installs wimy-git; remove with pacman -R wimy-git
```

You get `wimy` and `wimyctl` in `/usr/bin`, the example config in
`/usr/share/doc/wimy/`, the contrib files (waybar modules, fuzzel and
kanshi configs) in `/usr/share/wimy/contrib/`, and a "River (wimy)"
session entry for display managers. pacman pulls in `river` (0.4+)
and the build tools; waybar/fuzzel/kanshi/alacritty are optional
dependencies. From a TTY, start with `river -c wimy`.

## Building

```sh
go build ./cmd/wimy ./cmd/wimyctl
```

Go 1.25+ is required. The checked-in bindings in `internal/proto` are
generated from the protocol XML in `protocol/`; regenerate with:

```sh
go generate ./internal/proto
```

## Running

river spawns the window manager itself:

```sh
river -c /path/to/wimy
```

For development, run a nested river inside your current session (river
from a TTY works too):

```sh
river -c ./wimy
```

Copy `config.kdl` to `~/.config/wimy/config.kdl` to customize; the
built-in defaults are the wmii key binding set with Mod4 (Super) as the
modifier. Use `mod "Mod1"` for the classic wmii Alt modifier. wimy logs
its control socket path, normally `$XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.sock`.

## Usage

All bindings are remappable; the defaults (wmii's set):

| Binding | Action |
|---|---|
| Mod-Return | terminal |
| Mod-p | program launcher (menu) |
| Mod-a | action menu |
| Mod-Shift-c | close window |
| Mod-h / Mod-l | focus left / right column |
| Mod-j / Mod-k | focus below / above |
| Mod-space | toggle focus between tiled and floating layer |
| Mod-t | select view by tag name (prompt) |
| Mod-n / Mod-b | next / previous view |
| Mod-0..9 | select numbered view |
| Mod-Shift-h / Mod-Shift-l | move window to column left / right (created at edge) |
| Mod-Shift-j / Mod-Shift-k | move window within column |
| Mod-Shift-space | toggle window floating |
| Mod-Shift-t | move window to tag (prompt) |
| Mod-Shift-0..9 | move window to numbered view |
| Mod-d / Mod-s / Mod-m | column mode: default / stack / max |
| Mod-Ctrl-h / Mod-Ctrl-l | shrink / grow focused column width |

## Mouse

- **Click** a window to focus it (floating windows raise).
- **Mod+left-drag** on a floating window: move it.
- **Mod+right-drag** on a floating window: resize it — corners resize
  two edges, sides one.
- **Mod+right-drag** on a tiled window: drag the nearest column
  boundary.
- Client-initiated interactive move/resize (e.g. CSD titlebar drags)
  works too.

New windows inherit the focused window's tags. Moving a window to a tag
replaces its tag set; use `wimyctl run 'tag +web'` to *add* a tag (the
window then appears on both views) and `wimyctl run 'tag -web'` to
remove it. Empty views are destroyed when you leave them.

## Control socket (JSON-RPC 2.0)

Newline-delimited JSON-RPC over a unix socket, four methods:

```sh
wimyctl run 'focus left'          # execute any command
wimyctl run 'tag +web'            # multi-tag the focused window
wimyctl run 'grow right 10'       # widen focused column by 10%
wimyctl state | jq                # full state snapshot
wimyctl subscribe                 # stream state notifications
wimyctl quit
```

Raw example:

```sh
echo '{"jsonrpc":"2.0","id":1,"method":"run","params":{"command":"view 2"}}' \
  | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.sock
```

`subscribe` sends an immediate `state` notification and a new one on
every change — this is the integration point for external bars. A
minimal bar script:

```sh
wimyctl subscribe | jq -r '.params.views | map(select(.output != "")) | .[].name'
```

### Command reference

`focus <left|right|up|down>`, `focus-toggle-layer`, `focus-output <next|prev>`,
`focus-window <id>`, `move <left|right|up|down>`, `toggle-float`,
`mode <default|stack|max>`, `grow <left|right> [pct]`,
`view [tag]`, `view-next`, `view-prev`, `view-n <n>`,
`moveto [tag]`, `moveto-n <n>`, `tag <[+-]name...>`,
`kill`, `spawn <cmd...>`, `spawn-terminal`, `spawn-menu`,
`action [name]`, `quit`.

## Configuration

KDL (`~/.config/wimy/config.kdl`); see the annotated [`config.kdl`](config.kdl)
in this repo. Inside single-line blocks, end the command with `;`:
`bind "Mod-h" { focus "left"; }`.

`launcher` is the Mod-p program launcher, `menu` the dmenu-style
prompter used for tag/action prompts.

Binding notes (inherited from river's matching semantics):

- Combos name the **physical key**: `bind "Mod-Shift-c"`, not
  `Mod-Shift-C`.
- Modifiers must match exactly — bindings do not fire while **NumLock
  or CapsLock** is active.
- A config file that declares no `bind` of its own keeps the default
  bindings; declaring any `bind` replaces them all.

## Troubleshooting

wimy logs to `$XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.log` in addition
to stderr (`-log` overrides). Check there first if wimy seems not to
be running (e.g. black screen, no bindings): a missing
`river_window_manager_v1` global means the compositor is river-classic
(0.3.x), not river 0.4+.

## Launcher look (dmenu-style fuzzel)

The default `launcher`/`menu` commands make fuzzel render as a bar
anchored to the top screen edge instead of a centered popup:

```kdl
launcher "fuzzel --anchor top --width 120 --lines 10 --border-radius 0"
menu "fuzzel --dmenu --anchor top --width 120 --lines 10 --border-radius 0"
```

`--width` is in characters (fuzzel has no percentage); ~chars =
screen pixels / 9, raise it to fill wide screens. Colors and font live
in fuzzel's own config — [`contrib/fuzzel/fuzzel.ini`](contrib/fuzzel/fuzzel.ini)
matches the waybar theme; copy it to `~/.config/fuzzel/fuzzel.ini`.
Tag/action prompts get labeled input (`go to tag: `, `action: `).

## Window decorations

wimy draws wmii-style slim **titlebars** itself (pure-Go renderer,
`river_decoration_v1` surfaces): a bar with the window title, accent
colored when focused. Clients are told to use server-side decorations
(`use_ssd`), so their own fat CSD titlebars disappear; clients that
insist on CSD (some GTK apps) keep theirs and get no wimy titlebar.

- Stack mode collapsed strips are the titlebars themselves.
- `titlebar off` in config.kdl gives dwm-style border-only
  decorations; `titlebar height=N` and the four colors are
  configurable (see config.kdl).
- Borders are compositor-drawn; with a titlebar the top border is
  omitted (the titlebar frame covers it).

## Waybar

Waybar's built-in `river/*` modules **do not work** with river 0.4 —
they speak `river-status-unstable-v1`, the river-classic (0.3.x) status
protocol, which river 0.4 removed (the window manager owns tags and
windows now). Use the custom modules in [`contrib/waybar/`](contrib/waybar)
instead — they stream wimy's state over the JSON-RPC socket:

```sh
mkdir -p ~/.config/waybar ~/.local/bin
cp contrib/waybar/config.jsonc contrib/waybar/style.css ~/.config/waybar/
cp contrib/waybar/wimy-*.sh ~/.local/bin/
```

You get wmii-style tags (click to choose a view, scroll to cycle), the
column mode, and the focused window title; the rest of the bar
(clock, battery, tray, …) uses waybar's standard modules as before.
Start waybar from wimy's autostart:

```kdl
autostart {
	exec "waybar -c ~/.config/waybar/config.jsonc -s ~/.config/waybar/style.css"
}
```

## Displays: resolution and scale

river does not configure outputs itself — resolution, scale and
position are set at runtime by an output manager
(`wlr-output-management`). If your display looks wrong coming from
sway, that config was sway's; the river equivalent is
[kanshi](https://sr.ht/~emersion/kanshi/) (or `wlr-randr` for
one-offs, `wdisplays` for a GUI):

```sh
cp contrib/kanshi/config ~/.config/kanshi/config   # then edit
```

and add `exec "kanshi"` to the `autostart` block of `config.kdl`.
See [`contrib/kanshi/config`](contrib/kanshi/config) for examples
converting sway-style `output … scale …` lines.

## Current limitations

- Fullscreen is only honored when a client requests it; there is no
  key binding to toggle it.
- Multiple seats share a single focus.

## Development

```sh
go test ./...           # unit tests (model, config)
./e2e.sh                # end-to-end: headless river + wimy via wimyctl
./e2e-multi.sh          # end-to-end with two headless outputs
./e2e-keys.sh           # end-to-end keybindings via virtual keyboard
./e2e-layer.sh          # end-to-end layer shell (fuzzel)
```

The e2e scripts use foot for client windows because it renders via
shared memory; alacritty needs GL and only works on real hardware.

## Layout of the code

```
cmd/wimy         daemon (Wayland event loop + RPC server)
cmd/wimyctl      control CLI
internal/wm      pure model: views, tags, columns, modes, layout solver (unit-tested)
internal/river   river-window-management-v1 backend
internal/rpc     JSON-RPC 2.0 server/client over unix socket
internal/command command registry shared by key bindings, RPC and config
internal/config  KDL configuration
internal/proto   generated protocol bindings (wlclgen)
protocol         vendored protocol XML
```
