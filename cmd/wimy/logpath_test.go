package main

import "testing"

func TestDefaultLogPath(t *testing.T) {
	if got := defaultLogPath("darwin", "/Users/me", "/tmp/wimy-501/wimy.sock"); got != "/Users/me/Library/Logs/wimy.log" {
		t.Errorf("darwin: %q", got)
	}
	if got := defaultLogPath("linux", "/home/me", "/run/user/1/wimy-wayland-1.sock"); got != "/run/user/1/wimy-wayland-1.log" {
		t.Errorf("linux: %q", got)
	}
}
