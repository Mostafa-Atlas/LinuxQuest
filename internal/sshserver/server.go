// Package sshserver exposes LinuxQuest over SSH using Charm's Wish.
//
// Players never get a shell. Each connection runs an isolated Bubble Tea
// program that talks to an in-memory simulator. Authentication is SSH public
// keys only: there are no passwords.
package sshserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	gossh "golang.org/x/crypto/ssh"

	"linuxquest/internal/missions"
	"linuxquest/internal/session"
	"linuxquest/internal/store"
	"linuxquest/internal/terminal"
)

// ctxKey is the context key carrying the authenticated account from the
// password handler to the session handler.
type ctxKey struct{}

// authResult is what password auth resolves: the account plus whether it was
// just registered.
type authResult struct {
	acct  *store.Account
	isNew bool
}

// Config controls the SSH server.
type Config struct {
	Host        string
	Port        string
	HostKeyPath string
	MaxSessions int           // concurrent sessions allowed (0 = unlimited)
	IdleTimeout time.Duration // disconnect after this much inactivity (0 = none)
	MaxDuration time.Duration // hard cap on session length (0 = none)

	// Store enables key accounts with self-registration. When nil the server
	// accepts anyone without credentials (original MVP behaviour) and sessions
	// are unbound and in-memory only.
	Store *store.Store
	// Admins marks usernames that become admins on register or next login.
	Admins map[string]bool
}

// Server wraps a Wish SSH server.
type Server struct {
	srv *ssh.Server
}

// New builds a server. catalog is shared read-only; every session copies what it needs.
func New(cfg Config, catalog []missions.Mission) (*Server, error) {
	if cfg.HostKeyPath == "" {
		return nil, errors.New("host key path is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.HostKeyPath), 0o700); err != nil {
		return nil, fmt.Errorf("create host key directory: %w", err)
	}

	handler := func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		// Each connection gets its own session state: nothing is shared between players.
		var sess *session.Session
		if ar, ok := s.Context().Value(ctxKey{}).(authResult); ok && ar.acct != nil {
			sess = session.NewForAccount(catalog, cfg.Store, ar.acct, ar.isNew)
		} else {
			sess = session.New(catalog)
		}
		model := terminal.New(sess, terminal.NewStyles(bm.MakeRenderer(s)))
		return model, append(bm.MakeOptions(s), tea.WithAltScreen())
	}

	opts := []ssh.Option{
		wish.WithAddress(net.JoinHostPort(cfg.Host, cfg.Port)),
		wish.WithHostKeyPath(cfg.HostKeyPath),
		// Order matters: the last middleware listed runs first.
		wish.WithMiddleware(
			bm.Middleware(handler),
			activeterm.Middleware(), // require an interactive terminal (ssh -t is implied by a normal login)
			limitSessions(cfg.MaxSessions),
			logging.Middleware(), // logs connect/disconnect only, never keystrokes
		),
	}
	if cfg.Store != nil {
		opts = append(opts, wish.WithPublicKeyAuth(publicKeyAuth(cfg)))
	}
	if cfg.IdleTimeout > 0 {
		opts = append(opts, wish.WithIdleTimeout(cfg.IdleTimeout))
	}
	if cfg.MaxDuration > 0 {
		opts = append(opts, wish.WithMaxTimeout(cfg.MaxDuration))
	}
	srv, err := wish.NewServer(opts...)
	if err != nil {
		return nil, err
	}
	return &Server{srv: srv}, nil
}

// publicKeyAuth implements self-registration with keys: an unknown username
// creates an account owning the presented key; a known username must present
// one of its keys. Rejections never say which half mismatched.
func publicKeyAuth(cfg Config) func(ssh.Context, ssh.PublicKey) bool {
	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		username := ctx.User()
		keyLine := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key)))
		acct, isNew, err := cfg.Store.RegisterOrLoginKey(username, keyLine, cfg.Admins)
		if err != nil {
			log.Warn("rejected login", "user", username, "err", err)
			return false
		}
		ctx.SetValue(ctxKey{}, authResult{acct: acct, isNew: isNew})
		if isNew {
			log.Info("registered account", "user", username, "admin", acct.IsAdmin)
		}
		return true
	}
}

// limitSessions caps concurrent sessions so a flood of connections cannot exhaust memory.
func limitSessions(max int) wish.Middleware {
	if max <= 0 {
		return func(next ssh.Handler) ssh.Handler { return next }
	}
	slots := make(chan struct{}, max)
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
				next(s)
			default:
				wish.Fatalln(s, "LinuxQuest is full right now. Please try again in a moment.")
			}
		}
	}
}

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.srv.Addr }

// ListenAndServe blocks until the server stops.
func (s *Server) ListenAndServe() error { return s.srv.ListenAndServe() }

// Serve accepts connections on an existing listener (used by tests).
func (s *Server) Serve(l net.Listener) error { return s.srv.Serve(l) }

// Shutdown stops accepting connections and waits for sessions to end.
func (s *Server) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }

// Close stops the server immediately.
func (s *Server) Close() error { return s.srv.Close() }

func init() { log.SetReportTimestamp(true) }
