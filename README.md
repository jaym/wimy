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
- Pure Go on Linux (no cgo) via [wlcl](https://codeberg.org/vyivel/wlcl).

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
modifier. Use `mod "Mod1"` for the classic wmii Alt modifier. (On
macOS the default is Option; see [macOS](#macos-work-in-progress).) wimy logs
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

Focus **follows the mouse** (sloppy focus): hovering a window focuses
it, in the tiled and floating layers alike; the pointer leaving all
windows keeps the last focus. Clicking a window focuses it too. Set
`focus-follows-mouse false` in config.kdl for plain click-to-focus.
Focusing a floating window raises it above the other floats. In
stack mode, collapsed window strips are exempt: hovering a strip does
not focus (and expand) the window — click it instead.

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
`action [name]`, `reload`, `quit`.

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

### Reloading

`wimyctl run reload` re-reads the config file and applies changes
live — bind it to a key or put it in the action menu:

```kdl
bind "Mod-Shift-r" { reload; }
action "reload" { run "wimyctl run reload"; }
```

Everything applies without a restart: key bindings and the primary
modifier (re-declared to the compositor), border/titlebar colors and
sizes, `terminal`/`launcher`/`menu`, `focus-follows-mouse`, actions,
and `stack-strip`. The log lists what changed on each reload.

Autostart programs are tracked (each in its own process group) and
reconciled:

- **added** entry → started
- **removed** entry → killed (SIGTERM to the process group; SIGKILL
  after 2 s if ignored)
- **changed** entry → killed and re-executed
- **unchanged** entry → keeps running — even if the program's *own*
  config changed (restart it by editing the entry or spawning it
  manually)
- an entry whose process **died** on its own is restarted on reload

Not reloadable / caveats (surfaced in the log):

- A config that fails to parse is rejected wholesale: the old config
  stays active, the error is logged and shown via zenity/notify-send
  when available.
- An already-open launcher/menu prompt keeps the old flags.
- `-config`/`-log` and the socket path are command-line flags, not
  config — changing them needs a wimy restart.

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
- `titlebar "off"` in config.kdl gives dwm-style border-only
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

## macOS (work in progress)

A macOS backend is being built (`plans/macos-port.md`).

**Install and update:**

```sh
make mac-signing-identity   # once: a self-signed code-signing certificate
make mac-install            # build, sign and install ~/Applications/Wimy.app
```

The first signing asks for your login password to let `codesign` use
the key: choose **Always Allow**, or every build asks again.

**With Nix (home-manager)**, the flake's module installs the signed
release and sets up a bar, batteries included:

```nix
# flake inputs
wimy.url = "github:jaym/wimy";
wimy.inputs.nixpkgs.follows = "nixpkgs";
wimy.inputs.home-manager.follows = "home-manager";

# home-manager configuration
imports = [ inputs.wimy.homeManagerModules.wimy ];
programs.wimy.enable = true;
programs.wimy.settings = ''
  // your config.kdl (wimy's defaults apply to the rest)
'';
```

Every switch installs `~/Applications/Wimy.app` (and `wimyctl` in
`~/.local/bin`) and restarts a running wimy in place. It also sets up
[SketchyBar](contrib/macos/sketchybar/) with wimy's views and column
mode, its Nerd Font, the native menu bar auto-hidden, and `bar-gap`
matching the bar (`programs.wimy.sketchybar.height`, `.extraConfig` for
your own items, `.font`, `.hideMenuBar`), and squares every app's
window corners to match wimy's borders (`programs.wimy.windowCornerRadius`,
default 1; 10 is the pre-Tahoe look; apps pick it up when they start).
Set
`programs.wimy.sketchybar.enable = false` to configure the bar yourself,
and `programs.wimy.settings = null` to manage `config.kdl` yourself (for
instance as an out-of-store link, for live `wimyctl run reload` edits).

`make mac-install` puts `wimyctl` in `~/.local/bin` and starts Wimy, or,
if it is running, restarts it in place with the new version: views,
tags, columns, floating windows and parked windows all carry over
(`wimyctl restart` does the same by hand; `wimyctl version` shows what
runs). Signing with the same identity every time keeps the Accessibility
permission across updates — with ad-hoc signing macOS forgets it for
every changed build. Wimy lives in the menu bar (view name; views,
reload, restart, quit) and starts at login through a launchd agent it
writes to `~/Library/LaunchAgents/io.github.jaym.wimy.login.plist`
(`start-at-login false` removes it; `status-item false` hides the
menu bar item). It logs to `~/Library/Logs/wimy.log`.
 It tiles every
screen, switches views, follows focus changes you make with the mouse
or Cmd-Tab, draws wimy's titlebars and borders, and runs the key
bindings; the mouse (drags) comes later. It needs the
Accessibility permission (System Settings → Privacy & Security →
Accessibility) for whatever starts it — for now the terminal you run
`wimy` from.

- **Config:** `~/.config/wimy/config.kdl` (or
  `$XDG_CONFIG_HOME/wimy/config.kdl`), the same place as on Linux.
- **Modifier:** `Mod` defaults to Option; `Option`/`Opt` and
  `Cmd`/`Command` are accepted as modifier names everywhere.
- **Terminal:** Mod-Return opens a new window of the first installed of
  Ghostty, Alacritty and kitty, else Terminal.app, reusing the running
  app. With Ghostty this goes through AppleScript, so macOS asks once
  to allow controlling Ghostty. `terminal` and `launcher` run through
  `sh -c`, so they may quote arguments.
- **Views:** macOS has no way to hide one window of an app, so windows
  of views that aren't shown are parked in a corner of their screen
  (bottom-right, or bottom-left when another screen is to the right),
  with a sliver left on screen. Their last frame is saved in
  `~/.local/state/wimy/hidden.json` (or `$XDG_STATE_HOME/wimy`) before
  they move: `wimyctl quit`, Ctrl-C and SIGTERM put them back, and if
  wimy is killed, the next start does.
- **Titlebars and borders:** every window gets wimy's titlebar and a
  border, drawn by a panel right behind the window, so apps keep their
  own titlebar too (the same double decoration as Firefox on Linux).
  Clicking a titlebar focuses its window. Floating windows (dialogs,
  utility windows) keep only their own titlebar, and — macOS has no
  window layers a window manager may use — they can go behind tiled
  windows; bring one back with a click, Cmd-Tab or Mod-space.
  Mod-f toggles fullscreen: the focused window fills its screen below
  the bar. In stack mode, collapsed
  windows are parked like hidden ones and only their titlebar strip
  shows; click it to expand the window. With `titlebar "off"`, windows
  get a border only and stack strips are `stack-strip` high.
- **Screens:** every screen is an output; windows already open when
  wimy starts join the view of the screen they are on.
- **Minimize and hide:** a minimized window leaves the tiling and
  returns to its views when restored. Hiding apps (Cmd-H, Hide Others)
  is undone at once — it would leave holes in the tiling.
- **Bars:** with a bar wimy can't see, such as
  [SketchyBar](contrib/macos/sketchybar/), set `bar-gap` to its height
  (points reserved at the top of every screen). wimy's SketchyBar config
  shows the views and the column mode like the waybar modules (the Nix
  module sets it all up).
- **Control socket:** `/tmp/wimy-<uid>/wimy.sock`, a private per-user
  directory, so bars started by launchd (which get no `TMPDIR`) find it.

### Known shortcomings of key bindings on macOS

macOS gives a window manager no single way to see every key combo:

- Bindings that include **Ctrl or Cmd** are Carbon hotkeys. They work
  everywhere, including in password fields and terminals.
- Bindings whose only modifiers are **Option (and Shift)** — which is
  every default binding, since `Mod` is Option — go through an event
  tap, because macOS 15+ never delivers such combos as hotkeys. An
  event tap receives **nothing while any app holds secure input**:
  Terminal.app with *Secure Keyboard Entry*, Ghostty's *Secure Keyboard
  Entry* (on by default at password prompts), and password fields in
  browsers. While that lasts those bindings do nothing and the keys
  reach the app instead (Option-h types ˙). wimy logs when an app
  takes or releases secure input.
- Option-only combos you *don't* bind still type their characters
  (Option-e is the accent dead key).
- `mod` must be a **single** modifier. Combinations such as
  `mod "Ctrl-Option"` or a "Hyper" key are **not supported**. If you
  need bindings that keep working under secure input, write the extra
  modifier into those bindings explicitly, e.g.
  `bind "Ctrl-Option-h" { focus "left"; }`.
- Key combos are macOS key codes of the US layout's physical keys, so
  on non-US layouts a combo names the key at that US position (Linux
  matches the active layout's keysym instead).

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
internal/backend platform-neutral backend core (queue, effects, autostart, reload, pointer ops)
internal/river   river-window-management-v1 backend (Linux)
internal/rpc     JSON-RPC 2.0 server/client over unix socket
internal/command command registry shared by key bindings, RPC and config
internal/config  KDL configuration
internal/proto   generated protocol bindings (wlclgen)
protocol         vendored protocol XML
```
