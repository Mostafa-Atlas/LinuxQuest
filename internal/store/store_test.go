package store

import (
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestFirstKeyIsAdmin(t *testing.T) {
	s := testStore(t)
	_, pub1 := TestKey(1)
	acct, isNew, err := s.RegisterOrLoginKey("neo", pub1, nil)
	if err != nil || !isNew {
		t.Fatalf("register: %v new=%v", err, isNew)
	}
	if !acct.IsAdmin {
		t.Fatal("first account should be admin")
	}
	_, pub2 := TestKey(2)
	acct2, isNew, err := s.RegisterOrLoginKey("trinity", pub2, nil)
	if err != nil || !isNew {
		t.Fatalf("register2: %v new=%v", err, isNew)
	}
	if acct2.IsAdmin {
		t.Fatal("second account should not be admin")
	}
}

func TestKeyLoginAndMismatch(t *testing.T) {
	s := testStore(t)
	_, pub := TestKey(3)
	if _, _, err := s.RegisterOrLoginKey("neo", pub, nil); err != nil {
		t.Fatal(err)
	}
	acct, isNew, err := s.RegisterOrLoginKey("neo", pub, nil)
	if err != nil || isNew || acct.Username != "neo" {
		t.Fatalf("login: %v new=%v %+v", err, isNew, acct)
	}
	// Same username, different key.
	_, other := TestKey(4)
	if _, _, err := s.RegisterOrLoginKey("neo", other, nil); err != ErrKeyMismatch {
		t.Fatalf("want ErrKeyMismatch, got %v", err)
	}
	// Same key, different username.
	if _, _, err := s.RegisterOrLoginKey("impostor", pub, nil); err != ErrKeyMismatch {
		t.Fatalf("want ErrKeyMismatch, got %v", err)
	}
}

func TestBadNamesAndKeysRejected(t *testing.T) {
	s := testStore(t)
	_, pub := TestKey(5)
	for _, u := range []string{"", "has space", "semi;colon", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if _, _, err := s.RegisterOrLoginKey(u, pub, nil); err != ErrBadUsername {
			t.Fatalf("user %q: want ErrBadUsername, got %v", u, err)
		}
	}
	for _, k := range []string{"", "not-a-key", "ssh-ed25519 !!!not-base64!!!", "ssh-rsa x"} {
		if _, _, err := s.RegisterOrLoginKey("neo", k, nil); err != ErrBadKey {
			t.Fatalf("key %q: want ErrBadKey, got %v", k, err)
		}
	}
}

func TestSecondKeyAndRemoval(t *testing.T) {
	s := testStore(t)
	_, pub1 := TestKey(6)
	acct, _, err := s.RegisterOrLoginKey("neo", pub1, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, pub2 := TestKey(7)
	if err := s.AddKey(acct.ID, pub2); err != nil {
		t.Fatalf("add second key: %v", err)
	}
	// Login works with either key.
	if _, isNew, err := s.RegisterOrLoginKey("neo", pub2, nil); err != nil || isNew {
		t.Fatalf("login with second key: %v new=%v", err, isNew)
	}
	if err := s.AddKey(acct.ID, pub2); err != ErrKeyExists {
		t.Fatalf("duplicate key: want ErrKeyExists, got %v", err)
	}
	keys, err := s.ListKeys(acct.ID)
	if err != nil || len(keys) != 2 {
		t.Fatalf("list keys: %v %v", err, keys)
	}
	fp, err := Fingerprint(pub1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveKey(acct.ID, fp[:16]); err != nil {
		t.Fatalf("remove key: %v", err)
	}
	if _, _, err := s.RegisterOrLoginKey("neo", pub1, nil); err != ErrKeyMismatch {
		t.Fatalf("removed key should no longer log in, got %v", err)
	}
	// Cannot remove the only remaining key.
	fp2, _ := Fingerprint(pub2)
	if err := s.RemoveKey(acct.ID, fp2[:16]); err == nil {
		t.Fatal("removing last key should fail")
	}
}

func TestCompletionXPOnce(t *testing.T) {
	s := testStore(t)
	_, pub := TestKey(8)
	acct, _, _ := s.RegisterOrLoginKey("neo", pub, nil)
	awarded, first, err := s.RecordCompletion(acct.ID, "welcome", 50)
	if err != nil || awarded != 50 || !first {
		t.Fatalf("first: %v %d %v", err, awarded, first)
	}
	awarded, first, err = s.RecordCompletion(acct.ID, "welcome", 50)
	if err != nil || awarded != 0 || first {
		t.Fatalf("replay: %v %d %v", err, awarded, first)
	}
	xp, completed, err := s.LoadProgress(acct.ID)
	if err != nil || xp != 50 || completed["welcome"] != 2 {
		t.Fatalf("progress: %v xp=%d %v", err, xp, completed)
	}
}

func TestStatsAndLeaderboard(t *testing.T) {
	s := testStore(t)
	_, pubA := TestKey(9)
	_, pubB := TestKey(10)
	a, _, _ := s.RegisterOrLoginKey("a", pubA, nil)
	b, _, _ := s.RegisterOrLoginKey("b", pubB, nil)
	if _, _, err := s.RecordCompletion(a.ID, "welcome", 50); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RecordCompletion(b.ID, "welcome", 50); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RecordCompletion(b.ID, "missing-clue", 75); err != nil {
		t.Fatal(err)
	}
	s.AddCommands(a.ID, 10)
	s.AddHints(a.ID, 2)
	s.RecordLogin(a.ID)
	st, err := s.PersonalStats(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalXP != 50 || st.MissionsCompleted != 1 || st.Commands != 10 || st.HintsUsed != 2 || st.Logins != 1 {
		t.Fatalf("stats: %+v", st)
	}
	top, err := s.Leaderboard(10)
	if err != nil || len(top) != 2 || top[0].Username != "b" || top[0].XP != 125 {
		t.Fatalf("leaderboard: %v %+v", err, top)
	}
	g, err := s.GlobalStats()
	if err != nil || g.Users != 2 || g.TotalCompletions != 3 || g.TotalXPAwarded != 175 || g.TotalCommands != 10 {
		t.Fatalf("global: %v %+v", err, g)
	}
}

func TestPasswordEraMigrates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the password-era schema, then reopen.
	if _, err := s.db.Exec(`DROP TABLE user_keys`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after password era: %v", err)
	}
	defer s2.Close()
	_, pub := TestKey(11)
	if _, isNew, err := s2.RegisterOrLoginKey("neo", pub, nil); err != nil || !isNew {
		t.Fatalf("fresh registration after migration: %v new=%v", err, isNew)
	}
}

func TestBanBlocksLogin(t *testing.T) {
	s := testStore(t)
	_, pub := TestKey(20)
	acct, _, err := s.RegisterOrLoginKey("neo", pub, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetBanned("neo", true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RegisterOrLoginKey("neo", pub, nil); err != ErrBanned {
		t.Fatalf("banned login: want ErrBanned, got %v", err)
	}
	_ = acct
	if err := s.SetBanned("neo", false); err != nil {
		t.Fatal(err)
	}
	if _, isNew, err := s.RegisterOrLoginKey("neo", pub, nil); err != nil || isNew {
		t.Fatalf("reinstated login: %v new=%v", err, isNew)
	}
	if err := s.SetBanned("ghost", true); err == nil {
		t.Fatal("banning unknown user should fail")
	}
}

func TestUserManagement(t *testing.T) {
	s := testStore(t)
	_, pubA := TestKey(21)
	_, pubB := TestKey(22)
	a, _, _ := s.RegisterOrLoginKey("alpha", pubA, nil)
	b, _, _ := s.RegisterOrLoginKey("beta", pubB, nil)
	if _, _, err := s.RecordCompletion(a.ID, "welcome", 50); err != nil {
		t.Fatal(err)
	}
	users, err := s.ListUsers()
	if err != nil || len(users) != 2 {
		t.Fatalf("list: %v %+v", err, users)
	}
	if users[0].Username != "alpha" || users[0].XP != 50 || !users[0].IsAdmin {
		t.Fatalf("first row: %+v", users[0])
	}
	if err := s.SetAdmin("beta", true); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetProgress("alpha"); err != nil {
		t.Fatal(err)
	}
	xp, completed, err := s.LoadProgress(a.ID)
	if err != nil || xp != 0 || len(completed) != 0 {
		t.Fatalf("after reset: %v xp=%d %v", err, xp, completed)
	}
	_ = b
	users, _ = s.ListUsers()
	if users[1].XP != 0 || !users[1].IsAdmin {
		t.Fatalf("after changes: %+v", users[1])
	}
	if err := s.ResetProgress("ghost"); err == nil {
		t.Fatal("resetting unknown user should fail")
	}
}
