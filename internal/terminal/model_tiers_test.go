package terminal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"linuxquest/internal/missions"
	"linuxquest/internal/session"
)

func tieredCatalog() []missions.Mission {
	return []missions.Mission{
		{ID: "b1", Title: "Bee One", Difficulty: "Beginner", XP: 50},
		{ID: "b2", Title: "Bee Two", Difficulty: "Beginner", XP: 50},
		{ID: "i1", Title: "Eye One", Difficulty: "Intermediate", XP: 75},
		{ID: "a1", Title: "Ay One", Difficulty: "Advanced", XP: 100},
	}
}

func TestMenuHasTierSections(t *testing.T) {
	m := New(session.New(tieredCatalog()), NewStyles(nil))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.screen = screenMenu
	v := view(m)
	for _, want := range []string{"Beginner", "Intermediate", "Advanced", "0/2", "0/1"} {
		if !strings.Contains(v, want) {
			t.Errorf("menu missing %q:\n%s", want, v)
		}
	}
	if n := strings.Count(v, "\u2500\u2500 Beginner \u2500\u2500"); n != 1 {
		t.Errorf("Beginner header should appear once, got %d:\n%s", n, v)
	}
	// Ordering: tiers in play order, numbering continuous.
	b, i, a := strings.Index(v, "Bee One"), strings.Index(v, "Eye One"), strings.Index(v, "Ay One")
	if b < 0 || i < 0 || a < 0 || !(b < i && i < a) {
		t.Errorf("tier order wrong:\n%s", v)
	}
	if !strings.Contains(v, "3. Eye One") || !strings.Contains(v, "4. Ay One") {
		t.Errorf("numbering should continue across tiers:\n%s", v)
	}
	// Navigation crosses tier boundaries.
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	if m.menuIdx != 2 {
		t.Fatalf("idx=%d", m.menuIdx)
	}
	if v := view(m); !strings.Contains(v, "> [ ]  3. Eye One") {
		t.Errorf("selection should sit on tier-2 mission:\n%s", v)
	}
}

func TestMenuTierProgressCounts(t *testing.T) {
	sess := session.New(tieredCatalog())
	if _, first := sess.Progress().Complete("b1", 50); !first {
		t.Fatal("setup completion failed")
	}
	m := New(sess, NewStyles(nil))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.screen = screenMenu
	v := view(m)
	for _, want := range []string{"1/2", "50/100 XP", "[x]  1. Bee One"} {
		if !strings.Contains(v, want) {
			t.Errorf("tier progress missing %q:\n%s", want, v)
		}
	}
}
