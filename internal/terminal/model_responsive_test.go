package terminal

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"linuxquest/internal/missions"
	"linuxquest/internal/session"
)

func bigCatalog(n int) []missions.Mission {
	out := make([]missions.Mission, n)
	for i := range out {
		out[i] = missions.Mission{
			ID:         fmt.Sprintf("m%02d", i+1),
			Title:      fmt.Sprintf("Mission number %d with a fairly long title", i+1),
			Difficulty: "Beginner",
			XP:         10,
		}
	}
	return out
}

func TestMenuScrollsForLongCatalog(t *testing.T) {
	m := New(session.New(bigCatalog(60)), NewStyles(nil))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16}) // page = 4
	m.screen = screenMenu
	if got := view(m); strings.Contains(got, "Mission number 5 with") {
		t.Errorf("first page should not show item 5:\n%s", got)
	}
	for i := 0; i < 10; i++ {
		key(m, tea.KeyDown)
	}
	if m.menuTop == 0 {
		t.Error("menu should have scrolled")
	}
	v := view(m)
	for _, want := range []string{"11/60", "↓", "↑", "Mission number 11"} {
		if !strings.Contains(v, want) {
			t.Errorf("scrolled menu missing %q:\n%s", want, v)
		}
	}
	// Every mission row starts its title in the same column (1. and 11. align).
	titleCol := -1
	for _, line := range strings.Split(v, "\n") {
		i := strings.Index(line, "Mission number")
		if i < 0 {
			continue
		}
		if titleCol < 0 {
			titleCol = i
		} else if i != titleCol {
			t.Errorf("ragged titles: col %d vs %d:\n%s", titleCol, i, v)
			break
		}
	}
	if strings.Contains(v, "Mission number 1 with") {
		t.Errorf("scrolled-off item still visible:\n%s", v)
	}
	// Wrap around returns to the top.
	for i := 0; i < 50; i++ {
		key(m, tea.KeyDown)
	}
	if m.menuIdx != 0 {
		t.Errorf("wraparound should return to first: idx=%d", m.menuIdx)
	}
	if v := view(m); !strings.Contains(v, "> [ ]  1. ") {
		t.Errorf("wraparound should show the first mission selected:\n%s", v)
	}
}

func TestNarrowScreensRenderWithinWidth(t *testing.T) {
	for _, size := range [][2]int{{20, 10}, {28, 12}, {40, 16}, {100, 30}} {
		m := New(session.New(bigCatalog(3)), NewStyles(nil))
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		// Welcome.
		checkWidth(t, size[0], "welcome", view(m))
		// Menu.
		key(m, tea.KeyEnter)
		checkWidth(t, size[0], "menu", view(m))
		// Mission.
		typ(m, "1")
		if m.screen != screenMission {
			t.Fatalf("size %v: mission did not start", size)
		}
		checkWidth(t, size[0], "mission", view(m))
		// Complete and summary.
		m.screen = screenComplete
		checkWidth(t, size[0], "complete", view(m))
		m.screen = screenSummary
		checkWidth(t, size[0], "summary", view(m))
	}
}

func checkWidth(t *testing.T, width int, what, v string) {
	t.Helper()
	for _, line := range strings.Split(v, "\n") {
		if n := utf8.RuneCountInString(line); n > width {
			t.Errorf("%s overflows width %d (%d): %q", what, width, n, line)
		}
	}
}

func TestTabCompletesInMission(t *testing.T) {
	m := newModel(t)
	key(m, tea.KeyEnter)
	typ(m, "1")
	typ(m, "pw")
	key(m, tea.KeyTab)
	if string(m.input) != "pwd " {
		t.Errorf("tab should complete command: %q", string(m.input))
	}
	m.resetInput()
	typ(m, "ls wel")
	key(m, tea.KeyTab)
	if string(m.input) != "ls welcome.txt " {
		t.Errorf("tab should complete path: %q", string(m.input))
	}
	m.resetInput()
	typ(m, "c")
	n := len(m.lines)
	key(m, tea.KeyTab)
	if string(m.input) != "c" {
		t.Errorf("ambiguous tab must not change input: %q", string(m.input))
	}
	if len(m.lines) <= n {
		t.Error("ambiguous tab should list candidates")
	}
}

func TestNarrowMissionPushesChecklistInline(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	key(m, tea.KeyEnter)
	typ(m, "1")
	if m.showSidebar() {
		t.Fatal("60 columns should hide the sidebar")
	}
	run(m, "pwd")
	if v := view(m); !strings.Contains(v, "[x] Discover directory with pwd") {
		t.Errorf("narrow screens should print marks inline:\n%s", v)
	}
}

func TestSidebarSingleLineUniform(t *testing.T) {
	m := newModel(t) // 80x24: sidebar on
	key(m, tea.KeyEnter)
	typ(m, "1")
	run(m, "pwd")
	v := view(m)
	lit, pending := false, false
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "[x]") {
			lit = true
			if !strings.Contains(line, "Discover directory with pwd") {
				t.Errorf("done row wrapped or cut:\n%s", line)
			}
		}
		if strings.Contains(line, "[ ]") {
			pending = true
			if !strings.Contains(line, "List home files with ls") && !strings.Contains(line, "Read welcome.txt with cat") {
				t.Errorf("pending row wrapped or cut:\n%s", line)
			}
		}
	}
	if !lit || !pending {
		t.Errorf("sidebar should show lit and pending rows:\n%s", v)
	}
}

func TestPromptScrollsWithLongInput(t *testing.T) {
	m := newModel(t)
	key(m, tea.KeyEnter)
	typ(m, "1")
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("k", 300))})
	line := ansi.ReplaceAllString(m.promptLine(), "")
	if n := utf8.RuneCountInString(line); n > 40 {
		t.Errorf("prompt overflows: %d runes: %q", n, line)
	}
	if !strings.Contains(line, "◀") {
		t.Errorf("expected left overflow marker:\n%s", line)
	}
	if !strings.HasSuffix(line, "k") {
		t.Errorf("cursor should sit at the end:\n%s", line)
	}
	// Cursor in the middle shows both markers.
	m.cursor = 150
	line = ansi.ReplaceAllString(m.promptLine(), "")
	if !strings.Contains(line, "◀") || !strings.Contains(line, "▶") {
		t.Errorf("expected both markers:\n%s", line)
	}
	if n := utf8.RuneCountInString(line); n > 40 {
		t.Errorf("prompt overflows: %d runes", n)
	}
	// Short input renders the classic prompt unchanged.
	m.input, m.cursor = nil, 0
	typ(m, "pwd")
	line = ansi.ReplaceAllString(m.promptLine(), "")
	if !strings.Contains(line, "player@linuxquest:~$ pwd") {
		t.Errorf("short prompt changed:\n%s", line)
	}
}
