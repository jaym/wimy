// keyinject injects key events into a Wayland compositor via the
// wlr virtual keyboard protocol. It is an end-to-end test tool:
//
//	keyinject "logo+enter" "logo+shift+c" "logo+2"
//
// Modifier names: logo/super, shift, ctrl, alt. Keys are single
// characters or: enter, space, esc, tab, backspace.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"hazelnut.eclair.cafe/wlcl"
	"wimy/internal/proto"
)

// evdev key codes.
var keyByRune = map[rune]uint32{
	'1': 2, '2': 3, '3': 4, '4': 5, '5': 6, '6': 7, '7': 8, '8': 9, '9': 10, '0': 11,
	'q': 16, 'w': 17, 'e': 18, 'r': 19, 't': 20, 'y': 21, 'u': 22, 'i': 23, 'o': 24, 'p': 25,
	'a': 30, 's': 31, 'd': 32, 'f': 33, 'g': 34, 'h': 35, 'j': 36, 'k': 37, 'l': 38,
	'z': 44, 'x': 45, 'c': 46, 'v': 47, 'b': 48, 'n': 49, 'm': 50,
}

var keyByName = map[string]uint32{
	"enter": 28, "return": 28, "space": 57, "esc": 1, "escape": 1,
	"tab": 15, "backspace": 14,
	"logo": 125, "super": 125, "shift": 42, "ctrl": 29, "alt": 56,
}

func keycode(tok string) (uint32, error) {
	if c, ok := keyByName[strings.ToLower(tok)]; ok {
		return c, nil
	}
	if r := []rune(tok); len(r) == 1 {
		if c, ok := keyByRune[r[0]]; ok {
			return c, nil
		}
	}
	return 0, fmt.Errorf("unknown key %q", tok)
}

type registry struct {
	proto.WlRegistryStub
	seat    proto.WlSeat
	manager proto.ZwpVirtualKeyboardManagerV1
	reg     proto.WlRegistry
}

func (r *registry) HandleWlRegistryGlobal(ctx context.Context, name uint32, iface string, version uint32) {
	switch iface {
	case proto.WlSeatName:
		r.seat = proto.As[proto.WlSeat](r.reg.Bind(name, iface, 1))
	case proto.ZwpVirtualKeyboardManagerV1Name:
		r.manager = proto.As[proto.ZwpVirtualKeyboardManagerV1](r.reg.Bind(name, iface, 1))
	}
}

// keymapText compiles a standard us keymap with xkbcli.
func keymapText() ([]byte, error) {
	out, err := exec.Command("xkbcli", "compile-keymap", "--layout", "us").Output()
	if err != nil {
		return nil, fmt.Errorf("xkbcli compile-keymap: %w", err)
	}
	return out, nil
}

func run(ctx context.Context, combos []string) error {
	conn, err := wlcl.Connect(ctx, "")
	if err != nil {
		return err
	}
	defer conn.Close()

	display := proto.CreateDisplay(conn)
	r := &registry{}
	r.reg = display.GetRegistry()
	r.reg.SetUserData(r)
	if err := wlcl.Roundtrip(ctx, display); err != nil {
		return err
	}
	if !r.seat.IsSet() || !r.manager.IsSet() {
		return fmt.Errorf("missing wl_seat or virtual keyboard manager")
	}

	keymap, err := keymapText()
	if err != nil {
		return err
	}
	fd, err := unix.MemfdCreate("keymap", 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if _, err := unix.Write(fd, keymap); err != nil {
		return err
	}

	vkb := r.manager.CreateVirtualKeyboard(r.seat)
	defer vkb.Destroy()
	vkb.Keymap(1 /* xkb_v1 */, fd, uint32(len(keymap)))
	if err := conn.Flush(); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	t := uint32(0)
	key := func(code uint32, pressed bool) {
		t += 10
		state := uint32(0)
		if pressed {
			state = 1
		}
		vkb.Key(t, code, state)
	}

	for _, combo := range combos {
		var codes []uint32
		for _, tok := range strings.Split(combo, "+") {
			c, err := keycode(tok)
			if err != nil {
				return err
			}
			codes = append(codes, c)
		}
		// press all, release in reverse — like a human chord
		for _, c := range codes {
			key(c, true)
		}
		for i := len(codes) - 1; i >= 0; i-- {
			key(codes[i], false)
		}
		if err := conn.Flush(); err != nil {
			return err
		}
		time.Sleep(300 * time.Millisecond)
	}
	return conn.Flush()
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s <combo...> e.g. \"logo+enter\"", os.Args[0])
	}
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
