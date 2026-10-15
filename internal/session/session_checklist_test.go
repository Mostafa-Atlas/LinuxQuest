package session

import (
	"strings"
	"testing"
)

func TestChecklistLightsUpOnCompletion(t *testing.T) {
	s := newSession(t)
	_ = s.Start("welcome")
	out := s.Submit("pwd")
	// The checklist travels in the result for the UI to place (sidebar when
	// wide, scrollback when narrow) — never in the command output itself.
	for _, l := range out.Lines {
		if strings.Contains(l.Text, "[x]") || strings.Contains(l.Text, "[ ]") {
			t.Errorf("marks belong in Checklist, not Lines: %q", l.Text)
		}
	}
	marks := ""
	for _, l := range out.Checklist {
		marks += l.Text + "\n"
	}
	if !strings.Contains(marks, "[x] Discover directory with pwd") {
		t.Errorf("done objective should light up:\n%s", marks)
	}
	if !strings.Contains(marks, "[ ] List home files with ls") {
		t.Errorf("pending objective should stay empty:\n%s", marks)
	}
	// The briefing shows the same live marks without resubmitting.
	brief := ""
	for _, l := range s.Briefing() {
		brief += l.Text + "\n"
	}
	if !strings.Contains(brief, "[x] Discover directory with pwd") {
		t.Errorf("briefing should reflect progress:\n%s", brief)
	}
}
