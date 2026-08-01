// wimy is a wmii-style window manager running on top of the river
// compositor. Start it with: river -c wimy
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"wimy/internal/config"
	"wimy/internal/river"
	"wimy/internal/rpc"
)

func main() {
	configPath := flag.String("config", "", "path to config.kdl (default ~/.config/wimy/config.kdl)")
	logPath := flag.String("log", "", "log file (default $XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.log)")
	flag.Parse()

	// log to a file as well as stderr: on a TTY river's stderr is
	// invisible once the session starts
	if *logPath == "" {
		*logPath = strings.TrimSuffix(rpc.SocketPath(), ".sock") + ".log"
	}
	if f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
		defer f.Close()
	}
	log.Printf("wimy: starting (log: %s)", *logPath)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("wimy: %v", err)
	}

	var server *rpc.Server
	backend := river.New(cfg, func() {
		if server != nil {
			server.Notify()
		}
	})

	server, err = rpc.Listen(backend)
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
