#!/bin/sh
# install.sh — copy the wimy bar into its own config directory.
#
#   ./install.sh [destination]
#
# Default destination is ${XDG_CONFIG_HOME:-~/.config}/waybar-wimy. This is
# deliberately NOT ~/.config/waybar: that path is shared by every waybar on
# the system (sway, Hyprland, ...) and wimy's bar would clobber it.
set -eu

src=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
dest=${1:-${XDG_CONFIG_HOME:-$HOME/.config}/waybar-wimy}

mkdir -p -- "$dest"
for f in config.jsonc style.css wimy-bar wimy-tags.sh wimy-mode.sh wimy-window.sh; do
	cp -- "$src/$f" "$dest/$f"
done
chmod +x -- "$dest/wimy-bar" "$dest/wimy-tags.sh" "$dest/wimy-mode.sh" "$dest/wimy-window.sh"

cat <<EOF
installed to $dest

run it:
  $dest/wimy-bar

start it with the window manager — in ~/.config/wimy/config.kdl:
  autostart {
    exec "$dest/wimy-bar"
  }
EOF
