//go:build linux

package river

import (
	"log"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"wimy/internal/config"
)

// autostartGrace is how long a terminated autostart process gets to
// exit after SIGTERM before SIGKILL follows.
const autostartGrace = 2 * time.Second

// autostartProc tracks one spawned autostart command. The process
// runs in its own process group so that shell wrappers which fork
// (instead of exec) don't leak children when wimy kills the group.
type autostartProc struct {
	cmdline string
	cmd     *exec.Cmd
	pgid    int
	dead    atomic.Bool
	done    chan struct{} // closed by the reaping goroutine
}

// spawnAutostart starts one autostart entry and tracks it.
func (b *Backend) spawnAutostart(cmdline string) {
	cmd := exec.Command("sh", "-c", cmdline)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		log.Printf("autostart %q: %v", cmdline, err)
		return
	}
	p := &autostartProc{
		cmdline: cmdline,
		cmd:     cmd,
		pgid:    cmd.Process.Pid, // Setpgid: pgid == pid
		done:    make(chan struct{}),
	}
	go func() {
		_ = cmd.Wait()
		p.dead.Store(true)
		close(p.done)
	}()
	if b.autostart == nil {
		b.autostart = make(map[string][]*autostartProc)
	}
	b.autostart[cmdline] = append(b.autostart[cmdline], p)
	log.Printf("autostart: started %q (pid %d)", cmdline, cmd.Process.Pid)
}

// takeAutostart removes and returns one tracked process for cmdline,
// preferring a live one (so its SIGTERM is meaningful).
func (b *Backend) takeAutostart(cmdline string) *autostartProc {
	procs := b.autostart[cmdline]
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
	b.autostart[cmdline] = append(procs[:idx], procs[idx+1:]...)
	if len(b.autostart[cmdline]) == 0 {
		delete(b.autostart, cmdline)
	}
	return p
}

// terminateAutostart SIGTERMs the process group, escalating to
// SIGKILL after the grace period. A process that already exited is
// reported, not an error.
func (b *Backend) terminateAutostart(p *autostartProc) {
	if p.dead.Load() {
		log.Printf("config reload: autostart %q already exited; nothing to kill", p.cmdline)
		return
	}
	log.Printf("config reload: terminating autostart %q (pid %d)", p.cmdline, p.cmd.Process.Pid)
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

// syncAutostart reconciles tracked processes with the new config
// list: removed entries are terminated, added entries spawned,
// changed entries terminated and re-executed, and unchanged entries
// keep running. Processes that exited on their own but whose entry is
// still configured are restarted — users typically edit and reload
// precisely to bring a dead bar back.
func (b *Backend) syncAutostart(oldList, newList []string) (killed, spawned, restarted int) {
	kill, spawn := config.DiffExecs(oldList, newList)
	for _, cmdline := range kill {
		if p := b.takeAutostart(cmdline); p != nil {
			b.terminateAutostart(p)
			killed++
		}
	}
	for _, cmdline := range spawn {
		b.spawnAutostart(cmdline)
		spawned++
	}
	// After the kill/spawn passes the tracked set matches newList
	// exactly; restart anything tracked that died on its own.
	for cmdline, procs := range b.autostart {
		alive := procs[:0]
		dead := 0
		for _, p := range procs {
			if p.dead.Load() {
				dead++
			} else {
				alive = append(alive, p)
			}
		}
		b.autostart[cmdline] = alive
		for i := 0; i < dead; i++ {
			log.Printf("config reload: autostart %q exited; restarting", cmdline)
			b.spawnAutostart(cmdline)
			restarted++
		}
	}
	return killed, spawned, restarted
}
