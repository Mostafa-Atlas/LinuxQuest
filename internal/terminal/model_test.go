package terminal

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"linuxquest/internal/missions"
	"linuxquest/internal/session"
	missionsdata "linuxquest/missions"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func newModel(t *testing.T) *Model {
	t.Helper()
	ms, err := missions.Load(missionsdata.FS)
	if err != nil {
		t.Fatal(err)
	}
	m := New(session.New(ms), NewStyles(nil))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func key(m *Model, k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }

func typ(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func run(m *Model, line string) {
	typ(m, line)
	key(m, tea.KeyEnter)
}

func view(m *Model) string { return ansi.ReplaceAllString(m.View(), "") }

func TestWelcomeScreen(t *testing.T) {
	m := newModel(t)
	v := view(m)
	for _, want := range []string{"LinuxQuest", "safe, simulated Linux terminal"} {
		if !strings.Contains(v, want) {
			t.Errorf("welcome missing %q:\n%s", want, v)
		}
	}
	key(m, tea.KeyEnter)
	if m.screen != screenMenu {
		t.Fatal("Enter should open the menu")
	}
	v = view(m)
	for _, want := range []string{"Welcome to the Terminal", "Find the Missing Clue", "50 XP", "75 XP"} {
		if !strings.Contains(v, want) {
			t.Errorf("menu missing %q:\n%s", want, v)
		}
	}
}

func TestMenuNavigation(t *testing.T) {
	m := newModel(t)
	key(m, tea.KeyEnter)
	n := len(m.sess.Catalog())
	key(m, tea.KeyDown)
	if m.menuIdx != 1 {
		t.Errorf("down: %d", m.menuIdx)
	}
	for i := 0; i < n-1; i++ {
		key(m, tea.KeyDown)
	}
	if m.menuIdx != 0 {
		t.Errorf("wraparound: %d", m.menuIdx)
	}
	key(m, tea.KeyUp)
	if m.menuIdx != n-1 {
		t.Errorf("up wrap: %d", m.menuIdx)
	}
	typ(m, "1")
	if m.screen != screenMission || m.sess.Run().Mission.ID != "welcome" {
		t.Errorf("number key should start mission 1")
	}
}

func TestPlayMission1ThroughUI(t *testing.T) {
	m := newModel(t)
	key(m, tea.KeyEnter)
	typ(m, "1")
	v := view(m)
	for _, want := range []string{"Welcome to the Terminal", "Objectives:", "Discover directory", "player@linuxquest:~$"} {
		if !strings.Contains(v, want) {
			t.Errorf("mission view missing %q:\n%s", want, v)
		}
	}
	run(m, "pwd")
	if v := view(m); !strings.Contains(v, "/home/player") || !strings.Contains(v, "Objective complete") || !strings.Contains(v, "1/3") {
		t.Errorf("after pwd:\n%s", v)
	}
	run(m, "ls")
	run(m, "cat welcome.txt")
	if m.screen != screenComplete {
		t.Fatalf("expected completion screen, at %v", m.screen)
	}
	v = view(m)
	for _, want := range []string{"Mission complete!", "+50 XP", "Total XP this session: 50", "replay", "Welcome to LinuxQuest, operator."} {
		if !strings.Contains(v, want) {
			t.Errorf("completion missing %q:\n%s", want, v)
		}
	}
	// replay
	typ(m, "r")
	if m.screen != screenMission || m.sess.Run().DoneCount() != 0 {
		t.Fatal("r should replay")
	}
	run(m, "pwd")
	run(m, "ls")
	run(m, "cat welcome.txt")
	if v := view(m); !strings.Contains(v, "Replay complete") || !strings.Contains(v, "Total XP this session: 50") {
		t.Errorf("replay completion:\n%s", v)
	}
	key(m, tea.KeyEnter)
	if m.screen != screenMenu || !strings.Contains(view(m), "[x]") {
		t.Errorf("Enter should return to menu showing completion:\n%s", view(m))
	}
}

func TestExitShowsSummaryThenQuits(t *testing.T) {
	m := newModel(t)
	key(m, tea.KeyEnter)
	typ(m, "1")
	run(m, "pwd")
	run(m, "exit")
	if m.screen != screenSummary {
		t.Fatal("exit should show summary")
	}
	if v := view(m); !strings.Contains(v, "Session summary") || !strings.Contains(v, "Commands entered:  2") {
		t.Errorf("summary:\n%s", v)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Error("any key on the summary should quit")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected tea.QuitMsg")
	}
}

func TestInputEditingAndHistory(t *testing.T) {
	m := newModel(t)
	key(m, tea.KeyEnter)
	typ(m, "1")
	typ(m, "pwx")
	key(m, tea.KeyBackspace)
	typ(m, "d")
	if string(m.input) != "pwd" {
		t.Errorf("input %q", string(m.input))
	}
	key(m, tea.KeyLeft)
	key(m, tea.KeyLeft)
	typ(m, "X")
	if string(m.input) != "pXwd" {
		t.Errorf("cursor insert: %q", string(m.input))
	}
	key(m, tea.KeyCtrlC) // clears the line first
	if len(m.input) != 0 || m.screen != screenMission {
		t.Error("Ctrl+C with input should only clear it")
	}
	run(m, "ls")
	run(m, "pwd")
	key(m, tea.KeyUp)
	if string(m.input) != "pwd" {
		t.Errorf("history up: %q", string(m.input))
	}
	key(m, tea.KeyUp)
	if string(m.input) != "ls" {
		t.Errorf("history up 2: %q", string(m.input))
	}
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	if len(m.input) != 0 {
		t.Errorf("history down to blank: %q", string(m.input))
	}
	// spaces and overlong input
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	typ(m, strings.Repeat("a", 1000))
	if len(m.input) > maxInput {
		t.Errorf("input not bounded: %d", len(m.input))
	}
}

func TestControlCharactersIgnored(t *testing.T) {
	m := newModel(t)
	key(m, tea.KeyEnter)
	typ(m, "1")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pwd\x1b[2J\x07")})
	if strings.ContainsAny(string(m.input), "\x1b\x07") {
		t.Errorf("control chars leaked into input: %q", string(m.input))
	}
}

func TestClearAndMissionsCommands(t *testing.T) {
	m := newModel(t)
	key(m, tea.KeyEnter)
	typ(m, "1")
	run(m, "pwd")
	run(m, "clear")
	if len(m.lines) != 0 {
		t.Errorf("clear left %d lines", len(m.lines))
	}
	run(m, "help")
	if v := view(m); !strings.Contains(v, "Supported commands") {
		t.Errorf("help:\n%s", v)
	}
	run(m, "missions")
	if m.screen != screenMenu || m.sess.Run() != nil {
		t.Error("missions should return to the menu")
	}
}

func TestSmallAndOddTerminalSizes(t *testing.T) {
	for _, sz := range [][2]int{{1, 1}, {10, 3}, {20, 8}, {40, 10}, {200, 60}} {
		m := newModel(t)
		m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		for _, step := range []func(){
			func() { _ = view(m) },
			func() { key(m, tea.KeyEnter); _ = view(m) },
			func() { typ(m, "1"); run(m, "help"); run(m, "find . -name x"); _ = view(m) },
			func() { run(m, "pwd"); run(m, "ls"); run(m, "cat welcome.txt"); _ = view(m) },
			func() { key(m, tea.KeyEnter); _ = view(m) },
		} {
			step()
		}
	}
}

func TestEachModelHasOwnSession(t *testing.T) {
	a, b := newModel(t), newModel(t)
	key(a, tea.KeyEnter)
	typ(a, "1")
	run(a, "pwd")
	if b.screen != screenWelcome || b.sess.Progress().XP() != 0 || b.sess.Run() != nil {
		t.Error("state leaked between UI models")
	}
}

func TestHardWrap(t *testing.T) {
	for _, in := range []string{"", "short", strings.Repeat("word ", 40), strings.Repeat("x", 300), "  indented " + strings.Repeat("y ", 50), "a\nb\n\nc"} {
		for _, w := range []int{10, 40, 79} {
			for _, l := range hardWrap(in, w) {
				if len([]rune(l)) > w {
					t.Errorf("line over width %d: %q", w, l)
				}
			}
		}
	}
}
