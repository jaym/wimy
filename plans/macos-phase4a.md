# macOS port, Phase 4a: app bundle, signing, restart with state, menu bar item — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** wimy runs as a signed `Wimy.app` (bundle id `io.github.jaym.wimy`) that starts at login, and an update is one command — `make mac-install` — that swaps the bundle and restarts wimy in place with its whole state (views, tags, columns, modes, focus, floating rects, fullscreen, parked windows, autostart children) and without touching the Accessibility permission.

**Architecture:**
- **Stable signing** is what keeps updates painless: TCC keys the Accessibility grant on the code signature, and ad-hoc signatures change with every build. A self-signed code-signing identity `wimy-dev` (created once by a script the user runs) makes the designated requirement stable across rebuilds.
- **Restart with state:** the `restart` command (shared registry → `Effects.Restart` → `Platform.Restart`) writes a *handoff* file — the whole `wm.State` as JSON (all its fields are exported), the live autostart pids, and platform extras (macOS: parked/hidden windows, last on-screen frames, minimized windows' tags) — then `exec`s the executable on disk, i.e. the updated one. The successor reads the handoff (same boot, < 2 min old, consumed once), restores the model instead of adopting windows afresh, keeps parked windows parked, prunes windows that vanished, and adopts the autostart children (after `exec` they are still children of the same pid). CGWindowIDs survive a wimy restart; river's window ids don't, so restart is macOS-only (river returns an error).
- **Bundle:** `make mac-app` builds `bin/Wimy.app` (Info.plist with `LSUIElement`, a LaunchAgent plist in `Contents/Library/LaunchAgents` for `SMAppService`), signs `wimyctl` then the bundle with `wimy-dev` (ad-hoc with a warning if missing). `make mac-install` syncs it to `~/Applications/Wimy.app`, links `wimyctl` into `~/.local/bin`, and runs `wimyctl restart` if wimy is running (else `open`s it).
- **Menu bar item** (`status-item`, default on): title = focused output's view; menu = views (click switches), Reload config, Open config, Restart wimy, Accessibility and login status, Quit — actions go through the command registry. While Accessibility isn't granted, the item says so, offers to open the Settings pane, and wimy polls until it is granted instead of exiting (launchd would otherwise restart it in a loop).
- **Start at login** (`start-at-login`, default on, macOS only): when running from the bundle, wimy registers/unregisters its agent with `SMAppService` at startup and on reload.
- **Single instance:** a second wimy whose socket already answers exits 0 (so launchd doesn't restart it).

**Spec:** `plans/macos-port.md` — "Background: what a macOS app bundle is", "Starting wimy on macOS", "Menu bar item", "Risks" (TCC resets on rebuild), Phase 4.

## Global Constraints

- Linux: build/vet/e2e unchanged; river's `Restart` returns "restart is supported on macOS only"; new config keys parse on Linux and are ignored there.
- Pure logic (handoff encode/decode, freshness, autostart adoption, version wait, config keys, log path, single-instance check) unit-tested; cgo/ObjC verified live.
- Never lose a window across a restart: parked windows stay recorded in the hidden store until the successor has taken them over.
- The user's keychain is only touched by the script they run themselves.
- Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Review Focus

1. A stale handoff (another boot, or minutes old — e.g. a crash right after writing it) is ignored and deleted, not applied. `TestReadHandoffStale`, `TestReadHandoffOtherBoot`.
2. Windows in the handoff that closed during the restart are pruned; windows that appeared are adopted normally. `TestRestoreStatePrunes`.
3. Autostart children survive a restart and are still killed/restarted by a later reload. `TestAutostartAdopt`.
4. `wimyctl restart` reports success only when a *new* instance answers. `TestWaitRestarted`.
5. A second wimy (login agent + manual `open`) doesn't steal the socket from the running one. `TestAnotherInstanceRunning`.

---

### Task 1: State handoff and autostart adoption (internal/backend)

**Interfaces:** `type Handoff struct{ Boot int64; Written time.Time; Model json.RawMessage; Autostart map[string][]int; Platform json.RawMessage }`; `func (c *Core) WriteHandoff(path string, boot int64, platform any) error`; `func (c *Core) ReadHandoff(path string, boot int64, now time.Time, platform any) (bool, error)` (consumes the file; true when applied); `func (c *Core) PruneWindows(present func(wm.WindowID) bool) []wm.WindowID`; `func (a *Autostart) Pids() map[string][]int`; `func (a *Autostart) Adopt(cmdline string, pid int)`.

- [ ] **Tests (RED)** — `internal/backend/handoff_test.go`:

```go
package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"wimy/internal/config"
	"wimy/internal/wm"
)

func coreWithLayout(t *testing.T) *Core {
	t.Helper()
	c, _ := newTestCore(t, nil)
	c.State.AddWindow(1, false)
	c.State.AddWindow(2, false)
	c.State.MoveDir(wm.DirRight)
	c.State.SetMode(wm.ModeStack)
	c.State.AddWindow(3, true)
	c.State.SetFloatRect(3, wm.Rect{X: 300, Y: 200, W: 400, H: 300})
	c.State.TagSpec("+web")
	c.State.FocusWindow(1)
	return c
}

type extras struct {
	Parked []wm.WindowID `json:"parked"`
}

func TestHandoffRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	if err := old.WriteHandoff(path, 42, extras{Parked: []wm.WindowID{9}}); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestCore(t, nil)
	var ex extras
	ok, err := next.ReadHandoff(path, 42, time.Now(), &ex)
	if err != nil || !ok {
		t.Fatalf("ReadHandoff = %v, %v", ok, err)
	}
	if got, want := layoutOf(next.State), layoutOf(old.State); got != want {
		t.Errorf("layout after handoff:\n%s\nwant\n%s", got, want)
	}
	if next.State.Focused != 1 || len(ex.Parked) != 1 || ex.Parked[0] != 9 {
		t.Errorf("focus %d, extras %+v", next.State.Focused, ex)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("handoff not consumed: %v", err)
	}
}

func TestHandoffKeepsConfigDerivedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	old.State.TitlebarHeight = 99
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Titlebar.Height = 22
	next, _ := newTestCore(t, cfg)
	if ok, _ := next.ReadHandoff(path, 1, time.Now(), nil); !ok {
		t.Fatal("not applied")
	}
	if next.State.TitlebarHeight != 22 {
		t.Errorf("titlebar height %d from the old process, want 22 from this config", next.State.TitlebarHeight)
	}
}

func TestReadHandoffStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestCore(t, nil)
	if ok, err := next.ReadHandoff(path, 1, time.Now().Add(10*time.Minute), nil); ok || err != nil {
		t.Errorf("stale handoff applied: %v %v", ok, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stale handoff not deleted")
	}
}

func TestReadHandoffOtherBoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestCore(t, nil)
	if ok, _ := next.ReadHandoff(path, 2, time.Now(), nil); ok {
		t.Errorf("handoff from another boot applied (window ids restart after a reboot)")
	}
}

func TestReadHandoffMissing(t *testing.T) {
	next, _ := newTestCore(t, nil)
	if ok, err := next.ReadHandoff(filepath.Join(t.TempDir(), "none.json"), 1, time.Now(), nil); ok || err != nil {
		t.Errorf("missing handoff: %v %v", ok, err)
	}
}

func TestRestoreStatePrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestCore(t, nil)
	next.ReadHandoff(path, 1, time.Now(), nil)
	gone := next.PruneWindows(func(id wm.WindowID) bool { return id != 2 }) // 2 closed meanwhile
	if len(gone) != 1 || gone[0] != 2 || next.State.Windows[2] != nil {
		t.Errorf("pruned %v, window 2 still %v", gone, next.State.Windows[2])
	}
}

// layoutOf renders a state's layout for comparison.
func layoutOf(s *wm.State) string {
	out := ""
	for _, p := range s.Layout() {
		out += fmt.Sprintf("%d %+v hidden=%v collapsed=%v float=%v focused=%v\n", p.ID, p.Rect, p.Hidden, p.Collapsed, p.Layer, p.Focused)
	}
	return out
}
```

(add `"fmt"` to the imports; `layoutOf` must iterate in a stable order — `Layout()` appends hidden windows by ranging over a map, so sort the lines before joining.)

`internal/backend/autostart_test.go`:

```go
func TestAutostartAdopt(t *testing.T) {
	// a restart execs in place: the autostart children stay children of
	// the same pid, and the successor takes them over
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var a Autostart
	a.Adopt("sleep 30", cmd.Process.Pid)
	if got := a.Pids()["sleep 30"]; len(got) != 1 || got[0] != cmd.Process.Pid {
		t.Fatalf("Pids = %v", a.Pids())
	}
	killed, _, _ := a.Sync([]string{"sleep 30"}, nil)
	if killed != 1 {
		t.Fatalf("killed = %d", killed)
	}
	waitFor(t, "adopted child to die", func() bool { return syscall.Kill(cmd.Process.Pid, 0) != nil })
}
```

(`os/exec` import; the test process reaps via the adopted waiter.)

- [ ] **Implement:** `handoff.go` in `internal/backend`:
  - `WriteHandoff`: marshal `c.State`, `c.autostart.Pids()`, `platform`; `Written = time.Now()`; atomic write (tmp + rename, 0600, `MkdirAll` 0700).
  - `ReadHandoff`: missing → false, nil; read then **always remove** the file; invalid JSON → error; `Boot != boot` or `now.Sub(Written) > 2*time.Minute` → false, nil; else unmarshal the model into `*c.State` (reset first, keep `c.State` pointer — the registry holds it), re-apply `StackStrip`/`TitlebarHeight` from `c.Cfg`, adopt each autostart pid, unmarshal `Platform` into `platform` if non-nil → true.
  - `PruneWindows`: remove (via `State.RemoveWindow`) every window `present` rejects; return their ids sorted.
  - `autostartProc` gets a `pid int` field (logs use it instead of `p.cmd.Process.Pid`); `Adopt` records `{cmdline, pid, pgid: pid}` and waits with `os.FindProcess(pid).Wait()` in a goroutine; `Pids` lists live pids per cmdline.
- [ ] **Commit** ("Hand wimy's whole state to its successor on restart").

### Task 2: `restart` command, version, `wimyctl restart`

**Interfaces:** `command.Effects.Restart() error`; `backend.Platform.Restart() error`; `Core.Restart` delegates; river returns an error. `rpc.Info{Version string; Started time.Time}`; `rpc.Listen(b Backend, info Info)`; RPC method `version` → `{"version":…, "started":…, "pid":…}`; `rpc.Running(path string) bool`. `wimyctl restart` and `wimyctl version`. Pure `waitRestarted(prev time.Time, fetch func() (time.Time, error), deadline time.Duration, sleep func(time.Duration)) error` in `cmd/wimyctl`.

- [ ] **Tests (RED):**
  - `internal/command`: `TestRestartCommand` (fakeFx records `restarted`).
  - `internal/backend`: fakePlatform gets `Restart() error { f.restarted = true; return nil }`; `TestRestartReachesPlatform` (queue "restart", drain, assert).
  - `internal/rpc/rpc_test.go`: `TestVersionAndRunning` — `Listen` a fake backend on a temp socket (`WIMY_SOCKET` env), `Running(path)` true, a raw JSON-RPC `version` call returns the info; after `Close`, `Running` false. `TestAnotherInstanceRunning`: while one server listens, `Listen` a second time returns `ErrAlreadyRunning`.
  - `cmd/wimyctl/restart_test.go`: `TestWaitRestarted` — fetch returns the old start time twice, then an error (restarting), then a new time → nil; a fetch that never changes → error after the deadline (fake sleep).
- [ ] **Implement:** registry `"restart": func(e *Env, _ []string) error { return e.Fx.Restart() }`; the error is logged by `DrainQueue` like any command error. `rpc.Listen` returns `ErrAlreadyRunning` when `Running(path)`; `main` treats it as "wimy is already running" and exits 0. `wimyctl version` prints version and start time; `wimyctl restart` reads `version`, runs `restart`, waits up to 20s for a different `started`, prints `wimy restarted: <old> -> <new version>`. `cmd/wimy`: `var version = "dev"` (set by `-ldflags -X main.version=`), passed in `rpc.Info`.
- [ ] **Commit** ("Add wimyctl restart and version").

### Task 3: Config keys and default log path

- [ ] **Tests (RED)** `internal/config`: `status-item false` and `start-at-login false` parse; defaults `StatusItem`/`StartAtLogin` true on darwin, false elsewhere (`runtime.GOOS`); non-bool → error. `cmd/wimy`: `TestDefaultLogPath` — darwin → `<home>/Library/Logs/wimy.log`; linux → socket path with `.log`.
- [ ] **Implement:** `Config.StatusItem`, `Config.StartAtLogin` (doc: macOS only), in `osDefaults`; parse like `focus-follows-mouse`; `ConfigChange.Login bool` (start-at-login changed) and status-item changes reported as `"status-item"` / `"start-at-login"`. `defaultLogPath(goos, home, sock string)` in `cmd/wimy`.
- [ ] **Commit** ("Add status-item and start-at-login; macOS logs to ~/Library/Logs").

### Task 4: macOS restart and handoff startup

- [ ] `Platform.Restart` (macOS): `b.store` saved; `WriteHandoff(stateDir/handoff.json, b.boot, macHandoff{Hidden, Parked, LastShown, Away})`; `exe, _ := os.Executable()`; `syscall.Exec(exe, os.Args, os.Environ())`; on failure remove the handoff and return the error (wimy keeps running).
- [ ] `Run` → `start()`: before tracking, `ReadHandoff`; when applied: `b.restarted = true`; `b.hidden/parked/lastShown/away` from the extras; `syncScreens` (existing outputs keep their names; afterwards `RemoveOutput` for model outputs that no screen has); `windowAdded` for a window already in the model only marks it known and refreshes app id/title (no `AddWindow`, no store restore, no corner rescue for parked ones); after tracking `PruneWindows(known)` and drop pruned ids from hidden/parked; skip spread, focus seeding (keep the model's focus and set `lastFocus` to it) and `StartAutostart`.
- [ ] Build matrix; commit ("macOS: restart in place with the handoff").

### Task 5: macOS permission wait, status item, start at login

- [ ] Bridge: `wimy_status_set(const char *title, const char **items, const int *flags, int n)` builds an `NSStatusItem` menu (flags: enabled, checked, separator); clicks → `goMenuItem(index)`; `wimy_status_remove()`. `int wimy_login_register(int on)` via `SMAppService.agentServiceWithPlistName:@"io.github.jaym.wimy.plist"` (register/unregister; returns status: 0 not registered, 1 enabled, 2 requires approval, -1 not in a bundle); `int wimy_in_bundle(void)` (main bundle id is `io.github.jaym.wimy`); `void wimy_open_accessibility_settings(void)`; `void wimy_start_trust_poll(void)` (1s timer → `goTrustTick`).
- [ ] Backend: `Run` inits the app; trusted → `start()`; else status item "wimy ⚠ needs Accessibility" with "Open Privacy & Security…", prompt once, poll; when granted → `start()`. Status menu rebuilt in `apply` when views/focused view/login status change: views (checked = shown on the focused output) → `view <name>`, separator, Reload config → `reload`, Open config → `spawn open -t <path>`, Restart wimy → `restart`, separator, "Accessibility: allowed", "Start at login: on/off/needs approval" (disabled), separator, Quit wimy → `quit`. `status-item false` removes it. `start-at-login` applied at start and on reload when in the bundle.
- [ ] Commit ("macOS: menu bar item, permission wait, start at login").

### Task 6: Bundle, signing, install

- [ ] `contrib/macos/Wimy.app/Contents/Info.plist` (template with `@VERSION@`): `CFBundleIdentifier io.github.jaym.wimy`, `CFBundleExecutable wimy`, `CFBundleName Wimy`, `CFBundlePackageType APPL`, `LSUIElement true`, `LSMinimumSystemVersion 26.0`, `CFBundleShortVersionString`/`CFBundleVersion`.
- [ ] `contrib/macos/Wimy.app/Contents/Library/LaunchAgents/io.github.jaym.wimy.plist`: `Label io.github.jaym.wimy`, `BundleProgram Contents/MacOS/wimy`, `AssociatedBundleIdentifiers [io.github.jaym.wimy]`, `RunAtLoad true`, `KeepAlive {SuccessfulExit false}`, `ProcessType Interactive`.
- [ ] `contrib/macos/make-signing-identity.sh`: creates a self-signed code-signing certificate `wimy-dev` (openssl, `extendedKeyUsage=codeSigning`), imports it into the login keychain, trusts it for code signing (`security add-trusted-cert -p codeSign`, which asks for the user's password), is idempotent (exits if `security find-identity -v -p codesigning` lists it).
- [ ] `Makefile`: `VERSION` from `git describe --always --dirty`; `mac-app` (build both binaries with `-X main.version`, assemble `bin/Wimy.app`, `codesign --force --sign "$(SIGN_ID)" --identifier io.github.jaym.wimy` on `wimyctl` then the bundle; ad-hoc + warning when the identity is missing); `mac-install` (rsync to `~/Applications/Wimy.app`, `ln -sf` wimyctl into `~/.local/bin`, then `wimyctl restart` if running else `open`); `build`, `test` convenience targets.
- [ ] Commit ("Build, sign and install Wimy.app").

### Task 7: Live run and docs

With the user: run the signing script; `make mac-install`; grant Accessibility to Wimy.app (and remove the Ghostty grant if desired); check the menu, login item approval; rebuild with a change, `make mac-install` → restarted in place, layout and parked windows intact, Accessibility still granted; reboot-free login check via `launchctl print gui/$UID/io.github.jaym.wimy`. Docs: README macOS (install, update, restart), AGENTS.md gotchas (signing/TCC, exec handoff), `plans/macos-port.md` Phase 4a status. Update the user's nix-config sketchybarrc WIMYCTL fallback to `~/.local/bin/wimyctl` if useful.
