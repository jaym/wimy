// wimy is a wmii-style window manager running on top of the river
// compositor. Start it with: river -c wimy
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"wimy/internal/config"
	"wimy/internal/river"
	"wimy/internal/rpc"
)

func main() {
	configPath := flag.String("config", "", "path to config.kdl (default ~/.config/wimy/config.kdl)")
	flag.Parse()

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
		backend.Quit()
	}()

	if err := backend.Run(ctx); err != nil {
		log.Fatalf("wimy: %v", err)
	}
	fmt.Fprintln(os.Stderr, "wimy: exiting")
}
