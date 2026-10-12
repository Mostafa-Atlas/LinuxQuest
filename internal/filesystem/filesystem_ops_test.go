package filesystem

import (
	"errors"
	"testing"
)

func TestRemove(t *testing.T) {
	f := newTestFS(t)
	if err := f.Remove("/home/player/a.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat("/home/player/a.txt"); !errors.Is(err, ErrNotExist) {
		t.Errorf("file should be gone: %v", err)
	}
	// Non-empty directory needs -r.
	if err := f.Remove("/home/player/d", false); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("want ErrNotEmpty, got %v", err)
	}
	if err := f.Remove("/home/player/d", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat("/home/player/d/e/c.txt"); !errors.Is(err, ErrNotExist) {
		t.Errorf("tree should be gone: %v", err)
	}
	// Missing and root.
	if err := f.Remove("/home/player/nope", false); !errors.Is(err, ErrNotExist) {
		t.Errorf("want ErrNotExist, got %v", err)
	}
	if err := f.Remove("/", true); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("root must be refused, got %v", err)
	}
}

func TestCopy(t *testing.T) {
	f := newTestFS(t)
	dst, err := f.Copy("/home/player/a.txt", "/home/player/b.txt", false)
	if err != nil || dst != "/home/player/b.txt" {
		t.Fatalf("copy file: %v %q", err, dst)
	}
	if c, _ := f.ReadFile("/home/player/b.txt"); c != "A" {
		t.Errorf("copy content %q", c)
	}
	// Into a directory keeps the base name.
	if err := f.MkdirAll("/home/player/box"); err != nil {
		t.Fatal(err)
	}
	dst, err = f.Copy("/home/player/a.txt", "/home/player/box", false)
	if err != nil || dst != "/home/player/box/a.txt" {
		t.Fatalf("copy into dir: %v %q", err, dst)
	}
	// Directories need -r, and copies are independent.
	dst, err = f.Copy("/home/player/d", "/home/player/d2", true)
	if err != nil || dst != "/home/player/d2" {
		t.Fatalf("copy dir: %v %q", err, dst)
	}
	if _, err := f.Copy("/home/player/d", "/home/player/d3", false); !errors.Is(err, ErrIsDir) {
		t.Errorf("dir without -r: want ErrIsDir, got %v", err)
	}
	if err := f.WriteFile("/home/player/d2/e/c.txt", "CHANGED"); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.ReadFile("/home/player/d/e/c.txt"); c != "C" {
		t.Errorf("copy aliases original: %q", c)
	}
	// Same file and missing source.
	if _, err := f.Copy("/home/player/a.txt", "/home/player/a.txt", false); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("same file: want ErrInvalidPath, got %v", err)
	}
	if _, err := f.Copy("/home/player/nope", "/home/player/x", false); !errors.Is(err, ErrNotExist) {
		t.Errorf("missing src: want ErrNotExist, got %v", err)
	}
	if _, err := f.Copy("/home/player/a.txt", "/home/player/nodir/x", false); !errors.Is(err, ErrNotExist) {
		t.Errorf("missing dst parent: want ErrNotExist, got %v", err)
	}
}

func TestMove(t *testing.T) {
	f := newTestFS(t)
	dst, err := f.Move("/home/player/a.txt", "/home/player/renamed.txt")
	if err != nil || dst != "/home/player/renamed.txt" {
		t.Fatalf("move: %v %q", err, dst)
	}
	if _, err := f.Stat("/home/player/a.txt"); !errors.Is(err, ErrNotExist) {
		t.Errorf("source should be gone: %v", err)
	}
	// Into a directory keeps the base name.
	dst, err = f.Move("/home/player/renamed.txt", "/home/player/d")
	if err != nil || dst != "/home/player/d/renamed.txt" {
		t.Fatalf("move into dir: %v %q", err, dst)
	}
	// Overwrites files.
	if err := f.WriteFile("/home/player/victim.txt", "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Move("/home/player/d/renamed.txt", "/home/player/victim.txt"); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.ReadFile("/home/player/victim.txt"); c != "A" {
		t.Errorf("overwrite content %q", c)
	}
	// Same file and missing source.
	if _, err := f.Move("/home/player/victim.txt", "/home/player/victim.txt"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("same file: want ErrInvalidPath, got %v", err)
	}
	if _, err := f.Move("/home/player/nope", "/home/player/x"); !errors.Is(err, ErrNotExist) {
		t.Errorf("missing src: want ErrNotExist, got %v", err)
	}
}
