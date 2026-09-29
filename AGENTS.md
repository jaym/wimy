# AGENTS.md — guidance for AI agents working on wimy

## What this is

**wimy** is a wmii-style window manager for the [river](https://codeberg.org/river/river)
Wayland compositor (0.4+), written in Go: pure Go on Linux; the macOS
backend (in progress, `plans/macos-port.md`) will use cgo behind
`//go:build darwin`. It runs as a
*client* of river via the `river-window-management-v1` protocol: river
owns rendering/input plumbing, wimy owns all window-management policy
(tags, columns, focus, keybindings). Control interface is JSON-RPC 2.0
over a unix socket (`wimyctl`); config is KDL; bar and launcher are
external (waybar/fuzzel).

## Repo layout

```
cmd/wimy           daemon: Wayland event loop + RPC server (main entry)
cmd/wimyctl        control CLI (run/state/subscribe/quit)
cmd/keyinject      test tool (Linux): injects key events via wlr virtual keyboard
cmd/ptrinject      test tool (Linux): injects pointer motion/buttons (wlr virtual pointer)
internal/wm        PURE model: views/tags/columns/modes/floating/focus +
                   layout solver. NO Wayland imports. Unit-tested heavily.
internal/backend   platform-neutral backend core shared by river and
                   macOS: command queue, spawn/prompt effects, autostart,
                   reload bookkeeping, pointer-op math. Unit-tested.
internal/river     (Linux) river protocol backend: manage/render sequences,
                   object tracking, borders/titlebars; embeds backend.Core
internal/rpc       JSON-RPC 2.0 server + client helpers, state snapshots
internal/command   command registry shared by keybindings, RPC, config
internal/config    KDL config loading/validation + defaults
internal/titlebar  pure-Go titlebar pixel renderer (x/image/font, shm)
internal/proto     generated protocol bindings (wlclgen) — DO NOT EDIT gen.go
protocol/          vendored protocol XML (river at pinned commit, wayland,
                   wlr virtual keyboard for tests)
contrib/           waybar custom modules + config, fuzzel.ini, kanshi config
e2e*.sh            end-to-end tests against headless river (see below)
```

## Build, test, verify

```sh
go build ./...          # build everything
go test ./...           # unit tests (wm, backend, config, command, rpc, titlebar)
go vet ./...            # must stay clean
gofmt -l cmd internal   # must print nothing (except gen.go is fine)

./e2e.sh        # 25 checks (+1 skipped as flaky): core WM flows via wimyctl
./e2e-multi.sh  #  7 checks: multi-output behavior
./e2e-keys.sh   #  6 checks: REAL key events → bindings (virtual keyboard)
./e2e-layer.sh  #  5 checks: layer shell (fuzzel survives, focus events)
./e2e-deco.sh   #  7 checks: decorations (use_ssd, titlebars, clips)
./e2e-mouse.sh  # 14 checks: pointer drags (virtual pointer), grow binding
./e2e-reload.sh # 22 checks: hot config reload (bindings, titlebar, autostart)
./e2e-all.sh    # builds bin/ and runs all seven suites
./ci.sh         # Linux CI: gofmt, vet, unit tests, CGO_ENABLED=0 build, e2e-all
```

The e2e suites need Linux. GitHub Actions (`.github/workflows/ci.yml`)
runs `ci.sh` in the devenv shell on Linux and build/vet/`go test -race`
on macOS. On a Mac, compile-check the river backend with
`CGO_ENABLED=0 GOOS=linux go build ./... && CGO_ENABLED=0 GOOS=linux go vet ./...`
(`CGO_ENABLED=0` matters: with a C compiler on PATH the cross-build
otherwise tries cgo and fails).

The e2e scripts run `river` with `WLR_BACKENDS=headless
WLR_RENDERER=pixman` in a throwaway `$XDG_RUNTIME_DIR` and drive wimy
via wimyctl / WAYLAND_DEBUG logs. **Run the relevant suite after any
behavior change; add checks when you add behavior.** All suites must
pass before committing. Binaries are expected in `bin/` (scripts build
what they need).

Regenerate protocol bindings after touching `protocol/*.xml`:

```sh
go generate ./internal/proto
```

## Hard invariants (don't break these)

1. **`internal/wm` stays pure.** No wayland/proto imports, no I/O, no
   globals. Everything it does must be unit-testable. New WM behavior
   starts here as model ops + solver output, with tests.
2. **Manage vs render sequences** (river-window-management-v1): window
   management state (propose_dimensions, focus, fullscreen, use_ssd,
   set_default) only inside `manage_start`…`manage_finish`; rendering
   state (node position, place_top, hide/show, clip boxes, borders,
   decoration commits) inside `render_start`…`render_finish` (or a
   manage sequence). Getting this wrong = protocol error = dead WM.
3. **Concurrency**: the Wayland dispatch goroutine owns all protocol
   traffic. Other goroutines (RPC, prompts) may only touch wayland
   objects inside `conn.DoSync` — and NEVER call DoSync from the
   dispatch goroutine itself (self-deadlock). Async commands go
   through `backend.Core.QueueCommand`, which calls the backend's
   `Wake` (`ManageDirty()` on river); key bindings, which fire on the
   dispatch goroutine, use `Enqueue`, which doesn't wake. The queue
   drains inside `manage_start`.
4. **One command registry** (`internal/command`): keybindings, RPC
   `run`, and config all dispatch through the same table. Add new
   actions there, not as special cases in the backend.
5. **Model is the source of truth**: the backend translates model state
   to protocol requests every sequence; don't stash layout-relevant
   state in the backend that isn't in the model.
6. **Every column owns its selection** (`Column.Sel`), and the view only
   tracks which column is focused (`View.FocusCol`). The solver must
   pick a column's expanded (stack) / visible (max) window from that
   column's own `Sel` — never from `View.focusedWindow()`, which
   belongs to whichever column has focus and would collapse every other
   column back to its first entry the moment focus moved away. Focusing
   across columns therefore restores the target column's selection
   (wmii does the same) rather than reusing the previous row index.

## Protocol gotchas (learned from real bugs — read before touching
`internal/river`)

- `river_seat_v1.modifiers` enum is `shift=1 ctrl=4 mod1=8 mod3=32
  mod4=64 mod5=128`. 2 and 16 are capslock/numlock internals and are
  NOT in the enum; bindings match modifiers **exactly**, so they don't
  fire while NumLock/CapsLock is active (same as river-classic).
- Binding keysyms: river matches EITHER base-layer (level 0) keysym +
  full modifier mask OR translated keysym + (mods − consumed). Shift
  combos must use the physical key: `"Mod-Shift-c"` → keysym `c` +
  shift|mod4. (`river/XkbBinding.zig` `match()`.)
- **Layer surfaces are not windows**: river CLOSES every layer surface
  unless the WM binds `river_layer_shell_v1` (fuzzel/bars die
  instantly otherwise). Per output: `get_output` → `non_exclusive_area`
  = usable tiling area; `set_default` on the active output. Per seat:
  focus_exclusive/non_exclusive/none events for launcher focus.
- `river_decoration_v1`: titlebars are plain wl_surfaces +
  `get_decoration_above`; `set_offset` is relative to the window's
  top-left corner; commit buffers with `sync_next_commit` inside a
  render sequence. `set_clip_box` clips content+borders+decorations (0
  disables); `set_content_clip_box` clips content only (borders wrap
  the intersection) — stack-mode strips use content-clip to 1px.
- **Every window gets a wimy titlebar, unconditionally**, and every
  window is sent `use_ssd`. `decoration_hint` is deliberately ignored —
  do not reintroduce a CSD special case (see below for why it looks
  tempting and is wrong).
- `decoration_hint` = `only_supports_csd` (0) is river's *default* for a
  window that has no xdg-decoration object at all; it does not mean the
  client answered anything. **Firefox and Zen never bind
  `zxdg_decoration_manager_v1`** (verified with WAYLAND_DEBUG: zero
  `get_toplevel_decoration` calls), so they report hint 0 forever and
  `use_ssd` is a no-op for them — river only calls `setMode()` `if
  (toplevel.decoration)`.
- **But hint 0 does NOT mean the client is incapable of SSD.** Firefox
  negotiates server-side decorations over the *older KDE* protocol,
  `org_kde_kwin_server_decoration`, which **river does not implement**
  (no hits in river's source; wlroots ships
  `wlr_server_decoration_manager_create()`, river just never calls it —
  sway does). Verified by running the same Firefox under headless sway
  vs wimy with WAYLAND_DEBUG:
  - `browser.tabs.inTitlebar=0` → Firefox `request_mode(2)` (Server);
    sway answers `mode(2)` and Firefox draws NOTHING → one titlebar.
    Under river the global is absent, so Firefox falls back to CSD and
    draws its own titlebar *under* wimy's → two titlebars.
  - `browser.tabs.inTitlebar=1` → Firefox `request_mode(1)` (None) and
    draws its window controls into the tab strip; one wimy titlebar
    plus the tab bar, which looks right. This is the workaround.
  If river ever gains `org_kde_kwin_server_decoration`, Firefox with
  `inTitlebar=0` goes SSD on its own and wimy needs no change.
- Suppressing our titlebar for clients reporting hint 0 (the obvious
  reading) is wrong twice over: `insetBar` still reserves
  `TitlebarHeight`, so the unreserved strip showed through as a black
  block; and in stack mode the window collapsed to a strip with no
  titlebar to click, making it unreachable. **sway decorates them too**
  — verified by running Firefox under headless sway: `border: normal`,
  `deco_rect.height: 27`, sway's titlebar drawn above Firefox's tab
  bar. Double decoration on such clients is the accepted cost (sway
  pays it); a per-app opt-out belongs in config, not in hint handling.
- river DOES send the decoration mode with every configure it emits
  (`XdgToplevel.configure()` calls `wlr_decoration.setMode()`
  unconditionally), so a client creating its decoration object late is
  told the mode by the next configure — no re-assert needed. The one
  real gap is a client that requests `client_side` while river's ssd
  state is already `true`: `needsConfigure()` sees no change, so no
  configure is sent and the `set_mode` request goes unanswered. That is
  a river-side bug; do not try to paper over it from the WM by toggling
  `use_csd` → `use_ssd` (it forces a real CSD frame).
- `exit_session` ends the WHOLE session (what `wimyctl quit` does);
  signals/`finished` must shut wimy down WITHOUT it (river stays,
  WM-less) — protocol intent.
- wl_shm ARGB8888 = premultiplied BGRA byte order; Go image.RGBA is
  premultiplied RGBA — swap B/R only. `image.Rect` canonicalizes
  (swaps inverted min/max) — guard "empty" rects yourself.
- `ManageDirty()` from any goroutine (via DoSync) forces a manage
  sequence; used by RPC/prompts to apply commands promptly.
- river 0.4 has no `river-status` (that's river-classic); external
  bars use `wimyctl subscribe` state notifications (see
  contrib/waybar/*.sh).
- Outputs: mode/scale/position is NOT the compositor's or WM's job —
  kanshi/wlr-output-management (contrib/kanshi/).

## macOS backend gotchas (`internal/macos`, learned on macOS 26)

- Key bindings take two paths (`hotkey.viaTap`): combos with Control or
  Command are Carbon `RegisterEventHotKey` hotkeys; Option/Shift-only
  combos go through a CGEventTap. Carbon silently never delivers
  Option-only combos on macOS 15+ (registration still succeeds), and
  the tap receives nothing while any app holds secure input (Terminal's
  Secure Keyboard Entry, Ghostty, password fields in browsers). The
  holder is `kCGSSessionSecureInputPID` in
  `CGSessionCopyCurrentDictionary()`; wimy logs changes. Don't "fix"
  this by moving everything to one path. Compound `mod` values (e.g.
  Ctrl-Option, Hyper) are deliberately unsupported — `parseModName`
  rejects them; users add modifiers per binding. README "macOS"
  documents the shortcomings for users.
- The event tap runs on its own thread (an active tap stalls all
  typing until its callback returns, and AX calls on the main thread
  can block for a second each); it reads bindings through
  `tapRouter`'s atomic table and dispatches commands to the main queue.
- `terminal`/`launcher` run through `sh -c`. The darwin `terminal`
  default (`pickMacTerminal`) must open a window in the running app:
  `open -na` starts a new app instance per press, and Ghostty, kitty and
  Terminal.app keep those running after their windows close (and
  Terminal.app then holds secure input). Ghostty's `+new-window` is
  Linux-only; its AppleScript `new window` works.
- Some apps return `kAXErrorFailure` from `AXUIElementSetAttributeValue`
  for a frame they did apply; judge by reading the frame back.
- The float heuristic needs the zoom button *enabled* (Calculator has a
  disabled one).
- A process started from a terminal is attributed to that terminal by
  TCC: the terminal needs the Accessibility permission for bare-binary
  runs.
- Everything AX/AppKit runs on the main thread (`runtime.LockOSThread`
  in `init`); other goroutines use `dispatch` (async) — `Snapshot`
  waits on a channel, so it must never run on the main thread.

## Config (KDL) conventions

- kdl-go requires `;` before `}` in single-line blocks:
  `bind "Mod-h" { focus "left"; }`.
- Defaults live in `config.Default()`; a user config that declares no
  `bind` keeps default bindings (declaring any replaces all); actions
  merge over defaults. Keep this semantics when extending.
- The config is hot-reloadable via the `reload` command (`wimyctl run
  reload`): bindings are re-declared to the compositor, live-read
  values swap, and autostart processes are reconciled (added → spawn,
  removed → SIGTERM/grace/SIGKILL, changed → re-exec, crashed →
  restart). `Core.applyConfig` (internal/backend/reload.go) is the
  single apply path; protocol-side sections go through the backend's
  `ApplyConfigChange` hook (internal/river/reload.go) — extend both
  when adding config sections.
- Key combos name the PHYSICAL key (see gotchas above).

## Testing philosophy

- Unit tests for all pure logic (solver geometry, tag algebra, view
  GC, focus restore, config parsing, titlebar pixels, command
  dispatch).
- e2e for anything that depends on protocol sequencing or compositor
  behavior — WAYLAND_DEBUG greps + wimyctl state assertions. Key
  bindings must be proven with real injected key events
  (`cmd/keyinject` + xkbcli-generated keymap); "binding created" in a
  debug log proves nothing about matching.

## Deferred / future work

- Not bound yet: river-input-management, river-libinput-config,
  river-xkb-config (input device configuration hooks).
- Multi-seat currently shares one focus.
- No per-app way to turn a titlebar off. Clients that draw their own
  CSD (Firefox, Zen, GTK apps) get both theirs and wimy's. If that
  becomes annoying, add a config rule keyed on app_id rather than
  resurrecting `decoration_hint` handling.
- Upstream: river implements neither `org_kde_kwin_server_decoration`
  (which Firefox uses — see the decorations section) nor a way to
  answer a client that requests `client_side` after SSD was configured.
  Both are river-side, not fixable from the WM.
- Tiled windows can't be moved between columns by mouse (keyboard only).

## Style

- Standard Go: gofmt clean, `go vet` clean, small files, doc comments
  on exported items. Pure logic in internal/wm, protocol plumbing in
  internal/river, effects (spawn/prompt/kill) behind the
  `command.Effects` interface.
- Commit messages: imperative, what + why, reference the protocol or
  river source file when behavior depends on it.
