package session

import (
	"strings"
	"testing"
)

func completeSession(t *testing.T) *Session {
	t.Helper()
	s := newSession(t)
	if err := s.Start("welcome"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCompleteCommand(t *testing.T) {
	s := completeSession(t)
	if got, lines := s.Complete("pw"); got != "pwd " || len(lines) != 0 {
		t.Errorf("unique command: %q %+v", got, lines)
	}
	got, lines := s.Complete("c")
	if got != "c" || len(lines) == 0 {
		t.Fatalf("ambiguous command should list: %q %+v", got, lines)
	}
	joined := ""
	for _, l := range lines {
		joined += l.Text + "\n"
	}
	for _, want := range []string{"cd", "cat", "clear"} {
		if !strings.Contains(joined, want) {
			t.Errorf("candidates missing %q:\n%s", want, joined)
		}
	}
	if got, lines := s.Complete("zz"); got != "zz" || len(lines) != 0 {
		t.Errorf("no match: %q %+v", got, lines)
	}
	if got, _ := s.Complete("keya"); got != "keyadd " {
		t.Errorf("keya: %q", got)
	}
}

func TestCompletePath(t *testing.T) {
	s := completeSession(t)
	if got, lines := s.Complete("ls wel"); got != "ls welcome.txt " || len(lines) != 0 {
		t.Errorf("file: %q %+v", got, lines)
	}
	if got, _ := s.Complete("ls miss"); got != "ls missions/" {
		t.Errorf("dir slash, no space so you can keep typing: %q", got)
	}
	if got, _ := s.Complete("cd missions/f"); got != "cd missions/first_clue.txt " {
		t.Errorf("nested: %q", got)
	}
	if got, lines := s.Complete("ls "); got != "ls " || len(lines) != 0 {
		t.Errorf("empty token: %q %+v", got, lines)
	}
	if got, lines := s.Complete("ls nope/x"); got != "ls nope/x" || len(lines) != 0 {
		t.Errorf("bad dir: %q %+v", got, lines)
	}
	if got, lines := s.Complete("cat nope"); got != "cat nope" || len(lines) != 0 {
		t.Errorf("no match: %q %+v", got, lines)
	}
}

func TestCompleteNeedsMission(t *testing.T) {
	s := newSession(t) // never started: no simulator
	if got, lines := s.Complete("pw"); got != "pw" || len(lines) != 0 {
		t.Errorf("menuless: %q %+v", got, lines)
	}
}
