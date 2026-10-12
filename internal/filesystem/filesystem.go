// Package filesystem implements a small in-memory virtual filesystem.
//
// Nothing in this package touches the host filesystem. All paths are
// normalised lexically against a virtual root, so ".." can never climb out of
// "/" no matter what the player types.
package filesystem

import (
	"errors"
	"path"
	"sort"
	"strings"
)

// MaxPathLen bounds the length of any path the simulator will accept.
const MaxPathLen = 1024

var (
	ErrNotExist    = errors.New("No such file or directory")
	ErrNotDir      = errors.New("Not a directory")
	ErrIsDir       = errors.New("Is a directory")
	ErrInvalidPath = errors.New("Invalid path")
	ErrExist       = errors.New("File exists")
	ErrNotEmpty    = errors.New("Directory not empty")
)

type node struct {
	dir      bool
	content  string
	children map[string]*node
}

// Info describes a file or directory.
type Info struct {
	Name  string // base name ("/" for the root)
	Path  string // absolute, normalised path
	IsDir bool
	Size  int
}

// FS is an in-memory filesystem. It is not safe for concurrent use; each
// player session owns its own FS.
type FS struct {
	root *node
	home string
}

// New returns an empty filesystem containing only "/". home is the directory
// that "~" and a bare "cd" resolve to.
func New(home string) *FS {
	return &FS{root: &node{dir: true, children: map[string]*node{}}, home: Clean(home)}
}

// Home returns the virtual home directory.
func (f *FS) Home() string { return f.home }

// Clean normalises a virtual path to an absolute one. ".." at the root stays
// at the root, exactly like a real Linux system.
func Clean(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

// Resolve turns a player-supplied path into an absolute, normalised virtual
// path. Relative paths are resolved against cwd; "~" and "~/x" against home.
// The result is always inside the virtual filesystem.
func (f *FS) Resolve(cwd, p string) (string, error) {
	if len(p) > MaxPathLen {
		return "", ErrInvalidPath
	}
	switch {
	case p == "":
		return Clean(cwd), nil
	case p == "~":
		return f.home, nil
	case strings.HasPrefix(p, "~/"):
		return Clean(f.home + "/" + p[2:]), nil
	case strings.HasPrefix(p, "/"):
		return Clean(p), nil
	default:
		return Clean(Clean(cwd) + "/" + p), nil
	}
}

func (f *FS) lookup(abs string) (*node, error) {
	abs = Clean(abs)
	n := f.root
	if abs == "/" {
		return n, nil
	}
	for _, part := range strings.Split(strings.TrimPrefix(abs, "/"), "/") {
		if !n.dir {
			return nil, ErrNotDir
		}
		c, ok := n.children[part]
		if !ok {
			return nil, ErrNotExist
		}
		n = c
	}
	return n, nil
}

// MkdirAll creates a directory and any missing parents.
func (f *FS) MkdirAll(abs string) error {
	abs = Clean(abs)
	n := f.root
	if abs == "/" {
		return nil
	}
	for _, part := range strings.Split(strings.TrimPrefix(abs, "/"), "/") {
		c, ok := n.children[part]
		if !ok {
			c = &node{dir: true, children: map[string]*node{}}
			n.children[part] = c
		}
		if !c.dir {
			return ErrNotDir
		}
		n = c
	}
	return nil
}

// WriteFile creates (or replaces) a file, creating parent directories.
func (f *FS) WriteFile(abs, content string) error {
	abs = Clean(abs)
	if abs == "/" {
		return ErrIsDir
	}
	if err := f.MkdirAll(path.Dir(abs)); err != nil {
		return err
	}
	parent, err := f.lookup(path.Dir(abs))
	if err != nil {
		return err
	}
	name := path.Base(abs)
	if existing, ok := parent.children[name]; ok && existing.dir {
		return ErrIsDir
	}
	parent.children[name] = &node{content: content}
	return nil
}

// Stat returns information about abs.
func (f *FS) Stat(abs string) (Info, error) {
	abs = Clean(abs)
	n, err := f.lookup(abs)
	if err != nil {
		return Info{}, err
	}
	return infoFor(abs, n), nil
}

func infoFor(abs string, n *node) Info {
	return Info{Name: path.Base(abs), Path: abs, IsDir: n.dir, Size: len(n.content)}
}

// ReadFile returns the contents of a regular file.
func (f *FS) ReadFile(abs string) (string, error) {
	n, err := f.lookup(abs)
	if err != nil {
		return "", err
	}
	if n.dir {
		return "", ErrIsDir
	}
	return n.content, nil
}

// ReadDir lists a directory, sorted by name.
func (f *FS) ReadDir(abs string) ([]Info, error) {
	abs = Clean(abs)
	n, err := f.lookup(abs)
	if err != nil {
		return nil, err
	}
	if !n.dir {
		return nil, ErrNotDir
	}
	out := make([]Info, 0, len(n.children))
	for name, c := range n.children {
		out = append(out, infoFor(path.Join(abs, name), c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Walk visits abs and everything beneath it in lexical, depth-first order.
func (f *FS) Walk(abs string, fn func(Info)) error {
	abs = Clean(abs)
	n, err := f.lookup(abs)
	if err != nil {
		return err
	}
	f.walk(abs, n, fn)
	return nil
}

func (f *FS) walk(abs string, n *node, fn func(Info)) {
	fn(infoFor(abs, n))
	if !n.dir {
		return
	}
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f.walk(path.Join(abs, name), n.children[name], fn)
	}
}

// Remove deletes the file or directory at abs. A non-empty directory needs
// recursive=true. The root is never removed.
func (f *FS) Remove(abs string, recursive bool) error {
	abs = Clean(abs)
	if abs == "/" {
		return ErrInvalidPath
	}
	parent, err := f.lookup(path.Dir(abs))
	if err != nil {
		return err
	}
	name := path.Base(abs)
	n, ok := parent.children[name]
	if !ok {
		return ErrNotExist
	}
	if n.dir && len(n.children) > 0 && !recursive {
		return ErrNotEmpty
	}
	delete(parent.children, name)
	return nil
}

// cloneNode deep-copies a subtree so copies never alias the original.
func cloneNode(n *node) *node {
	c := &node{dir: n.dir, content: n.content}
	if n.dir {
		c.children = make(map[string]*node, len(n.children))
		for name, child := range n.children {
			c.children[name] = cloneNode(child)
		}
	}
	return c
}

// attach places n at abs, replacing a file but never a directory. Missing
// parents are an error (like real cp/mv, they are not created).
func (f *FS) attach(abs string, n *node) error {
	abs = Clean(abs)
	if abs == "/" {
		return ErrInvalidPath
	}
	parent, err := f.lookup(path.Dir(abs))
	if err != nil {
		return err
	}
	name := path.Base(abs)
	if existing, ok := parent.children[name]; ok && existing.dir {
		return ErrIsDir
	}
	parent.children[name] = n
	return nil
}

// Copy copies src to dst, which take clean absolute paths. When dst names an
// existing directory, src lands inside it under its base name. Directories
// need recursive=true. Copying a path onto itself is refused.
func (f *FS) Copy(srcAbs, dstAbs string, recursive bool) (string, error) {
	srcAbs, dstAbs = Clean(srcAbs), Clean(dstAbs)
	if srcAbs == "/" {
		return "", ErrInvalidPath
	}
	src, err := f.lookup(srcAbs)
	if err != nil {
		return "", err
	}
	if src.dir && !recursive {
		return "", ErrIsDir
	}
	if dst, err := f.lookup(dstAbs); err == nil {
		if !dst.dir {
			if src.dir {
				return "", ErrNotDir
			}
		} else {
			dstAbs = path.Join(dstAbs, path.Base(srcAbs))
		}
	} else if !errors.Is(err, ErrNotExist) {
		return "", err
	}
	if srcAbs == dstAbs {
		return "", ErrInvalidPath
	}
	if err := f.attach(dstAbs, cloneNode(src)); err != nil {
		return "", err
	}
	return dstAbs, nil
}

// Move renames src to dst (clean absolute paths). A dst naming an existing
// directory means "move inside it". Files are overwritten; directories never
// are. Moving a path onto itself is refused.
func (f *FS) Move(srcAbs, dstAbs string) (string, error) {
	srcAbs, dstAbs = Clean(srcAbs), Clean(dstAbs)
	if srcAbs == "/" {
		return "", ErrInvalidPath
	}
	srcParent, err := f.lookup(path.Dir(srcAbs))
	if err != nil {
		return "", err
	}
	name := path.Base(srcAbs)
	src, ok := srcParent.children[name]
	if !ok {
		return "", ErrNotExist
	}
	if dst, err := f.lookup(dstAbs); err == nil {
		if !dst.dir {
			if src.dir {
				return "", ErrNotDir
			}
		} else {
			dstAbs = path.Join(dstAbs, name)
		}
	} else if !errors.Is(err, ErrNotExist) {
		return "", err
	}
	if srcAbs == dstAbs {
		return "", ErrInvalidPath
	}
	if err := f.attach(dstAbs, src); err != nil {
		return "", err
	}
	delete(srcParent.children, name)
	return dstAbs, nil
}
