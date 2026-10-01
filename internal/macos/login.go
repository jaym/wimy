package macos

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Start at login is a plain per-user launchd agent,
// ~/Library/LaunchAgents/<loginAgentLabel>.plist, pointing at the
// running Wimy.app's executable. SMAppService login items (what wimy
// used before) carry launch constraints that a self-signed build fails
// at login ("Launch Constraint Violation", OS_REASON_CODESIGNING), so
// wimy never started after a reboot. The label differs from the old
// SMAppService one (io.github.jaym.wimy): cleaning that registration up
// unloads its label, which must never be the running wimy.
const loginAgentLabel = "io.github.jaym.wimy.login"

// Start-at-login states, as the menu shows them.
const (
	loginNoBundle = -1 // not running from Wimy.app: nothing to register
	loginOff      = 0
	loginEnabled  = 1
)

// loginPlist is the launchd agent that starts exe at login, and again
// after a crash, but not after `wimyctl quit` (a successful exit).
func loginPlist(exe string) []byte {
	var path bytes.Buffer
	_ = xml.EscapeText(&path, []byte(exe))
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Written by wimy (start-at-login true in config.kdl); wimy removes
     it when start-at-login is false. -->
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>Program</key>
	<string>%s</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`, loginAgentLabel, path.String()))
}

// setLoginAgent writes (on) or removes the login agent in dir (the
// user's LaunchAgents) and returns the resulting state. It takes effect
// at the next login: a running wimy is left alone either way.
func setLoginAgent(dir, exe string, on bool) (int, error) {
	path := filepath.Join(dir, loginAgentLabel+".plist")
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return loginAgentStatus(dir), err
		}
		return loginOff, nil
	}
	want := loginPlist(exe)
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, want) {
		return loginEnabled, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return loginOff, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, want, 0o644); err != nil {
		return loginOff, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return loginOff, err
	}
	return loginEnabled, nil
}

// loginAgentStatus reports whether the login agent is installed in dir.
func loginAgentStatus(dir string) int {
	if _, err := os.Stat(filepath.Join(dir, loginAgentLabel+".plist")); err == nil {
		return loginEnabled
	}
	return loginOff
}
