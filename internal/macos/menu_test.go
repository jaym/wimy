package macos

import (
	"strings"
	"testing"

	"wimy/internal/wm"
)

func menuState() *wm.State {
	s := wm.NewState()
	a := s.AddOutput("Built-in")
	a.Rect = wm.Rect{W: 1512, H: 982}
	s.AddWindow(1, false)        // view 1 on Built-in
	s.AddWindow(2, false, "web") // view web, not shown
	b := s.AddOutput("BenQ")     // shows a new view
	b.Rect = wm.Rect{X: 1512, W: 1920, H: 1280}
	return s
}

func labels(items []menuItem) []string {
	var out []string
	for _, it := range items {
		switch {
		case it.Sep:
			out = append(out, "---")
		case it.Checked:
			out = append(out, "✓ "+it.Label)
		default:
			out = append(out, it.Label)
		}
	}
	return out
}

func TestMenuForViews(t *testing.T) {
	title, items := menuFor(menuState(), menuStatus{Trusted: true, Login: loginEnabled, ConfigPath: "/Users/me/.config/wimy/config.kdl"})
	if title != "1" {
		t.Errorf("title = %q, want the focused output's view", title)
	}
	got := strings.Join(labels(items), " | ")
	for _, want := range []string{"✓ 1", "web", "Reload config", "Open config", "Restart wimy", "Start at login: on", "Quit wimy"} {
		if !strings.Contains(got, want) {
			t.Errorf("menu %q lacks %q", got, want)
		}
	}
	cmds := map[string]string{}
	for _, it := range items {
		cmds[it.Label] = it.Cmd
	}
	if cmds["web"] != "view web" || cmds["Reload config"] != "reload" || cmds["Restart wimy"] != "restart" || cmds["Quit wimy"] != "quit" {
		t.Errorf("commands = %v", cmds)
	}
	if cmds["Open config"] != "spawn open -t /Users/me/.config/wimy/config.kdl" {
		t.Errorf("open config = %q", cmds["Open config"])
	}
}

func TestMenuForLoginStates(t *testing.T) {
	for status, want := range map[int]string{
		loginEnabled:  "Start at login: on",
		loginOff:      "Start at login: off",
		loginApproval: "Start at login: needs approval in Login Items",
		loginNoBundle: "Start at login: only from Wimy.app",
	} {
		_, items := menuFor(menuState(), menuStatus{Trusted: true, Login: status})
		if got := strings.Join(labels(items), " | "); !strings.Contains(got, want) {
			t.Errorf("login %d: menu %q lacks %q", status, got, want)
		}
	}
}

func TestMenuForUntrusted(t *testing.T) {
	title, items := menuFor(menuState(), menuStatus{Trusted: false})
	if !strings.Contains(title, "⚠") {
		t.Errorf("title %q doesn't warn", title)
	}
	got := strings.Join(labels(items), " | ")
	if strings.Contains(got, "Reload config") || !strings.Contains(got, "Open Privacy & Security") || !strings.Contains(got, "Quit wimy") {
		t.Errorf("waiting-for-permission menu = %q", got)
	}
	for _, it := range items {
		if it.Label == "Open Privacy & Security…" && it.Cmd != cmdOpenAccessibility {
			t.Errorf("settings item runs %q", it.Cmd)
		}
	}
}

func TestRunsBeforeStart(t *testing.T) {
	// while wimy waits for the Accessibility permission only quit and
	// restart make sense (restart re-asks for the permission); the rest
	// waits for startup
	for cmd, want := range map[string]bool{"quit": true, "restart": true, "view 2": false, "reload": false, "focus left": false} {
		if got := runsBeforeStart(cmd); got != want {
			t.Errorf("runsBeforeStart(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestMenuForSecureInput(t *testing.T) {
	title, items := menuFor(menuState(), menuStatus{Trusted: true, Login: loginEnabled, SecureApp: "Terminal"})
	if title != "⚠ 1" {
		t.Errorf("title = %q, want the view with a warning", title)
	}
	got := strings.Join(labels(items), " | ")
	if !strings.Contains(got, "Option keys blocked by Terminal") {
		t.Errorf("menu %q doesn't name the app holding secure input", got)
	}
	if items[0].Enabled || items[0].Cmd != "" {
		t.Errorf("the notice should come first and be informational: %+v", items[0])
	}
	if title, _ := menuFor(menuState(), menuStatus{Trusted: true}); title != "1" {
		t.Errorf("no secure input: title %q", title)
	}
}
