package filesystem

import "testing"

func newTestFS(t *testing.T) *FS {
	t.Helper()
	f := New("/home/player")
	for p, c := range map[string]string{
		"/home/player/a.txt":     "A",
		"/home/player/d/b.txt":   "B",
		"/home/player/d/e/c.txt": "C",
	} {
		if err := f.WriteFile(p, c); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestResolve(t *testing.T) {
	f := newTestFS(t)
	cases := []struct{ cwd, in, want string }{
		{"/home/player", "", "/home/player"},
		{"/home/player", ".", "/home/player"},
		{"/home/player", "d", "/home/player/d"},
		{"/home/player", "d/e/..", "/home/player/d"},
		{"/home/player/d", "..", "/home/player"},
		{"/home/player/d", "../..", "/home"},
		{"/home/player", "/", "/"},
		{"/home/player", "~", "/home/player"},
		{"/home/player/d", "~/d/e", "/home/player/d/e"},
		{"/home/player", "d//e/./", "/home/player/d/e"},
		{"/home/player", "/home/player/../player/d", "/home/player/d"},
	}
	for _, c := range cases {
		got, err := f.Resolve(c.cwd, c.in)
		if err != nil || got != c.want {
			t.Errorf("Resolve(%q,%q) = %q,%v; want %q", c.cwd, c.in, got, err, c.want)
		}
	}
}

func TestResolveCannotEscapeRoot(t *testing.T) {
	f := newTestFS(t)
	for _, in := range []string{"..", "../..", "../../../../../..", "/..", "/../../etc/passwd", "~/../../../..", "d/../../../../.."} {
		got, err := f.Resolve("/home/player", in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != "/" && !hasPrefix(got, "/") {
			t.Errorf("%q escaped: %q", in, got)
		}
		if _, err := f.Stat(got); got == "/" && err != nil {
			t.Errorf("root must exist: %v", err)
		}
	}
	// ".." above the root stays at the root.
	if got, _ := f.Resolve("/", ".."); got != "/" {
		t.Errorf("got %q", got)
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func TestResolveRejectsBadInput(t *testing.T) {
	f := newTestFS(t)
	if _, err := f.Resolve("/", "a\x00b"); err != ErrInvalidPath {
		t.Errorf("NUL byte: %v", err)
	}
	long := make([]byte, MaxPathLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := f.Resolve("/", string(long)); err != ErrInvalidPath {
		t.Errorf("long path: %v", err)
	}
}

func TestReadAndList(t *testing.T) {
	f := newTestFS(t)
	if c, err := f.ReadFile("/home/player/d/b.txt"); err != nil || c != "B" {
		t.Errorf("%q %v", c, err)
	}
	if _, err := f.ReadFile("/home/player/d"); err != ErrIsDir {
		t.Errorf("dir read: %v", err)
	}
	if _, err := f.ReadFile("/nope"); err != ErrNotExist {
		t.Errorf("missing: %v", err)
	}
	if _, err := f.ReadFile("/home/player/a.txt/x"); err != ErrNotDir {
		t.Errorf("through file: %v", err)
	}
	ents, err := f.ReadDir("/home/player")
	if err != nil || len(ents) != 2 || ents[0].Name != "a.txt" || ents[1].Name != "d" || !ents[1].IsDir {
		t.Errorf("%+v %v", ents, err)
	}
	if _, err := f.ReadDir("/home/player/a.txt"); err != ErrNotDir {
		t.Errorf("readdir file: %v", err)
	}
}

func TestWalkOrder(t *testing.T) {
	f := newTestFS(t)
	var got []string
	_ = f.Walk("/home/player", func(i Info) { got = append(got, i.Path) })
	want := []string{"/home/player", "/home/player/a.txt", "/home/player/d", "/home/player/d/b.txt", "/home/player/d/e", "/home/player/d/e/c.txt"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %q != %q", i, got[i], want[i])
		}
	}
}

func TestInstancesAreIndependent(t *testing.T) {
	a, b := newTestFS(t), newTestFS(t)
	_ = a.WriteFile("/home/player/only-a.txt", "x")
	if _, err := b.Stat("/home/player/only-a.txt"); err != ErrNotExist {
		t.Errorf("filesystems leaked: %v", err)
	}
}
