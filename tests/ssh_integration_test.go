// Package tests contains end-to-end tests that talk to a real SSH server.
package tests

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"linuxquest/internal/missions"
	"linuxquest/internal/sshserver"
	missionsdata "linuxquest/missions"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b[()][A-Z0-9]`)

type client struct {
	t     *testing.T
	sess  *gossh.Session
	in    io.WriteCloser
	mu    sync.Mutex
	out   bytes.Buffer
	mark  int // bytes of output already seen when the last input was sent
	close func()
}

func startServer(t *testing.T, max int) (addr, hostKey string) {
	t.Helper()
	catalog, err := missions.Load(missionsdata.FS)
	if err != nil {
		t.Fatal(err)
	}
	hostKey = filepath.Join(t.TempDir(), "keys", "host_ed25519")
	srv, err := sshserver.New(sshserver.Config{
		Host: "127.0.0.1", Port: "0", HostKeyPath: hostKey,
		MaxSessions: max, IdleTimeout: time.Minute, MaxDuration: 5 * time.Minute,
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
	return l.Addr().String(), hostKey
}

func dial(t *testing.T, addr string, pty bool) *client {
	t.Helper()
	cfg := &gossh.ClientConfig{
		User:            "player",
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

type lockedWriter struct{ c *client }

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	return w.c.out.Write(p)
}

// screen returns all output so far, ANSI-stripped.
func (c *client) screen() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ansi.ReplaceAllString(c.out.String(), "")
}

// fresh returns only output produced since the last send.
func (c *client) fresh() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ansi.ReplaceAllString(string(c.out.Bytes()[c.mark:]), "")
}

// waitFor blocks until the (ANSI-stripped) output contains want.
func (c *client) waitFor(want string) {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(c.fresh(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatalf("timed out waiting for %q in new output; got:\n%s", want, c.fresh())
}

func (c *client) send(s string) {
	c.t.Helper()
	c.mu.Lock()
	c.mark = c.out.Len()
	c.mu.Unlock()
	if _, err := io.WriteString(c.in, s); err != nil {
		c.t.Fatalf("send: %v", err)
	}
}

// run types a command and waits for the given output.
func (c *client) run(cmd, want string) {
	c.t.Helper()
	c.send(cmd + "\r")
	c.waitFor(want)
}

func TestPlayBothMissionsOverSSH(t *testing.T) {
	addr, _ := startServer(t, 10)
	c := dial(t, addr, true)

	c.waitFor("safe, simulated Linux terminal")
	c.send("\r")
	c.waitFor("Welcome to the Terminal")
	c.waitFor("Find the Missing Clue")
	c.send("1")
	c.waitFor("Objectives:")
	c.run("pwd", "/home/player")
	c.run("ls", "welcome.txt")
	c.run("cat welcome.txt", "Mission complete!")
	c.waitFor("+50 XP")

	c.send("m") // back to menu
	c.waitFor("[x]")
	c.send("2")
	c.waitFor("Find the Missing Clue")
	c.run("cd documents", "(1/4)")
	c.run("cd ..", "(2/4)")
	c.run("find . -name clue.txt", "./secrets/clue.txt")
	c.run("cat secrets/clue.txt", "BLUE-LANTERN")
	c.waitFor("+75 XP")
	c.waitFor("Total XP this session: 125")
	c.send("q")
	c.waitFor("Session summary")
	c.waitFor("XP earned:         125")
	c.send("x")
}

func TestSimultaneousSessionsAreIsolated(t *testing.T) {
	addr, _ := startServer(t, 10)
	a, b := dial(t, addr, true), dial(t, addr, true)
	for _, c := range []*client{a, b} {
		c.waitFor("safe, simulated Linux terminal")
		c.send("\r")
		c.waitFor("Find the Missing Clue")
		c.send("2")
		c.waitFor("Objectives:")
	}
	a.run("cd documents", "~/documents$")
	// B must still be in the home directory and have no progress.
	b.run("pwd", "/home/player")
	if strings.Contains(b.screen(), "(1/4)") || strings.Contains(b.screen(), "~/documents$") {
		t.Errorf("session B saw A's state:\n%s", b.screen())
	}
	a.run("cd ..", "(2/4)")
	b.run("cd secrets", "~/secrets$")
	a.run("pwd", "/home/player")
	if strings.Contains(a.screen(), "~/secrets$") {
		t.Errorf("session A saw B's directory:\n%s", a.screen())
	}
}

func TestHostIsNeverExposed(t *testing.T) {
	addr, _ := startServer(t, 10)
	c := dial(t, addr, true)
	c.waitFor("safe, simulated Linux terminal")
	c.send("\r")
	c.send("1")
	c.waitFor("Objectives:")
	hostUser := os.Getenv("USER")
	for _, cmd := range []string{
		"cat /etc/passwd", "cat /proc/self/environ", "ls /", "whoami", "id", "env", "cd /; ls",
		"cat ../../../../etc/passwd", "$(id)", "`id`", "sh -c id", "ls | cat", "find / -name passwd",
	} {
		c.send(cmd + "\r")
	}
	c.run("pwd", "/home/player")
	time.Sleep(200 * time.Millisecond)
	s := c.screen()
	for _, bad := range []string{"root:x:0:0", "uid=", "PATH=", "/bin/bash", "HOME=/root", "nologin"} {
		if strings.Contains(s, bad) {
			t.Errorf("host data %q leaked:\n%s", bad, s)
		}
	}
	if hostUser != "" && hostUser != "player" && strings.Contains(s, "/home/"+hostUser) {
		t.Errorf("host username leaked")
	}
	if !strings.Contains(s, "command not found") || !strings.Contains(s, "are not") || !strings.Contains(s, "supported in this simulator") {
		t.Errorf("expected rejection messages:\n%s", s)
	}
}

func TestRejectsNonInteractiveSessions(t *testing.T) {
	addr, _ := startServer(t, 10)
	c := dial(t, addr, false) // no PTY
	c.waitFor("Requires an active PTY")
	// An exec request must never run anything either.
	conn, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{User: "player", HostKeyCallback: gossh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s, _ := conn.NewSession()
	defer s.Close()
	out, _ := s.CombinedOutput("id")
	if strings.Contains(string(out), "uid=") {
		t.Errorf("exec ran on host: %s", out)
	}
}

func TestForwardingAndSubsystemsAreDisabled(t *testing.T) {
	addr, _ := startServer(t, 10)
	// A local service the player might try to reach through the server.
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	conn, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{User: "player", HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if c, err := conn.Dial("tcp", target.Addr().String()); err == nil {
		c.Close()
		t.Error("direct-tcpip port forwarding must be refused")
	}
	if l, err := conn.Listen("tcp", "127.0.0.1:0"); err == nil {
		l.Close()
		t.Error("remote port forwarding must be refused")
	}
	s, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RequestSubsystem("sftp"); err == nil {
		t.Error("sftp subsystem must not be available")
	}
}

func TestDisconnectIsHandledGracefully(t *testing.T) {
	addr, _ := startServer(t, 10)
	for i := 0; i < 5; i++ {
		c := dial(t, addr, true)
		c.waitFor("safe, simulated Linux terminal")
		c.send("\r1")
		c.close() // abrupt disconnect mid-mission
	}
	// Server still serves new players afterwards.
	c := dial(t, addr, true)
	c.waitFor("safe, simulated Linux terminal")
}

func TestSessionLimit(t *testing.T) {
	addr, _ := startServer(t, 1)
	first := dial(t, addr, true)
	first.waitFor("safe, simulated Linux terminal")
	second := dial(t, addr, true)
	second.waitFor("is full right now")
	first.close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c := dial(t, addr, true)
		time.Sleep(300 * time.Millisecond)
		if strings.Contains(c.screen(), "safe, simulated Linux terminal") {
			return
		}
		c.close()
	}
	t.Error("slot was never released after the first session closed")
}

func TestHostKeyPersists(t *testing.T) {
	catalog, _ := missions.Load(missionsdata.FS)
	path := filepath.Join(t.TempDir(), "nested", "key")
	fingerprint := func() string {
		srv, err := sshserver.New(sshserver.Config{Host: "127.0.0.1", Port: "0", HostKeyPath: path}, catalog)
		if err != nil {
			t.Fatal(err)
		}
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		go srv.Serve(l)
		defer srv.Close()
		var fp string
		cfg := &gossh.ClientConfig{User: "p", Timeout: 3 * time.Second, HostKeyCallback: func(_ string, _ net.Addr, k gossh.PublicKey) error {
			fp = gossh.FingerprintSHA256(k)
			return nil
		}}
		conn, err := gossh.Dial("tcp", l.Addr().String(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
		return fp
	}
	first, second := fingerprint(), fingerprint()
	if first == "" || first != second {
		t.Errorf("host key changed across restarts: %q vs %q", first, second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("host key is group/world accessible: %v", info.Mode())
	}
	_ = fmt.Sprint()
}
