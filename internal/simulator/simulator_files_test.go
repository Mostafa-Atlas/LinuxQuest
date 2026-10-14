package simulator

import (
	"strings"
	"testing"
)

func TestMkdirTouch(t *testing.T) {
	s := newSim(t)
	r := s.Execute("mkdir projects")
	if !r.OK || len(r.Created) != 1 || r.Created[0] != "/home/player/projects" {
		t.Errorf("mkdir: %+v", r)
	}
	r = s.Execute("mkdir deep/nested/dir")
	if !r.OK || r.Created[0] != "/home/player/deep/nested/dir" {
		t.Errorf("mkdir -p style: %+v", r)
	}
	if r := s.Execute("mkdir projects"); r.OK || !strings.Contains(r.Stderr, "File exists") {
		t.Errorf("mkdir existing should fail: %+v", r)
	}
	if r := s.Execute("mkdir"); r.OK {
		t.Errorf("mkdir without operand should fail: %+v", r)
	}
	r = s.Execute("touch fresh.txt notes.txt")
	if !r.OK || len(r.Created) != 1 || r.Created[0] != "/home/player/fresh.txt" {
		t.Errorf("touch: %+v", r)
	}
	if r := s.Execute("touch notes.txt"); !r.OK {
		t.Errorf("touch existing should succeed: %+v", r)
	}
	if r := s.Execute("cat fresh.txt"); !r.OK || len(r.Read) != 1 {
		t.Errorf("touched file should be readable: %+v", r)
	}
}

func TestRm(t *testing.T) {
	s := newSim(t)
	r := s.Execute("rm notes.txt")
	if !r.OK || len(r.Removed) != 1 || r.Removed[0] != "/home/player/notes.txt" {
		t.Errorf("rm: %+v", r)
	}
	if r := s.Execute("cat notes.txt"); r.OK {
		t.Errorf("removed file should be gone: %+v", r)
	}
	if r := s.Execute("rm documents"); r.OK || !strings.Contains(r.Stderr, "rm -r") {
		t.Errorf("rm dir should demand -r: %+v", r)
	}
	r = s.Execute("rm -r documents secrets")
	if !r.OK || len(r.Removed) != 2 {
		t.Errorf("rm -r: %+v", r)
	}
	if r := s.Execute("rm -f ghost.txt"); !r.OK {
		t.Errorf("rm -f should ignore missing: %+v", r)
	}
	if r := s.Execute("rm ghost.txt"); r.OK {
		t.Errorf("rm missing should fail: %+v", r)
	}
	if r := s.Execute("rm /"); r.OK || !strings.Contains(r.Stderr, "refusing") {
		t.Errorf("rm / should be refused: %+v", r)
	}
	if r := s.Execute("rm --bogus x"); r.OK {
		t.Errorf("bad flag should fail: %+v", r)
	}
}

func TestCopyMove(t *testing.T) {
	s := newSim(t)
	r := s.Execute("cp notes.txt copy.txt")
	if !r.OK || len(r.Created) != 1 || r.Created[0] != "/home/player/copy.txt" {
		t.Errorf("cp: %+v", r)
	}
	if r := s.Execute("cat copy.txt"); !r.OK || r.Stdout != "notes" {
		t.Errorf("copy content: %+v", r)
	}
	r = s.Execute("cp -r documents backup")
	if !r.OK || r.Created[0] != "/home/player/backup" {
		t.Errorf("cp -r: %+v", r)
	}
	if r := s.Execute("cp documents nodir"); r.OK || !strings.Contains(r.Stderr, "-r not specified") {
		t.Errorf("cp dir should demand -r: %+v", r)
	}
	if r := s.Execute("cp notes.txt notes.txt"); r.OK {
		t.Errorf("cp onto itself should fail: %+v", r)
	}
	r = s.Execute("mv copy.txt moved.txt")
	if !r.OK || r.Created[0] != "/home/player/moved.txt" || r.Removed[0] != "/home/player/copy.txt" {
		t.Errorf("mv: %+v", r)
	}
	if r := s.Execute("mv"); r.OK {
		t.Errorf("mv without args should fail: %+v", r)
	}
}

func TestEchoGrep(t *testing.T) {
	s := newSim(t)
	if r := s.Execute("echo hello there"); !r.OK || r.Stdout != "hello there" {
		t.Errorf("echo: %+v", r)
	}
	if r := s.Execute("echo"); !r.OK || r.Stdout != "" {
		t.Errorf("bare echo: %+v", r)
	}
	r := s.Execute("grep CLUE secrets/clue.txt")
	if !r.OK || r.Stdout != "secrets/clue.txt:CLUE" {
		t.Errorf("grep file: %+v", r)
	}
	r = s.Execute("grep -r readme .")
	if !r.OK || !strings.Contains(r.Stdout, "documents/readme.txt:readme") {
		t.Errorf("grep -r: %+v", r)
	}
	r = s.Execute("grep -i clue secrets/clue.txt")
	if !r.OK || !strings.Contains(r.Stdout, "CLUE") {
		t.Errorf("grep -i: %+v", r)
	}
	if r := s.Execute("grep -r zzzzzz ."); !r.OK || r.Stdout != "" {
		t.Errorf("no match should succeed empty: %+v", r)
	}
	if r := s.Execute("grep CLUE documents"); r.OK || !strings.Contains(r.Stderr, "grep -r") {
		t.Errorf("grep dir should demand -r: %+v", r)
	}
	if r := s.Execute("grep"); r.OK {
		t.Errorf("grep without pattern should fail: %+v", r)
	}
}
