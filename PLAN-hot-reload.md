# PLAN: Hot-reloadable configuration for wimy

## Context

Today the KDL config is loaded exactly once in `cmd/wimy/main.go`
(`config.Load`) and handed to `river.New`; changing anything requires
restarting wimy. This plan adds hot reload:

1. A **`reload` command** re-reads `config.kdl` and applies changes
   live: keybindings, modifier, borders, titlebars, menu/launcher/
   terminal, focus-follows-mouse, actions, stack-strip.
2. **Autostart programs are tracked** (process groups) and reconciled
   on reload: added → spawned, removed → killed (SIGTERM, ~2 s grace,
   then SIGKILL), changed → killed + re-executed, unchanged → left
   running.
3. Everything that happens — and everything that could **not** be
   applied safely — is **logged** to wimy's log (stderr +
   `$XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.log`).

**Trigger**: explicit only — `wimyctl run reload` (flows through RPC
`run` → command queue → manage sequence). The user binds it to a
config action/key. No file watcher, no SIGHUP.

**Atomicity**: the new file is loaded and validated *first*; on any
error the old config stays active and the error is logged. The WM
never goes config-less.

## How each config section is reloaded

| Section | Consumer today | Reload mechanism |
|---|---|---|
| `terminal`, `launcher`, `menu` | read live per invocation in `effects.go` | swap `b.cfg` — instant (already-running prompts keep old flags) |
| `focus-follows-mouse` | read live in `syncModel` | swap — instant |
| `action` | read live in `effects.go` | swap — instant |
| `bind` (+ `mod` in combos) | `XkbBinding` objects created per seat (`HandleRiverWindowManagerV1Seat`), enabled in `applyManage` | destroy all binding objects, recreate per seat from the new config, enable in the same manage sequence (`bindsNeedEnable` flag) |
| `mod` (pointer drags) | `GetPointerBinding(button, b.cfg.ModMask)` per seat | if ModMask changed: destroy + recreate pointer bindings, enable in same sequence |
| `border` | `setBorder` reads live but per-window cache (`BorderSet/FocusSent/BarSent`) skips re-apply | set `w.BorderSet = false` on all windows → re-applied next render; titlebar renderer embeds border colors/width → rebuild it |
| `titlebar` (height/colors/`off`) | renderer built once in `New`; height copied to `state.TitlebarHeight`; per-window deco cache | rebuild `titlebar.Renderer`; update model height; on→off: destroy decoration objects + zero the handles; any change: reset deco caches (`DecoWidth = -1`) so bars re-render; same sequence re-layouts (insetBar) |
| `stack-strip` | copied to `state.StackStrip` in `New` | set model field; same sequence re-layouts |
| `autostart` | `runAutostart` once at startup; PIDs untracked | track spawned process groups; reconcile diff (below) |

Key ordering facts that make this safe:
- `drainQueue` runs **inside** the manage sequence
  (`HandleRiverWindowManagerV1ManageStart`), before `applyManage` — so
  `reload` executes there, new bindings are enabled in `applyManage`
  (protocol requires enable inside a manage sequence), and model field
  changes are picked up by the same sequence's `Layout()`.
- `b.cfg` is swapped only after old-vs-new comparisons are computed.

## Autostart reconciliation

New `internal/river/autostart.go`:

- `autostartProc{ cmdline string; cmd *exec.Cmd; dead atomic.Bool; done chan struct{} }`.
- Spawn: `sh -c <cmdline>` with `SysProcAttr{Setpgid: true}` (own
  process group, so shell wrappers that fork don't leak children);
  `Wait` goroutine reaps, sets `dead`, closes `done`.
  `runAutostart` (startup) uses this too — tracking begins at boot.
- `syncAutostart(old, new []string)` — pure multiset diff (duplicate
  entries handled by occurrence count), unit-tested in
  `internal/config` as `DiffExecs(old, new) (kill, spawn []string)`:
  - entry gone or count decreased → **terminate**: if `dead`, log
    *"already exited, nothing to kill"* (surfaced, not an error); else
    `kill(-pgid, SIGTERM)` + goroutine: `<-done` or 2 s →
    `kill(-pgid, SIGKILL)`.
  - entry added or count increased → spawn.
  - changed entry = old string killed + new string spawned (this *is*
    the "kill and re-exec" case).
- Liveness via the `Wait` goroutine means we never signal a reaped or
  reused PID.

## Parts NOT safe / caveats — surfaced via log + docs

- Removed/changed autostart entry whose process **already exited**
  (crashed or killed externally): logged, nothing killed.
- **Unchanged autostart entries keep running** even if the program's
  own config (waybar's config.jsonc, …) changed — restart by editing
  the entry or respawning manually.
- An already-open launcher/menu prompt keeps the old flags.
- Invalid new config → whole reload aborted, old config kept, error
  logged (`command "reload": …` via `drainQueue` + explicit log line).
- Command-line flags (`-log`, `-config`, socket path) are not config —
  changing them needs a wimy restart.

## Files to modify

- `internal/command/command.go` — add `Reload() error` to `Effects`;
  register `"reload"` handler (one-line, like `quit`).
- `internal/command/command_test.go` — `fakeFx` gains `Reload`; test
  that `reload` dispatches.
- `internal/config/config.go` — add pure `DiffExecs` helper.
- `internal/config/config_test.go` — table tests for `DiffExecs`.
- `internal/river/river.go` —
  - `New(cfg, configArg string, notify)`: store the config flag arg so
    reload re-runs `config.Load(configArg)` with startup semantics
    (missing default file = defaults; missing explicit file = error).
  - Extract `createXkbBindings(s)` / `createPointerBindings(s)` from
    `HandleRiverWindowManagerV1Seat` for reuse by reload.
  - `applyManage`: honor `bindsNeedEnable` (enable all seats'
    bindings, clear flag; keep the `s.New` path).
  - `Reload()` (Effects impl) + `applyConfig(newCfg)`: compare
    sections (`==` for comparable structs, `slices.Equal` for
    Binds/Autostart, `maps.Equal` for Actions), swap `b.cfg`, apply
    side effects, log per-section report
    (`config reloaded: keybindings, border, autostart (1 killed, 2 spawned)`).
- `internal/river/objects.go` — `destroyDeco` also zeroes
  `Deco`/`DecoSurface` handles and resets the deco cache fields
  (needed when titlebars toggle off→on; today only used at window
  close where the window is dropped).
- `internal/river/effects.go` — `runAutostart` switches to tracked
  spawn; `Reload` effect.
- `internal/river/autostart.go` — **new**: tracked spawn, terminate
  with SIGTERM/grace/SIGKILL, `syncAutostart`.
- `cmd/wimy/main.go` — pass `*configPath` to `river.New`.
- `README.md` — command reference gains `reload`; new "Reloading the
  configuration" subsection (what's live, autostart semantics, the
  not-safe list above).
- `config.kdl` — comments: reload via `wimyctl run reload`; commented
  examples `action "reload" { run "wimyctl run reload"; }` and
  `bind "Mod-Shift-r" { reload; }`; autostart restart semantics.
- `AGENTS.md` — one line under Config conventions: live-reloadable via
  `reload`; autostart diff semantics.
- `e2e-reload.sh` — **new** e2e suite.

## Reuse

- Binding creation logic in `HandleRiverWindowManagerV1Seat`
  (river.go:317-358) → extracted helpers, reused by reload.
- Binding enable path in `applyManage` (river.go:415-427) → extended
  with the `bindsNeedEnable` flag, not duplicated.
- `titlebar.New` + `toRGBA` (river.go:88-101) for renderer rebuild.
- `Window.destroyDeco` (deco.go) for titlebar-off teardown.
- `config.Load` unchanged — reload reuses it verbatim (same path
  argument as startup).
- `drainQueue`'s existing error logging surfaces reload failures.
- e2e techniques: `keyinject` (e2e-keys.sh), `WAYLAND_DEBUG=1` + grep
  of wimy log (e2e-deco.sh), socket/ctl scaffolding (e2e.sh).

## Steps

- [ ] `internal/command`: add `Reload()` to `Effects`, register
      `reload`, update `fakeFx` + dispatch test.
- [ ] `internal/config`: `DiffExecs(old, new []string) (kill, spawn
      []string)` multiset diff + table tests.
- [ ] `internal/river/autostart.go`: tracked spawn (Setpgid, Wait
      goroutine), terminate (SIGTERM → 2 s → SIGKILL), `syncAutostart`
      using `DiffExecs`; switch `runAutostart` to it.
- [ ] `river.go`: `configArg` in `New`; extract binding-creation
      helpers; `bindsNeedEnable` in `applyManage`.
- [ ] `river.go`: `Reload()` + `applyConfig()` — section comparisons,
      cfg swap, xkb/pointer binding recreation, border-cache +
      deco-cache invalidation, renderer rebuild, model fields,
      autostart reconcile, per-section log report.
- [ ] `objects.go`/`deco.go`: `destroyDeco` zeroes handles + resets
      deco cache (titlebar off→on correctness).
- [ ] `cmd/wimy/main.go`: pass config path arg.
- [ ] Docs: README (command ref + "Reloading" subsection), config.kdl
      comments + commented reload action/bind, AGENTS.md line.
- [ ] `e2e-reload.sh` + run full test suite.

## Verification

- `go build ./...`, `go vet ./...`, `gofmt -l cmd internal` clean.
- `go test ./...` — `DiffExecs` tables (add/remove/change/duplicates),
  `reload` command dispatch.
- `./e2e-reload.sh` (headless river, foot, keyinject):
  1. Custom `bind "Mod-x"` fires before reload (keyinject `logo+x`
     spawns foot).
  2. Rewrite config (Mod-x removed, Mod-y added) → `wimyctl run
     reload` → `logo+y` spawns, `logo+x` does nothing.
  3. Border color change → reload → new `set_borders` calls in
     WAYLAND_DEBUG log after the reload line.
  4. `titlebar off` → reload → focused window content grows by the
     titlebar height (`wimyctl state` geometry); borders regain top
     edge; toggling back on re-creates decorations (no blank bars).
  5. Autostart: add `exec "sleep 1000"` → reload → process appears;
     reload again with it removed → process SIGTERMed away; unchanged
     second entry keeps the **same PID** across both reloads.
  6. Write invalid KDL → reload → error in wimy log, wimy alive, old
     bindings still work.
- Existing suites still pass: `go test ./...`, `e2e.sh`,
  `e2e-keys.sh`, `e2e-deco.sh` (reload must not disturb seat/binding
  lifecycle or decoration flow).
