package missions

import (
	"strings"
	"testing"
	"testing/fstest"

	missionsdata "linuxquest/missions"
)

func TestShippedMissionsLoad(t *testing.T) {
	ms, err := Load(missionsdata.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 30 {
		t.Fatalf("expected 30 missions in 3 tiers of 10, got %d", len(ms))
	}
	tiers := map[string]int{}
	for _, m := range ms {
		tiers[m.Difficulty]++
	}
	for _, want := range []struct {
		d string
		n int
	}{{"Beginner", 10}, {"Intermediate", 10}, {"Advanced", 10}} {
		if tiers[want.d] != want.n {
			t.Errorf("tier %s: want %d missions, got %d", want.d, want.n, tiers[want.d])
		}
	}
	if ms[0].ID != "welcome" || ms[0].XP != 50 || len(ms[0].Objectives) != 3 {
		t.Errorf("mission 1: %+v", ms[0])
	}
	if ms[1].ID != "missing-clue" || ms[1].XP != 75 || len(ms[1].Objectives) != 4 {
		t.Errorf("mission 2: %+v", ms[1])
	}
	for _, m := range ms {
		if _, err := m.BuildFS(); err != nil {
			t.Errorf("%s: %v", m.ID, err)
		}
		for _, o := range m.Objectives {
			if len(o.Hints) == 0 || o.Success == "" || o.Text == "" {
				t.Errorf("%s/%s: missing hints, success, or text", m.ID, o.ID)
			}
		}
	}
}

func TestLoadRejectsBadMissions(t *testing.T) {
	bad := map[string]string{
		"not json":         `{`,
		"no objectives":    `{"id":"a","title":"A","xp":1,"objectives":[]}`,
		"unknown kind":     `{"id":"a","title":"A","xp":1,"objectives":[{"id":"o","kind":"teleport"}]}`,
		"ran w/o command":  `{"id":"a","title":"A","xp":1,"objectives":[{"id":"o","kind":"ran"}]}`,
		"read w/o path":    `{"id":"a","title":"A","xp":1,"objectives":[{"id":"o","kind":"read"}]}`,
		"bad requires":     `{"id":"a","title":"A","xp":1,"objectives":[{"id":"o","kind":"ran","command":"pwd","requires":["x"]}]}`,
		"zero xp":          `{"id":"a","title":"A","xp":0,"objectives":[{"id":"o","kind":"ran","command":"pwd"}]}`,
		"duplicate obj id": `{"id":"a","title":"A","xp":1,"objectives":[{"id":"o","kind":"ran","command":"pwd"},{"id":"o","kind":"ran","command":"ls"}]}`,
	}
	for name, body := range bad {
		if _, err := Load(fstest.MapFS{"x.json": {Data: []byte(body)}}); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	dup := `{"id":"a","title":"A","xp":1,"objectives":[{"id":"o","kind":"ran","command":"pwd"}]}`
	if _, err := Load(fstest.MapFS{"1.json": {Data: []byte(dup)}, "2.json": {Data: []byte(dup)}}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate ids: %v", err)
	}
}

func TestBuildFSIsFreshEachTime(t *testing.T) {
	ms, _ := Load(missionsdata.FS)
	a, _ := ms[0].BuildFS()
	b, _ := ms[0].BuildFS()
	_ = a.WriteFile("/home/player/extra.txt", "x")
	if _, err := b.Stat("/home/player/extra.txt"); err == nil {
		t.Error("BuildFS shares state between calls")
	}
}
