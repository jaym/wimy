package macos

import (
	"sort"
	"strconv"

	"wimy/internal/wm"
)

// Start-at-login states (SMAppService status as the bridge reports it).
const (
	loginNoBundle = -1 // not running from Wimy.app: nothing to register
	loginOff      = 0
	loginEnabled  = 1
	loginApproval = 2 // registered, waiting for approval in Login Items
)

// cmdOpenAccessibility is a menu action handled by the backend itself
// (it isn't a window manager command): open the Accessibility pane.
const cmdOpenAccessibility = "!open-accessibility"

// menuItem is one entry of the menu bar item's menu. Cmd is a command
// for the registry (or cmdOpenAccessibility); empty for info lines.
type menuItem struct {
	Label   string
	Cmd     string
	Enabled bool
	Checked bool
	Sep     bool
}

// menuStatus is what the menu shows besides the model.
type menuStatus struct {
	Trusted    bool // Accessibility permission granted
	Login      int  // loginOff, loginEnabled, ...
	ConfigPath string
	// SecureApp names the app holding secure input while it blinds the
	// key tap (bindings without Ctrl or Cmd); "" otherwise.
	SecureApp string
}

// menuFor builds the menu bar item: its title (the focused output's
// view) and menu. Every action goes through the command registry, so
// it does what the matching key binding or `wimyctl run` does. Before
// the Accessibility permission is granted it only says so.
func menuFor(s *wm.State, st menuStatus) (string, []menuItem) {
	if !st.Trusted {
		return "wimy ⚠", []menuItem{
			{Label: "wimy needs the Accessibility permission"},
			{Label: "Open Privacy & Security…", Cmd: cmdOpenAccessibility, Enabled: true},
			{Sep: true},
			{Label: "Quit wimy", Cmd: "quit", Enabled: true},
		}
	}
	title := "wimy"
	shown := ""
	if s.FocusOutput >= 0 && s.FocusOutput < len(s.Outputs) {
		shown = s.Outputs[s.FocusOutput].View
		title = shown
	}
	var items []menuItem
	if st.SecureApp != "" {
		title = "⚠ " + title
		items = append(items,
			menuItem{Label: "Option keys blocked by " + st.SecureApp},
			menuItem{Label: "(secure input: a password prompt, or Secure Keyboard Entry)"},
			menuItem{Sep: true},
		)
	}
	names := make([]string, 0, len(s.Views))
	for _, v := range s.Views {
		names = append(names, v.Name)
	}
	sort.Slice(names, func(i, j int) bool { return viewLess(names[i], names[j]) })
	for _, n := range names {
		items = append(items, menuItem{Label: n, Cmd: "view " + n, Enabled: true, Checked: n == shown})
	}
	items = append(items,
		menuItem{Sep: true},
		menuItem{Label: "Reload config", Cmd: "reload", Enabled: true},
	)
	if st.ConfigPath != "" {
		items = append(items, menuItem{Label: "Open config", Cmd: "spawn open -t " + st.ConfigPath, Enabled: true})
	}
	items = append(items,
		menuItem{Label: "Restart wimy", Cmd: "restart", Enabled: true},
		menuItem{Sep: true},
		menuItem{Label: "Accessibility: allowed"},
		menuItem{Label: loginLabel(st.Login)},
		menuItem{Sep: true},
		menuItem{Label: "Quit wimy", Cmd: "quit", Enabled: true},
	)
	return title, items
}

func loginLabel(status int) string {
	switch status {
	case loginEnabled:
		return "Start at login: on"
	case loginApproval:
		return "Start at login: needs approval in Login Items"
	case loginNoBundle:
		return "Start at login: only from Wimy.app"
	}
	return "Start at login: off"
}

// viewLess orders views like the bars do: numbered ones first,
// numerically, then the rest by name.
func viewLess(a, b string) bool {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	switch {
	case ea == nil && eb == nil:
		return na < nb
	case ea == nil:
		return true
	case eb == nil:
		return false
	}
	return a < b
}

// runsBeforeStart reports whether a command runs while wimy waits for
// the Accessibility permission: quit, and restart (a new start asks
// for the permission again). Everything else waits until wimy is up.
func runsBeforeStart(cmd string) bool { return cmd == "quit" || cmd == "restart" }
