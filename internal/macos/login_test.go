package macos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginPlist(t *testing.T) {
	p := string(loginPlist("/Users/a & b/Applications/Wimy.app/Contents/MacOS/wimy"))
	for _, want := range []string{
		"<key>Label</key>\n\t<string>io.github.jaym.wimy.login</string>",
		// XML-escaped: the path is the user's
		"<key>Program</key>\n\t<string>/Users/a &amp; b/Applications/Wimy.app/Contents/MacOS/wimy</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
		// back after a crash, not after `wimyctl quit`
		"<key>SuccessfulExit</key>\n\t\t<false/>",
		// only in a GUI login session
		"<key>LimitLoadToSessionType</key>\n\t<string>Aqua</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q:\n%s", want, p)
		}
	}
}

func TestSetLoginAgentOnWritesThePlist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "LaunchAgents") // not there yet
	exe := "/Applications/Wimy.app/Contents/MacOS/wimy"
	got, err := setLoginAgent(dir, exe, true)
	if err != nil || got != loginEnabled {
		t.Fatalf("setLoginAgent(on) = %d, %v; want %d", got, err, loginEnabled)
	}
	data, err := os.ReadFile(filepath.Join(dir, loginAgentLabel+".plist"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(loginPlist(exe)) {
		t.Errorf("plist = %s", data)
	}
	if s := loginAgentStatus(dir); s != loginEnabled {
		t.Errorf("status = %d, want %d", s, loginEnabled)
	}
}

func TestSetLoginAgentFollowsAMovedApp(t *testing.T) {
	dir := t.TempDir()
	if _, err := setLoginAgent(dir, "/old/Wimy.app/Contents/MacOS/wimy", true); err != nil {
		t.Fatal(err)
	}
	exe := "/new/Wimy.app/Contents/MacOS/wimy"
	if _, err := setLoginAgent(dir, exe, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, loginAgentLabel+".plist"))
	if !strings.Contains(string(data), exe) {
		t.Errorf("plist still points elsewhere: %s", data)
	}
}

func TestSetLoginAgentOffRemovesIt(t *testing.T) {
	dir := t.TempDir()
	if _, err := setLoginAgent(dir, "/x/wimy", true); err != nil {
		t.Fatal(err)
	}
	got, err := setLoginAgent(dir, "/x/wimy", false)
	if err != nil || got != loginOff {
		t.Fatalf("setLoginAgent(off) = %d, %v; want %d", got, err, loginOff)
	}
	if _, err := os.Stat(filepath.Join(dir, loginAgentLabel+".plist")); !os.IsNotExist(err) {
		t.Errorf("plist still there: %v", err)
	}
	// already off: nothing to do, no error
	if got, err := setLoginAgent(dir, "/x/wimy", false); err != nil || got != loginOff {
		t.Errorf("setLoginAgent(off) again = %d, %v", got, err)
	}
	if s := loginAgentStatus(dir); s != loginOff {
		t.Errorf("status = %d, want %d", s, loginOff)
	}
}
