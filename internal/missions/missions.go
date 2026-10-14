// Package missions loads data-driven mission definitions and evaluates player
// progress against them. Objectives are judged from the effects of commands
// (resulting directory, files read, directories listed, find and grep matches,
// paths created or removed), not by
// matching exact command strings, so reasonable alternative approaches work.
package missions

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"

	"linuxquest/internal/filesystem"
	"linuxquest/internal/simulator"
)

// Objective kinds.
const (
	KindRan     = "ran"     // command ran successfully
	KindListed  = "listed"  // directory at Path was listed successfully
	KindRead    = "read"    // file at Path was read successfully
	KindEntered = "entered" // working directory is Path after a successful cd
	KindFound   = "found"   // find printed Path as a match
	KindCreated = "created" // Path was created by mkdir, touch, cp, or mv
	KindRemoved = "removed" // Path was removed by rm or moved away by mv
	KindGrepped = "grepped" // grep printed a matching line from Path
)

// Objective is one step of a mission.
type Objective struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Kind     string   `json:"kind"`
	Command  string   `json:"command,omitempty"`
	Path     string   `json:"path,omitempty"`
	Requires []string `json:"requires,omitempty"`
	Success  string   `json:"success"`
	Hints    []string `json:"hints"`
}

// Mission is a playable scenario.
type Mission struct {
	ID         string            `json:"id"`
	Order      int               `json:"order"`
	Title      string            `json:"title"`
	Difficulty string            `json:"difficulty"`
	XP         int               `json:"xp"`
	Story      string            `json:"story"`
	Home       string            `json:"home"`
	StartDir   string            `json:"start_dir"`
	Files      map[string]string `json:"files"`
	Objectives []Objective       `json:"objectives"`
}

// Load reads every *.json mission from fsys and returns them ordered.
func Load(fsys fs.FS) ([]Mission, error) {
	entries, err := fs.Glob(fsys, "*.json")
	if err != nil {
		return nil, err
	}
	var out []Mission
	seen := map[string]bool{}
	for _, name := range entries {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		var m Mission
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := m.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if seen[m.ID] {
			return nil, fmt.Errorf("%s: duplicate mission id %q", name, m.ID)
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

func (m *Mission) validate() error {
	if m.ID == "" || m.Title == "" || m.XP <= 0 || len(m.Objectives) == 0 {
		return fmt.Errorf("mission needs id, title, positive xp, and objectives")
	}
	if m.Home == "" {
		m.Home = "/home/player"
	}
	if m.StartDir == "" {
		m.StartDir = m.Home
	}
	ids := map[string]bool{}
	for _, o := range m.Objectives {
		if o.ID == "" || ids[o.ID] {
			return fmt.Errorf("objective id %q missing or duplicated", o.ID)
		}
		ids[o.ID] = true
		switch o.Kind {
		case KindRan:
			if o.Command == "" {
				return fmt.Errorf("objective %q: kind ran needs command", o.ID)
			}
		case KindListed, KindRead, KindEntered, KindFound, KindCreated, KindRemoved, KindGrepped:
			if o.Path == "" {
				return fmt.Errorf("objective %q: kind %s needs path", o.ID, o.Kind)
			}
		default:
			return fmt.Errorf("objective %q: unknown kind %q", o.ID, o.Kind)
		}
		for _, r := range o.Requires {
			if !ids[r] {
				return fmt.Errorf("objective %q requires unknown or later objective %q", o.ID, r)
			}
		}
	}
	return nil
}

// BuildFS creates a fresh virtual filesystem for this mission. Every call
// returns an independent copy, so sessions never share state.
func (m *Mission) BuildFS() (*filesystem.FS, error) {
	vfs := filesystem.New(m.Home)
	if err := vfs.MkdirAll(m.Home); err != nil {
		return nil, err
	}
	for p, content := range m.Files {
		if err := vfs.WriteFile(p, content); err != nil {
			return nil, fmt.Errorf("mission %s: %s: %w", m.ID, p, err)
		}
	}
	return vfs, nil
}

// Run tracks a player's progress through one attempt at a mission.
type Run struct {
	Mission *Mission
	done    map[string]bool
	hints   map[string]int
}

// NewRun starts a fresh attempt.
func NewRun(m *Mission) *Run {
	return &Run{Mission: m, done: map[string]bool{}, hints: map[string]int{}}
}

// IsDone reports whether an objective is complete.
func (r *Run) IsDone(id string) bool { return r.done[id] }

// DoneCount returns how many objectives are complete.
func (r *Run) DoneCount() int { return len(r.done) }

// Complete reports whether every objective is complete.
func (r *Run) Complete() bool { return len(r.done) == len(r.Mission.Objectives) }

func (r *Run) unlocked(o Objective) bool {
	for _, req := range o.Requires {
		if !r.done[req] {
			return false
		}
	}
	return true
}

func matches(o Objective, res simulator.Result) bool {
	if !res.OK && o.Kind != KindFound && o.Kind != KindRead && o.Kind != KindListed && o.Kind != KindCreated && o.Kind != KindRemoved && o.Kind != KindGrepped {
		return false
	}
	switch o.Kind {
	case KindRan:
		return res.Known && res.OK && res.Command == o.Command
	case KindListed:
		return contains(res.Listed, o.Path)
	case KindRead:
		return contains(res.Read, o.Path)
	case KindEntered:
		return res.Command == "cd" && res.OK && res.CwdAfter == o.Path
	case KindFound:
		return res.Command == "find" && contains(res.Matches, o.Path)
	case KindCreated:
		return contains(res.Created, o.Path)
	case KindRemoved:
		return contains(res.Removed, o.Path)
	case KindGrepped:
		return res.Command == "grep" && contains(res.Grepped, o.Path)
	}
	return false
}

func contains(list []string, p string) bool {
	for _, x := range list {
		if x == p {
			return true
		}
	}
	return false
}

// Evaluate applies a command result and returns the objectives newly completed
// by it, in mission order.
func (r *Run) Evaluate(res simulator.Result) []Objective {
	var newly []Objective
	for _, o := range r.Mission.Objectives {
		if r.done[o.ID] || !r.unlocked(o) {
			continue
		}
		if matches(o, res) {
			r.done[o.ID] = true
			newly = append(newly, o)
		}
	}
	return newly
}

// NextHint returns the next progressive hint for the first unfinished,
// unlocked objective. level/total describe how far into that objective's
// hints the player is. ok is false when nothing is left to hint at.
func (r *Run) NextHint() (text string, level, total int, ok bool) {
	for _, o := range r.Mission.Objectives {
		if r.done[o.ID] || !r.unlocked(o) || len(o.Hints) == 0 {
			continue
		}
		i := r.hints[o.ID]
		if i >= len(o.Hints) {
			i = len(o.Hints) - 1
		} else {
			r.hints[o.ID]++
		}
		return o.Hints[i], i + 1, len(o.Hints), true
	}
	return "", 0, 0, false
}
