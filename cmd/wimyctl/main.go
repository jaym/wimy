// wimyctl is the command-line client for wimy's JSON-RPC control
// socket.
//
// Usage:
//
//	wimyctl [-socket path] run <command...>   execute a command
//	wimyctl [-socket path] state              print the state as JSON
//	wimyctl [-socket path] subscribe          stream state notifications
//	wimyctl [-socket path] quit               exit wimy
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"wimy/internal/rpc"
)

func usage() {
	fmt.Fprintf(os.Stderr, `usage: wimyctl [-socket path] <command>

commands:
  run <command...>   execute a wimy command (e.g. run focus left)
  state              print the full state as JSON
  subscribe          stream state notifications as JSON lines
  quit               exit the window manager
`)
	os.Exit(2)
}

func main() {
	socketPath := flag.String("socket", rpc.SocketPath(), "control socket path")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
	}

	switch args[0] {
	case "run":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "wimyctl: run needs a command")
			os.Exit(2)
		}
		cmd := strings.Join(args[1:], " ")
		result, conn, err := rpc.Call(*socketPath, "run", map[string]string{"command": cmd})
		if conn != nil {
			conn.Close()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "wimyctl: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(result))

	case "state":
		result, conn, err := rpc.Call(*socketPath, "state", nil)
		if conn != nil {
			conn.Close()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "wimyctl: %v\n", err)
			os.Exit(1)
		}
		var pretty any
		if json.Unmarshal(result, &pretty) == nil {
			out, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(out))
		} else {
			fmt.Println(string(result))
		}

	case "subscribe":
		_, conn, err := rpc.Call(*socketPath, "subscribe", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wimyctl: %v\n", err)
			os.Exit(1)
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			fmt.Println(sc.Text())
		}

	case "quit":
		_, conn, err := rpc.Call(*socketPath, "quit", nil)
		if conn != nil {
			conn.Close()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "wimyctl: %v\n", err)
			os.Exit(1)
		}

	default:
		usage()
	}
}
