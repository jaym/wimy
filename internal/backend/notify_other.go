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
