// Package simulator interprets a handful of Linux commands against a virtual
// filesystem. It never executes anything on the host: no os/exec, no shell, and
// no access to the real filesystem or environment.
package simulator

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"linuxquest/internal/filesystem"
)

// Commands lists the filesystem commands handled by the simulator, in help order.
var Commands = []string{"pwd", "ls", "cd", "cat", "find", "mkdir", "touch", "rm", "cp", "mv", "echo", "grep"}

// Result describes what a command did. Mission logic evaluates these facts
// (resulting state and effects) rather than matching on raw command strings.
type Result struct {
	Command string
	Args    []string
	OK      bool   // command ran without errors
	Stdout  string // text to show (no trailing newline)
	Stderr  string // error text to show (no trailing newline)
	Known   bool   // false if the command is not part of the simulator

	CwdBefore string
	CwdAfter  string

	Read    []string // absolute paths of files successfully read
	Listed  []string // absolute paths of directories successfully listed
	Matches []string // absolute paths printed by find
	Created []string // absolute paths created (mkdir, touch, cp, mv)
	Removed []string // absolute paths removed (rm, mv)
	Grepped []string // absolute paths grep printed a match from
}

// Sim is one player's simulated shell: a filesystem plus a working directory.
type Sim struct {
	fs  *filesystem.FS
	cwd string
}

// New creates a simulator that starts in startDir.
func New(fs *filesystem.FS, startDir string) *Sim {
	return &Sim{fs: fs, cwd: filesystem.Clean(startDir)}
}

// Cwd returns the current virtual working directory.
func (s *Sim) Cwd() string { return s.cwd }

// FS returns the underlying virtual filesystem.
func (s *Sim) FS() *filesystem.FS { return s.fs }

// PromptDir returns the working directory in prompt form ("~", "~/docs", "/etc").
func (s *Sim) PromptDir() string {
	home := s.fs.Home()
	switch {
	case s.cwd == home:
		return "~"
	case strings.HasPrefix(s.cwd, home+"/"):
		return "~" + strings.TrimPrefix(s.cwd, home)
	}
	return s.cwd
}

// Execute parses and runs one command line.
func (s *Sim) Execute(line string) Result {
	words, err := Parse(line)
	if err != nil {
		return Result{Stderr: "linuxquest: " + err.Error(), CwdBefore: s.cwd, CwdAfter: s.cwd}
	}
	if len(words) == 0 {
		return Result{OK: true, Known: true, CwdBefore: s.cwd, CwdAfter: s.cwd}
	}
	res := Result{Command: words[0], Args: words[1:], Known: true, CwdBefore: s.cwd}
	switch words[0] {
	case "pwd":
		s.pwd(&res)
	case "ls":
		s.ls(&res)
	case "cd":
		s.cd(&res)
	case "cat":
		s.cat(&res)
	case "find":
		s.find(&res)
	case "mkdir":
		s.mkdir(&res)
	case "touch":
		s.touch(&res)
	case "rm":
		s.rm(&res)
	case "cp":
		s.cp(&res)
	case "mv":
		s.mv(&res)
	case "echo":
		s.echo(&res)
	case "grep":
		s.grep(&res)
	default:
		res.Known = false
		res.Stderr = fmt.Sprintf("%s: command not found", words[0])
	}
	res.CwdAfter = s.cwd
	return res
}

func errText(err error) string {
	switch {
	case errors.Is(err, filesystem.ErrNotExist):
		return "No such file or directory"
	case errors.Is(err, filesystem.ErrNotDir):
		return "Not a directory"
	case errors.Is(err, filesystem.ErrIsDir):
		return "Is a directory"
	case errors.Is(err, filesystem.ErrInvalidPath):
		return "Invalid path"
	}
	return "Operation failed"
}

func (s *Sim) pwd(r *Result) {
	if len(r.Args) > 0 {
		r.Stderr = "pwd: too many arguments"
		return
	}
	r.Stdout, r.OK = s.cwd, true
}

func (s *Sim) cd(r *Result) {
	if len(r.Args) > 1 {
		r.Stderr = "cd: too many arguments"
		return
	}
	target := "~"
	if len(r.Args) == 1 {
		target = r.Args[0]
	}
	abs, err := s.fs.Resolve(s.cwd, target)
	if err != nil {
		r.Stderr = fmt.Sprintf("cd: %s: %s", trunc(target), errText(err))
		return
	}
	info, err := s.fs.Stat(abs)
	if err != nil {
		r.Stderr = fmt.Sprintf("cd: %s: %s", trunc(target), errText(err))
		return
	}
	if !info.IsDir {
		r.Stderr = fmt.Sprintf("cd: %s: Not a directory", trunc(target))
		return
	}
	s.cwd, r.OK = abs, true
}

func (s *Sim) ls(r *Result) {
	var targets []string
	for _, a := range r.Args {
		if strings.HasPrefix(a, "-") && a != "-" {
			r.Stderr = fmt.Sprintf("ls: option '%s' is not supported in this simulator (try: ls [path])", trunc(a))
			return
		}
		targets = append(targets, a)
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	var out, errs []string
	for i, t := range targets {
		abs, err := s.fs.Resolve(s.cwd, t)
		if err != nil {
			errs = append(errs, fmt.Sprintf("ls: cannot access '%s': %s", trunc(t), errText(err)))
			continue
		}
		info, err := s.fs.Stat(abs)
		if err != nil {
			errs = append(errs, fmt.Sprintf("ls: cannot access '%s': %s", trunc(t), errText(err)))
			continue
		}
		if !info.IsDir {
			out = append(out, t)
			continue
		}
		entries, _ := s.fs.ReadDir(abs)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir {
				names = append(names, e.Name+"/")
			} else {
				names = append(names, e.Name)
			}
		}
		if len(targets) > 1 {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, t+":")
		}
		if len(names) > 0 {
			out = append(out, strings.Join(names, "  "))
		}
		r.Listed = append(r.Listed, abs)
	}
	r.Stdout = strings.Join(out, "\n")
	r.Stderr = strings.Join(errs, "\n")
	r.OK = len(errs) == 0
}

func (s *Sim) cat(r *Result) {
	if len(r.Args) == 0 {
		r.Stderr = "cat: missing file operand (try: cat <file>)"
		return
	}
	var out, errs []string
	for _, a := range r.Args {
		abs, err := s.fs.Resolve(s.cwd, a)
		if err == nil {
			var content string
			if content, err = s.fs.ReadFile(abs); err == nil {
				out = append(out, strings.TrimRight(content, "\n"))
				r.Read = append(r.Read, abs)
				continue
			}
		}
		errs = append(errs, fmt.Sprintf("cat: %s: %s", trunc(a), errText(err)))
	}
	r.Stdout = strings.Join(out, "\n")
	r.Stderr = strings.Join(errs, "\n")
	r.OK = len(errs) == 0
}

// find supports: find [path ...] [-name pattern] [-type f|d]
func (s *Sim) find(r *Result) {
	var starts []string
	args := r.Args
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		starts = append(starts, args[0])
		args = args[1:]
	}
	if len(starts) == 0 {
		starts = []string{"."}
	}
	var pattern, ftype string
	for len(args) > 0 {
		switch args[0] {
		case "-name":
			if len(args) < 2 {
				r.Stderr = "find: missing argument to '-name'"
				return
			}
			pattern = args[1]
			if _, err := path.Match(pattern, "x"); err != nil {
				r.Stderr = fmt.Sprintf("find: invalid pattern '%s'", trunc(pattern))
				return
			}
			args = args[2:]
		case "-type":
			if len(args) < 2 || (args[1] != "f" && args[1] != "d") {
				r.Stderr = "find: -type must be followed by 'f' (file) or 'd' (directory)"
				return
			}
			ftype = args[1]
			args = args[2:]
		default:
			r.Stderr = fmt.Sprintf("find: unsupported expression '%s' (supported: -name pattern, -type f|d)", trunc(args[0]))
			return
		}
	}

	var out, errs []string
	for _, st := range starts {
		abs, err := s.fs.Resolve(s.cwd, st)
		if err == nil {
			_, err = s.fs.Stat(abs)
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("find: '%s': %s", trunc(st), errText(err)))
			continue
		}
		shown := strings.TrimRight(st, "/")
		if shown == "" {
			shown = "/"
		}
		_ = s.fs.Walk(abs, func(i filesystem.Info) {
			if pattern != "" {
				base := i.Name
				if i.Path == abs {
					base = path.Base(shown)
				}
				if ok, _ := path.Match(pattern, base); !ok {
					return
				}
			}
			if (ftype == "f" && i.IsDir) || (ftype == "d" && !i.IsDir) {
				return
			}
			rel := strings.TrimPrefix(i.Path, abs)
			display := shown + rel
			if shown == "/" {
				display = i.Path
			}
			out = append(out, display)
			r.Matches = append(r.Matches, i.Path)
		})
	}
	sort.Strings(r.Matches)
	r.Stdout = strings.Join(out, "\n")
	r.Stderr = strings.Join(errs, "\n")
	r.OK = len(errs) == 0
}

// trunc keeps echoed user input short in error messages.
func trunc(s string) string {
	const max = 60
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "..."
	}
	return s
}

// mkdir creates directories, parents included (like mkdir -p).
func (s *Sim) mkdir(r *Result) {
	var dirs []string
	for _, a := range r.Args {
		if a == "-p" {
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			r.Stderr = fmt.Sprintf("mkdir: option '%s' is not supported (only -p)", trunc(a))
			return
		}
		dirs = append(dirs, a)
	}
	if len(dirs) == 0 {
		r.Stderr = "mkdir: missing operand (try: mkdir <directory>)"
		return
	}
	var errs []string
	for _, d := range dirs {
		abs, err := s.fs.Resolve(s.cwd, d)
		if err != nil {
			errs = append(errs, fmt.Sprintf("mkdir: cannot create directory '%s': %s", trunc(d), errText(err)))
			continue
		}
		if _, err := s.fs.Stat(abs); err == nil {
			errs = append(errs, fmt.Sprintf("mkdir: cannot create directory '%s': File exists", trunc(d)))
			continue
		}
		if err := s.fs.MkdirAll(abs); err != nil {
			errs = append(errs, fmt.Sprintf("mkdir: cannot create directory '%s': %s", trunc(d), errText(err)))
			continue
		}
		r.Created = append(r.Created, abs)
	}
	r.Stderr = strings.Join(errs, "\n")
	r.OK = len(errs) == 0
}

// touch creates an empty file when missing; touching what exists is a no-op.
func (s *Sim) touch(r *Result) {
	if len(r.Args) == 0 {
		r.Stderr = "touch: missing file operand (try: touch <file>)"
		return
	}
	var errs []string
	for _, a := range r.Args {
		abs, err := s.fs.Resolve(s.cwd, a)
		if err != nil {
			errs = append(errs, fmt.Sprintf("touch: cannot touch '%s': %s", trunc(a), errText(err)))
			continue
		}
		if _, err := s.fs.Stat(abs); err == nil {
			continue
		}
		if err := s.fs.WriteFile(abs, ""); err != nil {
			errs = append(errs, fmt.Sprintf("touch: cannot touch '%s': %s", trunc(a), errText(err)))
			continue
		}
		r.Created = append(r.Created, abs)
	}
	r.Stderr = strings.Join(errs, "\n")
	r.OK = len(errs) == 0
}

// rm supports: rm [-r] [-f] path...
func (s *Sim) rm(r *Result) {
	var recursive, force bool
	var targets []string
	for _, a := range r.Args {
		if strings.HasPrefix(a, "-") && a != "-" && a != "--" {
			for _, f := range a[1:] {
				switch f {
				case 'r', 'R':
					recursive = true
				case 'f':
					force = true
				default:
					r.Stderr = fmt.Sprintf("rm: invalid option -- '%c' (try: rm [-r] [-f] path...)", f)
					return
				}
			}
			continue
		}
		targets = append(targets, a)
	}
	if len(targets) == 0 {
		r.Stderr = "rm: missing operand (try: rm [-r] path...)"
		return
	}
	var errs []string
	for _, tg := range targets {
		abs, err := s.fs.Resolve(s.cwd, tg)
		if err != nil {
			if !force {
				errs = append(errs, fmt.Sprintf("rm: cannot remove '%s': %s", trunc(tg), errText(err)))
			}
			continue
		}
		if abs == "/" {
			errs = append(errs, "rm: refusing to remove '/'")
			continue
		}
		info, err := s.fs.Stat(abs)
		if err != nil {
			if !force {
				errs = append(errs, fmt.Sprintf("rm: cannot remove '%s': %s", trunc(tg), errText(err)))
			}
			continue
		}
		if info.IsDir && !recursive {
			errs = append(errs, fmt.Sprintf("rm: cannot remove '%s': Is a directory (use rm -r)", trunc(tg)))
			continue
		}
		if err := s.fs.Remove(abs, recursive); err != nil {
			if !force {
				errs = append(errs, fmt.Sprintf("rm: cannot remove '%s': %s", trunc(tg), errText(err)))
			}
			continue
		}
		r.Removed = append(r.Removed, abs)
	}
	r.Stderr = strings.Join(errs, "\n")
	r.OK = len(errs) == 0
}

// cp supports: cp [-r] source dest
func (s *Sim) cp(r *Result) {
	var recursive bool
	var operands []string
	for _, a := range r.Args {
		if a == "-r" || a == "-R" {
			recursive = true
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			r.Stderr = fmt.Sprintf("cp: option '%s' is not supported (only -r)", trunc(a))
			return
		}
		operands = append(operands, a)
	}
	if len(operands) < 2 {
		r.Stderr = "cp: missing destination (try: cp [-r] <source> <dest>)"
		return
	}
	if len(operands) > 2 {
		r.Stderr = fmt.Sprintf("cp: target '%s': Not a directory", trunc(operands[2]))
		return
	}
	src, err := s.fs.Resolve(s.cwd, operands[0])
	if err != nil {
		r.Stderr = fmt.Sprintf("cp: cannot stat '%s': %s", trunc(operands[0]), errText(err))
		return
	}
	if _, err := s.fs.Stat(src); err != nil {
		r.Stderr = fmt.Sprintf("cp: cannot stat '%s': %s", trunc(operands[0]), errText(err))
		return
	}
	dst, err := s.fs.Resolve(s.cwd, operands[1])
	if err != nil {
		r.Stderr = fmt.Sprintf("cp: cannot copy to '%s': %s", trunc(operands[1]), errText(err))
		return
	}
	if info, err := s.fs.Stat(src); err == nil && info.IsDir && !recursive {
		r.Stderr = fmt.Sprintf("cp: -r not specified; omitting directory '%s'", trunc(operands[0]))
		return
	}
	if src == dst {
		r.Stderr = fmt.Sprintf("cp: '%s' and '%s' are the same file", trunc(operands[0]), trunc(operands[1]))
		return
	}
	final, err := s.fs.Copy(src, dst, recursive)
	if err != nil {
		r.Stderr = fmt.Sprintf("cp: cannot copy '%s' to '%s': %s", trunc(operands[0]), trunc(operands[1]), errText(err))
		return
	}
	r.Created = append(r.Created, final)
	r.OK = true
}

// mv supports: mv source dest
func (s *Sim) mv(r *Result) {
	if len(r.Args) < 2 {
		r.Stderr = "mv: missing destination (try: mv <source> <dest>)"
		return
	}
	if len(r.Args) > 2 {
		r.Stderr = fmt.Sprintf("mv: target '%s': Not a directory", trunc(r.Args[2]))
		return
	}
	src, err := s.fs.Resolve(s.cwd, r.Args[0])
	if err != nil {
		r.Stderr = fmt.Sprintf("mv: cannot stat '%s': %s", trunc(r.Args[0]), errText(err))
		return
	}
	if _, err := s.fs.Stat(src); err != nil {
		r.Stderr = fmt.Sprintf("mv: cannot stat '%s': %s", trunc(r.Args[0]), errText(err))
		return
	}
	dst, err := s.fs.Resolve(s.cwd, r.Args[1])
	if err != nil {
		r.Stderr = fmt.Sprintf("mv: cannot move to '%s': %s", trunc(r.Args[1]), errText(err))
		return
	}
	if src == dst {
		r.Stderr = fmt.Sprintf("mv: '%s' and '%s' are the same file", trunc(r.Args[0]), trunc(r.Args[1]))
		return
	}
	final, err := s.fs.Move(src, dst)
	if err != nil {
		r.Stderr = fmt.Sprintf("mv: cannot move '%s' to '%s': %s", trunc(r.Args[0]), trunc(r.Args[1]), errText(err))
		return
	}
	r.Created = append(r.Created, final)
	r.Removed = append(r.Removed, src)
	r.OK = true
}

// echo prints its arguments. There is no redirection or expansion.
func (s *Sim) echo(r *Result) {
	r.Stdout, r.OK = strings.Join(r.Args, " "), true
}

// grep supports: grep [-i] [-r] pattern [path...]
func (s *Sim) grep(r *Result) {
	var insensitive, recursive bool
	args := r.Args
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" {
		for _, f := range args[0][1:] {
			switch f {
			case 'i':
				insensitive = true
			case 'r', 'R':
				recursive = true
			default:
				r.Stderr = fmt.Sprintf("grep: invalid option -- '%c' (try: grep [-i] [-r] pattern [path...])", f)
				return
			}
		}
		args = args[1:]
	}
	if len(args) == 0 {
		r.Stderr = "grep: missing pattern (try: grep [-i] [-r] pattern [path...])"
		return
	}
	pattern, paths := args[0], args[1:]
	if len(paths) == 0 {
		paths = []string{"."}
	}
	match := func(line string) bool {
		if insensitive {
			return strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
		}
		return strings.Contains(line, pattern)
	}
	var out, errs []string
	scan := func(shown, abs string) {
		content, err := s.fs.ReadFile(abs)
		if err != nil {
			errs = append(errs, fmt.Sprintf("grep: %s: %s", trunc(shown), errText(err)))
			return
		}
		hit := false
		for _, line := range strings.Split(content, "\n") {
			if match(line) {
				out = append(out, shown+":"+line)
				hit = true
			}
		}
		if hit {
			r.Grepped = append(r.Grepped, abs)
		}
	}
	for _, p := range paths {
		abs, err := s.fs.Resolve(s.cwd, p)
		if err != nil {
			errs = append(errs, fmt.Sprintf("grep: %s: %s", trunc(p), errText(err)))
			continue
		}
		info, err := s.fs.Stat(abs)
		if err != nil {
			errs = append(errs, fmt.Sprintf("grep: %s: %s", trunc(p), errText(err)))
			continue
		}
		shown := strings.TrimRight(p, "/")
		if shown == "" {
			shown = "/"
		}
		if !info.IsDir {
			scan(shown, abs)
			continue
		}
		if !recursive {
			errs = append(errs, fmt.Sprintf("grep: %s: Is a directory (use grep -r)", trunc(shown)))
			continue
		}
		_ = s.fs.Walk(abs, func(i filesystem.Info) {
			if i.IsDir {
				return
			}
			rel := strings.TrimPrefix(i.Path, abs)
			display := shown + rel
			if shown == "/" {
				display = i.Path
			}
			scan(display, i.Path)
		})
	}
	r.Stdout = strings.Join(out, "\n")
	r.Stderr = strings.Join(errs, "\n")
	r.OK = len(errs) == 0
}
