// Package backend holds the platform-neutral half of a wimy backend:
// the command queue, the command effects that only start processes,
// autostart supervision, config-reload bookkeeping and pointer-op
// math. Concrete backends (internal/river on Linux, internal/macos on
// darwin) embed *Core and add the protocol or OS translation.
package backend
