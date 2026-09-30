package backend

import (
	"log"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"wimy/internal/config"
)

// autostartGrace is how long a terminated autostart process gets to
// exit after SIGTERM before SIGKILL follows.
const autostartGrace = 2 * time.Second

// Autostart supervises the processes started from the config's
// autostart list so a reload can reconcile them. The zero value is
// ready to use. Not safe for concurrent use: call it from the
// backend's dispatch thread.
type Autostart struct {
	procs map[string][]*autostartProc
}

// autostartProc tracks one spawned autostart command. The process
// runs in its own process group so that shell wrappers which fork
// (instead of exec) don't leak children when wimy kills the group.
type autostartProc struct {
	cmdline string
	pid     int
	pgid    int
	dead    atomic.Bool
	done    chan struct{} // closed by the reaping goroutine
}

// StartAll starts every entry of list.
func (a *Autostart) StartAll(list []string) {
	for _, cmdline := range list {
		a.Start(cmdline)
	}
}

// Start starts one autostart entry and tracks it.
func (a *Autostart) Start(cmdline string) {
	cmd := exec.Command("sh", "-c", cmdline)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		log.Printf("autostart %q: %v", cmdline, err)
		return
	}
	p := &autostartProc{
		cmdline: cmdline,
		pid:     cmd.Process.Pid,
		pgid:    cmd.Process.Pid, // Setpgid: pgid == pid
		done:    make(chan struct{}),
	}
	go func() {
		_ = cmd.Wait()
		p.dead.Store(true)
		close(p.done)
	}()
	a.track(p)
	log.Printf("autostart: started %q (pid %d)", cmdline, p.pid)
}

func (a *Autostart) track(p *autostartProc) {
	if a.procs == nil {
		a.procs = make(map[string][]*autostartProc)
	}
	a.procs[p.cmdline] = append(a.procs[p.cmdline], p)
}

// Adopt takes over an autostart process started by the wimy this one
// replaced (a restart execs in place, so it is still our child): it is
// tracked, reaped and reconciled like one started here.
func (a *Autostart) Adopt(cmdline string, pid int) {
	p := &autostartProc{cmdline: cmdline, pid: pid, pgid: pid, done: make(chan struct{})}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	go func() {
		_, _ = proc.Wait()
		p.dead.Store(true)
		close(p.done)
	}()
	a.track(p)
}

// Pids returns the live autostart processes by command line, for the
// restart handoff.
func (a *Autostart) Pids() map[string][]int {
	out := make(map[string][]int)
	for cmdline, procs := range a.procs {
		for _, p := range procs {
			if !p.dead.Load() {
				out[cmdline] = append(out[cmdline], p.pid)
			}
		}
	}
	return out
}

// take removes and returns one tracked process for cmdline,
// preferring a live one (so its SIGTERM is meaningful).
func (a *Autostart) take(cmdline string) *autostartProc {
	procs := a.procs[cmdline]
	if len(procs) == 0 {
		return nil
	}
	idx := 0
	for i, p := range procs {
		if !p.dead.Load() {
			idx = i
			break
		}
	}
	p := procs[idx]
	a.procs[cmdline] = append(procs[:idx], procs[idx+1:]...)
	if len(a.procs[cmdline]) == 0 {
		delete(a.procs, cmdline)
	}
	return p
}

// terminate SIGTERMs the process group, escalating to SIGKILL after
// the grace period. A process that already exited is reported, not an
// error.
func terminate(p *autostartProc) {
	if p.dead.Load() {
		log.Printf("config reload: autostart %q already exited; nothing to kill", p.cmdline)
		return
	}
	log.Printf("config reload: terminating autostart %q (pid %d)", p.cmdline, p.pid)
	if err := syscall.Kill(-p.pgid, syscall.SIGTERM); err != nil {
		log.Printf("autostart %q: SIGTERM: %v", p.cmdline, err)
	}
	go func() {
		select {
		case <-p.done:
		case <-time.After(autostartGrace):
			if !p.dead.Load() {
				log.Printf("autostart %q ignored SIGTERM; sending SIGKILL", p.cmdline)
				_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
			}
		}
	}()
}

// Sync reconciles tracked processes with the new config list: removed
// entries are terminated, added entries spawned, changed entries
// terminated and re-executed, and unchanged entries keep running.
// Processes that exited on their own but whose entry is still
// configured are restarted — users typically edit and reload
// precisely to bring a dead bar back.
func (a *Autostart) Sync(oldList, newList []string) (killed, spawned, restarted int) {
	kill, spawn := config.DiffExecs(oldList, newList)
	for _, cmdline := range kill {
		if p := a.take(cmdline); p != nil {
			terminate(p)
			killed++
		}
	}
	for _, cmdline := range spawn {
		a.Start(cmdline)
		spawned++
	}
	// After the kill/spawn passes the tracked set matches newList
	// exactly; restart anything tracked that died on its own.
	for cmdline, procs := range a.procs {
		alive := procs[:0]
		dead := 0
		for _, p := range procs {
			if p.dead.Load() {
				dead++
			} else {
				alive = append(alive, p)
			}
		}
		a.procs[cmdline] = alive
		for i := 0; i < dead; i++ {
			log.Printf("config reload: autostart %q exited; restarting", cmdline)
			a.Start(cmdline)
			restarted++
		}
	}
	return killed, spawned, restarted
}
