package macos

import "testing"

func TestLoginPath(t *testing.T) {
	got := loginPath("/usr/bin:/bin:/usr/sbin:/sbin", "/Users/me", "me")
	want := "/Users/me/.nix-profile/bin:/etc/profiles/per-user/me/bin:/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin:" +
		"/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	if got != want {
		t.Errorf("loginPath =\n%s\nwant\n%s", got, want)
	}
	// started from a shell: its own folders keep their order and come
	// first, nothing twice, the system folders last
	got = loginPath("/Users/me/bin:/usr/bin:/opt/homebrew/bin", "/Users/me", "me")
	want = "/Users/me/bin:/opt/homebrew/bin:/Users/me/.nix-profile/bin:/etc/profiles/per-user/me/bin:" +
		"/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	if got != want {
		t.Errorf("loginPath from a shell =\n%s\nwant\n%s", got, want)
	}
}
