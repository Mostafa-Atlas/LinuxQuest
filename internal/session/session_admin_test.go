package session

import (
	"path/filepath"
	"strings"
	"testing"

	"linuxquest/internal/missions"
	"linuxquest/internal/store"
)

func adminTestSetup(t *testing.T) (*Session, *Session, *store.Store) {
	t.Helper()
	catalog := []missions.Mission{
		{ID: "m1", Title: "First", Difficulty: "Beginner", XP: 50, Home: "/home/player", StartDir: "/home/player",
			Files:      map[string]string{"/home/player/a.txt": "a"},
			Objectives: []missions.Objective{{ID: "o1", Text: "read it", Kind: "read", Path: "/home/player/a.txt", Success: "ok", Hints: []string{"h"}}}},
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, pubA := store.TestKey(40)
	_, pubB := store.TestKey(41)
	acctA, _, err := db.RegisterOrLoginKey("boss", pubA, nil)
	if err != nil {
		t.Fatal(err)
	}
	acctB, _, err := db.RegisterOrLoginKey("guest", pubB, nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewForAccount(catalog, db, acctA, false), NewForAccount(catalog, db, acctB, false), db
}

func adminOut(s *Session, cmd string) string {
	var b strings.Builder
	for _, l := range s.Submit(cmd).Lines {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}

func TestAdminDashboardAndUsers(t *testing.T) {
	admin, player, _ := adminTestSetup(t)
	out := adminOut(admin, "admin")
	for _, want := range []string{"Registered players: 2", "admin users", "admin ban"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q:\n%s", want, out)
		}
	}
	out = adminOut(admin, "admin users")
	if !strings.Contains(out, "boss") || !strings.Contains(out, "guest") || !strings.Contains(out, "admin") {
		t.Errorf("users list wrong:\n%s", out)
	}
	out = adminOut(player, "admin")
	if !strings.Contains(out, "admin accounts only") {
		t.Errorf("non-admin should be refused:\n%s", out)
	}
	out = adminOut(player, "admin users")
	if !strings.Contains(out, "admin accounts only") {
		t.Errorf("non-admin subcommand should be refused:\n%s", out)
	}
}

func TestAdminInspectResetBanRole(t *testing.T) {
	admin, player, db := adminTestSetup(t)

	out := adminOut(admin, "admin inspect guest")
	if !strings.Contains(out, "guest (player, active)") || !strings.Contains(out, "SHA256:") {
		t.Errorf("inspect wrong:\n%s", out)
	}
	if out := adminOut(admin, "admin inspect ghost"); !strings.Contains(out, "No player named") {
		t.Errorf("inspect unknown:\n%s", out)
	}

	// Guest completes the mission, then gets reset.
	player.Start("m1")
	player.Submit("cat a.txt")
	if xp, _, _ := db.LoadProgress(player.account.ID); xp != 50 {
		t.Fatalf("setup completion failed")
	}
	out = adminOut(admin, "admin reset guest")
	if !strings.Contains(out, "wiped") {
		t.Errorf("reset:\n%s", out)
	}
	if xp, _, _ := db.LoadProgress(player.account.ID); xp != 0 {
		t.Errorf("reset did not wipe XP")
	}

	// Ban blocks login; unban restores.
	out = adminOut(admin, "admin ban guest")
	if !strings.Contains(out, "suspended") {
		t.Errorf("ban:\n%s", out)
	}
	_, pubB := store.TestKey(41)
	if _, _, err := db.RegisterOrLoginKey("guest", pubB, nil); err != store.ErrBanned {
		t.Errorf("banned key should fail: %v", err)
	}
	out = adminOut(admin, "admin unban guest")
	if !strings.Contains(out, "reinstated") {
		t.Errorf("unban:\n%s", out)
	}
	// A ban lands mid-session: the next command kicks the player out.
	if out := adminOut(admin, "admin ban guest"); !strings.Contains(out, "suspended") {
		t.Fatalf("re-ban:\n%s", out)
	}
	kicked := player.Submit("pwd")
	kickText := ""
	for _, l := range kicked.Lines {
		kickText += l.Text + "\n"
	}
	if !kicked.Quit || !strings.Contains(kickText, "suspended") {
		t.Errorf("banned player should be kicked: quit=%v:\n%s", kicked.Quit, kickText)
	}
	if out := adminOut(admin, "admin unban guest"); !strings.Contains(out, "reinstated") {
		t.Fatalf("final unban:\n%s", out)
	}

	// Promote then demote. Roles apply live, without reconnecting.
	out = adminOut(admin, "admin promote guest")
	if !strings.Contains(out, "now an admin") {
		t.Errorf("promote:\n%s", out)
	}
	player.Submit("stats") // any command refreshes the account snapshot
	if !player.IsAdmin() {
		t.Error("promotion did not apply to the live session")
	}
	out = adminOut(admin, "admin demote guest")
	if !strings.Contains(out, "no longer an admin") {
		t.Errorf("demote:\n%s", out)
	}

	// Self-harm guards.
	for cmd, want := range map[string]string{
		"admin ban boss":    "cannot ban yourself",
		"admin demote boss": "cannot demote yourself",
	} {
		if out := adminOut(admin, cmd); !strings.Contains(out, want) {
			t.Errorf("%s: want %q, got:\n%s", cmd, want, out)
		}
	}
	out = adminOut(admin, "admin frobnicate")
	if !strings.Contains(out, "Admin console:") {
		t.Errorf("unknown subcommand should show usage:\n%s", out)
	}
}
