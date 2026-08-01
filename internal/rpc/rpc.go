// Package rpc implements wimy's JSON-RPC 2.0 control interface over a
// unix socket. Messages are newline-delimited JSON objects.
//
// Methods:
//
//	run       {"command": "focus left"}  execute a command
//	state                              full state snapshot
//	subscribe                          immediate state + state notifications
//	quit                               exit the window manager
package rpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"wimy/internal/wm"
)

// Backend is the interface the server needs from the window manager.
type Backend interface {
	// QueueCommand executes a command string asynchronously.
	QueueCommand(cmd string)
	// CommandNames returns the valid command names.
	CommandNames() []string
	// Snapshot runs fn with exclusive read access to the model.
	Snapshot(fn func(*wm.State))
}

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification.
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	ErrParse      = -32700
	ErrMethod     = -32601
	ErrParams     = -32602
	ErrInternal   = -32603
	ErrUnknownCmd = 1
)

// SocketPath returns the socket path for the current session:
// $WIMY_SOCKET if set, else $XDG_RUNTIME_DIR/wimy-$WAYLAND_DISPLAY.sock
// (falling back to /tmp).
func SocketPath() string {
	if p := os.Getenv("WIMY_SOCKET"); p != "" {
		return p
	}
	disp := os.Getenv("WAYLAND_DISPLAY")
	if disp == "" {
		disp = "wayland-0"
	}
	disp = strings.ReplaceAll(disp, "/", "_")
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "wimy-"+disp+".sock")
}

// Server is the JSON-RPC server.
type Server struct {
	b    Backend
	ln   net.Listener
	path string

	mu     sync.Mutex
	subs   map[*client]bool
	closed bool

	changed chan struct{}
}

// client is a connected peer with its own writer goroutine.
type client struct {
	conn net.Conn
	send chan []byte
	sub  bool
}

// Listen creates the socket and starts serving.
func Listen(b Backend) (*Server, error) {
	path := SocketPath()
	if err := os.RemoveAll(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", path, err)
	}
	s := &Server{
		b:       b,
		ln:      ln,
		path:    path,
		subs:    make(map[*client]bool),
		changed: make(chan struct{}, 1),
	}
	go s.acceptLoop()
	go s.broadcastLoop()
	return s, nil
}

// Path returns the socket path.
func (s *Server) Path() string { return s.path }

// Notify signals that the model changed; subscribers receive a state
// notification. It never blocks.
func (s *Server) Notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Close stops the server and removes the socket file.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	for c := range s.subs {
		c.conn.Close()
	}
	s.mu.Unlock()
	err := s.ln.Close()
	os.Remove(s.path)
	return err
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed
		}
		c := &client{conn: conn, send: make(chan []byte, 32)}
		go s.writeLoop(c)
		go s.readLoop(c)
	}
}

func (s *Server) writeLoop(c *client) {
	for msg := range c.send {
		msg = append(msg, '\n')
		if _, err := c.conn.Write(msg); err != nil {
			c.conn.Close()
			return
		}
	}
}

func (s *Server) drop(c *client) {
	s.mu.Lock()
	delete(s.subs, c)
	s.mu.Unlock()
	c.conn.Close()
}

func (s *Server) readLoop(c *client) {
	defer s.drop(c)
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		resp := s.handle(c, line)
		if resp != nil {
			data, err := json.Marshal(resp)
			if err != nil {
				log.Printf("rpc: marshal response: %v", err)
				continue
			}
			if !s.trySend(c, data) {
				return
			}
		}
	}
}

func (s *Server) trySend(c *client, data []byte) bool {
	select {
	case c.send <- data:
		return true
	default:
		// client too slow; drop the frame rather than blocking
		log.Printf("rpc: dropping frame for slow client")
		return true
	}
}

// handle executes one request and returns the response (nil for
// notifications, which we don't support from clients).
func (s *Server) handle(c *client, line []byte) *Response {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return &Response{JSONRPC: "2.0", Error: &Error{ErrParse, "parse error: " + err.Error()}}
	}
	resp := &Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "run":
		var p struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Command == "" {
			resp.Error = &Error{ErrParams, `want {"command": "..."}`}
			break
		}
		name := strings.Fields(p.Command)[0]
		if !s.validCommand(name) {
			resp.Error = &Error{ErrUnknownCmd, "unknown command " + name}
			break
		}
		s.b.QueueCommand(p.Command)
		resp.Result = "ok"

	case "state":
		resp.Result = s.stateSnapshot()

	case "subscribe":
		s.mu.Lock()
		if !s.subs[c] {
			s.subs[c] = true
			c.sub = true
		}
		s.mu.Unlock()
		resp.Result = "ok"
		s.trySend(c, s.stateNotification())

	case "quit":
		s.b.QueueCommand("quit")
		resp.Result = "ok"

	default:
		resp.Error = &Error{ErrMethod, "unknown method " + req.Method}
	}
	return resp
}

func (s *Server) validCommand(name string) bool {
	for _, n := range s.b.CommandNames() {
		if n == name {
			return true
		}
	}
	return false
}

func (s *Server) stateSnapshot() State {
	var st State
	s.b.Snapshot(func(state *wm.State) { st = buildState(state) })
	return st
}

func (s *Server) stateNotification() []byte {
	data, err := json.Marshal(Notification{
		JSONRPC: "2.0",
		Method:  "state",
		Params:  s.stateSnapshot(),
	})
	if err != nil {
		return nil
	}
	return data
}

func (s *Server) broadcastLoop() {
	for range s.changed {
		data := s.stateNotification()
		if data == nil {
			continue
		}
		s.mu.Lock()
		for c := range s.subs {
			s.trySend(c, data)
		}
		s.mu.Unlock()
	}
}

// --- client side ---

// Call dials the socket and executes one request, returning the
// result. For subscribe, the returned connection remains open and the
// caller reads notifications from it.
func Call(socketPath, method string, params any) (json.RawMessage, net.Conn, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w (is wimy running?)", socketPath, err)
	}
	var rawParams json.RawMessage
	if params != nil {
		rawParams, err = json.Marshal(params)
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
	}
	req, err := json.Marshal(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  method,
		Params:  rawParams,
	})
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		conn.Close()
		return nil, nil, err
	}
	// read until we get the response (id=1); notifications may
	// interleave on subscribe
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		var resp Response
		if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
			continue
		}
		if string(resp.ID) != "1" {
			continue // notification
		}
		if resp.Error != nil {
			conn.Close()
			return nil, nil, errors.New(resp.Error.Message)
		}
		result, _ := json.Marshal(resp.Result)
		return result, conn, nil
	}
	conn.Close()
	return nil, nil, errors.New("connection closed without a response")
}
