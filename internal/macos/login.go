package macos

// loginStep is what applying start-at-login does to the login item.
type loginStep int

const (
	loginRegister         loginStep = iota // register (or keep) the login agent
	loginUnregister                        // unregister it now
	loginUnregisterAtExit                  // unregister it when wimy quits
)

// loginAction decides how to apply start-at-login. Unregistering a
// running launchd agent kills it (SMAppService.h), so a wimy that runs
// as the login agent defers that until it quits instead of killing the
// window manager on a config reload.
func loginAction(want, asAgent bool) loginStep {
	switch {
	case want:
		return loginRegister
	case asAgent:
		return loginUnregisterAtExit
	}
	return loginUnregister
}
