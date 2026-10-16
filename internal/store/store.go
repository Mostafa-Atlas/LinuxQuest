// Package store persists LinuxQuest accounts, mission progress, and usage
// statistics in SQLite. It is the only package that touches durable state:
// sessions keep a fast in-memory copy and write through to the store, so a
// crash or disconnect loses at most the current command.
//
// Authentication is SSH public keys only: each account owns one or more
// authorized keys, and there are no passwords anywhere. The driver is
// modernc.org/sqlite (pure Go, no CGO), which keeps the static distroless
// Docker build working.
package store

import (
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	gossh "golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"
)

// Limits for self-registered usernames.
const MaxUsernameLen = 32

// Sentinel errors for key authentication.
var (
	ErrBadUsername = errors.New("username must be 1-32 chars of letters, digits, . _ -")
	ErrBadKey      = errors.New("that is not a valid SSH public key (expected: ssh-ed25519 AAAAC3... comment)")
	ErrKeyMismatch = errors.New("that username is registered to a different key")
	ErrKeyExists   = errors.New("that key is already registered")
	ErrBanned      = errors.New("that account is suspended - contact an admin")
)

// Account is one registered player.
type Account struct {
	ID        int64
	Username  string
	IsAdmin   bool
	Banned    bool
	CreatedAt time.Time
}

// UserRow is one row of the admin user list.
type UserRow struct {
	Username  string
	IsAdmin   bool
	Banned    bool
	XP        int
	CreatedAt time.Time
}

// Stats is one player's persistent totals.
type Stats struct {
	Username          string
	TotalXP           int
	MissionsCompleted int
	TotalCompletions  int
	Commands          int
	HintsUsed         int
	Logins            int
}

// LeaderRow is one leaderboard entry.
type LeaderRow struct {
	Username string
	XP       int
	Missions int
}

// GlobalStats aggregates the whole server for the admin view.
type GlobalStats struct {
	Users            int
	TotalCompletions int
	TotalXPAwarded   int
	TotalCommands    int
}

// Store is a SQLite-backed account/progress database. It is safe for
// concurrent use.
type Store struct {
	db *sql.DB
}

// Open creates (or opens) the database at path and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite writers serialize; one conn avoids lock churn.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	if s.isPasswordEra() {
		// One-way upgrade from the password era: password accounts cannot
		// become key accounts, so everyone re-registers with a key.
		for _, q := range []string{
			`DROP TABLE IF EXISTS mission_progress`,
			`DROP TABLE IF EXISTS user_stats`,
			`DROP TABLE IF EXISTS users`,
		} {
			if _, err := s.db.Exec(q); err != nil {
				return fmt.Errorf("migrate from password era: %w", err)
			}
		}
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			is_admin INTEGER NOT NULL DEFAULT 0,
			banned INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS user_keys (
			key TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id),
			added_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS mission_progress (
			user_id INTEGER NOT NULL REFERENCES users(id),
			mission_id TEXT NOT NULL,
			completions INTEGER NOT NULL DEFAULT 0,
			xp_awarded INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, mission_id)
		)`,
		`CREATE TABLE IF NOT EXISTS user_stats (
			user_id INTEGER PRIMARY KEY REFERENCES users(id),
			commands INTEGER NOT NULL DEFAULT 0,
			hints_used INTEGER NOT NULL DEFAULT 0,
			logins INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// Column added after the key-era launch: patch older databases.
	if !s.hasColumn("users", "banned") {
		if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN banned INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("migrate banned column: %w", err)
		}
	}
	return nil
}

// hasColumn reports whether a table already has a column.
func (s *Store) hasColumn(table, column string) bool {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

// isPasswordEra reports a database from before key-only auth.
func (s *Store) isPasswordEra() bool {
	rows, err := s.db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == "password_hash" {
			return true
		}
	}
	return false
}

// validUsername allows a small safe charset so names are always printable in
// the prompt, the leaderboard, and logs.
func validUsername(u string) bool {
	if len(u) == 0 || len(u) > MaxUsernameLen {
		return false
	}
	for _, r := range u {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// canonicalKey parses an authorized-key line and returns it in canonical
// "type base64" form (comment stripped) for stable comparison.
func canonicalKey(line string) (string, error) {
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(strings.TrimSpace(line)))
	if err != nil {
		return "", ErrBadKey
	}
	return strings.TrimSpace(string(gossh.MarshalAuthorizedKey(pub))), nil
}

// Fingerprint returns the SHA256 fingerprint of an authorized-key line,
// in the same form OpenSSH prints (SHA256:...).
func Fingerprint(line string) (string, error) {
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(strings.TrimSpace(line)))
	if err != nil {
		return "", ErrBadKey
	}
	return gossh.FingerprintSHA256(pub), nil
}

// RegisterOrLoginKey implements self-registration with keys: an unknown
// username creates an account owning the given key; a known username must
// present one of its keys. adminNames marks extra admins; the very first
// account ever created is always admin so the server is never left without
// one. It returns the account and whether it was just created.
func (s *Store) RegisterOrLoginKey(username, keyLine string, adminNames map[string]bool) (*Account, bool, error) {
	username = strings.TrimSpace(username)
	if !validUsername(username) {
		return nil, false, ErrBadUsername
	}
	key, err := canonicalKey(keyLine)
	if err != nil {
		return nil, false, err
	}

	var userID int64
	var name string
	var adminInt, bannedInt int
	var created string
	err = s.db.QueryRow(`SELECT u.id, u.username, u.is_admin, u.banned, u.created_at
		FROM users u JOIN user_keys k ON k.user_id = u.id WHERE k.key = ?`, key).
		Scan(&userID, &name, &adminInt, &bannedInt, &created)
	if err == nil {
		acct := &Account{ID: userID, Username: name, IsAdmin: adminInt != 0, Banned: bannedInt != 0}
		acct.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if acct.Banned {
			return nil, false, ErrBanned
		}
		if acct.Username != username {
			// Right key, wrong username: do not say which half matched.
			return nil, false, ErrKeyMismatch
		}
		if !acct.IsAdmin && adminNames[username] {
			if _, err := s.db.Exec(`UPDATE users SET is_admin = 1 WHERE id = ?`, acct.ID); err == nil {
				acct.IsAdmin = true
			}
		}
		return acct, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("lookup key: %w", err)
	}

	var dummy int
	if err := s.db.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&dummy); err == nil {
		// Known username, unknown key.
		return nil, false, ErrKeyMismatch
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("lookup user: %w", err)
	}

	var userCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return nil, false, fmt.Errorf("count users: %w", err)
	}
	isAdmin := userCount == 0 || adminNames[username]
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`INSERT INTO users (username, is_admin, banned, created_at) VALUES (?, ?, 0, ?)`,
		username, boolToInt(isAdmin), now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			// Lost a simultaneous registration race: retry as a login.
			return s.RegisterOrLoginKey(username, keyLine, adminNames)
		}
		return nil, false, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	if _, err := s.db.Exec(`INSERT INTO user_keys (key, user_id, added_at) VALUES (?, ?, ?)`,
		key, id, now); err != nil {
		_, _ = s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
		if strings.Contains(err.Error(), "UNIQUE") {
			return s.RegisterOrLoginKey(username, keyLine, adminNames)
		}
		return nil, false, fmt.Errorf("store key: %w", err)
	}
	_, _ = s.db.Exec(`INSERT OR IGNORE INTO user_stats (user_id) VALUES (?)`, id)
	createdAt, _ := time.Parse(time.RFC3339, now)
	return &Account{ID: id, Username: username, IsAdmin: isAdmin, CreatedAt: createdAt}, true, nil
}

// AddKey attaches another authorized key to an account (second device).
func (s *Store) AddKey(userID int64, keyLine string) error {
	key, err := canonicalKey(keyLine)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`INSERT INTO user_keys (key, user_id, added_at) VALUES (?, ?, ?)`,
		key, userID, now); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrKeyExists
		}
		return fmt.Errorf("add key: %w", err)
	}
	return nil
}

// ListKeys returns an account's authorized keys in canonical form.
func (s *Store) ListKeys(userID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT key FROM user_keys WHERE user_id = ? ORDER BY added_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan key: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RemoveKey deletes the key whose fingerprint starts with fpPrefix. The last
// key of an account cannot be removed (that would lock the player out).
func (s *Store) RemoveKey(userID int64, fpPrefix string) error {
	keys, err := s.ListKeys(userID)
	if err != nil {
		return err
	}
	if len(keys) <= 1 {
		return errors.New("cannot remove your only key (that would lock you out)")
	}
	for _, k := range keys {
		fp, err := Fingerprint(k)
		if err != nil {
			continue
		}
		if strings.HasPrefix(strings.ToLower(fp), strings.ToLower(fpPrefix)) {
			_, err := s.db.Exec(`DELETE FROM user_keys WHERE key = ? AND user_id = ?`, k, userID)
			return err
		}
	}
	return errors.New("no key starts with that fingerprint")
}

// GetAccount reloads one account by id (role and suspension state).
func (s *Store) GetAccount(id int64) (*Account, error) {
	var a Account
	var adminInt, bannedInt int
	var created string
	err := s.db.QueryRow(`SELECT id, username, is_admin, banned, created_at FROM users WHERE id = ?`,
		id).Scan(&a.ID, &a.Username, &adminInt, &bannedInt, &created)
	if err != nil {
		return nil, fmt.Errorf("lookup account: %w", err)
	}
	a.IsAdmin = adminInt != 0
	a.Banned = bannedInt != 0
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &a, nil
}

// ListUsers returns every account with lifetime XP, for admins.
func (s *Store) ListUsers() ([]UserRow, error) {
	rows, err := s.db.Query(`SELECT u.username, u.is_admin, u.banned, u.created_at,
		COALESCE(SUM(p.xp_awarded), 0) FROM users u
		LEFT JOIN mission_progress p ON p.user_id = u.id
		GROUP BY u.id ORDER BY u.created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var r UserRow
		var adminInt, bannedInt int
		var created string
		if err := rows.Scan(&r.Username, &adminInt, &bannedInt, &created, &r.XP); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		r.IsAdmin = adminInt != 0
		r.Banned = bannedInt != 0
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserID resolves a username to its id.
func (s *Store) UserID(username string) (int64, error) {
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("no player named %q", username)
		}
		return 0, fmt.Errorf("lookup user: %w", err)
	}
	return id, nil
}

// SetBanned suspends (true) or reinstates (false) an account. Banned players
// cannot log in; their data is kept.
func (s *Store) SetBanned(username string, banned bool) error {
	id, err := s.UserID(username)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET banned = ? WHERE id = ?`, boolToInt(banned), id)
	return err
}

// SetAdmin grants (true) or revokes (false) the admin role.
func (s *Store) SetAdmin(username string, admin bool) error {
	id, err := s.UserID(username)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET is_admin = ? WHERE id = ?`, boolToInt(admin), id)
	return err
}

// ResetProgress wipes one player's mission progress (XP and completions).
// Keys, role, and usage counters are kept.
func (s *Store) ResetProgress(username string) error {
	id, err := s.UserID(username)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM mission_progress WHERE user_id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		auditLog("progress-reset user=%d username=%s rows=%d", id, username, n)
	}
	return nil
}

// RecordLogin bumps the login counter; failure is non-fatal for gameplay.
func (s *Store) RecordLogin(userID int64) {
	_, _ = s.db.Exec(`INSERT INTO user_stats (user_id, logins) VALUES (?, 1)
		ON CONFLICT(user_id) DO UPDATE SET logins = logins + 1`, userID)
}

// LoadProgress returns total XP and per-mission completion counts.
func (s *Store) LoadProgress(userID int64) (xp int, completed map[string]int, err error) {
	completed = map[string]int{}
	rows, err := s.db.Query(`SELECT mission_id, completions, xp_awarded FROM mission_progress WHERE user_id = ?`, userID)
	if err != nil {
		return 0, completed, fmt.Errorf("load progress: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mid string
		var n, awarded int
		if err := rows.Scan(&mid, &n, &awarded); err != nil {
			return 0, completed, fmt.Errorf("scan progress: %w", err)
		}
		completed[mid] = n
		xp += awarded
	}
	return xp, completed, rows.Err()
}

// RecordCompletion stores a mission finish. XP is granted only the first
// time this user completes this mission, across all sessions. Every mutation
// is logged so progress can always be traced to a cause.
func (s *Store) RecordCompletion(userID int64, missionID string, xp int) (awarded int, first bool, err error) {
	var n int
	err = s.db.QueryRow(`SELECT completions FROM mission_progress WHERE user_id = ? AND mission_id = ?`,
		userID, missionID).Scan(&n)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = s.db.Exec(`INSERT INTO mission_progress (user_id, mission_id, completions, xp_awarded)
			VALUES (?, ?, 1, ?)`, userID, missionID, xp)
		if err != nil {
			return 0, false, fmt.Errorf("record completion: %w", err)
		}
		auditLog("xp-awarded user=%d mission=%s xp=%d", userID, missionID, xp)
		return xp, true, nil
	case err != nil:
		return 0, false, fmt.Errorf("lookup completion: %w", err)
	default:
		_, err = s.db.Exec(`UPDATE mission_progress SET completions = completions + 1
			WHERE user_id = ? AND mission_id = ?`, userID, missionID)
		if err != nil {
			return 0, false, fmt.Errorf("record replay: %w", err)
		}
		auditLog("replay user=%d mission=%s", userID, missionID)
		return 0, false, nil
	}
}

// AddCommands adds n to the lifetime command counter.
func (s *Store) AddCommands(userID int64, n int) {
	_, _ = s.db.Exec(`INSERT INTO user_stats (user_id, commands) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET commands = commands + ?`, userID, n, n)
}

// AddHints adds n to the lifetime hint counter.
func (s *Store) AddHints(userID int64, n int) {
	_, _ = s.db.Exec(`INSERT INTO user_stats (user_id, hints_used) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET hints_used = hints_used + ?`, userID, n, n)
}

// PersonalStats returns one player's lifetime totals.
func (s *Store) PersonalStats(userID int64) (Stats, error) {
	var st Stats
	err := s.db.QueryRow(`SELECT username FROM users WHERE id = ?`, userID).Scan(&st.Username)
	if err != nil {
		return st, fmt.Errorf("lookup user: %w", err)
	}
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(xp_awarded),0), COALESCE(SUM(completions),0),
		COUNT(*) FROM mission_progress WHERE user_id = ?`,
		userID).Scan(&st.TotalXP, &st.TotalCompletions, &st.MissionsCompleted)
	_ = s.db.QueryRow(`SELECT commands, hints_used, logins FROM user_stats WHERE user_id = ?`,
		userID).Scan(&st.Commands, &st.HintsUsed, &st.Logins)
	return st, nil
}

// Leaderboard returns the top players by XP.
func (s *Store) Leaderboard(limit int) ([]LeaderRow, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.db.Query(`SELECT u.username, COALESCE(SUM(p.xp_awarded),0),
		COUNT(p.mission_id) FROM users u
		LEFT JOIN mission_progress p ON p.user_id = u.id
		GROUP BY u.id ORDER BY 2 DESC, u.username ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("leaderboard: %w", err)
	}
	defer rows.Close()
	var out []LeaderRow
	for rows.Next() {
		var r LeaderRow
		if err := rows.Scan(&r.Username, &r.XP, &r.Missions); err != nil {
			return nil, fmt.Errorf("scan leaderboard: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GlobalStats aggregates server-wide totals for the admin view.
func (s *Store) GlobalStats() (GlobalStats, error) {
	var g GlobalStats
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&g.Users)
	if err != nil {
		return g, fmt.Errorf("count users: %w", err)
	}
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(completions),0), COALESCE(SUM(xp_awarded),0)
		FROM mission_progress`).Scan(&g.TotalCompletions, &g.TotalXPAwarded)
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(commands),0) FROM user_stats`).Scan(&g.TotalCommands)
	return g, nil
}

// TestKey generates a throwaway Ed25519 keypair for tests and returns the
// private key and the authorized-key line for the public half.
func TestKey(seed byte) (ed25519.PrivateKey, string) {
	var s [32]byte
	for i := range s {
		s[i] = seed + byte(i)
	}
	priv := ed25519.NewKeyFromSeed(s[:])
	pub, _ := gossh.NewPublicKey(priv.Public())
	line := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(pub)))
	return priv, line + " test-key"
}

// auditLog records progress mutations where the operator will see them
// (docker logs). Progress must always be traceable to a cause.
func auditLog(format string, args ...any) {
	log.Info("progress-audit", "event", fmt.Sprintf(format, args...))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
