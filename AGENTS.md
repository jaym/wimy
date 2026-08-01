# AGENTS.md — guidance for AI agents working on wimy

## What this is

**wimy** is a wmii-style window manager for the [river](https://codeberg.org/river/river)
Wayland compositor (0.4+), written in pure Go (no cgo). It runs as a
*client* of river via the `river-window-management-v1` protocol: river
owns rendering/input plumbing, wimy owns all window-management policy
(tags, columns, focus, keybindings). Control interface is JSON-RPC 2.0
over a unix socket (`wimyctl`); config is KDL; bar and launcher are
external (waybar/fuzzel).

## Repo layout

```
cmd/wimy           daemon: Wayland event loop + RPC server (main entry)
cmd/wimyctl        control CLI (run/state/subscribe/quit)
cmd/keyinject      test tool: injects key events via wlr virtual keyboard
internal/wm        PURE model: views/tags/columns/modes/floating/focus +
                   layout solver. NO Wayland imports. Unit-tested heavily.
internal/river     river protocol backend: manage/render sequences,
                   object tracking, borders/titlebars, effects (spawn etc.)
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
go test ./...           # unit tests (internal/wm, config, command, titlebar)
go vet ./...            # must stay clean
gofmt -l cmd internal   # must print nothing (except gen.go is fine)

./e2e.sh       # 22 checks: core WM flows via wimyctl (headless river)
./e2e-multi.sh #  7 checks: multi-output behavior
./e2e-keys.sh  #  6 checks: REAL key events → bindings (virtual keyboard)
./e2e-layer.sh #  5 checks: layer shell (fuzzel survives, focus events)
./e2e-deco.sh  #  7 checks: decorations (use_ssd, titlebars, clips)
```

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
   through the queue + `ManageDirty()`; the queue drains inside
   `manage_start`.
4. **One command registry** (`internal/command`): keybindings, RPC
   `run`, and config all dispatch through the same table. Add new
   actions there, not as special cases in the backend.
5. **Model is the source of truth**: the backend translates model state
   to protocol requests every sequence; don't stash layout-relevant
   state in the backend that isn't in the model.

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
- Clients default to CSD: send `use_ssd` to suppress; clients that
  only support CSD report it via `decoration_hint` (they keep CSD and
  get no wimy titlebar).
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

## Config (KDL) conventions

- kdl-go requires `;` before `}` in single-line blocks:
  `bind "Mod-h" { focus "left"; }`.
- Defaults live in `config.Default()`; a user config that declares no
  `bind` keeps default bindings (declaring any replaces all); actions
  merge over defaults. Keep this semantics when extending.
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

- Mouse: pointer bindings, interactive move/resize, click-focus (the
  protocol has GetPointerBinding/Op events; see tinyrwm for the
  pattern). PLAN.md step 12.
- Not bound yet: river-input-management, river-libinput-config,
  river-xkb-config (input device configuration hooks).
- Multi-seat currently shares one focus.

## Style

- Standard Go: gofmt clean, `go vet` clean, small files, doc comments
  on exported items. Pure logic in internal/wm, protocol plumbing in
  internal/river, effects (spawn/prompt/kill) behind the
  `command.Effects` interface.
- Commit messages: imperative, what + why, reference the protocol or
  river source file when behavior depends on it.
