package simulator

import (
	"strings"
	"testing"

	"linuxquest/internal/filesystem"
)

func newSim(t *testing.T) *Sim {
	t.Helper()
	f := filesystem.New("/home/player")
	for p, c := range map[string]string{
		"/home/player/notes.txt":            "notes\n",
		"/home/player/documents/readme.txt": "readme\n",
		"/home/player/secrets/clue.txt":     "CLUE\n",
	} {
		if err := f.WriteFile(p, c); err != nil {
			t.Fatal(err)
		}
	}
	return New(f, "/home/player")
}

func TestPwd(t *testing.T) {
	s := newSim(t)
	if r := s.Execute("pwd"); !r.OK || r.Stdout != "/home/player" {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("pwd extra"); r.OK || r.Stderr == "" {
		t.Errorf("expected error: %+v", r)
	}
}

func TestLs(t *testing.T) {
	s := newSim(t)
	if r := s.Execute("ls"); !r.OK || r.Stdout != "documents/  notes.txt  secrets/" {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("ls documents"); r.Stdout != "readme.txt" || r.Listed[0] != "/home/player/documents" {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("ls notes.txt"); !r.OK || r.Stdout != "notes.txt" {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("ls missing"); r.OK || !strings.Contains(r.Stderr, "No such file or directory") {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("ls -l"); r.OK || !strings.Contains(r.Stderr, "not supported") {
		t.Errorf("flags: %+v", r)
	}
}

func TestCd(t *testing.T) {
	s := newSim(t)
	if r := s.Execute("cd documents"); !r.OK || s.Cwd() != "/home/player/documents" || r.CwdAfter != s.Cwd() {
		t.Errorf("%+v cwd=%s", r, s.Cwd())
	}
	if s.PromptDir() != "~/documents" {
		t.Errorf("prompt dir %q", s.PromptDir())
	}
	if r := s.Execute("cd .."); !r.OK || s.Cwd() != "/home/player" {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("cd /home/player/secrets"); !r.OK || s.Cwd() != "/home/player/secrets" {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("cd"); !r.OK || s.Cwd() != "/home/player" {
		t.Errorf("bare cd: %+v", r)
	}
	before := s.Cwd()
	if r := s.Execute("cd nowhere"); r.OK || s.Cwd() != before || !strings.Contains(r.Stderr, "No such file") {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("cd notes.txt"); r.OK || !strings.Contains(r.Stderr, "Not a directory") || s.Cwd() != before {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("cd a b"); r.OK {
		t.Errorf("too many args should fail: %+v", r)
	}
}

func TestCat(t *testing.T) {
	s := newSim(t)
	if r := s.Execute("cat notes.txt"); !r.OK || r.Stdout != "notes" || r.Read[0] != "/home/player/notes.txt" {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("cat secrets/clue.txt documents/readme.txt"); !r.OK || r.Stdout != "CLUE\nreadme" || len(r.Read) != 2 {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("cat documents"); r.OK || !strings.Contains(r.Stderr, "Is a directory") {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("cat"); r.OK || r.Stderr == "" {
		t.Errorf("%+v", r)
	}
	r := s.Execute("cat notes.txt nope.txt")
	if r.OK || r.Stdout != "notes" || len(r.Read) != 1 || !strings.Contains(r.Stderr, "nope.txt") {
		t.Errorf("partial failure: %+v", r)
	}
}

func TestFind(t *testing.T) {
	s := newSim(t)
	tests := []struct {
		line string
		out  string
	}{
		{"find . -name clue.txt", "./secrets/clue.txt"},
		{"find -name clue.txt", "./secrets/clue.txt"},
		{"find / -name clue.txt", "/home/player/secrets/clue.txt"},
		{"find /home/player -name 'clue*'", "/home/player/secrets/clue.txt"},
		{`find . -name "*.txt" -type f`, "./documents/readme.txt\n./notes.txt\n./secrets/clue.txt"},
		{"find secrets", "secrets\nsecrets/clue.txt"},
		{"find . -type d", ".\n./documents\n./secrets"},
	}
	for _, tc := range tests {
		r := s.Execute(tc.line)
		if !r.OK || r.Stdout != tc.out {
			t.Errorf("%q => %+v; want %q", tc.line, r, tc.out)
		}
	}
	r := s.Execute("find . -name clue.txt")
	if len(r.Matches) != 1 || r.Matches[0] != "/home/player/secrets/clue.txt" {
		t.Errorf("matches: %v", r.Matches)
	}
	s.Execute("cd documents")
	if r := s.Execute("find .. -name clue.txt"); r.Stdout != "../secrets/clue.txt" {
		t.Errorf("relative from subdir: %+v", r)
	}
	if r := s.Execute("find missing -name x"); r.OK || !strings.Contains(r.Stderr, "No such file or directory") {
		t.Errorf("%+v", r)
	}
	for _, bad := range []string{"find . -exec ls", "find . -name", "find . -type x", "find . -name '['"} {
		if r := s.Execute(bad); r.OK || r.Stderr == "" {
			t.Errorf("%q should fail: %+v", bad, r)
		}
	}
}

func TestEscapeAttempts(t *testing.T) {
	s := newSim(t)
	for _, line := range []string{
		"cd ../../../../../..", "cat /etc/passwd", "cat ../../../../etc/shadow", "ls /proc", "ls /etc",
		"find / -name passwd", "cat ~/../../../etc/hostname", "cat $HOME/notes.txt", "cat /proc/self/environ",
	} {
		r := s.Execute(line)
		if strings.Contains(r.Stdout, "root:") || strings.Contains(r.Stdout, "PATH=") {
			t.Errorf("%q leaked host data: %q", line, r.Stdout)
		}
	}
	s.Execute("cd ../../../../../..")
	if s.Cwd() != "/" {
		t.Errorf("expected virtual root, got %q", s.Cwd())
	}
	if r := s.Execute("cat /etc/passwd"); r.OK {
		t.Errorf("host file readable: %+v", r)
	}
	if r := s.Execute("ls /"); r.Stdout != "home/" {
		t.Errorf("virtual root should only contain home/, got %q", r.Stdout)
	}
}

func TestUnsupportedAndInjection(t *testing.T) {
	s := newSim(t)
	for _, line := range []string{"rmdir /", "sudo ls", "bash", "sh -c id", "whoami", "env", "ping 127.0.0.1"} {
		r := s.Execute(line)
		if r.OK || r.Known || !strings.Contains(r.Stderr, "command not found") {
			t.Errorf("%q: %+v", line, r)
		}
	}
	// rm exists now, so destructive lines must fail safely inside the sandbox
	// (on a throwaway shell: rm -r /home really deletes there).
	d := newSim(t)
	for _, line := range []string{"rm -rf /", "rm /home/player"} {
		r := d.Execute(line)
		if r.OK || !r.Known {
			t.Errorf("%q should be known-but-failed: %+v", line, r)
		}
	}
	if r := d.Execute("rm -rf /"); !strings.Contains(r.Stderr, "refusing") {
		t.Errorf("root removal should be refused: %+v", r)
	}
	if _, err := s.FS().Stat("/home/player/notes.txt"); err != nil {
		t.Errorf("other shells must be unaffected: %v", err)
	}
	for _, line := range []string{
		"ls; id", "ls && id", "ls | cat", "cat notes.txt > out", "cat < notes.txt", "echo `id`", "ls & id",
	} {
		r := s.Execute(line)
		if r.OK || r.Stdout != "" || !strings.Contains(r.Stderr, "not supported") {
			t.Errorf("%q should be rejected as shell syntax: %+v", line, r)
		}
	}
	// $(...) is just text: it is never expanded or executed.
	if r := s.Execute("cat $(id)"); r.OK || strings.Contains(r.Stdout, "uid=") {
		t.Errorf("%+v", r)
	}
	if r := s.Execute("   "); !r.OK || r.Stdout != "" || r.Stderr != "" {
		t.Errorf("blank line: %+v", r)
	}
}

func TestParse(t *testing.T) {
	ok := map[string][]string{
		`ls`:                   {"ls"},
		`  cat   a   b  `:      {"cat", "a", "b"},
		`cat "my file.txt"`:    {"cat", "my file.txt"},
		`find . -name 'clue*'`: {"find", ".", "-name", "clue*"},
		`cat a\ b`:             {"cat", "a b"},
		`cat ""`:               {"cat", ""},
		"cat a\x1b[31mb":       {"cat", "a[31mb"},
	}
	for in, want := range ok {
		got, err := Parse(in)
		if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Parse(`cat "oops`); err != ErrUnterminated {
		t.Errorf("unterminated: %v", err)
	}
	if _, err := Parse("cat a\\"); err != ErrUnterminated {
		t.Errorf("trailing backslash: %v", err)
	}
	if _, err := Parse(strings.Repeat("a", MaxLineLen+1)); err != ErrTooLong {
		t.Errorf("too long: %v", err)
	}
	if _, err := Parse("ls ; pwd"); err != ErrShellSyntax {
		t.Errorf("operator: %v", err)
	}
	// Quoted operators are plain text.
	if got, err := Parse(`find . -name "a;b"`); err != nil || got[3] != "a;b" {
		t.Errorf("quoted: %q %v", got, err)
	}
}

func TestMalformedInputNeverPanics(t *testing.T) {
	s := newSim(t)
	inputs := []string{
		"\x00", "cd \x00", "cat \x00\x00", "ls ''", "cd ''", "cat ''", `find "" -name ""`, "find -name", "cd " + strings.Repeat("../", 5000),
		strings.Repeat("a", 5000), "\xff\xfe", "ls \xff", "cat \xc3\x28", "find . -name '*[*'", "~", "cd ~~", "cd ~/..", "cat -", "ls -", "'", `"`, "\\",
	}
	for _, in := range inputs {
		_ = s.Execute(in)
	}
}

func TestControlCharactersAreStripped(t *testing.T) {
	s := newSim(t)
	for _, line := range []string{"cat '\x1b[2Jmissing'", "cat \"\x07\x1b]0;pwned\x07x\"", "ls \x1b[31m"} {
		r := s.Execute(line)
		if strings.ContainsAny(r.Stdout+r.Stderr, "\x1b\x07") {
			t.Errorf("%q echoed a control character: %q", line, r.Stdout+r.Stderr)
		}
	}
	long := "cat " + strings.Repeat("é", 200)
	if r := s.Execute(long); !strings.Contains(r.Stderr, "...") || strings.ContainsRune(r.Stderr, '\ufffd') {
		t.Errorf("long/UTF-8 error text should be cut cleanly: %q", r.Stderr)
	}
}
