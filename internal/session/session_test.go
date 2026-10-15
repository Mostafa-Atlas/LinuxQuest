package session

import (
	"strings"
	"testing"

	"linuxquest/internal/missions"
	missionsdata "linuxquest/missions"
)

func newSession(t *testing.T) *Session {
	t.Helper()
	ms, err := missions.Load(missionsdata.FS)
	if err != nil {
		t.Fatal(err)
	}
	return New(ms)
}

func play(t *testing.T, s *Session, lines ...string) Outcome {
	t.Helper()
	var out Outcome
	for _, l := range lines {
		out = s.Submit(l)
	}
	return out
}

func text(o Outcome) string {
	var b strings.Builder
	for _, l := range o.Lines {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}

func TestMission1Complete(t *testing.T) {
	s := newSession(t)
	if err := s.Start("welcome"); err != nil {
		t.Fatal(err)
	}
	o := s.Submit("pwd")
	if !strings.Contains(text(o), "Objective complete") || !strings.Contains(text(o), "(1/3)") || o.Completed {
		t.Errorf("after pwd: %s", text(o))
	}
	o = s.Submit("ls")
	if !strings.Contains(text(o), "(2/3)") || !strings.Contains(text(o), "welcome.txt") {
		t.Errorf("after ls: %s", text(o))
	}
	o = s.Submit("cat welcome.txt")
	if !o.Completed || o.XPAwarded != 50 || !o.FirstTime || s.Progress().XP() != 50 {
		t.Errorf("completion: %+v xp=%d", o, s.Progress().XP())
	}
}

func TestMission1AnyOrderAndAlternatives(t *testing.T) {
	for _, seq := range [][]string{
		{"cat welcome.txt", "ls", "pwd"},
		{"cat /home/player/welcome.txt", "ls .", "pwd"},
		{"ls /home/player", "cat ~/welcome.txt", "pwd"},
		{"pwd", "cd missions", "cat ../welcome.txt", "cd ..", "ls"},
	} {
		s := newSession(t)
		_ = s.Start("welcome")
		o := play(t, s, seq...)
		if !s.Run().Complete() || s.Progress().XP() != 50 {
			t.Errorf("%v did not complete (last: %s)", seq, text(o))
		}
	}
}

func TestMission1WrongCommandsDoNotCount(t *testing.T) {
	s := newSession(t)
	_ = s.Start("welcome")
	play(t, s, "cat nothing.txt", "ls missions", "pwd extra", "cat missions/first_clue.txt")
	if s.Run().DoneCount() != 0 {
		t.Errorf("no objective should be complete, got %d", s.Run().DoneCount())
	}
}

func TestMission2Complete(t *testing.T) {
	s := newSession(t)
	_ = s.Start("missing-clue")
	o := s.Submit("cd documents")
	if !strings.Contains(text(o), "(1/4)") || s.Prompt() != "player@linuxquest:~/documents$" {
		t.Errorf("%s / %s", text(o), s.Prompt())
	}
	if !strings.Contains(text(s.Submit("cd ..")), "(2/4)") {
		t.Error("cd .. should complete objective 2")
	}
	o = s.Submit("find . -name clue.txt")
	if !strings.Contains(text(o), "./secrets/clue.txt") || !strings.Contains(text(o), "(3/4)") {
		t.Errorf("find: %s", text(o))
	}
	o = s.Submit("cat secrets/clue.txt")
	if !o.Completed || o.XPAwarded != 75 || !strings.Contains(text(o), "BLUE-LANTERN") {
		t.Errorf("final: %+v %s", o, text(o))
	}
}

func TestMission2AlternativeSequences(t *testing.T) {
	seqs := [][]string{
		{"cd /home/player/documents", "cd /home/player", "find / -name clue.txt", "cat /home/player/secrets/clue.txt"},
		{"cd documents", "cd ~", "find -name 'clue*'", "cat ./secrets/clue.txt"},
		{"cd ./documents/", "cd ../", "find /home/player -name clue.txt", "cd secrets", "cat clue.txt"},
		{"cat secrets/clue.txt", "cd documents", "cd", "find secrets -name clue.txt"},
		{"cd documents", "cd ..", "find . -type f -name '*.txt'", "cat secrets/clue.txt"},
	}
	for _, seq := range seqs {
		s := newSession(t)
		_ = s.Start("missing-clue")
		o := play(t, s, seq...)
		if !s.Run().Complete() || s.Progress().XP() != 75 {
			t.Errorf("%v did not complete: done=%d last=%s", seq, s.Run().DoneCount(), text(o))
		}
	}
}

func TestMission2ReturnRequiresVisitingDocuments(t *testing.T) {
	s := newSession(t)
	_ = s.Start("missing-clue")
	play(t, s, "cd ~", "cd") // already home: going "back" without leaving does not count
	if s.Run().IsDone("return_home") {
		t.Error("return_home must require entering documents first")
	}
	play(t, s, "cd documents", "cd ..")
	if !s.Run().IsDone("return_home") {
		t.Error("return_home should complete after documents then home")
	}
}

func TestMission2FindNeedsMatchingResult(t *testing.T) {
	s := newSession(t)
	_ = s.Start("missing-clue")
	play(t, s, "find . -name notes.txt", "find documents -name clue.txt", "find . -name 'zzz*'")
	if s.Run().IsDone("find_clue") {
		t.Error("find that does not locate the clue must not count")
	}
}

func TestReplayGivesNoExtraXP(t *testing.T) {
	s := newSession(t)
	_ = s.Start("welcome")
	play(t, s, "pwd", "ls", "cat welcome.txt")
	if s.Progress().XP() != 50 {
		t.Fatal("setup")
	}
	if err := s.Start("welcome"); err != nil {
		t.Fatal(err)
	}
	if s.Run().DoneCount() != 0 || s.Sim().Cwd() != "/home/player" {
		t.Error("replay must start fresh")
	}
	o := play(t, s, "pwd", "ls", "cat welcome.txt")
	if !o.Completed || o.XPAwarded != 0 || o.FirstTime || s.Progress().XP() != 50 || s.Progress().Completions("welcome") != 2 {
		t.Errorf("%+v xp=%d", o, s.Progress().XP())
	}
}

func TestReplayResetsFilesystemAndDirectory(t *testing.T) {
	s := newSession(t)
	_ = s.Start("missing-clue")
	play(t, s, "cd documents")
	_ = s.Start("missing-clue")
	if s.Sim().Cwd() != "/home/player" {
		t.Errorf("cwd after replay: %s", s.Sim().Cwd())
	}
}

func TestSessionIsolation(t *testing.T) {
	a, b := newSession(t), newSession(t)
	_ = a.Start("missing-clue")
	_ = b.Start("missing-clue")
	play(t, a, "cd documents", "cd ..", "find . -name clue.txt", "cat secrets/clue.txt")
	if !a.Run().Complete() || a.Progress().XP() != 75 {
		t.Fatal("A should be done")
	}
	if b.Sim().Cwd() != "/home/player" || b.Run().DoneCount() != 0 || b.Progress().XP() != 0 {
		t.Errorf("B affected by A: cwd=%s done=%d xp=%d", b.Sim().Cwd(), b.Run().DoneCount(), b.Progress().XP())
	}
	play(t, b, "cd secrets")
	if a.Sim().Cwd() != "/home/player" {
		t.Errorf("A cwd changed by B: %s", a.Sim().Cwd())
	}
	if b.Summary().XP != 0 || len(b.Summary().Completed) != 0 {
		t.Errorf("B summary: %+v", b.Summary())
	}
}

func TestHintsProgressiveAndContextual(t *testing.T) {
	s := newSession(t)
	_ = s.Start("welcome")
	h1 := text(s.Submit("hint"))
	h2 := text(s.Submit("hint"))
	h3 := text(s.Submit("hint"))
	h4 := text(s.Submit("hint")) // stays on the final hint
	if !strings.Contains(h1, "(1/3)") || !strings.Contains(h2, "(2/3)") || !strings.Contains(h3, "(3/3)") || !strings.Contains(h4, "(3/3)") {
		t.Errorf("progressive hints: %q %q %q %q", h1, h2, h3, h4)
	}
	if strings.Contains(h1, "pwd") {
		t.Errorf("first hint must not reveal the command: %q", h1)
	}
	s.Submit("pwd")
	if hn := text(s.Submit("hint")); strings.Contains(hn, "print working directory") || !strings.Contains(hn, "(1/3)") {
		t.Errorf("hint should move to the next objective: %q", hn)
	}
	s2 := newSession(t)
	_ = s2.Start("missing-clue")
	if h := text(s2.Submit("hint")); !strings.Contains(h, "folder") {
		t.Errorf("mission 2 first hint: %q", h)
	}
	// "return_home" hint must not appear before entering documents.
	s2.Submit("cd documents")
	if h := text(s2.Submit("hint")); !strings.Contains(h, "documents") && !strings.Contains(h, "one level up") {
		t.Errorf("after entering documents: %q", h)
	}
}

func TestHintWhenNothingLeftAndInMenu(t *testing.T) {
	s := newSession(t)
	if h := text(s.Submit("hint")); !strings.Contains(h, "Pick a mission") {
		t.Errorf("menu hint: %q", h)
	}
	_ = s.Start("welcome")
	play(t, s, "pwd", "ls", "cat welcome.txt")
	if h := text(s.Submit("hint")); !strings.Contains(h, "No hints needed") {
		t.Errorf("%q", h)
	}
}

func TestBuiltinCommands(t *testing.T) {
	s := newSession(t)
	_ = s.Start("welcome")
	if o := s.Submit("help"); !strings.Contains(text(o), "find [path] -name") || !strings.Contains(text(o), "hint") {
		t.Errorf("help: %s", text(o))
	}
	if o := s.Submit("help me"); !strings.Contains(text(o), "Supported commands") {
		t.Errorf("help with args: %s", text(o))
	}
	if o := s.Submit("clear"); !o.Clear {
		t.Error("clear")
	}
	if o := s.Submit("missions"); !o.ToMenu {
		t.Error("missions")
	}
	if o := s.Submit("exit"); !o.Quit {
		t.Error("exit")
	}
	if o := s.Submit("mission"); !strings.Contains(text(o), "Welcome to the Terminal") || !strings.Contains(text(o), "[ ]") {
		t.Errorf("mission: %s", text(o))
	}
	s.Submit("pwd")
	if o := s.Submit("mission"); !strings.Contains(text(o), "[x] Discover directory with pwd") {
		t.Errorf("progress in mission view: %s", text(o))
	}
}

func TestInvalidInputFeedback(t *testing.T) {
	s := newSession(t)
	_ = s.Start("welcome")
	o := s.Submit("foobar")
	if !strings.Contains(text(o), "command not found") || !strings.Contains(text(o), "supports") {
		t.Errorf("unsupported: %s", text(o))
	}
	o = s.Submit("cat missing.txt")
	if !strings.Contains(text(o), "No such file or directory") || !strings.Contains(text(o), "Tip:") {
		t.Errorf("missing file: %s", text(o))
	}
	o = s.Submit("ls; whoami")
	if strings.Contains(text(o), "player\n") && !strings.Contains(text(o), "not supported") {
		t.Errorf("chaining must not execute: %s", text(o))
	}
	if o := s.Submit(strings.Repeat("x", 5000)); !strings.Contains(text(o), "too long") {
		t.Errorf("long input: %s", text(o))
	}
	if o := s.Submit("   "); len(o.Lines) != 0 {
		t.Errorf("blank: %+v", o)
	}
	if s.Run().DoneCount() != 0 {
		t.Error("invalid input must not advance objectives")
	}
}

func TestCommandsWithoutMission(t *testing.T) {
	s := newSession(t)
	if o := s.Submit("pwd"); !strings.Contains(text(o), "No mission is active") {
		t.Errorf("%s", text(o))
	}
	if err := s.Start("nope"); err == nil {
		t.Error("unknown mission should error")
	}
}

func TestSummary(t *testing.T) {
	s := newSession(t)
	_ = s.Start("welcome")
	play(t, s, "pwd", "ls", "cat welcome.txt")
	sum := s.Summary()
	if sum.XP != 50 || sum.Commands != 3 || len(sum.Completed) != 1 || sum.Completed[0] != "Welcome to the Terminal" {
		t.Errorf("%+v", sum)
	}
}
