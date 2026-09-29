package config

// ParseBind parses a key combination like "Mod-Shift-h" with the
// given primary modifier name (e.g. "Mod4", "Option") into a Bind.
func ParseBind(mod, combo, cmd string) (Bind, error) {
	mask, err := parseModName(mod)
	if err != nil {
		return Bind{}, err
	}
	c := &Config{Mod: mod, ModMask: mask}
	return c.parseBind(combo, cmd)
}
