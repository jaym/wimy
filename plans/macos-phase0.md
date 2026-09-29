# macOS port, Phase 0: shared backend core — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move wimy's platform-neutral backend code out of `internal/river` into a new `internal/backend` package, make the tree build on darwin, and prove with CI that the river backend behaves exactly as before.

**Architecture:** `internal/backend.Core` owns the command queue, the process-spawning half of `command.Effects`, autostart supervision, config-reload bookkeeping and pointer-op math. `river.Backend` embeds `*backend.Core` and implements a small `backend.Platform` interface (wake, config-change hook, kill, quit). Everything under `internal/river` and the two injector tools becomes `//go:build linux`; `cmd/wimy` picks its backend through build-tagged files, with a darwin stub until Phase 1.

**Tech Stack:** Go 1.26, river-window-management-v1 (wlcl bindings), GitHub Actions with Nix/devenv for the Linux e2e suites.

**Spec:** `plans/macos-port.md` (section "Phase 0 — extract the shared backend core"). Read it first.

## Global Constraints

- `internal/wm` stays pure: no new imports, no I/O (AGENTS.md invariant 1).
- One command registry: every command still dispatches through `internal/command` (invariant 4).
- Never call `conn.DoSync` from the Wayland dispatch goroutine (invariant 3). Key-binding presses run on that goroutine, so they enqueue without waking.
- Linux build stays pure Go: `GOOS=linux CGO_ENABLED=0 go build ./...` must succeed. No cgo is introduced in Phase 0 at all.
- Do not edit `internal/proto/gen.go`.
- `go vet ./...` clean and `gofmt -l cmd internal` prints nothing, on both darwin and `GOOS=linux`.
- All seven e2e suites (`e2e.sh`, `e2e-multi.sh`, `e2e-keys.sh`, `e2e-layer.sh`, `e2e-deco.sh`, `e2e-mouse.sh`, `e2e-reload.sh`) pass **unchanged** — do not edit them to make them pass.
- Linux default config is unchanged (`Mod` = Mod4, alacritty, fuzzel). The e2e suites depend on it.
- Reload log lines keep the form `config reloaded: <section>, <section>…` with the same section names and order (`e2e-reload.sh` greps them).
- Commit messages: imperative, what + why, and end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Where you can run what

The development machine is a Mac. The e2e suites need Linux + river, so they run in GitHub Actions (Task 2). Locally on the Mac you can run:

```sh
go build ./... && go vet ./... && go test ./...          # darwin
GOOS=linux go build ./... && GOOS=linux go vet ./...     # compiles internal/river
GOOS=linux go test -c -o /dev/null ./internal/backend    # compiles Linux tests
```

`GOOS=linux go test` cannot *run* tests on a Mac; the Linux test run happens in CI. `internal/backend` tests must pass on darwin locally.

Pushing to GitHub is outward-facing: **ask the user before the first `git push`** of the branch.

## Review Focus

1. A key binding pressed while `wimyctl run` commands arrive concurrently: every command runs exactly once, in the order it was queued, with no data race. Pinned by `TestQueueCommandConcurrent` (Task 5, run with `-race`).
2. `wimyctl run reload` with a broken config: the old config stays active, autostart processes are untouched, and the user is notified. Pinned by `TestReloadBadConfigKeepsOld` (Task 5).
3. A prompt the user cancels (Escape in fuzzel/choose → non-zero exit, or empty answer) queues nothing. Pinned by `TestPromptFollowUp` (Task 5).
4. `wimyctl` finds the daemon's socket with no environment tweaks: unchanged on Linux (including from a TTY with no `WAYLAND_DISPLAY`), `$TMPDIR/wimy.sock` on darwin. Pinned by `TestSocketPath` (Task 6).
5. Dragging a floating window's left or top edge past the minimum size doesn't push the opposite edge. Pinned by `TestResizeOpLeftEdgeClamps` (Task 4).

## File structure

| file | status | responsibility |
|---|---|---|
| `internal/backend/doc.go` | create | package doc |
| `internal/backend/core.go` | create | `Core`, `Platform`, queue, `NewCore` |
| `internal/backend/effects.go` | create | `Spawn`, `SpawnTerminal`, `SpawnMenu`, `Prompt`, `Action(s)`, `Kill`/`Quit` delegation |
| `internal/backend/reload.go` | create | `Reload`, `ConfigChange`, `applyConfig` report |
| `internal/backend/notify_darwin.go` / `notify_other.go` | create | reload-error desktop notification per OS |
| `internal/backend/autostart.go` | create (moved) | `Autostart` supervisor |
| `internal/backend/pointer.go` | create (moved) | `Edges`, `Button`, `PointerOp`, move/resize/column ops |
| `internal/backend/*_test.go` | create | unit tests |
| `internal/river/*.go` | modify | `//go:build linux`; embed `*backend.Core`; keep protocol only |
| `internal/river/autostart.go` | delete | moved |
| `cmd/wimy/main.go` | modify | backend chosen via `newBackend` |
| `cmd/wimy/backend_linux.go` / `backend_darwin.go` | create | river / not-yet-implemented stub |
| `cmd/keyinject/main.go`, `cmd/ptrinject/main.go` | modify | `//go:build linux` |
| `internal/rpc/rpc.go` + `rpc_test.go` | modify/create | darwin socket fallback |
| `internal/config/config.go`, `defaults_darwin.go`, `defaults_other.go`, `config_test.go` | modify/create | per-OS defaults, Option/Cmd names |
| `ci.sh`, `e2e-all.sh`, `.github/workflows/ci.yml` | create | CI |
| `AGENTS.md`, `README.md`, `plans/macos-port.md` | modify | docs |

`internal/proto` is **not** tagged: it compiles on darwin as-is, and a build tag would have to live in the generated `gen.go`. Only Linux packages import it. (Deviation from the spec, which said to tag it.)

The darwin keycode table the spec lists under Phase 0 moves to Phase 1: only `internal/macos` consumes it and nothing in Phase 0 could exercise it. (Deviation, recorded in Task 7.)

---

### Task 0: Branch

- [ ] **Step 1: Create the working branch**

```bash
git switch -c macos-phase0
```

---

### Task 1: Make the tree build on darwin

Today `go build ./...` on a Mac fails: `unix.MemfdCreate` is Linux-only (`internal/river/deco.go:34`, `cmd/keyinject/main.go:98`, `cmd/ptrinject/main.go:71`).

**Files:**
- Modify: every `internal/river/*.go`, `cmd/keyinject/main.go`, `cmd/ptrinject/main.go` (add build tag)
- Modify: `cmd/wimy/main.go`
- Create: `cmd/wimy/backend_linux.go`, `cmd/wimy/backend_darwin.go`

**Interfaces:**
- Produces: `cmd/wimy` type `wmBackend interface { rpc.Backend; Run(context.Context) error; Shutdown() }` and `func newBackend(cfg *config.Config, configArg string, notify func()) (wmBackend, error)` per OS. Phase 1 replaces the darwin body with `macos.New`.

- [ ] **Step 1: Confirm the failure**

Run: `go build ./...`
Expected: FAIL with `undefined: unix.MemfdCreate` in `wimy/cmd/keyinject`, `wimy/cmd/ptrinject`, `wimy/internal/river`.

- [ ] **Step 2: Tag the Linux-only packages**

Put `//go:build linux` followed by a blank line as the very first line of each of these files (above the package doc comment, which must stay directly above `package`):

```
internal/river/autostart.go  internal/river/deco.go     internal/river/effects.go
internal/river/objects.go    internal/river/pointer.go  internal/river/reload.go
internal/river/river.go      cmd/keyinject/main.go      cmd/ptrinject/main.go
```

For example the top of `internal/river/river.go` becomes:

```go
//go:build linux

// Package river implements the Wayland backend of wimy: it speaks the
```

- [ ] **Step 3: Pick the backend per OS in `cmd/wimy`**

In `cmd/wimy/main.go`, drop the `wimy/internal/river` import, add the `wmBackend` interface after the imports, and replace the backend construction:

```go
// wmBackend is what main needs from a platform backend.
type wmBackend interface {
	rpc.Backend
	// Run runs the event loop until Shutdown or a fatal error.
	Run(ctx context.Context) error
	// Shutdown stops the event loop.
	Shutdown()
}
```

```go
	var server *rpc.Server
	backend, err := newBackend(cfg, *configPath, func() {
		if server != nil {
			server.Notify()
		}
	})
	if err != nil {
		log.Fatalf("wimy: %v", err)
	}
```

Create `cmd/wimy/backend_linux.go`:

```go
//go:build linux

package main

import (
	"wimy/internal/config"
	"wimy/internal/river"
)

// newBackend returns the river backend.
func newBackend(cfg *config.Config, configArg string, notify func()) (wmBackend, error) {
	return river.New(cfg, configArg, notify), nil
}
```

Create `cmd/wimy/backend_darwin.go`:

```go
//go:build darwin

package main

import (
	"errors"

	"wimy/internal/config"
)

// newBackend will return the macOS backend (plans/macos-port.md,
// Phase 1). Until then wimy builds on darwin but refuses to start.
func newBackend(cfg *config.Config, configArg string, notify func()) (wmBackend, error) {
	return nil, errors.New("the macOS backend is not implemented yet (plans/macos-port.md, Phase 1)")
}
```

- [ ] **Step 4: Verify both OSes build and vet**

Run: `go build ./... && go vet ./... && go test ./... && GOOS=linux go build ./... && GOOS=linux go vet ./... && GOOS=linux CGO_ENABLED=0 go build ./... && gofmt -l cmd internal`
Expected: builds and tests pass, `gofmt` prints nothing (or only `internal/proto/gen.go`). Packages excluded by build constraints are skipped silently by `./...`; if Go instead reports "build constraints exclude all Go files", stop and report it.

- [ ] **Step 5: Check the darwin stub message**

Run: `go run ./cmd/wimy -log /dev/null; echo "exit=$?"`
Expected: `wimy: the macOS backend is not implemented yet (plans/macos-port.md, Phase 1)` and `exit=1`.

- [ ] **Step 6: Commit**

```bash
git add internal/river cmd
git commit -m "Build on darwin: tag river and injectors linux-only

internal/river, keyinject and ptrinject use unix.MemfdCreate, which
only exists on Linux, so go build ./... failed on a Mac. cmd/wimy now
picks its backend through build-tagged files; darwin gets a stub until
the macOS backend lands (plans/macos-port.md Phase 1).

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: CI — unit tests on both OSes, e2e on Linux

This establishes a green baseline **before** any code moves, so a later red run is caused by the refactor.

**Files:**
- Create: `e2e-all.sh`, `ci.sh`, `.github/workflows/ci.yml`

**Interfaces:**
- Produces: `./ci.sh` (Linux: fmt, vet, unit tests, pure-Go build, all e2e suites; exits non-zero on any failure). Later tasks verify with it through CI.

- [ ] **Step 1: Write `e2e-all.sh`**

`e2e.sh` and `e2e-multi.sh` don't build binaries themselves, so build everything first:

```bash
#!/usr/bin/env bash
# Runs every e2e suite against headless river. Linux only.
set -u
cd "$(dirname "$0")"

mkdir -p bin
for c in wimy wimyctl keyinject ptrinject; do
  go build -o "bin/$c" "./cmd/$c" || exit 1
done

failed=()
for s in e2e.sh e2e-multi.sh e2e-keys.sh e2e-layer.sh e2e-deco.sh e2e-mouse.sh e2e-reload.sh; do
  echo "=== $s"
  if ! "./$s"; then
    failed+=("$s")
  fi
done

echo
if [ ${#failed[@]} -ne 0 ]; then
  echo "FAILED: ${failed[*]}"
  exit 1
fi
echo "all e2e suites passed"
```

- [ ] **Step 2: Write `ci.sh`**

```bash
#!/usr/bin/env bash
# Linux CI: formatting, vet, unit tests, pure-Go build, e2e suites.
set -eu
cd "$(dirname "$0")"

unformatted=$(gofmt -l cmd internal | grep -v '^internal/proto/gen.go$' || true)
if [ -n "$unformatted" ]; then
  echo "gofmt needed:"; echo "$unformatted"; exit 1
fi
go vet ./...
go test ./...  # -race needs cgo; the darwin CI job runs it
CGO_ENABLED=0 go build ./...
./e2e-all.sh
```

Run: `chmod +x e2e-all.sh ci.sh`

- [ ] **Step 3: Write `.github/workflows/ci.yml`**

The Linux job uses the repo's `devenv.nix` so CI gets the same river 0.4, foot, fuzzel, jq and xkbcli as local development.

```yaml
name: ci
on:
  push:
  pull_request:

jobs:
  linux:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: cachix/install-nix-action@v31
      - uses: cachix/cachix-action@v16
        with:
          name: devenv
      - name: Install devenv
        run: nix profile install nixpkgs#devenv
      - name: Lint, unit tests, e2e
        run: devenv shell ./ci.sh

  darwin:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Format
        run: test -z "$(gofmt -l cmd internal | grep -v '^internal/proto/gen.go$')"
      - name: Build, vet, test
        run: go build ./... && go vet ./... && go test -race ./...
```

- [ ] **Step 4: Commit**

```bash
git add e2e-all.sh ci.sh .github/workflows/ci.yml
git commit -m "Add CI: e2e suites on Linux via devenv, unit tests on macOS

The e2e suites need Linux and headless river, which the macOS
development machine can't run; CI runs them in the devenv shell so
they use the same river 0.4 as local development.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 5: Push and get a green baseline**

Ask the user before pushing. Then:

```bash
git push -u origin macos-phase0
gh run watch --exit-status "$(gh run list --branch macos-phase0 --limit 1 --json databaseId -q '.[0].databaseId')"
```

Expected: both jobs pass. If a suite fails here, nothing has been refactored yet, so it is a CI-environment problem (missing package, timing on a slow runner). Fix the environment, not the suites; if you can't, stop and report the failing check and log to the user.

---

### Task 3: Move autostart supervision to `internal/backend`

**Files:**
- Create: `internal/backend/doc.go`, `internal/backend/autostart.go`, `internal/backend/autostart_test.go`
- Delete: `internal/river/autostart.go`
- Modify: `internal/river/river.go` (field), `internal/river/effects.go` (`runAutostart`), `internal/river/reload.go` (`syncAutostart` call)

**Interfaces:**
- Produces:
  - `type Autostart struct` (zero value ready to use)
  - `func (a *Autostart) StartAll(list []string)`
  - `func (a *Autostart) Start(cmdline string)`
  - `func (a *Autostart) Sync(oldList, newList []string) (killed, spawned, restarted int)`

- [ ] **Step 1: Package doc**

`internal/backend/doc.go`:

```go
// Package backend holds the platform-neutral half of a wimy backend:
// the command queue, the command effects that only start processes,
// autostart supervision, config-reload bookkeeping and pointer-op
// math. Concrete backends (internal/river on Linux, internal/macos on
// darwin) embed *Core and add the protocol or OS translation.
package backend
```

- [ ] **Step 2: Write the failing tests**

`internal/backend/autostart_test.go`:

```go
package backend

import (
	"testing"
	"time"
)

// waitFor polls cond until it holds or 5s pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAutostartSyncKillsRemovedSpawnsAdded(t *testing.T) {
	var a Autostart
	a.StartAll([]string{"sleep 30"})
	old := a.procs["sleep 30"][0]

	killed, spawned, restarted := a.Sync([]string{"sleep 30"}, []string{"sleep 31"})
	t.Cleanup(func() { a.Sync([]string{"sleep 31"}, nil) })

	if killed != 1 || spawned != 1 || restarted != 0 {
		t.Fatalf("got killed=%d spawned=%d restarted=%d, want 1 1 0", killed, spawned, restarted)
	}
	waitFor(t, "removed process to exit", old.dead.Load)
	if _, ok := a.procs["sleep 30"]; ok {
		t.Errorf("removed entry still tracked")
	}
	if n := len(a.procs["sleep 31"]); n != 1 {
		t.Errorf("added entry: %d processes, want 1", n)
	}
}

func TestAutostartSyncKeepsUnchanged(t *testing.T) {
	var a Autostart
	a.StartAll([]string{"sleep 30"})
	t.Cleanup(func() { a.Sync([]string{"sleep 30"}, nil) })
	before := a.procs["sleep 30"][0]

	killed, spawned, restarted := a.Sync([]string{"sleep 30"}, []string{"sleep 30"})

	if killed+spawned+restarted != 0 {
		t.Fatalf("got killed=%d spawned=%d restarted=%d, want all 0", killed, spawned, restarted)
	}
	if a.procs["sleep 30"][0] != before {
		t.Errorf("unchanged entry was restarted")
	}
}

func TestAutostartSyncRestartsExited(t *testing.T) {
	var a Autostart
	a.StartAll([]string{"true"})
	p := a.procs["true"][0]
	waitFor(t, "true to exit", p.dead.Load)

	_, _, restarted := a.Sync([]string{"true"}, []string{"true"})

	if restarted != 1 {
		t.Fatalf("restarted=%d, want 1", restarted)
	}
}

func TestAutostartKillsWholeProcessGroup(t *testing.T) {
	var a Autostart
	// the shell forks sleep instead of exec'ing it
	a.StartAll([]string{"sleep 30; true"})
	p := a.procs["sleep 30; true"][0]

	a.Sync([]string{"sleep 30; true"}, nil)

	waitFor(t, "shell to exit", p.dead.Load)
}
```

- [ ] **Step 3: Run to see them fail**

Run: `go test ./internal/backend/`
Expected: FAIL to compile, `undefined: Autostart`.

- [ ] **Step 4: Move the implementation**

Create `internal/backend/autostart.go` from `internal/river/autostart.go` with these changes (behavior and log text unchanged):
- `package backend`, no build tag; import only `log`, `os/exec`, `sync/atomic`, `syscall`, `time`, `wimy/internal/config`.
- The per-backend map becomes a type:

```go
// Autostart supervises the processes started from the config's
// autostart list so a reload can reconcile them. The zero value is
// ready to use. Not safe for concurrent use: call it from the
// backend's dispatch thread.
type Autostart struct {
	procs map[string][]*autostartProc
}

// StartAll starts every entry of list.
func (a *Autostart) StartAll(list []string) {
	for _, cmdline := range list {
		a.Start(cmdline)
	}
}
```

- `func (b *Backend) spawnAutostart` → `func (a *Autostart) Start(cmdline string)`, with `b.autostart` → `a.procs`.
- `takeAutostart` → `func (a *Autostart) take(cmdline string) *autostartProc`, `terminateAutostart` → `func terminate(p *autostartProc)` (it used no backend state).
- `syncAutostart` → `func (a *Autostart) Sync(oldList, newList []string) (killed, spawned, restarted int)`, calling `a.take`, `terminate`, `a.Start`.
- Keep `autostartGrace` and `autostartProc` as they are.

Delete `internal/river/autostart.go`.

- [ ] **Step 5: Rewire river**

In `internal/river/river.go`, replace the field `autostart map[string][]*autostartProc` with `autostart backend.Autostart` and import `wimy/internal/backend`.

In `internal/river/effects.go`, `runAutostart` becomes:

```go
// runAutostart executes the configured autostart commands.
func (b *Backend) runAutostart() {
	b.autostart.StartAll(b.cfg.Autostart)
}
```

In `internal/river/reload.go`, replace `b.syncAutostart(old.Autostart, newCfg.Autostart)` with `b.autostart.Sync(old.Autostart, newCfg.Autostart)`.

- [ ] **Step 6: Run tests and cross-builds**

Run: `go test -race ./internal/backend/ && GOOS=linux go build ./... && GOOS=linux go vet ./... && go vet ./... && gofmt -l cmd internal`
Expected: PASS; gofmt prints nothing but possibly `gen.go`.

- [ ] **Step 7: Commit**

```bash
git add internal/backend internal/river
git commit -m "Move autostart supervision into internal/backend

Process-group spawning and reload reconciliation have nothing
river-specific (Setpgid and kill(-pgid) work on darwin too), so the
macOS backend can reuse them. Adds unit tests for the reconcile rules
that e2e-reload.sh covered only on Linux.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Move pointer-op math to `internal/backend`

**Files:**
- Create: `internal/backend/pointer.go`, `internal/backend/pointer_test.go`
- Modify: `internal/river/pointer.go`, `internal/river/objects.go:190` (`Seat.Op` type), `internal/river/river.go` (`applyManage` op handling)

**Interfaces:**
- Produces:
  - `type Edges uint32` with `EdgeTop`, `EdgeBottom`, `EdgeLeft`, `EdgeRight`
  - `type Button int` with `ButtonMove` (left), `ButtonResize` (right)
  - `type PointerOp interface { Apply(dx, dy int32) }`
  - `type MoveOp struct{ Win wm.WindowID; … }`, `type ResizeOp struct{ Win wm.WindowID; Edges Edges; … }`, `type ColumnResizeOp struct{ … }` — all implement `PointerOp`
  - `func ResizeEdges(r wm.Rect, px, py int32) Edges`
  - `func StartPointerOp(st *wm.State, win wm.WindowID, b Button, px, py int32) PointerOp` (nil = no op)
  - `func ClientMoveOp(st *wm.State, win wm.WindowID) PointerOp` (nil = no op)
  - `func ClientResizeOp(st *wm.State, win wm.WindowID, edges Edges, px int32) PointerOp` (nil = no op)

- [ ] **Step 1: Write the failing tests**

`internal/backend/pointer_test.go`:

```go
package backend

import (
	"testing"

	"wimy/internal/wm"
)

// newPointerState returns a state with one 1280x720 output.
func newPointerState() *wm.State {
	s := wm.NewState()
	o := s.AddOutput("out")
	o.Rect = wm.Rect{X: 0, Y: 0, W: 1280, H: 720}
	return s
}

func TestResizeEdgesGrid(t *testing.T) {
	r := wm.Rect{X: 100, Y: 100, W: 300, H: 300}
	cases := []struct {
		x, y int32
		want Edges
	}{
		{110, 110, EdgeLeft | EdgeTop},
		{390, 250, EdgeRight},
		{250, 390, EdgeBottom},
		{250, 250, EdgeRight | EdgeBottom}, // centre: default corner
	}
	for _, c := range cases {
		if got := ResizeEdges(r, c.x, c.y); got != c.want {
			t.Errorf("ResizeEdges(%d,%d) = %b, want %b", c.x, c.y, got, c.want)
		}
	}
}

func TestMoveOpIsCumulative(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, true)
	start := wm.Rect{X: 100, Y: 100, W: 400, H: 300}
	s.SetFloatRect(1, start)

	op := StartPointerOp(s, 1, ButtonMove, 150, 150)
	if op == nil {
		t.Fatal("no op for Mod+left on a floating window")
	}
	op.Apply(10, 20)
	op.Apply(30, 5) // deltas are from the op start, not incremental

	want := wm.Rect{X: 130, Y: 105, W: 400, H: 300}
	if got := s.FloatRectOf(1); got != want {
		t.Errorf("float rect = %+v, want %+v", got, want)
	}
}

func TestResizeOpLeftEdgeClamps(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, true)
	s.SetFloatRect(1, wm.Rect{X: 100, Y: 100, W: 200, H: 200})

	op := ClientResizeOp(s, 1, EdgeLeft, 0)
	op.Apply(190, 0) // would leave 10px wide

	// minimum width 50, right edge (x=300) stays put
	want := wm.Rect{X: 250, Y: 100, W: 50, H: 200}
	if got := s.FloatRectOf(1); got != want {
		t.Errorf("float rect = %+v, want %+v", got, want)
	}
}

func TestResizeOpTopEdgeClamps(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, true)
	s.SetFloatRect(1, wm.Rect{X: 100, Y: 100, W: 200, H: 200})

	op := ClientResizeOp(s, 1, EdgeTop, 0)
	op.Apply(0, 190)

	want := wm.Rect{X: 100, Y: 250, W: 200, H: 50}
	if got := s.FloatRectOf(1); got != want {
		t.Errorf("float rect = %+v, want %+v", got, want)
	}
}

func TestStartPointerOpTiled(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, false)
	s.AddWindow(2, false)
	s.MoveDir(wm.DirRight) // window 2 into its own column: boundary at x=640
	v := s.ActiveViewOf(2)
	if len(v.Columns) != 2 {
		t.Fatalf("setup: %d columns, want 2", len(v.Columns))
	}

	if op := StartPointerOp(s, 2, ButtonMove, 700, 300); op != nil {
		t.Errorf("Mod+left on a tiled window started %T, want nothing", op)
	}

	op := StartPointerOp(s, 2, ButtonResize, 650, 300)
	if _, ok := op.(*ColumnResizeOp); !ok {
		t.Fatalf("Mod+right on a tiled window started %T, want *ColumnResizeOp", op)
	}
	op.Apply(128, 0)
	if !(v.Columns[0].Factor > v.Columns[1].Factor) {
		t.Errorf("dragging the boundary right should widen column 0: %v %v",
			v.Columns[0].Factor, v.Columns[1].Factor)
	}
	op.Apply(0, 0) // back to the start position
	if v.Columns[0].Factor != 1 || v.Columns[1].Factor != 1 {
		t.Errorf("factors after returning = %v %v, want 1 1",
			v.Columns[0].Factor, v.Columns[1].Factor)
	}
}

func TestClientMoveOpOnlyFloats(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, false)
	if op := ClientMoveOp(s, 1); op != nil {
		t.Errorf("client move of a tiled window started %T, want nothing", op)
	}
	s.AddWindow(2, true)
	s.FocusWindow(1)
	if _, ok := ClientMoveOp(s, 2).(*MoveOp); !ok {
		t.Fatal("client move of a floating window started no MoveOp")
	}
	if s.Focused != 2 {
		t.Errorf("focused = %d, want 2 (client move focuses the window)", s.Focused)
	}
}

func TestPointerOpUnknownWindow(t *testing.T) {
	s := newPointerState()
	if op := StartPointerOp(s, 99, ButtonResize, 0, 0); op != nil {
		t.Errorf("got %T for an unknown window", op)
	}
	if op := ClientResizeOp(s, 99, EdgeLeft, 0); op != nil {
		t.Errorf("got %T for an unknown window", op)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/backend/ -run 'Resize|Move|Pointer|Tiled'`
Expected: FAIL to compile, `undefined: ResizeEdges`.

- [ ] **Step 3: Implement `internal/backend/pointer.go`**

```go
package backend

import "wimy/internal/wm"

// Edges is a set of window edges for resize operations.
type Edges uint32

// Window edges. Backends convert to and from their protocol's values
// explicitly.
const (
	EdgeTop Edges = 1 << iota
	EdgeBottom
	EdgeLeft
	EdgeRight
)

// Button identifies the Mod+button pointer bindings.
type Button int

const (
	// ButtonMove (left) moves a floating window.
	ButtonMove Button = iota
	// ButtonResize (right) resizes a floating window, or drags the
	// nearest column boundary of a tiled one.
	ButtonResize
)

// minFloatSize is the smallest floating width/height a resize drag
// produces.
const minFloatSize = 50

// PointerOp is an interactive pointer operation. Apply receives the
// pointer delta accumulated since the op started.
type PointerOp interface {
	Apply(dx, dy int32)
}

// MoveOp moves a floating window.
type MoveOp struct {
	Win   wm.WindowID
	state *wm.State
	start wm.Rect
}

// Apply implements PointerOp.
func (o *MoveOp) Apply(dx, dy int32) {
	o.state.SetFloatRect(o.Win, wm.Rect{
		X: o.start.X + dx, Y: o.start.Y + dy, W: o.start.W, H: o.start.H,
	})
}

// ResizeOp resizes a floating window by the given edges.
type ResizeOp struct {
	Win   wm.WindowID
	Edges Edges
	state *wm.State
	start wm.Rect
}

// Apply implements PointerOp.
func (o *ResizeOp) Apply(dx, dy int32) {
	r := o.start
	if o.Edges&EdgeLeft != 0 {
		r.X = o.start.X + dx
		r.W = o.start.W - dx
	}
	if o.Edges&EdgeRight != 0 {
		r.W = o.start.W + dx
	}
	if o.Edges&EdgeTop != 0 {
		r.Y = o.start.Y + dy
		r.H = o.start.H - dy
	}
	if o.Edges&EdgeBottom != 0 {
		r.H = o.start.H + dy
	}
	// keep the window from inverting past its minimum size
	if r.W < minFloatSize {
		if o.Edges&EdgeLeft != 0 {
			r.X -= minFloatSize - r.W
		}
		r.W = minFloatSize
	}
	if r.H < minFloatSize {
		if o.Edges&EdgeTop != 0 {
			r.Y -= minFloatSize - r.H
		}
		r.H = minFloatSize
	}
	o.state.SetFloatRect(o.Win, r)
}

// ColumnResizeOp drags a tiled column boundary.
type ColumnResizeOp struct {
	state    *wm.State
	view     *wm.View
	boundary int
	area     wm.Rect
	factors  []float64 // factors at op start (deltas are cumulative)
}

// Apply implements PointerOp.
func (o *ColumnResizeOp) Apply(dx, dy int32) {
	for i, f := range o.factors {
		o.view.Columns[i].Factor = f
	}
	o.state.ResizeColumnBoundary(o.view, o.boundary, dx, o.area.W)
}

// ResizeEdges computes the resize edges for a press at (px,py) inside
// rect r: the window is divided into a 3x3 grid; corners resize two
// edges, sides one, and the centre resizes the bottom-right corner.
func ResizeEdges(r wm.Rect, px, py int32) Edges {
	var edges Edges
	thirdW := max(r.W/3, 1)
	thirdH := max(r.H/3, 1)
	switch x := px - r.X; {
	case x < thirdW:
		edges |= EdgeLeft
	case x >= 2*thirdW:
		edges |= EdgeRight
	}
	switch y := py - r.Y; {
	case y < thirdH:
		edges |= EdgeTop
	case y >= 2*thirdH:
		edges |= EdgeBottom
	}
	if edges == 0 {
		edges = EdgeRight | EdgeBottom
	}
	return edges
}

func newMoveOp(st *wm.State, win wm.WindowID) *MoveOp {
	return &MoveOp{Win: win, state: st, start: st.FloatRectOf(win)}
}

func newResizeOp(st *wm.State, win wm.WindowID, edges Edges) *ResizeOp {
	return &ResizeOp{Win: win, Edges: edges, state: st, start: st.FloatRectOf(win)}
}

// newColumnResizeOp drags the column boundary of v nearest to px. It
// returns nil when v has no tiling area or no columns.
func newColumnResizeOp(st *wm.State, v *wm.View, px int32) *ColumnResizeOp {
	area := st.OutputArea(v.Name)
	if area.W == 0 || len(v.Columns) < 1 {
		return nil
	}
	bounds := st.ColumnBoundaries(v, area)
	factors := make([]float64, len(v.Columns))
	for i, c := range v.Columns {
		factors[i] = c.Factor
	}
	return &ColumnResizeOp{
		state:    st,
		view:     v,
		boundary: wm.NearestColumnBoundary(bounds, px),
		area:     area,
		factors:  factors,
	}
}

// StartPointerOp picks the op for a Mod+button press at (px,py) over
// window win: move or resize a floating window, or drag a column
// boundary of a tiled one. It returns nil when the press starts
// nothing.
func StartPointerOp(st *wm.State, win wm.WindowID, b Button, px, py int32) PointerOp {
	if st.Windows[win] == nil {
		return nil
	}
	v := st.ActiveViewOf(win)
	floating := v != nil && v.FloatContains(win)
	switch {
	case b == ButtonMove && floating:
		return newMoveOp(st, win)
	case b == ButtonResize && floating:
		return newResizeOp(st, win, ResizeEdges(st.FloatRectOf(win), px, py))
	case b == ButtonResize && v != nil:
		if op := newColumnResizeOp(st, v, px); op != nil {
			return op
		}
	}
	return nil
}

// ClientMoveOp starts a client-requested move (e.g. a CSD titlebar
// drag). Only floating windows move freely; the window is focused.
// It returns nil when no move starts.
func ClientMoveOp(st *wm.State, win wm.WindowID) PointerOp {
	if st.Windows[win] == nil {
		return nil
	}
	v := st.ActiveViewOf(win)
	if v == nil || !v.FloatContains(win) {
		return nil
	}
	st.FocusWindow(win)
	return newMoveOp(st, win)
}

// ClientResizeOp starts a client-requested resize by edges. The
// window is focused; a floating window resizes, a tiled one drags the
// column boundary nearest to px. It returns nil when no resize
// starts.
func ClientResizeOp(st *wm.State, win wm.WindowID, edges Edges, px int32) PointerOp {
	if st.Windows[win] == nil {
		return nil
	}
	st.FocusWindow(win)
	v := st.ActiveViewOf(win)
	switch {
	case v == nil:
		return nil
	case v.FloatContains(win):
		return newResizeOp(st, win, edges)
	}
	if op := newColumnResizeOp(st, v, px); op != nil {
		return op
	}
	return nil
}
```

Note the `if op := …; op != nil { return op }` shape: returning a nil `*ColumnResizeOp` directly would produce a non-nil `PointerOp` interface.

Behavior note: river's client-resize path previously started a column drag even with a zero-width tiling area (a no-op drag). It now starts nothing in that case, like the Mod+right path already did.

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/backend/`
Expected: PASS.

- [ ] **Step 5: Rewire river onto the shared ops**

In `internal/river/pointer.go`: delete `SeatOp`, `seatOpMove`, `seatOpResize`, `seatOpColumnResize` and `resizeEdges`. Keep `btnLeft`/`btnRight`, `PointerBinding`, `seatFromObject`. Import `wimy/internal/backend`. Replace the three entry points and add the edge conversion:

```go
// edgesFromProto converts river_window_v1.edges to backend.Edges.
func edgesFromProto(e uint32) backend.Edges {
	var out backend.Edges
	if e&proto.RiverWindowV1EdgesTop != 0 {
		out |= backend.EdgeTop
	}
	if e&proto.RiverWindowV1EdgesBottom != 0 {
		out |= backend.EdgeBottom
	}
	if e&proto.RiverWindowV1EdgesLeft != 0 {
		out |= backend.EdgeLeft
	}
	if e&proto.RiverWindowV1EdgesRight != 0 {
		out |= backend.EdgeRight
	}
	return out
}

// pointerPress handles Mod+button presses over a window.
func (b *Backend) pointerPress(s *Seat, button uint32) {
	w := s.Hovered
	if w == nil || s.Op != nil {
		return
	}
	btn := backend.ButtonMove
	if button == btnRight {
		btn = backend.ButtonResize
	}
	op := backend.StartPointerOp(b.state, w.ID, btn, s.PointerX, s.PointerY)
	if op == nil {
		return
	}
	if _, ok := op.(*backend.ResizeOp); ok {
		w.Object.InformResizeStart()
	}
	s.Op = op
	s.Object.OpStartPointer()
}

// clientMoveRequest starts a client-initiated interactive move
// (e.g. a CSD titlebar drag).
func (b *Backend) clientMoveRequest(w *Window, seat proto.RiverSeatV1) {
	s := seatFromObject(b, seat)
	if s == nil || s.Op != nil {
		return
	}
	op := backend.ClientMoveOp(b.state, w.ID)
	if op == nil {
		return
	}
	s.Op = op
	s.Object.OpStartPointer()
}

// clientResizeRequest starts a client-initiated interactive resize.
func (b *Backend) clientResizeRequest(w *Window, seat proto.RiverSeatV1, edges uint32) {
	s := seatFromObject(b, seat)
	if s == nil || s.Op != nil {
		return
	}
	op := backend.ClientResizeOp(b.state, w.ID, edgesFromProto(edges), s.PointerX)
	if op == nil {
		return
	}
	w.Object.InformResizeStart()
	s.Op = op
	s.Object.OpStartPointer()
}

// endOp finishes an interactive op: a floating resize tells the
// client the resize is over (column drags never informed it).
func (b *Backend) endOp(op backend.PointerOp) {
	if r, ok := op.(*backend.ResizeOp); ok {
		if w := b.windowByID(r.Win); w != nil {
			w.Object.InformResizeEnd()
		}
	}
}
```

In `internal/river/objects.go`, the `Seat` field `Op SeatOp` becomes `Op backend.PointerOp` (add the import).

In `internal/river/river.go` `applyManage`, the op block becomes:

```go
		if s.Op != nil {
			switch {
			case s.OpReleased:
				b.endOp(s.Op)
				s.Object.OpEnd()
				s.Op = nil
				s.OpReleased = false
			default:
				s.Op.Apply(s.OpDx, s.OpDy)
			}
		}
```

Run: `GOOS=linux go build ./... && GOOS=linux go vet ./...` and fix any leftover references (`grep -rn "seatOp\|resizeEdges\|SeatOp" internal/river` must print nothing).

- [ ] **Step 6: Full local check**

Run: `go test -race ./... && go vet ./... && GOOS=linux go vet ./... && gofmt -l cmd internal`
Expected: PASS, nothing unformatted.

- [ ] **Step 7: Commit**

```bash
git add internal/backend internal/river
git commit -m "Move pointer move/resize math into internal/backend

Mod-drag and client move/resize requests pick and apply ops purely
against the wm model, so the macOS mouse event tap can drive the same
code. river keeps only the protocol side: informing the client of
resize start/end and op_start_pointer/op_end.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `backend.Core` — queue, effects, reload

**Files:**
- Create: `internal/backend/core.go`, `internal/backend/effects.go`, `internal/backend/reload.go`, `internal/backend/notify_darwin.go`, `internal/backend/notify_other.go`, `internal/backend/core_test.go`, `internal/backend/reload_test.go`
- Modify: `internal/river/river.go`, `internal/river/effects.go`, `internal/river/reload.go`, `internal/river/deco.go`, `internal/river/objects.go` (field renames)

**Interfaces:**
- Consumes: `Autostart` (Task 3).
- Produces:
  - `type Platform interface { Wake(); ApplyConfigChange(ch ConfigChange); Kill(id wm.WindowID); Quit() }`
  - `type Core struct { Cfg *config.Config; State *wm.State; Reg *command.Registry; … }`
  - `func NewCore(cfg *config.Config, configArg string, p Platform) *Core`
  - `func (c *Core) CommandNames() []string`
  - `func (c *Core) Enqueue(cmd string)` — no wake; for the dispatch thread
  - `func (c *Core) QueueCommand(cmd string)` — enqueue + `Wake`; any other goroutine
  - `func (c *Core) DrainQueue()`
  - `func (c *Core) StartAutostart()`
  - `*Core` implements all of `command.Effects` (`Kill`/`Quit` delegate to the Platform)
  - `type ConfigChange struct { Binds, Mod, Border, Titlebar bool }`
  - `func (c *Core) Reload() error`

- [ ] **Step 1: Write the failing tests**

`internal/backend/core_test.go`:

```go
package backend

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"wimy/internal/command"
	"wimy/internal/config"
	"wimy/internal/wm"
)

// fakePlatform records what the Core asks of its backend.
type fakePlatform struct {
	wakes   atomic.Int32
	changes []ConfigChange
	killed  []wm.WindowID
	quit    bool
}

func (f *fakePlatform) Wake()                             { f.wakes.Add(1) }
func (f *fakePlatform) ApplyConfigChange(ch ConfigChange) { f.changes = append(f.changes, ch) }
func (f *fakePlatform) Kill(id wm.WindowID)               { f.killed = append(f.killed, id) }
func (f *fakePlatform) Quit()                             { f.quit = true }

// newTestCore returns a Core over a state with one output.
func newTestCore(t *testing.T, cfg *config.Config) (*Core, *fakePlatform) {
	t.Helper()
	if cfg == nil {
		cfg = config.Default()
	}
	p := &fakePlatform{}
	c := NewCore(cfg, "", p)
	o := c.State.AddOutput("out")
	o.Rect = wm.Rect{W: 1280, H: 720}
	return c, p
}

func queued(c *Core) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.queue)
}

func TestQueueCommandWakesAndDrainRuns(t *testing.T) {
	c, p := newTestCore(t, nil)
	c.QueueCommand("view web")
	if p.wakes.Load() != 1 {
		t.Errorf("wakes = %d, want 1", p.wakes.Load())
	}
	c.DrainQueue()
	if got := c.State.Outputs[0].View; got != "web" {
		t.Errorf("view = %q, want web", got)
	}
	if q := queued(c); len(q) != 0 {
		t.Errorf("queue not drained: %v", q)
	}
}

func TestEnqueueDoesNotWake(t *testing.T) {
	c, p := newTestCore(t, nil)
	c.Enqueue("view web")
	if p.wakes.Load() != 0 {
		t.Errorf("Enqueue woke the backend; key bindings run on the dispatch thread")
	}
	if q := queued(c); !slices.Equal(q, []string{"view web"}) {
		t.Errorf("queue = %v", q)
	}
}

func TestQueueCommandConcurrent(t *testing.T) {
	c, _ := newTestCore(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				c.QueueCommand("view web")
			}
		}()
	}
	for j := 0; j < 50; j++ {
		c.Enqueue("view main")
	}
	wg.Wait()
	if n := len(queued(c)); n != 450 {
		t.Fatalf("queued %d commands, want 450", n)
	}
	c.DrainQueue()
	if n := len(queued(c)); n != 0 {
		t.Errorf("%d commands left after drain", n)
	}
}

func TestDrainQueueSurvivesBadCommand(t *testing.T) {
	c, _ := newTestCore(t, nil)
	c.Enqueue("no-such-command")
	c.Enqueue("view web")
	c.DrainQueue()
	if got := c.State.Outputs[0].View; got != "web" {
		t.Errorf("command after a bad one did not run: view = %q", got)
	}
}

func TestKillAndQuitReachPlatform(t *testing.T) {
	c, p := newTestCore(t, nil)
	c.State.AddWindow(7, false)
	c.Enqueue("kill")
	c.Enqueue("quit")
	c.DrainQueue()
	if !slices.Equal(p.killed, []wm.WindowID{7}) {
		t.Errorf("killed = %v, want [7]", p.killed)
	}
	if !p.quit {
		t.Errorf("quit did not reach the platform")
	}
}

func TestActionsSortedAndUnknown(t *testing.T) {
	cfg := config.Default()
	cfg.Actions = map[string]string{"zeta": "true", "alpha": "true", "mid": "true"}
	c, _ := newTestCore(t, cfg)
	if got := c.Actions(); !slices.Equal(got, []string{"alpha", "mid", "zeta"}) {
		t.Errorf("Actions() = %v", got)
	}
	if err := c.Action("nope"); err == nil {
		t.Errorf("unknown action: no error")
	}
	if err := c.Spawn(nil); err == nil {
		t.Errorf("empty spawn: no error")
	}
}

func TestPromptFollowUp(t *testing.T) {
	cases := []struct {
		kind   command.PromptKind
		out    string
		failed bool
		want   string
	}{
		{command.PromptView, "web\n", false, "view web"},
		{command.PromptMoveTo, " 3 ", false, "moveto 3"},
		{command.PromptAction, "lock", false, "action lock"},
		{command.PromptView, "web", true, ""}, // menu canceled (non-zero exit)
		{command.PromptView, "  \n", false, ""}, // empty answer
	}
	for _, tc := range cases {
		got, ok := promptFollowUp(tc.kind, tc.out, tc.failed)
		if (tc.want != "") != ok || got != tc.want {
			t.Errorf("promptFollowUp(%v, %q, %v) = %q, %v; want %q", tc.kind, tc.out, tc.failed, got, ok, tc.want)
		}
	}
}

func TestPromptQueuesAnswer(t *testing.T) {
	menu := filepath.Join(t.TempDir(), "menu")
	if err := os.WriteFile(menu, []byte("#!/bin/sh\ncat >/dev/null\necho web\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Menu = menu
	c, p := newTestCore(t, cfg)

	if err := c.Prompt(command.PromptView, []string{"1", "web"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "prompt answer to be queued", func() bool { return p.wakes.Load() == 1 })
	if q := queued(c); !slices.Equal(q, []string{"view web"}) {
		t.Errorf("queue = %v, want [view web]", q)
	}
}
```

`internal/backend/reload_test.go`:

```go
package backend

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"wimy/internal/config"
)

// newReloadCore returns a Core whose config path is a temp file
// holding src, with the error notifier captured.
func newReloadCore(t *testing.T, src string) (*Core, *fakePlatform, *[]string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.kdl")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var notes []string
	old := notifyError
	notifyError = func(msg string) { notes = append(notes, msg) }
	t.Cleanup(func() { notifyError = old })

	p := &fakePlatform{}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore(cfg, path, p)
	return c, p, &notes, path
}

func TestReloadBadConfigKeepsOld(t *testing.T) {
	c, p, notes, path := newReloadCore(t, "stack-strip 30\n")
	before := c.Cfg
	if err := os.WriteFile(path, []byte("no-such-setting 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.Reload(); err == nil {
		t.Fatal("Reload of a broken config: no error")
	}
	if c.Cfg != before {
		t.Errorf("config replaced by a broken one")
	}
	if len(p.changes) != 0 {
		t.Errorf("platform asked to apply %v", p.changes)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0], "keeping the old config") {
		t.Errorf("notifications = %q", *notes)
	}
}

func TestReloadAppliesModelSections(t *testing.T) {
	c, p, _, path := newReloadCore(t, "stack-strip 30\n")
	if err := os.WriteFile(path, []byte("stack-strip 40\ntitlebar height=30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.Reload(); err != nil {
		t.Fatal(err)
	}
	if c.State.StackStrip != 40 || c.State.TitlebarHeight != 30 {
		t.Errorf("state strip=%d titlebar=%d, want 40 30", c.State.StackStrip, c.State.TitlebarHeight)
	}
	want := []ConfigChange{{Titlebar: true}}
	if !slices.Equal(p.changes, want) {
		t.Errorf("platform changes = %+v, want %+v", p.changes, want)
	}
}

func TestApplyConfigReportOrder(t *testing.T) {
	c, _, _, _ := newReloadCore(t, "")
	next := *c.Cfg
	next.Binds = nil
	next.Terminal = "foot"
	next.StackStrip = c.Cfg.StackStrip + 1
	next.Autostart = []string{"true"}

	got := c.applyConfig(&next)
	t.Cleanup(func() { c.autostart.Sync(next.Autostart, nil) })

	want := []string{"keybindings", "stack-strip", "terminal", "autostart (0 killed, 1 spawned, 0 restarted)"}
	if !slices.Equal(got, want) {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestApplyConfigNoChanges(t *testing.T) {
	c, p, _, _ := newReloadCore(t, "")
	same := *c.Cfg
	if got := c.applyConfig(&same); len(got) != 0 {
		t.Errorf("report = %q, want nothing", got)
	}
	if !slices.Equal(p.changes, []ConfigChange{{}}) {
		t.Errorf("platform changes = %+v, want one empty change", p.changes)
	}
}
```

Before writing `titlebar height=30`, check the titlebar node syntax in `internal/config/config.go` (search for `case "titlebar"`) and use whatever property name it accepts for the height.

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/backend/`
Expected: FAIL to compile, `undefined: NewCore`.

- [ ] **Step 3: Implement `internal/backend/core.go`**

```go
package backend

import (
	"log"
	"sync"

	"wimy/internal/command"
	"wimy/internal/config"
	"wimy/internal/wm"
)

// Platform is what the Core needs from the concrete backend that
// embeds it.
type Platform interface {
	// Wake asks the backend to run a manage pass soon; the backend
	// calls DrainQueue from that pass. It is called from goroutines
	// other than the backend's dispatch thread.
	Wake()
	// ApplyConfigChange applies the platform side of a config reload.
	// Core.Cfg already points at the new config when it is called.
	ApplyConfigChange(ch ConfigChange)
	// Kill asks the window to close.
	Kill(id wm.WindowID)
	// Quit exits the window manager.
	Quit()
}

// Core is the platform-neutral backend state: the model, the config,
// the command registry and queue, and autostart supervision. It
// implements command.Effects.
type Core struct {
	Cfg   *config.Config
	State *wm.State
	Reg   *command.Registry

	// configArg is the -config flag value as passed at startup;
	// Reload re-runs config.Load with it.
	configArg string
	platform  Platform
	autostart Autostart

	mu    sync.Mutex
	queue []string
}

// NewCore creates the core for a backend. configArg is the config
// file path argument (as for config.Load) used by Reload.
func NewCore(cfg *config.Config, configArg string, p Platform) *Core {
	c := &Core{Cfg: cfg, State: wm.NewState(), configArg: configArg, platform: p}
	c.State.StackStrip = cfg.StackStrip
	c.State.TitlebarHeight = cfg.Titlebar.Height
	c.Reg = command.New(&command.Env{State: c.State, Fx: c})
	return c
}

// CommandNames returns the registered command names.
func (c *Core) CommandNames() []string { return c.Reg.Names() }

// Enqueue appends a command to the pending queue without waking the
// backend. It is for the backend's own dispatch thread (key
// bindings), which must not wait on itself.
func (c *Core) Enqueue(cmd string) {
	c.mu.Lock()
	c.queue = append(c.queue, cmd)
	c.mu.Unlock()
}

// QueueCommand appends a command and asks the backend for a manage
// pass, in which the queue is drained. Safe from any goroutine except
// the backend's dispatch thread.
func (c *Core) QueueCommand(cmd string) {
	c.Enqueue(cmd)
	c.platform.Wake()
}

// DrainQueue executes pending commands (key bindings, RPC, prompts)
// in the order they were queued.
func (c *Core) DrainQueue() {
	c.mu.Lock()
	queue := c.queue
	c.queue = nil
	c.mu.Unlock()
	for _, cmd := range queue {
		if err := c.Reg.Run(cmd); err != nil {
			log.Printf("command %q: %v", cmd, err)
		}
	}
}

// StartAutostart runs the configured autostart commands.
func (c *Core) StartAutostart() {
	c.autostart.StartAll(c.Cfg.Autostart)
}
```

- [ ] **Step 4: Implement `internal/backend/effects.go`**

Move `Spawn`, `SpawnTerminal`, `SpawnMenu`, `Prompt`, `Action`, `Actions` from `internal/river/effects.go` onto `*Core` (`b.cfg` → `c.Cfg`, `b.Spawn` → `c.Spawn`, `b.QueueCommand` → `c.QueueCommand`). Replace the insertion sort in `Actions` with `slices.Sort(out)`. Split the answer handling out of `Prompt` so it can be tested:

```go
// promptFollowUp turns the menu program's output into the command to
// queue. failed is true when the menu exited non-zero (canceled).
func promptFollowUp(kind command.PromptKind, out string, failed bool) (string, bool) {
	answer := strings.TrimSpace(out)
	if failed || answer == "" {
		return "", false
	}
	switch kind {
	case command.PromptView:
		return "view " + answer, true
	case command.PromptMoveTo:
		return "moveto " + answer, true
	case command.PromptAction:
		return "action " + answer, true
	}
	return "", false
}
```

and the goroutine in `Prompt` becomes:

```go
	go func() {
		err := cmd.Wait()
		if follow, ok := promptFollowUp(kind, out.String(), err != nil); ok {
			c.QueueCommand(follow)
		}
	}()
```

Add the two delegating effects:

```go
// Kill implements command.Effects via the platform.
func (c *Core) Kill(id wm.WindowID) { c.platform.Kill(id) }

// Quit implements command.Effects via the platform.
func (c *Core) Quit() { c.platform.Quit() }
```

- [ ] **Step 5: Implement `internal/backend/reload.go` and the notifiers**

`reload.go` moves the platform-neutral half of river's `Reload`/`applyConfig`. The report order must match the old one exactly: keybindings, modifier, border, titlebar, stack-strip, terminal, launcher, menu, focus-follows-mouse, actions, autostart.

```go
package backend

import (
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"

	"wimy/internal/config"
)

// ConfigChange lists the reloaded config sections a platform must
// apply itself; everything else the Core applies.
type ConfigChange struct {
	Binds    bool // key bindings: recreate and re-enable
	Mod      bool // primary modifier: recreate pointer bindings
	Border   bool // border width or colors
	Titlebar bool // titlebar height or colors
}

// notifyError surfaces a reload failure on the desktop; tests replace it.
var notifyError = notifyReloadError

// Reload implements command.Effects: re-read the configuration file
// and apply it. It runs inside the backend's manage pass (commands
// drain there). A config that fails to load leaves the old config
// active; the error is logged and surfaced on the desktop.
func (c *Core) Reload() error {
	newCfg, err := config.Load(c.configArg)
	if err != nil {
		notifyError("wimy config reload failed, keeping the old config:\n" + err.Error())
		return fmt.Errorf("reload: keeping old config: %w", err)
	}
	changes := c.applyConfig(newCfg)
	if len(changes) == 0 {
		log.Printf("config reloaded: no changes")
	} else {
		log.Printf("config reloaded: %s", strings.Join(changes, ", "))
	}
	return nil
}

// applyConfig switches to newCfg, applies every section that changed
// (the platform's through ApplyConfigChange) and returns the names of
// the changed sections. Values read live (terminal, launcher, menu,
// focus-follows-mouse, actions) take effect with the pointer swap.
func (c *Core) applyConfig(newCfg *config.Config) []string {
	old := c.Cfg
	ch := ConfigChange{
		Binds:    !slices.Equal(old.Binds, newCfg.Binds),
		Mod:      old.ModMask != newCfg.ModMask,
		Border:   old.Border != newCfg.Border,
		Titlebar: old.Titlebar != newCfg.Titlebar,
	}
	c.Cfg = newCfg
	c.platform.ApplyConfigChange(ch)

	var changes []string
	if ch.Binds {
		changes = append(changes, "keybindings")
	}
	if ch.Mod {
		changes = append(changes, "modifier (pointer drags)")
	}
	if ch.Border {
		changes = append(changes, "border")
	}
	if ch.Titlebar {
		c.State.TitlebarHeight = newCfg.Titlebar.Height
		changes = append(changes, "titlebar")
	}
	if old.StackStrip != newCfg.StackStrip {
		c.State.StackStrip = newCfg.StackStrip
		changes = append(changes, "stack-strip")
	}
	if old.Terminal != newCfg.Terminal {
		changes = append(changes, "terminal")
	}
	if old.Launcher != newCfg.Launcher {
		changes = append(changes, "launcher")
	}
	if old.Menu != newCfg.Menu {
		changes = append(changes, "menu")
	}
	if old.FocusFollowsMouse != newCfg.FocusFollowsMouse {
		changes = append(changes, "focus-follows-mouse")
	}
	if !maps.Equal(old.Actions, newCfg.Actions) {
		changes = append(changes, "actions")
	}
	// autostart: removed → killed, added → spawned, changed →
	// re-executed, exited-but-configured → restarted
	killed, spawned, restarted := c.autostart.Sync(old.Autostart, newCfg.Autostart)
	if !slices.Equal(old.Autostart, newCfg.Autostart) || killed+spawned+restarted > 0 {
		changes = append(changes, fmt.Sprintf("autostart (%d killed, %d spawned, %d restarted)",
			killed, spawned, restarted))
	}
	return changes
}
```

Note the ordering change versus river: the platform hook runs *before* `State.TitlebarHeight` is updated. River's titlebar handling reads `b.Cfg.Titlebar.Height` (the new config), not the state, so this is safe; check `deco.go` when you rewire to confirm.

`notify_other.go` (the existing Linux behavior, moved from `internal/river/reload.go`):

```go
//go:build !darwin

package backend

import "os/exec"

// notifyReloadError surfaces a reload failure on the desktop when
// zenity or notify-send is installed; detached, best-effort.
func notifyReloadError(msg string) {
	if path, err := exec.LookPath("zenity"); err == nil {
		if detach(exec.Command(path, "--error", "--title=wimy", "--width=420", "--text="+msg)) {
			return
		}
	}
	if path, err := exec.LookPath("notify-send"); err == nil {
		detach(exec.Command(path, "-u", "critical", "wimy: config reload failed", msg))
	}
}
```

`notify_darwin.go`:

```go
//go:build darwin

package backend

import "os/exec"

// notifyReloadError shows a macOS notification. The message is passed
// as an argument, not spliced into the script, so it needs no quoting.
func notifyReloadError(msg string) {
	detach(exec.Command("osascript",
		"-e", "on run argv",
		"-e", `display notification (item 1 of argv) with title "wimy: config reload failed"`,
		"-e", "end run",
		msg))
}
```

Add `detach` to `effects.go` (it was a closure inside river's `notifyReloadError`) and use it in `Spawn` too, replacing Spawn's start+wait lines:

```go
// detach starts cmd without waiting for it; it reaps the process in
// the background. It reports whether the start succeeded.
func detach(cmd *exec.Cmd) bool {
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { _, _ = cmd.Process.Wait() }()
	return true
}
```

(`Spawn` needs the start error for its message, so there keep `if err := cmd.Start(); err != nil { return fmt.Errorf(…) }` followed by the reaping goroutine; use `detach` only in the notifiers.)

- [ ] **Step 6: Run the backend tests**

Run: `go test -race ./internal/backend/`
Expected: PASS.

- [ ] **Step 7: Rewire river onto the Core**

`internal/river/river.go`:
- Remove fields `cfg`, `state`, `reg`, `configArg`, `queue`, `autostart` (and their comments). Keep `mu` (it still guards `done`). Embed the core as the first field after the stubs:

```go
	*backend.Core
```

- `New`:

```go
func New(cfg *config.Config, configArg string, notify func()) *Backend {
	b := &Backend{
		wlOutputNames:  make(map[uint32]string),
		wlOutputScales: make(map[uint32]int32),
		nextID:         1,
		notify:         notify,
	}
	b.Core = backend.NewCore(cfg, configArg, b)
	b.tbr = newTitlebarRenderer(cfg)
	return b
}
```

- Delete `State()` (no callers: `grep -rn '\.State()' --include='*.go' .` shows none), `CommandNames`, `QueueCommand` and `drainQueue`. Add:

```go
// Wake implements backend.Platform: it asks river for a manage
// sequence, in which the command queue is drained. It must not be
// called from the dispatch goroutine.
func (b *Backend) Wake() {
	if b.conn == nil {
		return
	}
	b.conn.DoSync(func() {
		if b.wmg.IsSet() {
			b.wmg.ManageDirty()
			_ = b.conn.Flush()
		}
	})
}
```

- `HandleRiverWindowManagerV1ManageStart`: `b.drainQueue()` → `b.DrainQueue()`.
- `Run`: `b.runAutostart()` → `b.StartAutostart()`.
- `createXkbBindings`: `OnPressed: b.Enqueue,` (replacing the closure that locked `b.mu` and appended).

`internal/river/effects.go`: delete everything except `Kill` (and its imports). Keep the file for `Kill`.

`internal/river/reload.go`: keep `newTitlebarRenderer`. Delete `Reload` and `notifyReloadError`. Replace `applyConfig` with the platform hook, which holds only the protocol parts of the old function:

```go
// ApplyConfigChange implements backend.Platform: it applies the parts
// of a config reload that live in protocol objects. b.Cfg already
// holds the new config. It runs inside a manage sequence, so binding
// re-creation and border/deco invalidation apply within it.
func (b *Backend) ApplyConfigChange(ch backend.ConfigChange) {
	// key bindings: destroy and recreate the protocol objects, then
	// enable them in this manage sequence (applyManage honors
	// bindsNeedEnable).
	if ch.Binds {
		for _, xb := range b.bindings {
			xb.Object.Destroy()
		}
		b.bindings = nil
		for _, s := range b.seats {
			b.createXkbBindings(s)
		}
		b.bindsNeedEnable = true
	}
	if ch.Mod {
		for _, pb := range b.pointerBindings {
			pb.Object.Destroy()
		}
		b.pointerBindings = nil
		for _, s := range b.seats {
			b.createPointerBindings(s)
		}
		b.bindsNeedEnable = true
	}
	// borders: force re-application on the next render
	if ch.Border {
		for _, w := range b.windows {
			w.BorderSet = false
		}
	}
	// the titlebar renderer embeds the border colors and width
	if ch.Border || ch.Titlebar {
		b.tbr = newTitlebarRenderer(b.Cfg)
	}
	if ch.Titlebar {
		for _, w := range b.windows {
			if b.Cfg.Titlebar.Height <= 0 {
				// titlebars off: tear decorations down (also
				// resets the cached render state)
				w.destroyDeco()
			} else {
				// force re-render with the new colors/height
				w.DecoWidth = -1
			}
		}
	}
}
```

Rename the remaining field uses across the package:

```bash
perl -pi -e 's/\bb\.cfg\b/b.Cfg/g; s/\bb\.state\b/b.State/g' internal/river/*.go
grep -rn '\.cfg\b\|\.state\b\|\.reg\b' internal/river
```

The grep must print nothing; fix any hit by hand (e.g. a `w.Backend.cfg` in `objects.go` or `deco.go` becomes `w.Backend.Cfg`).

- [ ] **Step 8: Verify**

Run: `go test -race ./... && go vet ./... && GOOS=linux go build ./... && GOOS=linux go vet ./... && GOOS=linux CGO_ENABLED=0 go build ./... && gofmt -l cmd internal`
Expected: all pass; gofmt prints nothing but possibly `gen.go`.

Confirm `*river.Backend` still satisfies `rpc.Backend`, `backend.Platform` and `command.Effects`: `GOOS=linux go build ./cmd/wimy` compiling proves the first; add these to `internal/river/river.go` to pin the other two:

```go
var (
	_ backend.Platform = (*Backend)(nil)
	_ command.Effects  = (*Backend)(nil)
)
```

- [ ] **Step 9: Commit and run CI**

```bash
git add internal/backend internal/river
git commit -m "Extract backend.Core: command queue, spawn effects, reload

The queue, the process-spawning command effects and the reload
bookkeeping are platform-neutral; river now embeds backend.Core and
implements backend.Platform (Wake via manage_dirty, the protocol half
of a reload, kill, quit). The reload report keeps its section names
and order, which e2e-reload.sh checks. Reload errors notify through
osascript on darwin.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
git push
gh run watch --exit-status "$(gh run list --branch macos-phase0 --limit 1 --json databaseId -q '.[0].databaseId')"
```

Expected: both CI jobs green, all seven e2e suites pass. If an e2e check fails, it is a behavior change from this task: read the failing check in the suite, compare the old river code (`git show HEAD~1:internal/river/<file>`) with the new path, and fix the code, not the suite.

---

### Task 6: Socket path and config defaults for darwin

**Files:**
- Modify: `internal/rpc/rpc.go` (`SocketPath`)
- Create: `internal/rpc/rpc_test.go`
- Create: `internal/config/defaults_darwin.go`, `internal/config/defaults_other.go`
- Modify: `internal/config/config.go` (`Default`, `parseModName`, `parseBind`, doc comments), `internal/config/config_test.go`

**Interfaces:**
- Produces: `rpc.SocketPath()` behaves as before on Linux; on darwin with no `WAYLAND_DISPLAY` it returns `$TMPDIR/wimy.sock`. Config accepts `Option`/`Opt` (= Mod1) and `Cmd`/`Command` (= Mod4) as modifier names on every OS.

- [ ] **Step 1: Write the failing socket test**

`internal/rpc/rpc_test.go`:

```go
package rpc

import "testing"

func TestSocketPath(t *testing.T) {
	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want string
	}{
		{"override wins", "darwin", map[string]string{"WIMY_SOCKET": "/x.sock", "WAYLAND_DISPLAY": "w"}, "/x.sock"},
		{"linux session", "linux", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1", "WAYLAND_DISPLAY": "wayland-1"}, "/run/user/1/wimy-wayland-1.sock"},
		{"linux tty falls back to wayland-0", "linux", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1"}, "/run/user/1/wimy-wayland-0.sock"},
		{"linux no runtime dir", "linux", map[string]string{}, "/tmp/wimy-wayland-0.sock"},
		{"darwin", "darwin", map[string]string{}, "/tmp/wimy.sock"},
		{"darwin running a wayland compositor", "darwin", map[string]string{"XDG_RUNTIME_DIR": "/r", "WAYLAND_DISPLAY": "w"}, "/r/wimy-w.sock"},
	}
	for _, c := range cases {
		if got := socketPath(c.goos, env(c.env), "/tmp"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
```

Run: `go test ./internal/rpc/`
Expected: FAIL, `undefined: socketPath`.

- [ ] **Step 2: Implement**

In `internal/rpc/rpc.go`, import `runtime` and replace `SocketPath`:

```go
// SocketPath returns the socket path for the current session:
// $WIMY_SOCKET if set, else $XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.sock
// (falling back to the temp dir and wayland-0). On macOS, which has no
// Wayland display, it is $TMPDIR/wimy.sock; launchd gives every agent
// of a user the same $TMPDIR, so wimyctl and the daemon agree.
func SocketPath() string {
	return socketPath(runtime.GOOS, os.Getenv, os.TempDir())
}

func socketPath(goos string, getenv func(string) string, tmp string) string {
	if p := getenv("WIMY_SOCKET"); p != "" {
		return p
	}
	disp := getenv("WAYLAND_DISPLAY")
	if disp == "" && goos == "darwin" {
		return filepath.Join(tmp, "wimy.sock")
	}
	if disp == "" {
		disp = "wayland-0"
	}
	disp = strings.ReplaceAll(disp, "/", "_")
	dir := getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = tmp
	}
	return filepath.Join(dir, "wimy-"+disp+".sock")
}
```

Run: `go test ./internal/rpc/` → PASS.

- [ ] **Step 3: Write the failing config tests**

Append to `internal/config/config_test.go` (add `"runtime"` to its imports):

```go
func TestPlatformDefaultMod(t *testing.T) {
	c := Default()
	want, wantMask := "Mod4", Mod4
	if runtime.GOOS == "darwin" {
		want, wantMask = "Mod1", Mod1 // Option
	}
	if c.Mod != want || c.ModMask != wantMask {
		t.Errorf("default mod = %q/%d, want %q/%d", c.Mod, c.ModMask, want, wantMask)
	}
}

func TestMacModifierNames(t *testing.T) {
	for name, mask := range map[string]uint32{"Option": Mod1, "opt": Mod1, "Cmd": Mod4, "command": Mod4} {
		got, err := parseModName(name)
		if err != nil || got != mask {
			t.Errorf("parseModName(%q) = %d, %v; want %d", name, got, err, mask)
		}
	}
	c := Default()
	b, err := c.parseBind("Cmd-Option-h", "focus left")
	if err != nil {
		t.Fatal(err)
	}
	if b.Mods != Mod4|Mod1 || b.Keysym != 'h' {
		t.Errorf("Cmd-Option-h: mods=%d keysym=%x", b.Mods, b.Keysym)
	}
}
```

In the existing default-bindings test (around line 138), the `Mod-Shift-h` check hard-codes Mod4, which is wrong on darwin. Change it to:

```go
	// Mod-Shift-h must use the physical keysym h with Mod|Shift
	for _, b := range c.Binds {
		if b.Combo == "Mod-Shift-h" {
			if b.Keysym != 'h' || b.Mods != c.ModMask|ModShift {
				t.Errorf("Mod-Shift-h: keysym=%x mods=%x", b.Keysym, b.Mods)
			}
		}
	}
```

Run: `go test ./internal/config/`
Expected: FAIL — `TestPlatformDefaultMod` on darwin (still Mod4) and `TestMacModifierNames` (unknown modifier "Option").

- [ ] **Step 4: Implement per-OS defaults**

`internal/config/defaults_other.go`:

```go
//go:build !darwin

package config

// platformDefaults are the Linux/river defaults.
var platformDefaults = osDefaults{
	mod:      "Mod4",
	modMask:  Mod4,
	terminal: "alacritty",
	// dmenu-style: a bar anchored to the top screen edge
	launcher: "fuzzel --anchor top --width 120 --lines 10 --border-radius 0",
	menu:     "fuzzel --dmenu --anchor top --width 120 --lines 10 --border-radius 0",
}
```

`internal/config/defaults_darwin.go`:

```go
//go:build darwin

package config

// platformDefaults are the macOS defaults. Mod is Option: Cmd would
// collide with every application's shortcuts. The programs are
// provisional; Phase 1 of plans/macos-port.md verifies them on a real
// Mac. menu is choose-gui (brew install choose-gui).
var platformDefaults = osDefaults{
	mod:      "Mod1",
	modMask:  Mod1,
	terminal: "open -na Terminal",
	launcher: "open -a Spotlight",
	menu:     "choose",
}
```

In `internal/config/config.go`, add the type next to `Config`:

```go
// osDefaults are the defaults that differ between operating systems
// (defaults_darwin.go, defaults_other.go).
type osDefaults struct {
	mod      string
	modMask  uint32
	terminal string
	launcher string
	menu     string
}
```

and in `Default()` replace the five hard-coded fields:

```go
	c := &Config{
		Mod:               platformDefaults.mod,
		ModMask:           platformDefaults.modMask,
		Terminal:          platformDefaults.terminal,
		FocusFollowsMouse: true,
		Launcher:          platformDefaults.launcher,
		Menu:              platformDefaults.menu,
		StackStrip:        28,
```

Update the doc comment on `Default()` to: "the built-in configuration: wmii's key binding set with Mod4 as the modifier on Linux (wmii used Mod1; set `mod "Mod1"` for the classic feel) and Option on macOS." Update the `Mod` field comment to "(default Mod4; Option on macOS)".

Add the macOS names to `parseModName`:

```go
	case "mod1", "alt", "option", "opt":
		return Mod1, nil
	case "mod3":
		return Mod3, nil
	case "mod4", "super", "logo", "cmd", "command":
		return Mod4, nil
```

and its error message: `"unknown modifier %q (want Mod1, Mod3, Mod4, Mod5, Alt/Option or Super/Cmd)"`. In `parseBind`:

```go
		case "alt", "option", "opt":
			b.Mods |= Mod1
		case "super", "logo", "cmd", "command":
			b.Mods |= Mod4
```

Update the `parseBind` doc: "Shift, Ctrl, Alt/Option and Super/Cmd are also recognized."

- [ ] **Step 5: Verify**

Run: `go test -race ./... && go vet ./... && GOOS=linux go vet ./... && gofmt -l cmd internal`
Expected: PASS on darwin. The Linux CI job runs the same tests with `defaults_other.go`.

- [ ] **Step 6: Commit and run CI**

```bash
git add internal/rpc internal/config
git commit -m "Add darwin socket path and config defaults

On macOS there is no WAYLAND_DISPLAY, so wimyctl and the daemon meet
at \$TMPDIR/wimy.sock (launchd gives a user's agents the same TMPDIR).
Mod defaults to Option on darwin since Cmd collides with app
shortcuts; Option/Cmd are accepted as modifier names everywhere.
Linux defaults are unchanged.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
git push
gh run watch --exit-status "$(gh run list --branch macos-phase0 --limit 1 --json databaseId -q '.[0].databaseId')"
```

Expected: both jobs green.

---

### Task 7: Docs

**Files:**
- Modify: `AGENTS.md`, `README.md`, `plans/macos-port.md`

- [ ] **Step 1: AGENTS.md**

- "What this is": "written in pure Go (no cgo)" → "written in Go: pure Go on Linux; the macOS backend (in progress, `plans/macos-port.md`) uses cgo behind `//go:build darwin`".
- Repo layout: add `internal/backend  platform-neutral backend core: command queue, spawn/prompt effects, autostart, reload bookkeeping, pointer-op math (shared by river and macOS)`, and mark `internal/river`, `cmd/keyinject` (and add `cmd/ptrinject`) as Linux-only.
- Build/test: the suite list gains `./e2e-reload.sh` and `./e2e-all.sh` (runs all seven); note `./ci.sh` and that GitHub Actions runs it on Linux plus `go test` on macOS; on a Mac use `GOOS=linux go build ./... && GOOS=linux go vet ./...` to compile river.
- Invariant 3: "Async commands go through the queue + `ManageDirty()`" → "Async commands go through `backend.Core.QueueCommand` (which calls the backend's `Wake`, i.e. `ManageDirty()` on river); key bindings use `Enqueue`, which doesn't wake. The queue drains inside `manage_start`."
- Config conventions: "`Backend.applyConfig` (internal/river/reload.go) is the single apply path" → "`Core.applyConfig` (internal/backend/reload.go) is the single apply path; protocol-side sections go through the backend's `ApplyConfigChange` hook — extend both when adding config sections."

- [ ] **Step 2: README.md**

Find the "no cgo"/"pure Go" wording (`grep -n -i "cgo\|pure go" README.md`) and change it to "pure Go on Linux". If README describes the config's `mod` setting or defaults, add that macOS defaults to Option and accepts `Option`/`Cmd`.

- [ ] **Step 3: plans/macos-port.md**

Under "Phase 0", add a short "Status" paragraph: done on branch `macos-phase0`, with these deviations: `internal/proto` is not build-tagged (compiles on darwin; tagging would mean editing generated `gen.go`); the darwin keycode table moved to Phase 1 (only `internal/macos` consumes it); e2e suites run in GitHub Actions because the development Mac can't run river; darwin `terminal`/`launcher`/`menu` defaults are provisional (`open -na Terminal`, `open -a Spotlight`, `choose`) and must be verified in Phase 1. Also fix "all six e2e suites" → "all seven".

- [ ] **Step 4: Commit and push**

```bash
git add AGENTS.md README.md plans/macos-port.md
git commit -m "Document internal/backend, CI and the darwin build

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
git push
gh run watch --exit-status "$(gh run list --branch macos-phase0 --limit 1 --json databaseId -q '.[0].databaseId')"
```

Expected: both jobs green. Phase 0 is done when this run is green; merging the branch is the user's call.
