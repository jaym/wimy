package main

import (
	"encoding/json"
	"fmt"
	"time"

	"wimy/internal/rpc"
)

// version asks the running wimy for its version info.
func version(socketPath string) (rpc.VersionInfo, error) {
	var v rpc.VersionInfo
	res, conn, err := rpc.Call(socketPath, "version", nil)
	if conn != nil {
		conn.Close()
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(res, &v)
	return v, err
}

// waitRestarted polls fetch (the running wimy's start time) until an
// instance started at a different time than prev answers, or deadline
// passes. While wimy is replacing itself the socket is briefly gone;
// fetch errors then are expected.
func waitRestarted(prev time.Time, fetch func() (time.Time, error), deadline time.Duration, sleep func(time.Duration)) error {
	const step = 200 * time.Millisecond
	for waited := time.Duration(0); waited < deadline; waited += step {
		if t, err := fetch(); err == nil && !t.Equal(prev) {
			return nil
		}
		sleep(step)
	}
	return fmt.Errorf("wimy did not come back within %v (check its log)", deadline)
}

// restart replaces the running wimy with the executable on disk and
// waits for it to answer.
func restart(socketPath string) error {
	old, err := version(socketPath)
	if err != nil {
		return fmt.Errorf("wimy isn't running: %w", err)
	}
	if _, conn, err := rpc.Call(socketPath, "run", map[string]string{"command": "restart"}); err != nil {
		return err
	} else if conn != nil {
		conn.Close()
	}
	fetch := func() (time.Time, error) {
		v, err := version(socketPath)
		return v.Started, err
	}
	if err := waitRestarted(old.Started, fetch, 20*time.Second, time.Sleep); err != nil {
		return err
	}
	now, err := version(socketPath)
	if err != nil {
		return err
	}
	fmt.Printf("wimy restarted: %s -> %s\n", old.Version, now.Version)
	return nil
}
