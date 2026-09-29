package config

import (
	"strings"
	"testing"
)

func installed(paths ...string) func(string) bool {
	return func(p string) bool {
		for _, q := range paths {
			if p == q {
				return true
			}
		}
		return false
	}
}

func TestPickMacTerminalPreference(t *testing.T) {
	all := installed("/Applications/Ghostty.app", "/Applications/Alacritty.app", "/Applications/kitty.app")
	if got := pickMacTerminal(all, "/Users/me"); !strings.Contains(got, `application "Ghostty"`) {
		t.Errorf("with everything installed got %q, want Ghostty", got)
	}
	if got := pickMacTerminal(installed("/Applications/Alacritty.app", "/Applications/kitty.app"), "/Users/me"); got != "open -na Alacritty" {
		t.Errorf("without Ghostty got %q, want Alacritty", got)
	}
	got := pickMacTerminal(installed("/Applications/kitty.app"), "/Users/me")
	if got != "'/Applications/kitty.app/Contents/MacOS/kitty' --single-instance --directory ~" {
		t.Errorf("kitty only: got %q", got)
	}
	if got := pickMacTerminal(installed(), "/Users/me"); got != "open -a Terminal ~" {
		t.Errorf("nothing installed: got %q, want Terminal.app", got)
	}
}

func TestPickMacTerminalUserApplications(t *testing.T) {
	got := pickMacTerminal(installed("/Users/me/Applications/kitty.app"), "/Users/me")
	if !strings.HasPrefix(got, "'/Users/me/Applications/kitty.app/Contents/MacOS/kitty'") {
		t.Errorf("kitty in ~/Applications: got %q", got)
	}
}

func TestPickMacTerminalGhosttyScriptReusesInstance(t *testing.T) {
	got := pickMacTerminal(installed("/Applications/Ghostty.app"), "/Users/me")
	for _, want := range []string{`is running`, `to new window`, `to activate`} {
		if !strings.Contains(got, want) {
			t.Errorf("Ghostty command %q lacks %q", got, want)
		}
	}
}
