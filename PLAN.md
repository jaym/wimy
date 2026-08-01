# PLAN: wimy — a wmii-style window manager for river, in Go

## Context

Build **wimy**: a new window manager running as a separate client process on top
of river 0.4.0+ via the stable `river-window-management-v1` protocol. River (the
compositor) owns rendering, input plumbing, and frame-perfect commits; wimy owns
all window management policy (layout, focus, keybindings, tags).

UI model: **wmii** — dynamic string-tag views (windows taggable to multiple
views), columns with three modes (default / stack / max), floating layer,
keyboard-driven. Control interface: **JSON-RPC 2.0 over a unix socket**
(replacing wmii's 9P filesystem); no plan9. Bar and launcher are **external**;
wimy exposes state + event subscriptions over RPC so any bar can integrate.

## Decisions (from user)

1. Config format: **KDL** (`github.com/sblinch/kdl-go`, supports KDL v1+v2).
2. Bar: **external** (waybar etc.); wimy provides `state`/`subscribe` RPC.
3. Launcher: **external**, configurable command (default `fuzzel`).
4. Tags: arbitrary strings; a window may carry **multiple tags** (appears on all
   matching views). Keybinds do exclusive move; full tag algebra via RPC.
5. Multi-monitor: **one selected view per output**; selecting a view already
   shown on another output swaps the two outputs' views (i3-style).
6. Mouse (Mod+drag move/resize, click-focus): **deferred** to a later milestone.
7. Name: **wimy** → daemon `wimy`, control CLI `wimyctl`.

## Research findings (confirmed)

- **Bindings**: `hazelnut.eclair.cafe/wlcl` v1.1.2 (codeberg.org/vyivel/wlcl) —
  pure-Go Wayland client, no cgo, BSD-2-Clause. Codegen via
  `go tool hazelnut.eclair.cafe/wlcl/cmd/wlclgen`; `go generate ./internal/proto`
  from vendored XML in `./protocol`. Used by the official tinyrwm Go example
  (codeberg.org/river/tinyrwm `go/`, the example the user remembered) and by
  crofflewm. Requires Go ≥ 1.25.
- **Protocol (v5)** flow: server sends state + `manage_start` → WM makes
  window-management requests (`propose_dimensions`, focus, fullscreen,
  keybindings) + `manage_finish` → server sends `render_start` → WM makes
  rendering requests (node position/z-order, `set_clip_box`, borders) +
  `render_finish`; render loop repeats until dimensions settle; `manage_dirty`
  forces a new manage sequence. Key objects: `river_window_v1`,
  `river_output_v1`, `river_seat_v1` (declare key bindings; `pressed` →
  `manage_start`), `river_node_v1`. `river_window_manager_v1.unavailable` =
  another WM is running → fatal.
- **Run model**: `river -c ./wimy` — river spawns the WM; wlcl
  `Connect(ctx, "")` / `ConnectToFD` covers both socket-passing paths.
- **nixpkgs**: `pkgs.river` is 0.4.x (old river renamed `river-classic`) →
  devenv can provide river + foot + fuzzel for nested testing.
- **wmii keybindings spec** (man page, three tables) — reproduced below.

## Approach

Single Go module, two binaries, clean split between pure layout logic and
Wayland plumbing:

```
wimy/
├── go.mod                      # module path TBD by user (placeholder: wimy)
├── protocol/                   # vendored XML: wayland.xml,
│   │                           #   river-window-management-v1.xml (pinned from
│   │                           #   river commit 0116203…)
│   └── river-window-management-v1.xml
├── cmd/
│   ├── wimy/main.go            # daemon: wayland loop + RPC server
│   └── wimyctl/main.go         # CLI: run/state/subscribe/quit subcommands
└── internal/
    ├── proto/                  # wlclgen-generated bindings (gen.go + go:generate)
    ├── wm/                     # PURE layout model, no wayland imports:
    │   │                       #   Output, View, Column, Window types; tag sets;
    │   │                       #   focus stacks; column modes; layout solver
    │   ├── wm.go               #   state + operations (focus/move/mode/tag/view)
    │   ├── layout.go           #   solver: usable rect → per-window dims/pos/clip/z
    │   └── wm_test.go          #   unit tests (table-driven)
    ├── river/                  # wlcl event loop: object tracking, manage/render
    │                           #   sequences, seat binding dispatch, borders
    ├── rpc/                    # JSON-RPC 2.0 server, newline-delimited, unix socket
    ├── command/                # command registry: name → fn(args) on *wm.State;
    │                           #   shared by keybinds, RPC, config
    └── config/                 # KDL load/validate + defaults
config.kdl                      # annotated example config
```

**Command model**: every action is a named command in one registry
(`focus left`, `move right`, `mode stack`, `view 3`, `view web`, `moveto 2`,
`tag +web`, `kill`, `spawn foot`, `spawn-menu`, `action quit`, `grow left 10`,
`quit`, …). Three frontends share it: config keybindings, JSON-RPC `run`,
config autostart.

**wm semantics** (mirroring wmii):
- A **view** = one tag string, ordered by creation. Selecting view *v* on an
  output shows every window whose tag set contains *v*.
- New window inherits the focused window's tag set (fallback: current view);
  appended to the focused column. New views start with one column.
- `Mod-Shift-t`/`Mod-Shift-N` = **move** (replace tag set with the target);
  `wimyctl run 'tag +web'` / `'tag -web'` = add/remove tags (multi-tag).
- Empty unselected views are destroyed (wmii behavior); selected empty view stays.
- `Mod-n`/`Mod-b` cycle views on the current output; `Mod-[0-9]` selects the
  nth view in order.
- Column **modes**: `default` = windows split column height equally;
  `stack` = focused window gets full column dimensions, others proposed full
  dims but clipped to a top strip via `set_clip_box` (frame-perfect, no
  decoration drawing needed); `max` = all windows full column dims, focused on
  top, others clipped to zero. Column widths split usable width equally;
  `grow`/`shrink` via RPC adjusts width factors.
- **Floating layer** per view, rendered above tiled; `Mod-space` toggles focus
  between layers, `Mod-Shift-space` moves window between layers. New floats
  (e.g. dialogs) get centered 50% geometry.
- Focus indicated with server-side borders (`set_borders`: width +
  focused/unfocused colors, configurable).

**Keybindings** (defaults; all remappable in KDL; `mod` default `Mod4`):

| Binding | Command |
|---|---|
| Mod-h / Mod-l | `focus left` / `focus right` |
| Mod-j / Mod-k | `focus down` / `focus up` |
| Mod-space | `focus-toggle-layer` |
| Mod-t \<tag\> | `view <tag>` (tag read via launcher prompt) |
| Mod-n / Mod-b | `view-next` / `view-prev` |
| Mod-[0-9] | `view-n <n>` |
| Mod-Shift-h / Mod-Shift-l | `move left` / `move right` (create column at edge) |
| Mod-Shift-j / Mod-Shift-k | `move down` / `move up` |
| Mod-Shift-space | `toggle-float` |
| Mod-Shift-t \<tag\> | `moveto <tag>` |
| Mod-Shift-[0-9] | `moveto-n <n>` |
| Mod-d / Mod-s / Mod-m | `mode default` / `mode stack` / `mode max` |
| Mod-Shift-c | `kill` |
| Mod-p | `spawn-menu` |
| Mod-a \<action\> | `action <name>` (prompt via launcher) |
| Mod-Return | `spawn $terminal` |

**JSON-RPC 2.0** over `$XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.sock`,
newline-delimited, hand-rolled with `encoding/json`:
- `run` `{command: "focus left"}` → execute via command registry.
- `state` → outputs, views (order, selected-per-output), windows (tags, app_id,
  title, focused, layer, geometry).
- `subscribe` → server pushes notifications on that connection (view
  created/destroyed/selected, focus changed, window opened/closed/retagged,
  title changed) — the integration point for external bars.
- `quit` → clean exit.

**Config sketch** (`~/.config/wimy/config.kdl`; `-config` flag overrides):

```kdl
mod "Mod4"
terminal "foot"
menu "fuzzel"                       // used by Mod-p and tag/action prompts

border width=2 focused=0x8aadf4 normal=0x363a4f

bind "Mod-Return" { spawn "foot" }
bind "Mod-h" { focus "left" }
bind "Mod-Shift-l" { move "right" }
bind "Mod-s" { mode "stack" }
// … full default table shipped in example config

action "quit" { run "wimyctl quit" }
action "lock" { run "swaylock" }

autostart { exec "waybar" }        // optional, runs at startup
```

## Files to modify / create

- `devenv.nix` — add `pkgs.river` (0.4.x), `pkgs.foot`, `pkgs.fuzzel`; keep
  `languages.go.enable` (needs Go ≥ 1.25 for wlcl).
- New: `go.mod`, `protocol/`, `cmd/wimy/`, `cmd/wimyctl/`,
  `internal/{proto,wm,river,rpc,command,config}/`, `config.kdl`, `README.md`.

## Reuse

- `hazelnut.eclair.cafe/wlcl` v1.1.2 + `wlclgen` (go tool directive; pin in
  go.mod like tinyrwm does).
- tinyrwm Go example (0BSD) as reference for the manage/render loop, object
  tracking, and codegen wiring (`codeberg.org/river/tinyrwm`, `go/tinyrwm.go`).
- `github.com/sblinch/kdl-go` for config parsing.
- stdlib: `encoding/json`, `net` (unix sockets), `os/exec`, `context`.
- river-window-management-v1.xml vendored from river commit
  `011620314585a82ac6de9851448c3d7e1269d86b`.

## Steps

- [ ] devenv.nix: add river, foot, fuzzel; verify `go version` ≥ 1.25.
- [ ] Scaffold: `go mod init`, vendor `protocol/*.xml`, wire
      `go generate ./internal/proto` with wlclgen; build generated bindings.
- [ ] `internal/wm`: pure model + layout solver (views/tags/columns/modes/
      floating/focus) with table-driven unit tests — no Wayland code.
- [ ] `internal/command`: registry + parser shared by all frontends.
- [ ] `internal/river`: connect, bind `river_window_manager_v1`, track
      outputs/windows/seats; manage/render sequence handling; apply layout
      solver results (`propose_dimensions`, node placement, `set_clip_box`,
      `set_borders`); window closed/inform-focus events; `unavailable` → fatal.
- [ ] Seat key bindings from config → command registry dispatch.
- [ ] `internal/rpc`: JSON-RPC server (run/state/subscribe/quit) + socket
      lifecycle (create, cleanup on exit).
- [ ] `cmd/wimyctl`: subcommands `run`, `state`, `subscribe`, `quit`.
- [ ] `internal/config`: KDL load/validate, defaults, example `config.kdl`.
- [ ] Multi-output: per-output selected view, add/remove outputs, swap-on-select.
- [ ] README: build, `river -c ./wimy` (incl. nested session), config, RPC
      examples for bar authors.
- [ ] (Later milestone) mouse: pointer bindings, interactive move/resize,
      click-focus.

## Verification

- `go build ./...`, `go vet ./...`.
- `go test ./internal/wm/` — layout solver: equal/stack/max geometry, column
  create/move/swap, tag algebra (+/-/set, multi-tag visibility), view
  ordering/cycling/destruction, per-output selection + swap semantics.
- Manual (nested): from an existing session run `river -c './wimy'` (devenv
  provides river/foot/fuzzel); spawn several terminals; walk every keybinding
  in the table; check stack/max clipping renders cleanly; move windows between
  views; multi-tag a window via `wimyctl run 'tag +web'` and confirm it appears
  on both views.
- RPC: `wimyctl state | jq`, `wimyctl subscribe` stream during view/focus
  changes, `echo '{"jsonrpc":"2.0","id":1,"method":"run","params":{"command":"view 2"}}' | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.sock`.
- Second output (or `WLR_WL_OUTPUTS=2`-style nested setup): verify
  one-view-per-output and swap behavior.
