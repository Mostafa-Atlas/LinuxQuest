// Package progress tracks XP and completed missions for a single SSH session.
// Progress is intentionally in-memory only: closing the connection discards it.
package progress

import (
	"sort"
	"sync"
)

// Progress is one player's session progress. It is safe for concurrent use.
type Progress struct {
	mu        sync.Mutex
	xp        int
	completed map[string]int // mission ID -> number of completions
}

// New returns empty progress.
func New() *Progress { return &Progress{completed: map[string]int{}} }

// Complete records a mission completion. XP is awarded only the first time a
// mission is completed in this session; replays are recorded but give 0 XP,
// so a mission cannot be farmed. It returns the XP awarded and whether this
// was the first completion.
func (p *Progress) Complete(id string, xp int) (awarded int, first bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	first = p.completed[id] == 0
	p.completed[id]++
	if first {
		p.xp += xp
		return xp, true
	}
	return 0, false
}

// XP returns total XP earned this session.
func (p *Progress) XP() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.xp
}

// IsCompleted reports whether a mission was completed at least once.
func (p *Progress) IsCompleted(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.completed[id] > 0
}

// Completions returns how many times a mission has been completed.
func (p *Progress) Completions(id string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.completed[id]
}

// Restore seeds progress from durable storage (e.g. the account database)
// when a session starts, so previously earned XP and completions show up.
func (p *Progress) Restore(xp int, completed map[string]int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.xp = xp
	p.completed = map[string]int{}
	for id, n := range completed {
		if n > 0 {
			p.completed[id] = n
		}
	}
}

// ForceComplete records a completion whose XP decision was already made by
// durable storage (award is the XP granted, 0 on replays).
func (p *Progress) ForceComplete(id string, award int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completed[id]++
	p.xp += award
}

// CompletedIDs returns the IDs of completed missions, sorted.
func (p *Progress) CompletedIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, 0, len(p.completed))
	for id := range p.completed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
