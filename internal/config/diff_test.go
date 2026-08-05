package config

import (
	"slices"
	"testing"
)

func TestDiffExecs(t *testing.T) {
	tests := []struct {
		name      string
		old, new  []string
		wantKill  []string
		wantSpawn []string
	}{
		{"both empty", nil, nil, nil, nil},
		{"identical", []string{"waybar", "kanshi"}, []string{"waybar", "kanshi"}, nil, nil},
		{"order ignored", []string{"a", "b"}, []string{"b", "a"}, nil, nil},
		{"added", []string{"waybar"}, []string{"waybar", "kanshi"}, nil, []string{"kanshi"}},
		{"removed", []string{"waybar", "kanshi"}, []string{"waybar"}, []string{"kanshi"}, nil},
		{"changed", []string{"waybar"}, []string{"quickshell"}, []string{"waybar"}, []string{"quickshell"}},
		{"all added to empty", nil, []string{"a", "b"}, nil, []string{"a", "b"}},
		{"all removed to empty", []string{"a", "b"}, nil, []string{"a", "b"}, nil},
		{"duplicate lost one", []string{"a", "a"}, []string{"a"}, []string{"a"}, nil},
		{"duplicate gained two", []string{"a"}, []string{"a", "a", "a"}, nil, []string{"a", "a"}},
		{"duplicates stable", []string{"a", "a"}, []string{"a", "a"}, nil, nil},
		{"mixed", []string{"a", "b", "b"}, []string{"b", "c"}, []string{"a", "b"}, []string{"c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kill, spawn := DiffExecs(tt.old, tt.new)
			if !slices.Equal(kill, tt.wantKill) {
				t.Errorf("kill: got %v, want %v", kill, tt.wantKill)
			}
			if !slices.Equal(spawn, tt.wantSpawn) {
				t.Errorf("spawn: got %v, want %v", spawn, tt.wantSpawn)
			}
		})
	}
}
