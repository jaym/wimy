// wimy is a wmii-style window manager running on top of the river
// compositor. Start it with: river -c wimy
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"wimy/internal/config"
	"wimy/internal/rpc"
)

// wmBackend is what main needs from a platform backend.
type wmBackend interface {
	rpc.Backend
	// Run runs the event loop until Shutdown or a fatal error.
	Run(ctx context.Context) error
	// Shutdown stops the event loop.
	Shutdown()
}

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

func main() {
	configPath := flag.String("config", "", "path to config.kdl (default ~/.config/wimy/config.kdl)")
	logPath := flag.String("log", "", "log file (default $XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.log; ~/Library/Logs/wimy.log on macOS)")
	checkOnly := flag.Bool("check", false, "load the config and exit (restart runs this with the new binary first)")
	flag.Parse()

	if *checkOnly {
		if err := check(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "wimy: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("wimy: ok")
		return
	}

	// log to a file as well as stderr: on a TTY river's stderr is
	// invisible once the session starts
	if *logPath == "" {
		home, _ := os.UserHomeDir()
		*logPath = defaultLogPath(runtime.GOOS, home, rpc.SocketPath())
	}
	if f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
		defer f.Close()
	}
	log.Printf("wimy %s: starting (log: %s)", version, *logPath)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("wimy: %v", err)
	}

	var server *rpc.Server
	backend, err := newBackend(cfg, *configPath, func() {
		if server != nil {
			server.Notify()
		}
	})
	if err != nil {
		log.Fatalf("wimy: %v", err)
	}

	server, err = rpc.Listen(backend, rpc.Info{Version: version, Started: time.Now()})
	if errors.Is(err, rpc.ErrAlreadyRunning) {
		// e.g. the login agent and a manual start: exit successfully,
		// so launchd doesn't restart this one
		log.Printf("wimy: %v; exiting", err)
		return
	}
	if err != nil {
		log.Fatalf("wimy: rpc: %v", err)
	}
	defer server.Close()
	log.Printf("wimy: control socket at %s", server.Path())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		backend.Shutdown()
	}()

	if err := backend.Run(ctx); err != nil {
		log.Fatalf("wimy: %v", err)
	}
	fmt.Fprintln(os.Stderr, "wimy: exiting")
}

// defaultLogPath is where wimy logs without -log: on macOS the usual
// ~/Library/Logs (Console.app shows it), elsewhere next to the socket.
func defaultLogPath(goos, home, socket string) string {
	if goos == "darwin" && home != "" {
		return filepath.Join(home, "Library", "Logs", "wimy.log")
	}
	return strings.TrimSuffix(socket, ".sock") + ".log"
}
