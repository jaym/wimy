package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.kdl")
	bad := filepath.Join(dir, "bad.kdl")
	os.WriteFile(good, []byte("bar-gap 37\n"), 0o644)
	os.WriteFile(bad, []byte("no-such-setting 1\n"), 0o644)
	if err := check(good); err != nil {
		t.Errorf("check(good) = %v", err)
	}
	if err := check(bad); err == nil {
		t.Errorf("check(bad) passed: a restart would replace a working wimy with one that can't start")
	}
}
