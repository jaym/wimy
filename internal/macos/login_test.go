package macos

import "testing"

func TestLoginAction(t *testing.T) {
	cases := []struct {
		name          string
		want, asAgent bool
		act           loginStep
	}{
		{"turn on", true, false, loginRegister},
		{"turn on as agent", true, true, loginRegister},
		{"turn off, started by hand", false, false, loginUnregister},
		// unregistering the agent kills it: when wimy *is* the agent,
		// wait until it quits
		{"turn off while running as the agent", false, true, loginUnregisterAtExit},
	}
	for _, c := range cases {
		if got := loginAction(c.want, c.asAgent); got != c.act {
			t.Errorf("%s: loginAction = %v, want %v", c.name, got, c.act)
		}
	}
}
