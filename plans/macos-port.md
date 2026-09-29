# PLAN: run wimy on macOS with the same UX

## Context

Goal (user): the same wmii-style UX on macOS as on Linux/river. Sharing
the codebase is preferred; a Swift macOS-only rewrite is acceptable if
needed.

**Decisions (user, 2026-09-29):** cgo is allowed for the darwin backend;
`Mod` defaults to Option on macOS; target macOS 26 (Tahoe); **both
backends ship**: river on Linux, macOS on darwin. Nothing is removed.

macOS has no window-manager protocol. The WindowServer owns every
window, and a third-party tiler can only observe and move/resize other
apps' windows through the **Accessibility (AX) API** after the app has
drawn them. This is how AeroSpace (Swift, no SIP changes), Amethyst and
yabai (C, some features need SIP partially disabled) all work. wimy on
macOS is therefore an ordinary app with the Accessibility permission,
not a compositor client.

**Decision: stay in Go, share the model, add a macOS backend.** Do not
rewrite in Swift.

- Everything that defines wimy's UX is already platform-free
  (import lists checked):

  | package | lines | imports | macOS |
  |---|---|---|---|
  | `internal/wm` (model + solver) | 1,416 | `fmt`, `strconv` | unchanged |
  | `internal/command` | 303 | `wm` | unchanged |
  | `internal/config` | 574 | kdl-go | + macOS key names, per-OS `Mod` default |
  | `internal/rpc`, `cmd/wimyctl` | 594 | net/json | socket path fallback only |
  | `internal/titlebar` | 362 | go-text, x/image | reused for overlay titlebars |
  | `internal/river` | 1,801 | wayland/proto | **kept** for Linux; `internal/macos` added alongside |

- The solver emits backend-neutral `wm.Placement`
  (`internal/wm/layout.go:7`: content rect, bar, collapsed strip,
  hidden, layer, focused, output). `rpc.Backend`
  (`internal/rpc/rpc.go:28`) and `command.Effects`
  (`internal/command/command.go:32`) are already interfaces.
- A Swift rewrite would duplicate the solver, tag algebra, command
  table and config semantics. The two copies would drift, which works
  against "same UX". Swift only makes the AX/AppKit calls easier, and
  those are a small part of the code.
- The UX gaps listed below come from macOS and would be the same in Swift.

**cgo on darwin (approved).** The macOS backend needs AX observers
(C callbacks on a CFRunLoop), CGEventTap and AppKit overlay windows, so
it uses cgo plus a small Objective-C file (Xcode command-line tools are
always present on a Mac that builds Go). **The Linux build stays pure Go**:
all cgo lives behind `//go:build darwin`, so `CGO_ENABLED=0 go build`
on Linux keeps working. Update the "pure Go, no cgo" wording in the
README and AGENTS.md to "pure Go on Linux".

### Two backends, one tree

Both backends stay first-class. Each OS builds exactly one:

```
internal/backend   shared core (queue, effects, autostart, reload, pointer ops)
internal/river     //go:build linux   — unchanged role: river protocol
internal/macos     //go:build darwin  — AX/AppKit
cmd/wimy           backend_linux.go → river.New, backend_darwin.go → macos.New
```

- Any new WM behavior still starts in `internal/wm` and reaches both
  backends through `wm.Placement`. A feature is done only when both
  backends render it, or the plan records why one can't (e.g. clip boxes).
- The Linux e2e suites keep guarding the river backend. The darwin
  checklist (see Testing) guards the macOS one.
- `cmd/keyinject`, `cmd/ptrinject` and `internal/proto` are Linux-only
  (add build tags so `go build ./...` works on darwin).

## Concept mapping

| wimy / river | macOS mechanism | fidelity |
|---|---|---|
| new/closed window, title, app_id | `NSWorkspace` launch/terminate notifications → per-pid `AXObserver` (`kAXWindowCreated`, `kAXUIElementDestroyed`, `kAXTitleChanged`, `kAXFocusedWindowChanged`, moved/resized); app_id = bundle identifier | ✅ |
| window identity | `_AXUIElementGetWindow` (private but stable; AeroSpace/yabai use it) → `CGWindowID` → `wm.WindowID` | ✅ |
| propose_dimensions / position | `AXUIElementSetAttributeValue` `kAXPosition`/`kAXSize` | ⚠️ apps with minimum sizes refuse to shrink; slow apps lag. Read back the actual frame and report it |
| tags / views | no public Spaces API → AeroSpace approach: windows of hidden views are moved into a screen corner (recent macOS keeps a sliver on-screen). All wimy views live on one macOS Space | ✅ multi-tag windows still work (our emulation) |
| xkb bindings | `CGEventTap` on keyDown (Accessibility + Input Monitoring) — swallows matched combos. Carbon `RegisterEventHotKey` is the no-permission fallback | ✅ |
| modifiers | `Mod` default = **Option** on darwin (decided); Cmd collides with app shortcuts; Hyper via Karabiner documented as an alternative | ✅ config names stay the same |
| physical-key combos | combos still name the physical key; map names → macOS virtual keycodes (`kVK_ANSI_*`, layout-independent) | ✅ |
| focus | `kAXRaiseAction` + `kAXMainAttribute` + `NSRunningApplication.activate`; private `_SLPSSetFrontProcessWithOptions` if needed (yabai) | ⚠️ focusing a specific window of a multi-window app can occasionally land on the wrong one |
| borders | click-through, always-on-top borderless `NSWindow`/`NSPanel` per window (JankyBorders approach) | ✅ may lag a frame on moves |
| titlebars | overlay `NSPanel` above each window showing the `internal/titlebar` bitmap (via `CGImage`); click → focus | ⚠️ looks the same, but apps keep their native titlebar (double decoration) and the overlay can lag |
| stack-mode collapsed strip | can't clip another app's window → move the collapsed window into the hidden corner and show only our titlebar panel at the strip rect | ⚠️ same look, different mechanism |
| clip boxes | none; only the collapsed strip above needs clipping | — |
| fullscreen | fill the output's rect (no native fullscreen: it creates a separate Space) | ✅ |
| layer-shell usable area | `NSScreen.visibleFrame` (excludes menu bar and Dock) minus a configurable `bar-gap` for SketchyBar | ✅ |
| layer focus (launcher) | not needed; launchers are ordinary apps | — |
| outputs | `NSScreen` + `didChangeScreenParameters`; name = `localizedName` (+ display ID for uniqueness) | ✅ |
| pointer move/resize (Mod-drag, grow) | `CGEventTap` on mouse events, drive the existing `seatOp*` logic | ✅ |
| client move/resize requests | no equivalent; detect user drags from `kAXMoved`/`kAXResized` when wimy didn't cause them, then float the window or snap it back | ⚠️ |
| dialogs / transients | `kAXSubrole` ≠ `AXStandardWindow`, or window has no zoom/fullscreen button → float | ✅ heuristics, same as AeroSpace |
| waybar | SketchyBar plugin fed by `wimyctl subscribe` (same JSON) | ✅ |
| fuzzel (menu/prompt) | `choose-gui` (dmenu-style stdin/stdout); config `menu` already arbitrary | ✅ |
| notify-send on reload error | `osascript -e 'display notification …'` | ✅ |
| running wimy | `Wimy.app` bundle + bundled LaunchAgent (see Starting wimy) | ✅ |
| `exit_session` / quit | quit just exits wimy (restore all windows to visible first) | ✅ |

### Option key gotcha

Option is also macOS's key for typing special characters (Option-e →
´, Option-p → π). With `Mod` = Option, every bound Option combo is
swallowed by the event tap, so those characters can't be typed with the
bound keys. Unbound Option combos still pass through. That is the same
trade-off as Mod1/Alt on Linux. Document it, and note that the combos
match the physical key (keycode), not the character Option would produce.

### Coordinate systems

AX uses top-left origin, global points; AppKit/`NSScreen` uses
bottom-left origin. Convert once at the backend boundary; the model stays
top-left, global, integer (`wm.Rect` is `int32`). Work in **points**; draw
titlebar bitmaps at `backingScaleFactor`.

### Safety: never lose windows

Hidden windows sit in a screen corner. If wimy crashes, they stay there.
Required from day one:
- On quit, SIGTERM and any recovered panic, move every managed window
  back on-screen.
- Keep a state file with each hidden window's last visible rect; on
  startup, restore any window found in the hide corner.

## Background: what a macOS app bundle is

(For readers new to macOS.) An "app" is a directory named `*.app` with a
fixed layout. Finder shows it as one icon; underneath it's plain files:

```
Wimy.app/
  Contents/
    Info.plist                  metadata (XML)
    MacOS/wimy                  the Go binary, unchanged
    MacOS/wimyctl
    Resources/                  icon (optional)
    Library/LaunchAgents/io.github.wimy.plist   login/restart agent
    _CodeSignature/             written by codesign
```

`Info.plist` keys: `CFBundleIdentifier` (`io.github.wimy`; macOS keys
permissions and login items on it), `CFBundleExecutable` (`wimy`),
`LSUIElement = true` (no Dock icon, no app menu),
`LSMinimumSystemVersion = 26.0`, name and version. No Xcode project or
Swift is involved: a Makefile target runs `go build`, creates the
directories, copies files and signs.

**Cost: free for personal use.** Xcode Command Line Tools
(`xcode-select --install`, not full Xcode) provide `codesign` and the C
compiler cgo needs. Signing options:

| option | cost | effect |
|---|---|---|
| ad-hoc (`codesign -s -`; the Go linker does this automatically on arm64) | free | runs locally; identity is the binary's hash, so every changed build must be re-approved in Privacy & Security |
| self-signed certificate (created once in Keychain Access) | free | stable identity across rebuilds; trusted on this machine only. **Used for dev builds** |
| Developer ID + notarization | $99/yr Apple Developer Program | others can download it without Gatekeeper warnings. Only needed to distribute binaries |

Gatekeeper only checks quarantined (downloaded) files, so an app built
locally opens without warnings.

## Starting wimy on macOS

On Linux nothing changes: `river -c wimy`.

On macOS wimy ships as an **app bundle**, `Wimy.app` (bundle id e.g.
`io.github.wimy`, `LSUIElement = YES` so there's no Dock icon), with the
daemon at `Contents/MacOS/wimy` and `wimyctl` next to it. Reasons for a
bundle rather than a bare binary:

- **Permissions attach to the right thing.** macOS grants Accessibility
  to the *responsible* process. A bare `wimy` started from Terminal is
  attributed to Terminal, so Terminal would need the permission, and
  wimy dies when that Terminal closes. A bundle has its own identity in
  System Settings → Privacy & Security.
- Login items and a menu bar item (below) both expect a bundle.

Launch paths:
1. **Normal use, start at login:** the bundle contains
   `Contents/Library/LaunchAgents/io.github.wimy.plist` with
   `KeepAlive = { SuccessfulExit = false }`, registered via
   `SMAppService.agent(plistName:)`. Crash → launchd restarts wimy (and
   startup recovers hidden windows). `wimyctl quit` exits 0 → stays
   stopped. Config key `start-at-login` (default `#true` on darwin,
   ignored on Linux) registers or unregisters it at startup and on
   `reload`. The user approves it once in System Settings → Login Items.
2. **Manually:** `open -a Wimy`, or from the menu bar item's Quit/relaunch.
3. **Development:** `make mac-app` builds, assembles the bundle in
   `bin/Wimy.app`, signs it with the stable `wimy-dev` identity (see
   Risks), then `open bin/Wimy.app`. Logs go to
   `~/Library/Logs/wimy.log` (same `-log` flag).

`wimyctl` is symlinked onto `PATH` (`make mac-install` →
`/usr/local/bin`, or a Homebrew cask later). It finds the daemon through
`rpc.SocketPath()`, which falls back to `$TMPDIR/wimy.sock` on darwin.
launchd gives each agent the same per-user `$TMPDIR`, so the CLI and the
daemon agree.

First run: wimy calls `AXIsProcessTrustedWithOptions(prompt)`. Until the
permission is granted it shows a menu bar item saying so and polls
(the permission takes effect without a restart on recent macOS; fall back
to relaunching if the poll never sees it).

## Menu bar item

Not technically required: an `LSUIElement` app runs fine with no UI at
all. But without one, a background tiler is invisible, and the only way
to tell it's running, quit it, or see that permission is missing is a
terminal. AeroSpace, Amethyst and Rectangle all have one for these
reasons. It's cheap (one `NSStatusItem`), so include it, with an off
switch:

- Title: the focused output's current view name (same info as the
  waybar/SketchyBar module; useful when no bar is running).
- Menu: current views (click → `view <name>`), Reload config, Open config
  file, Permission status (with a button that opens the right Settings
  pane), Start at login toggle, Quit wimy.
- Every menu action goes through the command registry (invariant 4), so
  it behaves exactly like the matching key binding or `wimyctl run`.
- Config: `status-item #true` (default on darwin; ignored on Linux,
  where waybar fills this role). Users running SketchyBar can turn it off.

## Approach

### Phase 0 — extract the shared backend core (Linux-only change, fully testable here)

Move platform-neutral code out of `internal/river` into a new
`internal/backend` (name TBD) so both backends reuse it:

- command queue + `QueueCommand` / `CommandNames` / `Snapshot` /
  `drainQueue` (river.go:111–160, 480)
- `command.Effects` parts that don't touch protocol: `Spawn`,
  `SpawnTerminal`, `SpawnMenu`, `Prompt`, `Action(s)` (effects.go)
- autostart reconciliation (autostart.go; `Setpgid` works on darwin)
- config apply bookkeeping that isn't protocol (reload.go `Reload`,
  `notifyReloadError` behind a small per-OS notifier; `applyConfig` split
  into shared + backend hooks)
- pointer ops math (`seatOpMove/Resize/ColumnResize`, `resizeEdges`),
  written against `wm` only

`internal/river` embeds the core and keeps only protocol translation.
Also:
- `rpc.SocketPath`: fall back to `$TMPDIR/wimy.sock` when
  `WAYLAND_DISPLAY` is unset (darwin).
- `cmd/wimy/main.go`: pick the backend via build-tagged files
  (`backend_linux.go` → `river.New`, `backend_darwin.go` → `macos.New`).
- `config`: per-OS `Mod` default, per-OS `terminal`/`menu` defaults,
  keycode table under `//go:build darwin`.

Verification: `go test ./...`, `go vet`, gofmt, **all six e2e suites
pass unchanged**. This phase is safe to land on its own.

### Phase 1 — macOS spike: tiling + keys on one screen (go/no-go)

`internal/macos` (`//go:build darwin`, cgo + `ax.m`):
- main thread locked (`runtime.LockOSThread` in `init`), runs
  `NSApplication` (accessory activation policy: no Dock icon) /
  CFRunLoop. This thread plays the role of river's dispatch goroutine:
  it owns every AX/AppKit call.
- `DoSync` equivalent: `dispatch_async_f(main_queue)` + wait; same rule
  as today (never call it from the main thread).
- "Manage sequence" equivalent: coalesce events and queued commands, then
  run one `apply()` on the next run-loop pass: `syncModel` → drain queue
  → `state.Layout()` → diff against last applied frames → AX set
  position/size only for windows that changed.
- `AXIsProcessTrustedWithOptions` prompt on startup; exit with a clear
  message if not trusted.
- Enumerate existing windows at startup; tile standard windows, float the
  rest.
- Keyboard: CGEventTap, match against config bindings (exact modifier
  match, as on river).

Go/no-go question: **is AX fast enough to feel like wimy?** Measure the
time to re-tile a view of 6 windows (Terminal, Safari, Finder, an
Electron app) and flag any app that ignores size requests. If it's too
slow, see the Swift fallback under Risks.

### Phase 2 — views/tags, focus, multi-output

- Hide/show via the corner (with the safety requirements above).
- Focus: raise + activate; sync `kAXFocusedWindowChanged` (and app
  activation from Cmd-Tab or Dock clicks) back into the model, so
  external focus changes don't fight wimy.
- Multiple outputs from `NSScreen`; screen-change notifications.
- Float heuristics for dialogs, sheets, panels.

### Phase 3 — decorations

- Borders as overlay panels driven from `Placement.Focused` and colors
  from config.
- Titlebars: `internal/titlebar` renders RGBA → `CGImage` in an overlay
  `NSPanel`, redrawn only when dirty (same dirty-check as `deco.go`).
  Click → focus/select.
- Stack mode: collapsed windows hidden in the corner; strips are
  titlebar panels only.
- Titlebars stay unconditional, as on Linux: stack mode depends on them.
  Document the double titlebar (native + wimy).

### Phase 4 — mouse, contrib, docs

- Mod-drag move/resize and the grow binding via the mouse event tap,
  reusing Phase 0 pointer ops.
- Handle user drags of tiled windows (snap back / float).
- Menu bar item (see above).
- `contrib/macos/`: SketchyBar plugin using `wimyctl subscribe`,
  `choose-gui` example, Karabiner Hyper snippet (optional alternative Mod).
- Makefile targets `mac-app`, `mac-install`; bundle `Info.plist` and
  LaunchAgent plist under `contrib/macos/Wimy.app/`.
- Nix packaging (see below).
- README + AGENTS.md: macOS section (permissions, signing, cgo exception,
  gotchas learned during the port).

### Packaging: Nix flake + nix-darwin module

nix-darwin already has modules for comparable tools
(`services.aerospace`, `services.yabai`, `services.jankyborders`,
`services.sketchybar`); model `services.wimy` on `services.aerospace`.
The repo has no Nix packaging yet (only `contrib/arch/PKGBUILD`).

Add `flake.nix` at the repo root exporting:
- `packages.<system>.wimy` for `x86_64-linux`, `aarch64-linux` (river
  backend, `CGO_ENABLED=0`) and `aarch64-darwin` (macOS backend, cgo; the
  nixpkgs darwin stdenv ships the Apple SDK). Built with `buildGoModule`
  (`vendorHash` pinned). On darwin, `postInstall` assembles
  `$out/Applications/Wimy.app` from the same `Info.plist` template the
  Makefile uses; `wimyctl` goes in `$out/bin`.
- `darwinModules.wimy`:

  ```nix
  services.wimy = {
    enable = true;
    package = wimy.packages.aarch64-darwin.wimy;
    settings = ./config.kdl;   # path or string → ~/.config/wimy/config.kdl
  };
  ```

  The module adds the package to `environment.systemPackages` and declares
  `launchd.user.agents.wimy` (`RunAtLoad`, `KeepAlive.SuccessfulExit =
  false`, logs to `~/Library/Logs/wimy.log`). `ProgramArguments` must
  point at `…/Wimy.app/Contents/MacOS/wimy` **inside the bundle**, so TCC
  attributes the permission to Wimy's bundle id, not to launchd.
- Optional: a home-manager module for the Linux side (config file +
  autostart of river with wimy).

Differences from the non-Nix install:
- **Nix owns login startup.** The module writes `start-at-login #false`
  into the generated config so wimy doesn't also register its own
  `SMAppService` agent (two competing agents).
- **Signing.** Nix builds are sandboxed and can't reach the user's
  keychain, so the package only has the linker's ad-hoc signature.
  Because the build is reproducible, an unchanged wimy yields an
  identical binary and the TCC grant survives `darwin-rebuild switch`;
  it has to be re-approved only when wimy itself changes. If that is
  annoying, add an opt-in module option (`codesignIdentity`) that
  re-signs the *installed copy* with the user's certificate in an
  activation script (the store path itself can't be modified).
- **Verify on a real Mac:** nix-darwin has changed how it installs apps
  into `/Applications/Nix Apps` (symlinks vs copies), partly because of
  TCC and Spotlight problems. Confirm that the Accessibility grant
  persists across two `darwin-rebuild switch` runs, one with wimy
  unchanged and one with it changed, and record the result in AGENTS.md.

If it works well, upstream `services.wimy` to nix-darwin.

## Testing

- Shared packages: the existing unit tests run on both OSes; add a darwin
  CI job for `go test ./...` + `go vet` (GitHub `macos-latest`).
- Pure translation logic in `internal/macos` (coordinate flips, frame
  diffing, float heuristics given AX attributes, keycode mapping) goes in
  a cgo-free file with unit tests.
- Headless river has no macOS equivalent, and CI runners can't be granted
  the Accessibility permission non-interactively. So integration testing
  is **a manual checklist on a real Mac** (`plans/macos-checklist.md`),
  mirroring the e2e suites: spawn/tile, focus dirs, move, views/tags,
  stack/max modes, float, multi-output, reload, autostart, Mod-drag,
  crash recovery of hidden windows.
- Optional later: a `wimy --selftest` mode that opens its own test
  windows (AppKit) and asserts placements via AX, runnable locally once
  permission is granted.

## Risks

- **TCC attribution when started from a terminal** (see Starting wimy):
  always launch the bundle, never the bare binary, except for quick
  debugging.
- **TCC permission resets on rebuild.** Accessibility/Input Monitoring
  grants are tied to the code signature, and ad-hoc signatures change
  every build. Sign every dev build with a stable self-signed identity
  (`codesign -s wimy-dev`) from a make target. Nix installs can't do
  this at build time; see Packaging.
- **AX latency / uncooperative apps** (Electron, Java, some Catalyst
  apps). AeroSpace and yabai live with it; measured in Phase 1.
- **Private APIs** (`_AXUIElementGetWindow`, maybe
  `_SLPSSetFrontProcessWithOptions`) can break across macOS releases.
  Keep them in one file with a fallback path.
- **Corner hiding** depends on how much macOS clamps off-screen windows;
  this changed in past releases (AeroSpace tracks it).
- **Double titlebars** on every app (native + wimy). Accepted, as with
  Firefox on river. Per-app opt-out stays future work (config rule keyed
  on bundle id / app_id).
- **Swift fallback** if Go+cgo turns out painful for the AX/AppKit layer:
  write only `internal/macos` as a Swift static library exposing a C ABI
  (`@_cdecl`) and link it via cgo. The shared Go core stays; the solver is
  never rewritten.

## Not in scope

- Controlling native macOS Spaces or anything needing SIP disabled
  (yabai scripting addition).
- Window animations, blur, or shadows.
- Input device configuration (same as the Linux deferred list).

## Target platform

- **macOS 26 (Tahoe)** minimum. The code can use current APIs
  (`SMAppService`, modern `NSStatusItem`) without availability checks.
- Build `darwin/arm64` first. macOS 26 is the last release for Intel
  Macs, so a `darwin/amd64` build is cheap to add but only matters if an
  Intel Mac is in use.
- Tahoe windows have larger rounded corners. Overlay borders should draw
  with a matching corner radius, or they'll look off at the corners.
  Check this on a real screen in Phase 3.

## Open questions

- Bundle identifier / app name (`Wimy.app`, `io.github.wimy`?).
- Hardware to test on: Apple Silicon only?
