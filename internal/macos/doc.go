// Package macos is wimy's macOS backend: an ordinary app with the
// Accessibility permission that tiles other apps' windows through the
// AX API and runs wimy's key bindings as Carbon hotkeys. Pure
// translation logic (key codes, coordinates, heuristics) is in
// cgo-free files so it is unit-tested on every OS; the AppKit/AX side
// is in the _darwin files. See plans/macos-port.md.
package macos
