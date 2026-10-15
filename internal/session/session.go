// Package session ties the simulator, mission engine, and progress together
// for one player. It is independent of Bubble Tea and SSH, so game behaviour is
// testable without a terminal, and each Session is fully isolated: its own
// virtual filesystem, working directory, mission run, and progress.
//
// A session may be bound to a registered account (see NewForAccount). Bound
// sessions restore XP and completions from the store at login and write every
// completion, command, and hint back through, so progress survives disconnects.
// Unbound sessions (New) behave exactly like the original MVP: in-memory only.
package session

import (
	"fmt"
	"sort"
	"strings"

	"linuxquest/internal/missions"
	"linuxquest/internal/progress"
	"linuxquest/internal/simulator"
	"linuxquest/internal/store"
)

// LineKind classifies output so the UI can style it.
type LineKind int

const (
	Output  LineKind = iota // command stdout
	Error                   // command stderr or usage problems
	Info                    // neutral guidance
	Success                 // objective or mission progress
	Hint                    // hints and tips
)

// Line is one line of terminal output.
type Line struct {
	Kind LineKind
	Text string
}

// Outcome is the effect of one submitted command line.
type Outcome struct {
	Lines     []Line
	Clear     bool // clear the screen before showing Lines
	ToMenu    bool // player asked to return to the mission menu
	Quit      bool // player asked to leave
	Completed bool // the mission was finished by this command
	XPAwarded int  // XP awarded for the completion (0 on replays)
	FirstTime bool // first completion of this mission ever (across sessions when bound)
	// Checklist carries the objectives with fresh marks when a command
	// completed something (empty otherwise, and on mission completion,
	// whose own screen lists them). The UI decides where it goes: the
	// objectives sidebar on wide screens, the scrollback on narrow ones.
	Checklist []Line
}

// Summary describes a finished session.
type Summary struct {
	XP        int      // XP earned this session
	LifetimeXP int     // total XP across all sessions (0 when unbound)
	Completed []string // titles of completed missions
	Commands  int
}

// builtinCommands are the meta-commands handled by the session, not the simulator.
var builtinCommands = []string{"help", "mission", "missions", "hint", "stats", "top", "keys", "keyadd", "keyrm", "clear", "exit"}

// adminCommands are additionally available to admin accounts.
var adminCommands = []string{"admin"}

// Session is one player's game state.
type Session struct {
	catalog  []missions.Mission
	progress *progress.Progress
	sim      *simulator.Sim
	run      *missions.Run
	commands int
	earned   int // XP gained during this connection (restored XP excluded)

	db         *store.Store
	account    *store.Account
	newAccount bool
}

// New creates an unbound session over the given mission catalog (in-memory
// progress only, as in the MVP).
func New(catalog []missions.Mission) *Session {
	return &Session{catalog: catalog, progress: progress.New()}
}

// NewForAccount creates a session bound to a registered account. Previous XP
// and completions are restored from the store; every completion, command, and
// hint is written back. isNew reports a just-registered account so the UI can
// greet it. A nil db behaves like New.
func NewForAccount(catalog []missions.Mission, db *store.Store, acct *store.Account, isNew bool) *Session {
	s := New(catalog)
	s.db = db
	s.account = acct
	s.newAccount = isNew
	if db != nil && acct != nil {
		db.RecordLogin(acct.ID)
		if xp, completed, err := db.LoadProgress(acct.ID); err == nil {
			s.progress.Restore(xp, completed)
		}
	}
	return s
}

// Catalog returns the available missions.
func (s *Session) Catalog() []missions.Mission { return s.catalog }

// Progress returns this session's progress.
func (s *Session) Progress() *progress.Progress { return s.progress }

// Username returns the account name, or "player" for unbound sessions.
func (s *Session) Username() string {
	if s.account != nil {
		return s.account.Username
	}
	return "player"
}

// IsAdmin reports whether the bound account is an admin.
func (s *Session) IsAdmin() bool { return s.account != nil && s.account.IsAdmin }

// Bound reports whether the session is tied to a registered account with storage.
func (s *Session) Bound() bool { return s.account != nil && s.db != nil }

// IsNewAccount reports a just-registered account.
func (s *Session) IsNewAccount() bool { return s.newAccount }

// Run returns the active mission run, or nil in the menu.
func (s *Session) Run() *missions.Run { return s.run }

// Sim returns the active simulator, or nil in the menu.
func (s *Session) Sim() *simulator.Sim { return s.sim }

// Start begins (or replays) a mission with a fresh filesystem and directory.
func (s *Session) Start(id string) error {
	for i := range s.catalog {
		m := &s.catalog[i]
		if m.ID != id {
			continue
		}
		vfs, err := m.BuildFS()
		if err != nil {
			return err
		}
		s.sim = simulator.New(vfs, m.StartDir)
		s.run = missions.NewRun(m)
		return nil
	}
	return fmt.Errorf("unknown mission %q", id)
}

// Leave abandons the active mission and returns to the menu.
func (s *Session) Leave() { s.sim, s.run = nil, nil }

// Prompt returns the shell prompt for the active mission.
func (s *Session) Prompt() string {
	dir := "~"
	if s.sim != nil {
		dir = s.sim.PromptDir()
	}
	return s.Username() + "@linuxquest:" + dir + "$"
}

// Summary returns the session summary.
func (s *Session) Summary() Summary {
	sum := Summary{XP: s.earned, Commands: s.commands}
	for _, m := range s.catalog {
		if s.progress.IsCompleted(m.ID) {
			sum.Completed = append(sum.Completed, m.Title)
		}
	}
	if s.db != nil && s.account != nil {
		if st, err := s.db.PersonalStats(s.account.ID); err == nil {
			sum.LifetimeXP = st.TotalXP
		}
	} else {
		sum.LifetimeXP = sum.XP
	}
	return sum
}

// Briefing returns the mission description and the objective checklist.
func (s *Session) Briefing() []Line {
	if s.run == nil {
		return nil
	}
	return append(s.BriefingIntro(), s.Checklist()...)
}

// BriefingIntro returns the mission title and story without the objectives:
// while playing, objectives live in the sidebar, not the scrollback.
func (s *Session) BriefingIntro() []Line {
	if s.run == nil {
		return nil
	}
	m := s.run.Mission
	return []Line{
		{Info, fmt.Sprintf("%s  (%s, %d XP)", m.Title, m.Difficulty, m.XP)},
		{Info, m.Story},
		{Info, ""},
	}
}

// Checklist returns the objective list with live completion marks. It is
// shown at mission start and re-shown every time an objective completes, so
// progress visibly lights up without retyping anything.
func (s *Session) Checklist() []Line {
	if s.run == nil {
		return nil
	}
	lines := []Line{{Info, "Objectives:"}}
	for _, o := range s.run.Mission.Objectives {
		mark := "[ ]"
		kind := Info
		if s.run.IsDone(o.ID) {
			mark, kind = "[x]", Success
		}
		lines = append(lines, Line{kind, "  " + mark + " " + o.Text})
	}
	return lines
}

// helpDetail documents one command for `help <name>`.
var helpDetail = map[string]string{
	"pwd":      "pwd prints the working directory you are standing in.",
	"ls":       "ls [path] lists a directory. Folders end in /. No flags.",
	"cd":       "cd [path] moves into a directory. cd .. goes up, cd or cd ~ goes home.",
	"cat":      "cat <file>... prints files. Several names print one after another.",
	"find":     "find [path] -name <pattern> [-type f|d] searches by name. Quote *-patterns: find . -name '*.txt'.",
	"mkdir":    "mkdir <dir>... creates directories, parents included (like mkdir -p).",
	"touch":    "touch <file>... creates empty files. Touching what exists changes nothing.",
	"rm":       "rm [-r] [-f] <path>... deletes. -r removes directories, -f ignores missing files.",
	"cp":       "cp [-r] <source> <dest> copies. -r copies directories. Into a directory keeps the name.",
	"mv":       "mv <source> <dest> moves or renames. Overwrites files, never directories.",
	"echo":     "echo <text> prints its arguments. No redirection or variables here.",
	"grep":     "grep [-i] [-r] <pattern> [path...] prints matching lines as file:line. -r descends into directories.",
	"mission":  "mission repeats the briefing: title, story, objectives with marks.",
	"missions": "missions leaves for the mission menu.",
	"hint":     "hint shows the next nudge for the current objective. Repeat for more.",
	"stats":    "stats shows your totals: XP, missions, commands, hints, logins.",
	"top":      "top shows the XP leaderboard across all players.",
	"keys":     "keys lists your registered SSH keys by fingerprint.",
	"keyadd":   "keyadd <key> registers another key, e.g. from a second device. Paste its .pub line.",
	"keyrm":    "keyrm <fingerprint> removes one of your keys. The last one cannot go.",
	"admin":    "admin opens the admin console (admins only). Try: admin users.",
	"clear":    "clear blanks the screen. Ctrl+L does the same.",
	"exit":     "exit ends the session and shows your summary. Ctrl+D on an empty line too.",
}

// HelpLines explains the supported commands.
func HelpLines() []Line {
	// Grouped flowing text: always a handful of rows, at any width.
	return []Line{
		{Info, "Supported commands:"},
		{Info, "  Files: pwd · ls [path] · cd [path] · cat <file> · find [path] -name <pattern> · mkdir <dir> · touch <file> · rm [-r] <path> · cp [-r] <src> <dst> · mv <src> <dst> · echo <text> · grep [-i] [-r] <pattern> [path]"},
		{Info, "  Game:  mission · missions · hint · stats · top · keys · keyadd <key> · keyrm <fingerprint> · admin · clear · exit"},
		{Info, "Type help <name> for what one command does (try: help find)."},
		{Info, "Stuck? Looking up Linux help online is allowed. That is part of the job."},
	}
}

// HelpTopic documents one command, or points back at the list.
func HelpTopic(name string) []Line {
	if d, ok := helpDetail[strings.ToLower(name)]; ok {
		return []Line{{Info, d}}
	}
	return append(HelpLines(), Line{Hint, fmt.Sprintf("No detail for %q.", truncName(name))})
}

func truncName(s string) string {
	if r := []rune(s); len(r) > 24 {
		return string(r[:24]) + "..."
	}
	return s
}

// Submit handles one command line typed by the player. The input is only ever
// parsed as data: it is never passed to a shell or to the host.
func (s *Session) Submit(line string) Outcome {
	line = strings.TrimSpace(line)
	if line == "" {
		return Outcome{}
	}
	s.commands++
	if s.db != nil && s.account != nil {
		// Roles and suspensions apply live: re-read the account so a ban,
		// unban, promotion, or demotion lands without reconnecting.
		if fresh, err := s.db.GetAccount(s.account.ID); err == nil {
			s.account = fresh
		} else {
			// Account vanished under us: stop the session rather than
			// running accountless.
			return Outcome{Lines: []Line{{Error, "Your account is gone. Reconnect to start over."}}, Quit: true}
		}
		if s.account.Banned {
			return Outcome{Lines: []Line{{Error, "Your account is suspended. Contact an admin."}}, Quit: true}
		}
	}
	s.persistCommands(1)
	if len(line) > simulator.MaxLineLen {
		return Outcome{Lines: []Line{{Error, "Input is too long."}}}
	}
	fields := strings.Fields(line)
	switch fields[0] {
	case "help":
		if len(fields) > 1 {
			return Outcome{Lines: HelpTopic(fields[1])}
		}
		return Outcome{Lines: HelpLines()}
	case "clear":
		return Outcome{Clear: true}
	case "missions":
		return Outcome{ToMenu: true}
	case "exit":
		return Outcome{Quit: true}
	case "mission":
		return Outcome{Lines: s.Briefing()}
	case "hint":
		return Outcome{Lines: s.hint()}
	case "stats":
		return Outcome{Lines: s.statsLines()}
	case "top":
		return Outcome{Lines: s.topLines()}
	case "admin":
		return Outcome{Lines: s.adminLines(strings.Fields(line)[1:])}
	case "keys":
		return Outcome{Lines: s.keysLines()}
	case "keyadd":
		return Outcome{Lines: s.keyAddLine(strings.Join(strings.Fields(line)[1:], " "))}
	case "keyrm":
		return Outcome{Lines: s.keyRmLine(strings.Join(strings.Fields(line)[1:], " "))}
	}
	if s.sim == nil || s.run == nil {
		return Outcome{Lines: []Line{{Error, "No mission is active. Type missions to choose one."}}}
	}

	res := s.sim.Execute(line)
	var out Outcome
	if res.Stdout != "" {
		for _, l := range strings.Split(res.Stdout, "\n") {
			out.Lines = append(out.Lines, Line{Output, l})
		}
	}
	if res.Stderr != "" {
		for _, l := range strings.Split(res.Stderr, "\n") {
			out.Lines = append(out.Lines, Line{Error, l})
		}
		out.Lines = append(out.Lines, Line{Hint, tipFor(res)})
	}

	newly := s.run.Evaluate(res)
	total := len(s.run.Mission.Objectives)
	for _, o := range newly {
		out.Lines = append(out.Lines,
			Line{Success, fmt.Sprintf("[ok] Objective complete: %s  (%d/%d)", o.Text, s.run.DoneCount(), total)},
			Line{Info, "     " + o.Success})
	}
	// Hand the fresh checklist to the UI, unless the mission just ended:
	// the completion screen lists every objective as done already.
	if len(newly) > 0 && !s.run.Complete() {
		out.Checklist = s.Checklist()
	}
	if len(newly) > 0 && s.run.Complete() {
		m := s.run.Mission
		out.XPAwarded, out.FirstTime = s.complete(m.ID, m.XP)
		out.Completed = true
	}
	return out
}

// complete records a mission finish in memory and, when bound, durably.
func (s *Session) complete(id string, xp int) (awarded int, first bool) {
	if s.db != nil && s.account != nil {
		awarded, first, err := s.db.RecordCompletion(s.account.ID, id, xp)
		if err != nil {
			// Storage trouble must never eat a player's victory: keep it in
			// memory so the session still behaves.
			awarded, first = s.progress.Complete(id, xp)
		} else {
			s.progress.ForceComplete(id, awarded)
		}
		s.earned += awarded
		return awarded, first
	}
	awarded, first = s.progress.Complete(id, xp)
	s.earned += awarded
	return awarded, first
}

// persistCommands writes command counts through without failing gameplay.
func (s *Session) persistCommands(n int) {
	if s.db != nil && s.account != nil {
		s.db.AddCommands(s.account.ID, n)
	}
}

func (s *Session) hint() []Line {
	if s.run == nil {
		return []Line{{Info, "Pick a mission first, then type hint for a nudge."}}
	}
	if s.db != nil && s.account != nil {
		s.db.AddHints(s.account.ID, 1)
	}
	text, level, total, ok := s.run.NextHint()
	if !ok {
		return []Line{{Info, "No hints needed. Every objective is complete."}}
	}
	return []Line{{Hint, fmt.Sprintf("Hint (%d/%d): %s", level, total, text)}}
}

// statsLines shows the player's lifetime stats (this session when unbound).
func (s *Session) statsLines() []Line {
	if s.db == nil || s.account == nil {
		sum := s.Summary()
		return []Line{
			{Info, fmt.Sprintf("Stats for %s (this session only, not saved):", s.Username())},
			{Info, fmt.Sprintf("  XP: %d   Missions complete: %d   Commands: %d",
				sum.XP, len(sum.Completed), sum.Commands)},
		}
	}
	st, err := s.db.PersonalStats(s.account.ID)
	if err != nil {
		return []Line{{Error, "Could not load your stats right now."}}
	}
	role := "player"
	if s.account.IsAdmin {
		role = "admin"
	}
	return []Line{
		{Info, fmt.Sprintf("Stats for %s (%s):", st.Username, role)},
		{Info, fmt.Sprintf("  Total XP: %d   Missions complete: %d (across %d finishes)",
			st.TotalXP, st.MissionsCompleted, st.TotalCompletions)},
		{Info, fmt.Sprintf("  Commands entered: %d   Hints used: %d   Logins: %d",
			st.Commands, st.HintsUsed, st.Logins)},
	}
}

// topLines shows the XP leaderboard.
func (s *Session) topLines() []Line {
	if s.db == nil {
		return []Line{{Info, "The leaderboard needs an account database (unavailable here)."}}
	}
	top, err := s.db.Leaderboard(10)
	if err != nil || len(top) == 0 {
		return []Line{{Error, "Could not load the leaderboard right now."}}
	}
	lines := []Line{{Info, "Top players by XP:"}}
	for i, r := range top {
		mark := "  "
		if s.account != nil && r.Username == s.account.Username {
			mark = "> "
		}
		lines = append(lines, Line{Info, fmt.Sprintf("%s%d. %-20s %4d XP  (%d missions)",
			mark, i+1, r.Username, r.XP, r.Missions)})
	}
	return lines
}

// adminLines runs the admin console. Admin accounts only. With no arguments
// it shows server totals; otherwise it manages players:
// users | inspect <name> | reset <name> | ban/unban <name> | promote/demote <name>
func (s *Session) adminLines(args []string) []Line {
	if !s.IsAdmin() {
		return []Line{{Error, "The admin command is for admin accounts only."}}
	}
	if s.db == nil {
		return []Line{{Error, "No account database is configured."}}
	}
	if len(args) == 0 {
		return s.adminDashboard()
	}
	switch args[0] {
	case "users":
		return s.adminUsers()
	case "inspect":
		if len(args) < 2 {
			return []Line{{Error, "Usage: admin inspect <username>"}}
		}
		return s.adminInspect(args[1])
	case "reset":
		if len(args) < 2 {
			return []Line{{Error, "Usage: admin reset <username>  (wipes their XP and completions)"}}
		}
		return s.adminReset(args[1])
	case "ban", "unban":
		if len(args) < 2 {
			return []Line{{Error, "Usage: admin " + args[0] + " <username>"}}
		}
		return s.adminBan(args[1], args[0] == "ban")
	case "promote", "demote":
		if len(args) < 2 {
			return []Line{{Error, "Usage: admin " + args[0] + " <username>"}}
		}
		return s.adminRole(args[1], args[0] == "promote")
	default:
		return s.adminUsage()
	}
}

func (s *Session) adminUsage() []Line {
	return []Line{
		{Info, "Admin console:"},
		{Info, "  admin                 server totals"},
		{Info, "  admin users           list every player"},
		{Info, "  admin inspect <name>  one player's stats and keys"},
		{Info, "  admin reset <name>    wipe their XP and completions"},
		{Info, "  admin ban <name>      suspend their logins (data kept)"},
		{Info, "  admin unban <name>    reinstate them"},
		{Info, "  admin promote <name>  grant admin   ·  admin demote <name>  revoke it"},
	}
}

func (s *Session) adminDashboard() []Line {
	g, err := s.db.GlobalStats()
	if err != nil {
		return []Line{{Error, "Could not load server statistics right now."}}
	}
	lines := []Line{
		{Info, "LinuxQuest server statistics:"},
		{Info, fmt.Sprintf("  Registered players: %d", g.Users)},
		{Info, fmt.Sprintf("  Mission completions: %d   Total XP awarded: %d", g.TotalCompletions, g.TotalXPAwarded)},
		{Info, fmt.Sprintf("  Commands entered: %d", g.TotalCommands)},
		{Info, "Type top for the leaderboard."},
	}
	return append(lines, s.adminUsage()[1:]...)
}

func (s *Session) adminUsers() []Line {
	users, err := s.db.ListUsers()
	if err != nil {
		return []Line{{Error, "Could not list players right now."}}
	}
	if len(users) == 0 {
		return []Line{{Info, "No players registered."}}
	}
	lines := []Line{{Info, fmt.Sprintf("%-20s %-6s %-7s %5s  joined", "player", "role", "status", "xp")}}
	for _, u := range users {
		role, status := "player", "active"
		if u.IsAdmin {
			role = "admin"
		}
		if u.Banned {
			status = "banned"
		}
		lines = append(lines, Line{Info, fmt.Sprintf("%-20s %-6s %-7s %5d  %s",
			u.Username, role, status, u.XP, u.CreatedAt.Format("2006-01-02"))})
	}
	return lines
}

func (s *Session) adminInspect(name string) []Line {
	users, err := s.db.ListUsers()
	if err != nil {
		return []Line{{Error, "Could not load players right now."}}
	}
	var found *store.UserRow
	for i := range users {
		if users[i].Username == name {
			found = &users[i]
			break
		}
	}
	if found == nil {
		return []Line{{Error, fmt.Sprintf("No player named %q.", name)}}
	}
	id, err := s.db.UserID(name)
	if err != nil {
		return []Line{{Error, err.Error() + "."}}
	}
	st, err := s.db.PersonalStats(id)
	if err != nil {
		return []Line{{Error, "Could not load their stats right now."}}
	}
	role, status := "player", "active"
	if found.IsAdmin {
		role = "admin"
	}
	if found.Banned {
		status = "banned"
	}
	lines := []Line{
		{Info, fmt.Sprintf("%s (%s, %s) joined %s:", found.Username, role, status,
			found.CreatedAt.Format("2006-01-02 15:04"))},
		{Info, fmt.Sprintf("  XP: %d   Missions: %d (%d finishes)   Commands: %d   Hints: %d   Logins: %d",
			st.TotalXP, st.MissionsCompleted, st.TotalCompletions, st.Commands, st.HintsUsed, st.Logins)},
	}
	keys, err := s.db.ListKeys(id)
	if err != nil {
		return append(lines, Line{Error, "  (keys unavailable)"})
	}
	lines = append(lines, Line{Info, fmt.Sprintf("  Keys (%d):", len(keys))})
	for _, k := range keys {
		fp, err := store.Fingerprint(k)
		if err != nil {
			continue
		}
		lines = append(lines, Line{Info, "    " + fp})
	}
	return lines
}

func (s *Session) adminReset(name string) []Line {
	if err := s.db.ResetProgress(name); err != nil {
		return []Line{{Error, err.Error() + "."}}
	}
	// A reset player who is online keeps stale in-memory progress until they
	// reconnect; say so instead of pretending otherwise.
	return []Line{{Success, fmt.Sprintf("Progress wiped for %s. Takes full effect on their next login.", name)}}
}

func (s *Session) adminBan(name string, ban bool) []Line {
	if s.account != nil && name == s.account.Username {
		action := "ban"
		if !ban {
			action = "unban"
		}
		return []Line{{Error, fmt.Sprintf("You cannot %s yourself.", action)}}
	}
	if err := s.db.SetBanned(name, ban); err != nil {
		return []Line{{Error, err.Error() + "."}}
	}
	if ban {
		return []Line{{Success, fmt.Sprintf("%s is suspended. They cannot log in; their data is kept.", name)}}
	}
	return []Line{{Success, fmt.Sprintf("%s is reinstated.", name)}}
}

func (s *Session) adminRole(name string, grant bool) []Line {
	if !grant && s.account != nil && name == s.account.Username {
		return []Line{{Error, "You cannot demote yourself."}}
	}
	if err := s.db.SetAdmin(name, grant); err != nil {
		return []Line{{Error, err.Error() + "."}}
	}
	if grant {
		return []Line{{Success, fmt.Sprintf("%s is now an admin.", name)}}
	}
	return []Line{{Success, fmt.Sprintf("%s is no longer an admin.", name)}}
}

// keysLines lists the account's registered SSH keys by fingerprint.
func (s *Session) keysLines() []Line {
	if s.db == nil || s.account == nil {
		return []Line{{Info, "Key management needs a registered account."}}
	}
	keys, err := s.db.ListKeys(s.account.ID)
	if err != nil {
		return []Line{{Error, "Could not load your keys right now."}}
	}
	lines := []Line{{Info, fmt.Sprintf("Registered keys for %s:", s.account.Username)}}
	for i, k := range keys {
		fp, err := store.Fingerprint(k)
		if err != nil {
			continue
		}
		lines = append(lines, Line{Info, fmt.Sprintf("  %d. %s", i+1, fp)})
	}
	lines = append(lines, Line{Hint, "Add a second device with: keyadd ssh-ed25519 AAAA... comment"})
	return lines
}

// keyAddLine registers another authorized key from a pasted public-key line.
func (s *Session) keyAddLine(keyLine string) []Line {
	if s.db == nil || s.account == nil {
		return []Line{{Info, "Key management needs a registered account."}}
	}
	if strings.TrimSpace(keyLine) == "" {
		return []Line{{Error, "Usage: keyadd ssh-ed25519 AAAA... optional-comment"}}
	}
	if err := s.db.AddKey(s.account.ID, keyLine); err != nil {
		if err == store.ErrBadKey || err == store.ErrKeyExists {
			return []Line{{Error, err.Error() + "."}}
		}
		return []Line{{Error, "Could not add that key right now."}}
	}
	fp, _ := store.Fingerprint(keyLine)
	return []Line{{Success, "Key added (" + fp + "). It can log in as " + s.account.Username + " now."}}
}

// keyRmLine removes one of the account's keys by fingerprint prefix.
func (s *Session) keyRmLine(prefix string) []Line {
	if s.db == nil || s.account == nil {
		return []Line{{Info, "Key management needs a registered account."}}
	}
	if strings.TrimSpace(prefix) == "" {
		return []Line{{Error, "Usage: keyrm <fingerprint>  (see keys for the list)"}}
	}
	if err := s.db.RemoveKey(s.account.ID, prefix); err != nil {
		return []Line{{Error, err.Error() + "."}}
	}
	return []Line{{Success, "Key removed."}}
}

// Complete performs Tab completion on an input line and returns the new
// line plus an optional candidate listing (shown, not submitted). The first
// word completes against known commands (with a trailing space); later words
// complete against paths in the virtual filesystem, where directories win a
// trailing slash and full files a trailing space. With several matches it
// extends the shared prefix, or lists the candidates when there is nothing
// certain to add. An empty trailing word and unresolvable paths complete to
// nothing.
func (s *Session) Complete(line string) (string, []Line) {
	if s.sim == nil || s.run == nil {
		return line, nil
	}
	cut := strings.LastIndex(line, " ")
	if cut < 0 {
		return completeWord(line, s.commandsForComplete())
	}
	token := line[cut+1:]
	if token == "" {
		return line, nil
	}
	newToken, candidates := s.completePath(token)
	if newToken == token && len(candidates) == 0 {
		return line, nil
	}
	if newToken != token {
		return line[:cut+1] + newToken, nil
	}
	return line, candidateLines(candidates)
}

// commandsForComplete lists every word the first position accepts.
func (s *Session) commandsForComplete() []string {
	cmds := append(append([]string{}, builtinCommands...), simulator.Commands...)
	if s.IsAdmin() {
		cmds = append(cmds, adminCommands...)
	}
	return cmds
}

// completeWord completes a command name and appends a space on a full hit.
func completeWord(token string, cmds []string) (string, []Line) {
	var matches []string
	for _, c := range cmds {
		if strings.HasPrefix(c, token) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return token, nil
	case 1:
		return matches[0] + " ", nil
	}
	if ext := commonPrefix(matches); len(ext) > len(token) {
		return ext, nil
	}
	return token, candidateLines(matches)
}

// completePath completes one path token against the virtual filesystem.
func (s *Session) completePath(token string) (string, []string) {
	dirPart, base := "", token
	if i := strings.LastIndex(token, "/"); i >= 0 {
		dirPart, base = token[:i+1], token[i+1:]
	}
	dirAbs, err := s.sim.FS().Resolve(s.sim.Cwd(), dirPart)
	if err != nil {
		return token, nil
	}
	dirInfo, err := s.sim.FS().Stat(dirAbs)
	if err != nil || !dirInfo.IsDir {
		return token, nil
	}
	entries, err := s.sim.FS().ReadDir(dirAbs)
	if err != nil {
		return token, nil
	}
	type cand struct {
		name string
		isDir bool
	}
	var matches []cand
	for _, e := range entries {
		if strings.HasPrefix(e.Name, base) {
			matches = append(matches, cand{e.Name, e.IsDir})
		}
	}
	if len(matches) == 0 {
		return token, nil
	}
	if len(matches) == 1 {
		suffix := " "
		if matches[0].isDir {
			suffix = "/"
		}
		return dirPart + matches[0].name + suffix, nil
	}
	names := make([]string, 0, len(matches))
	for _, c := range matches {
		names = append(names, c.name)
	}
	if ext := commonPrefix(names); len(ext) > len(base) {
		return dirPart + ext, nil
	}
	shown := make([]string, 0, len(matches))
	for _, c := range matches {
		n := c.name
		if c.isDir {
			n += "/"
		}
		shown = append(shown, n)
	}
	return token, shown
}

// commonPrefix returns the longest shared prefix of non-empty strings.
func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
			if p == "" {
				return ""
			}
		}
	}
	return p
}

// candidateLines renders ambiguous matches for the scrollback.
func candidateLines(names []string) []Line {
	sort.Strings(names)
	return []Line{{Info, "  " + strings.Join(names, "  ")}}
}

// tipFor gives contextual guidance for a failed command.
func tipFor(res simulator.Result) string {
	switch {
	case !res.Known && res.Command != "":
		return "Tip: this simulator supports: " + strings.Join(append(append([]string{}, builtinCommands...), append(adminCommands, simulator.Commands...)...), ", ") + "."
	case res.Command == "":
		return "Tip: type help to see what you can do here."
	case strings.Contains(res.Stderr, "No such file or directory"):
		return "Tip: run ls to see what is in this directory, or find to search for a name."
	case strings.Contains(res.Stderr, "Not a directory"):
		return "Tip: cd only works on directories (names ending in / in ls). Use cat to read files."
	case strings.Contains(res.Stderr, "Is a directory"):
		return "Tip: cat reads files. Use ls to look inside a directory, or cd to enter it."
	}
	return "Tip: type hint for a nudge, or help for the command list."
}
