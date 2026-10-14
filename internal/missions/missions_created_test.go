package missions

import (
	"testing"

	"linuxquest/internal/simulator"
)

func TestCreatedObjective(t *testing.T) {
	m := Mission{
		ID: "m", Title: "t", XP: 10,
		Objectives: []Objective{
			{ID: "mk", Text: "make it", Kind: KindCreated, Path: "/home/player/fresh.txt", Success: "s", Hints: []string{"h"}},
		},
	}
	run := NewRun(&m)
	got := run.Evaluate(simulator.Result{Command: "touch", Args: []string{"fresh.txt"},
		OK: true, Known: true, Created: []string{"/home/player/fresh.txt"}})
	if len(got) != 1 || !run.Complete() {
		t.Fatalf("created objective should complete: %+v", got)
	}
	// A removal does not satisfy a creation.
	run2 := NewRun(&m)
	got = run2.Evaluate(simulator.Result{Command: "rm", Args: []string{"fresh.txt"},
		OK: true, Known: true, Removed: []string{"/home/player/fresh.txt"}})
	if len(got) != 0 {
		t.Fatalf("removal must not satisfy created: %+v", got)
	}
}

func TestCreatedNeedsPath(t *testing.T) {
	m := Mission{ID: "m", Title: "t", XP: 10,
		Objectives: []Objective{{ID: "o", Text: "x", Kind: KindCreated, Success: "s", Hints: []string{"h"}}}}
	if err := m.validate(); err == nil {
		t.Fatal("created without path should fail validation")
	}
}
