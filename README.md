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
- **KDL configuration** with comments; external bar and launcher.
- No cgo: pure Go via [wlcl](https://codeberg.org/vyivel/wlcl).

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

## Current limitations

- Mouse support (Mod+drag move/resize, click focus) is not implemented
  yet (planned; the protocol supports it).
- Fullscreen is only honored when a client requests it; there is no
  key binding to toggle it.
- wimy tiles over the full output area; bars using layer-shell
  exclusive zones are not yet reserved space for.
- Multiple seats share a single focus.

## Development

```sh
go test ./...           # unit tests (model, config)
./e2e.sh                # end-to-end: headless river + wimy via wimyctl
./e2e-multi.sh          # end-to-end with two headless outputs
./e2e-keys.sh           # end-to-end keybindings via virtual keyboard
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
