package progress

import "testing"

func TestCompleteAwardsXPOnce(t *testing.T) {
	p := New()
	if xp, first := p.Complete("m1", 50); xp != 50 || !first {
		t.Fatalf("first: %d %v", xp, first)
	}
	if xp, first := p.Complete("m1", 50); xp != 0 || first {
		t.Fatalf("replay should give no XP: %d %v", xp, first)
	}
	if p.XP() != 50 || p.Completions("m1") != 2 || !p.IsCompleted("m1") || p.IsCompleted("m2") {
		t.Errorf("xp=%d completions=%d", p.XP(), p.Completions("m1"))
	}
	p.Complete("m2", 75)
	if p.XP() != 125 {
		t.Errorf("xp=%d", p.XP())
	}
	if ids := p.CompletedIDs(); len(ids) != 2 || ids[0] != "m1" || ids[1] != "m2" {
		t.Errorf("%v", ids)
	}
}

func TestProgressesAreIndependent(t *testing.T) {
	a, b := New(), New()
	a.Complete("m1", 50)
	if b.XP() != 0 || b.IsCompleted("m1") {
		t.Error("progress leaked between sessions")
	}
}
