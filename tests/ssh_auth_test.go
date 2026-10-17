package tests

import (
	"crypto/ed25519"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"linuxquest/internal/missions"
	"linuxquest/internal/sshserver"
	"linuxquest/internal/store"
	missionsdata "linuxquest/missions"
)

// startServerWithStore starts a server with key accounts backed by a
// temporary SQLite database.
func startServerWithStore(t *testing.T, max int, admins map[string]bool) string {
	t.Helper()
	catalog, err := missions.Load(missionsdata.FS)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hostKey := filepath.Join(t.TempDir(), "keys", "host_ed25519")
	srv, err := sshserver.New(sshserver.Config{
		Host: "127.0.0.1", Port: "0", HostKeyPath: hostKey,
		MaxSessions: max, IdleTimeout: time.Minute, MaxDuration: 5 * time.Minute,
		Store: db, Admins: admins,
	}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return l.Addr().String()
}

// testSigner makes a deterministic Ed25519 signer plus its authorized-key line.
func testSigner(t *testing.T, seed byte) (gossh.Signer, string) {
	t.Helper()
	var s [32]byte
	for i := range s {
		s[i] = seed + byte(i)
	}
	signer, err := gossh.NewSignerFromKey(ed25519.NewKeyFromSeed(s[:]))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(signer.PublicKey())))
	return signer, line
}

// dialKey connects with a username and an SSH key.
func dialKey(t *testing.T, addr, user string, signer gossh.Signer, pty bool) *client {
	t.Helper()
	cfg := &gossh.ClientConfig{
		User:            user,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	conn, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, sess: s, close: func() { s.Close(); conn.Close() }}
	t.Cleanup(c.close)
	if pty {
		if err := s.RequestPty("xterm-256color", 30, 100, gossh.TerminalModes{}); err != nil {
			t.Fatalf("pty: %v", err)
		}
	}
	c.in, _ = s.StdinPipe()
	s.Stdout = &lockedWriter{c}
	s.Stderr = &lockedWriter{c}
	if err := s.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	return c
}

func TestKeysRegisterLoginAndPersist(t *testing.T) {
	addr := startServerWithStore(t, 10, nil)
	signer, _ := testSigner(t, 21)

	// Unknown user self-registers with the presented key.
	c := dialKey(t, addr, "neo", signer, true)
	c.waitFor("Account created for neo")
	c.send("\r")
	c.waitFor("Welcome to the Terminal")
	c.send("1")
	c.waitFor("Objectives:")
	c.run("pwd", "/home/player")
	c.run("ls", "welcome.txt")
	c.run("cat welcome.txt", "Mission complete!")
	c.waitFor("+50 XP")
	c.send("m")
	c.waitFor("Missions")
	c.send("1")
	c.waitFor("Objectives:")
	c.run("stats", "Total XP: 50")
	if !strings.Contains(c.screen(), "neo@linuxquest") {
		t.Errorf("prompt does not show account name:\n%s", c.screen())
	}
	// Second device: register another key from inside the game.
	signer2, line2 := testSigner(t, 22)
	c.run("keyadd "+line2+" laptop", "Key added")
	c.run("keys", "SHA256:")
	c.close()

	// Second key logs in as the same account with history intact.
	c2 := dialKey(t, addr, "neo", signer2, true)
	c2.waitFor("Logged in as neo")
	c2.send("\r")
	c2.waitFor("XP: 50")
	c2.waitFor("[x]")
	c2.send("x")

	// Same username with an unknown key is rejected.
	other, _ := testSigner(t, 23)
	bad := &gossh.ClientConfig{
		User:            "neo",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(other)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	if _, err := gossh.Dial("tcp", addr, bad); err == nil {
		t.Fatal("unknown key for a known user was accepted")
	}
}

func TestAdminCommandRestricted(t *testing.T) {
	addr := startServerWithStore(t, 10, nil)

	// First registered account is automatically admin.
	adminSigner, _ := testSigner(t, 31)
	a := dialKey(t, addr, "root", adminSigner, true)
	a.waitFor("You are an admin")
	a.send("\r")
	a.waitFor("Welcome to the Terminal")
	a.send("1")
	a.waitFor("Objectives:")
	a.run("admin", "Registered players")

	// Second account is an ordinary player.
	guestSigner, _ := testSigner(t, 32)
	b := dialKey(t, addr, "guest", guestSigner, true)
	b.send("\r")
	b.waitFor("Welcome to the Terminal")
	b.send("1")
	b.waitFor("Objectives:")
	b.run("admin", "admin accounts only")
	b.run("top", "Top players by XP")
}
