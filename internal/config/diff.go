package config

// DiffExecs compares the old and new autostart exec lists as
// multisets (order-insensitive, duplicates counted). kill receives
// each entry whose occurrence count decreased (removed or changed),
// once per lost instance; spawn receives each entry whose count
// increased (added or changed), once per gained instance. Unchanged
// entries appear in neither list: their processes keep running.
func DiffExecs(old, new []string) (kill, spawn []string) {
	oldN := map[string]int{}
	for _, e := range old {
		oldN[e]++
	}
	newN := map[string]int{}
	for _, e := range new {
		newN[e]++
	}
	// deterministic output: kills in old list order, spawns in new
	seen := map[string]bool{}
	for _, e := range old {
		if seen[e] {
			continue
		}
		seen[e] = true
		for i := 0; i < oldN[e]-newN[e]; i++ {
			kill = append(kill, e)
		}
	}
	seen = map[string]bool{}
	for _, e := range new {
		if seen[e] {
			continue
		}
		seen[e] = true
		for i := 0; i < newN[e]-oldN[e]; i++ {
			spawn = append(spawn, e)
		}
	}
	return kill, spawn
}
