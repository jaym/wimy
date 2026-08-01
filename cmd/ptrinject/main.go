// ptrinject injects pointer drags into a Wayland compositor via the
// wlr virtual pointer protocol (with a virtual keyboard to hold a
// modifier). It is an end-to-end test tool:
//
//	ptrinject -from 700,360 -to 900,360 -button right -mod
//
// It moves to -from, presses the modifier (if -mod) and button, drags
// to -to in steps, then releases.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"hazelnut.eclair.cafe/wlcl"
	"wimy/internal/proto"
)

const (
	btnLeft  = 0x110
	btnRight = 0x111
	keyLogo  = 125
)

type registry struct {
	proto.WlRegistryStub
	seat   proto.WlSeat
	ptrMgr proto.ZwlrVirtualPointerManagerV1
	kbdMgr proto.ZwpVirtualKeyboardManagerV1
	reg    proto.WlRegistry
}

func (r *registry) HandleWlRegistryGlobal(ctx context.Context, name uint32, iface string, version uint32) {
	switch iface {
	case proto.WlSeatName:
		r.seat = proto.As[proto.WlSeat](r.reg.Bind(name, iface, 1))
	case proto.ZwlrVirtualPointerManagerV1Name:
		r.ptrMgr = proto.As[proto.ZwlrVirtualPointerManagerV1](r.reg.Bind(name, iface, 1))
	case proto.ZwpVirtualKeyboardManagerV1Name:
		r.kbdMgr = proto.As[proto.ZwpVirtualKeyboardManagerV1](r.reg.Bind(name, iface, 1))
	}
}

func parsePoint(s string) (int32, int32, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("want x,y: %q", s)
	}
	x, err1 := strconv.Atoi(parts[0])
	y, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("want x,y: %q", s)
	}
	return int32(x), int32(y), nil
}

func keymapFD() (int, int, error) {
	out, err := exec.Command("xkbcli", "compile-keymap", "--layout", "us").Output()
	if err != nil {
		return -1, 0, err
	}
	fd, err := unix.MemfdCreate("keymap", 0)
	if err != nil {
		return -1, 0, err
	}
	if _, err := unix.Write(fd, out); err != nil {
		return -1, 0, err
	}
	return fd, len(out), nil
}

func main() {
	from := flag.String("from", "", "start point x,y")
	to := flag.String("to", "", "end point x,y")
	button := flag.String("button", "left", "left or right")
	mod := flag.Bool("mod", false, "hold Mod4 (logo) during the drag")
	steps := flag.Int("steps", 8, "drag motion steps")
	extent := flag.String("extent", "1280,720", "layout coordinate space w,h (must match the compositor's layout size)")
	flag.Parse()

	x1, y1, err := parsePoint(*from)
	if err != nil {
		log.Fatal(err)
	}
	x2, y2, err := parsePoint(*to)
	if err != nil {
		log.Fatal(err)
	}
	ex, ey, err := parsePoint(*extent)
	if err != nil {
		log.Fatal(err)
	}
	btn := uint32(btnLeft)
	if *button == "right" {
		btn = btnRight
	}

	ctx := context.Background()
	conn, err := wlcl.Connect(ctx, "")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	display := proto.CreateDisplay(conn)
	r := &registry{}
	r.reg = display.GetRegistry()
	r.reg.SetUserData(r)
	if err := wlcl.Roundtrip(ctx, display); err != nil {
		log.Fatal(err)
	}
	if !r.seat.IsSet() || !r.ptrMgr.IsSet() || !r.kbdMgr.IsSet() {
		log.Fatal("missing seat / virtual pointer / virtual keyboard globals")
	}

	vkbd := r.kbdMgr.CreateVirtualKeyboard(r.seat)
	fd, size, err := keymapFD()
	if err != nil {
		log.Fatal(err)
	}
	defer unix.Close(fd)
	vkbd.Keymap(1, fd, uint32(size))

	vptr := r.ptrMgr.CreateVirtualPointer(r.seat)
	if err := conn.Flush(); err != nil {
		log.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	t := uint32(0)
	next := func() uint32 { t += 20; return t }
	abs := func(x, y int32) {
		vptr.MotionAbsolute(next(), uint32(x), uint32(y), uint32(ex), uint32(ey))
		vptr.Frame()
	}

	abs(x1, y1)
	time.Sleep(300 * time.Millisecond) // let hover land

	if *mod {
		vkbd.Key(next(), keyLogo, 1)
	}
	vptr.Button(next(), btn, 1)
	vptr.Frame()
	if err := conn.Flush(); err != nil {
		log.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // let the op start

	for i := 1; i <= *steps; i++ {
		x := x1 + (x2-x1)*int32(i)/int32(*steps)
		y := y1 + (y2-y1)*int32(i)/int32(*steps)
		abs(x, y)
		if err := conn.Flush(); err != nil {
			log.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	vptr.Button(next(), btn, 0)
	vptr.Frame()
	if *mod {
		vkbd.Key(next(), keyLogo, 0)
	}
	if err := conn.Flush(); err != nil {
		log.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
}
