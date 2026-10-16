// Package terminal is the Bubble Tea user interface for LinuxQuest. It only
// renders state and forwards typed lines to the session package.
package terminal

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"linuxquest/internal/session"
)

type screen int

const (
	screenWelcome screen = iota
	screenMenu
	screenMission
	screenComplete
	screenSummary
)

const (
	maxScrollback = 500
	maxInput      = 1024 // matches the simulator limit; fits a pasted SSH key for keyadd
	maxHistory    = 100
)

// Model is the Bubble Tea model for one SSH session.
type Model struct {
	sess   *session.Session
	st     Styles
	screen screen

	width, height int

	menuIdx int
	menuTop int // first visible menu row (scrolls for long catalogs)
	notice  string

	input   []rune
	cursor  int
	history []string
	histIdx int // len(history) means "not browsing"

	lines []session.Line

	// completion screen state
	lastXP    int
	lastFirst bool
}

// New creates the UI model for a session.
func New(sess *session.Session, st Styles) *Model {
	return &Model{sess: sess, st: st, width: 80, height: 24}
}

// Screen helpers for tests.
func (m *Model) Screen() screen { return m.screen }

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.width < 20 {
			m.width = 20
		}
		if m.height < 10 {
			m.height = 10
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Bubble Tea batches runes that arrive in a single read (fast typing,
	// pasting) into one message. Outside the command prompt every rune is its
	// own keypress, so replay them one at a time.
	if k.Type == tea.KeyRunes && len(k.Runes) > 1 && m.screen != screenMission {
		var cmd tea.Cmd
		for _, r := range k.Runes {
			_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			if m.screen == screenSummary && cmd != nil {
				break
			}
		}
		return m, cmd
	}
	switch m.screen {
	case screenWelcome:
		switch k.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			return m.toSummary()
		case tea.KeyEnter, tea.KeySpace:
			m.screen = screenMenu
		}
		return m, nil
	case screenMenu:
		return m.menuKey(k)
	case screenMission:
		return m.missionKey(k)
	case screenComplete:
		return m.completeKey(k)
	case screenSummary:
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) toSummary() (tea.Model, tea.Cmd) {
	m.screen = screenSummary
	return m, nil
}

// ----- menu -----

func (m *Model) menuKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.sess.Catalog())
	m.notice = ""
	switch k.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyEsc:
		return m.toSummary()
	case tea.KeyUp:
		m.menuIdx = (m.menuIdx + n - 1) % n
		m.syncMenu()
	case tea.KeyDown, tea.KeyTab:
		m.menuIdx = (m.menuIdx + 1) % n
		m.syncMenu()
	case tea.KeyEnter:
		return m.startMission(m.menuIdx)
	case tea.KeyRunes:
		s := string(k.Runes)
		switch s {
		case "q", "Q":
			return m.toSummary()
		case "k":
			m.menuIdx = (m.menuIdx + n - 1) % n
			m.syncMenu()
		case "j":
			m.menuIdx = (m.menuIdx + 1) % n
			m.syncMenu()
		default:
			if len(s) == 1 && s[0] >= '1' && int(s[0]-'0') <= n {
				return m.startMission(int(s[0] - '1'))
			}
		}
	}
	return m, nil
}

func (m *Model) startMission(i int) (tea.Model, tea.Cmd) {
	cat := m.sess.Catalog()
	if i < 0 || i >= len(cat) {
		return m, nil
	}
	if err := m.sess.Start(cat[i].ID); err != nil {
		m.notice = "Could not start that mission."
		return m, nil
	}
	m.menuIdx = i
	m.screen = screenMission
	m.lines = append([]session.Line(nil), m.sess.BriefingIntro()...)
	m.lines = append(m.lines, session.Line{Kind: session.Info, Text: ""},
		session.Line{Kind: session.Hint, Text: "Type help for commands, hint if you get stuck. Tab completes names. Searching the web for Linux help is fine too."})
	m.resetInput()
	return m, nil
}

// ----- mission -----

func (m *Model) resetInput() {
	m.input, m.cursor, m.histIdx = nil, 0, len(m.history)
}

func (m *Model) missionKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyCtrlC:
		if len(m.input) > 0 {
			m.resetInput()
			return m, nil
		}
		return m.toSummary()
	case tea.KeyCtrlD:
		if len(m.input) == 0 {
			return m.toSummary()
		}
	case tea.KeyCtrlL:
		m.lines = nil
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyBackspace:
		if m.cursor > 0 {
			m.input = append(m.input[:m.cursor-1], m.input[m.cursor:]...)
			m.cursor--
		}
	case tea.KeyDelete:
		if m.cursor < len(m.input) {
			m.input = append(m.input[:m.cursor], m.input[m.cursor+1:]...)
		}
	case tea.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyRight:
		if m.cursor < len(m.input) {
			m.cursor++
		}
	case tea.KeyHome, tea.KeyCtrlA:
		m.cursor = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		m.cursor = len(m.input)
	case tea.KeyCtrlU:
		m.input = m.input[m.cursor:]
		m.cursor = 0
	case tea.KeyUp:
		if m.histIdx > 0 {
			m.histIdx--
			m.setInput(m.history[m.histIdx])
		}
	case tea.KeyDown:
		if m.histIdx < len(m.history) {
			m.histIdx++
			if m.histIdx == len(m.history) {
				m.setInput("")
			} else {
				m.setInput(m.history[m.histIdx])
			}
		}
	case tea.KeySpace:
		m.insert([]rune{' '})
	case tea.KeyTab:
		m.tabComplete()
	case tea.KeyRunes:
		m.insert(k.Runes)
	}
	return m, nil
}

// tabComplete completes the input line against commands and paths.
func (m *Model) tabComplete() {
	line := string(m.input)
	completed, candidates := m.sess.Complete(line)
	if completed != line {
		m.setInput(completed)
		return
	}
	for _, c := range candidates {
		m.push(c)
	}
}

func (m *Model) setInput(s string) {
	m.input = []rune(s)
	m.cursor = len(m.input)
}

func (m *Model) insert(rs []rune) {
	for _, r := range rs {
		if unicode.IsControl(r) {
			continue
		}
		if len(m.input) >= maxInput {
			return
		}
		m.input = append(m.input, 0)
		copy(m.input[m.cursor+1:], m.input[m.cursor:])
		m.input[m.cursor] = r
		m.cursor++
	}
}

func (m *Model) submit() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(string(m.input))
	prompt := m.sess.Prompt()
	m.resetInput()
	if line == "" {
		return m, nil
	}
	if len(m.history) == 0 || m.history[len(m.history)-1] != line {
		m.history = append(m.history, line)
		if len(m.history) > maxHistory {
			m.history = m.history[1:]
		}
	}
	m.histIdx = len(m.history)

	out := m.sess.Submit(line)
	if out.Clear {
		m.lines = nil
	} else {
		m.push(session.Line{Kind: session.Info, Text: prompt + " " + line}) // echoed command
	}
	for _, l := range out.Lines {
		m.push(l)
	}
	if len(out.Checklist) > 0 && !m.showSidebar() {
		for _, l := range out.Checklist {
			m.push(l)
		}
	}
	switch {
	case out.Quit:
		return m.toSummary()
	case out.ToMenu:
		m.sess.Leave()
		m.screen = screenMenu
	case out.Completed:
		m.lastXP, m.lastFirst = out.XPAwarded, out.FirstTime
		m.screen = screenComplete
	}
	return m, nil
}

func (m *Model) push(l session.Line) {
	m.lines = append(m.lines, l)
	if len(m.lines) > maxScrollback {
		m.lines = m.lines[len(m.lines)-maxScrollback:]
	}
}

// ----- completion -----

func (m *Model) completeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyEsc:
		return m.toSummary()
	case tea.KeyEnter:
		m.sess.Leave()
		m.screen = screenMenu
	case tea.KeyRunes:
		switch strings.ToLower(string(k.Runes)) {
		case "m":
			m.sess.Leave()
			m.screen = screenMenu
		case "r":
			return m.startMission(m.menuIdx)
		case "q":
			return m.toSummary()
		}
	}
	return m, nil
}

// ----- views -----

func (m *Model) View() string {
	switch m.screen {
	case screenWelcome:
		return m.welcomeView()
	case screenMenu:
		return m.menuView()
	case screenMission:
		return m.missionView()
	case screenComplete:
		return m.completeView()
	default:
		return m.summaryView()
	}
}

func (m *Model) boxWidth() int {
	w := m.width - 4
	if w > 76 {
		w = 76
	}
	if w < 16 {
		w = 16
	}
	return w
}

// padRight pads s with spaces to exactly w runes (no-op when longer).
func padRight(s string, w int) string {
	if n := len([]rune(s)); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// truncateRunes shortens s to at most max runes, marking the cut.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max || max < 4 {
		return s
	}
	return string(r[:max-1]) + "…"
}

func (m *Model) welcomeView() string {
	body := m.st.Title.Render("LinuxQuest") + "\n" +
		m.st.Dim.Render("Learn Linux by doing.") + "\n\n" +
		"You are about to use a safe, simulated Linux terminal. Everything here\n" +
		"is make-believe: nothing you type can touch a real computer.\n\n" +
		"Complete short missions, run real Linux commands, and earn XP.\n\n" +
		m.accountLine() +
		m.st.Dim.Render("Press Enter to continue  ·  Ctrl+C to quit")
	return "\n" + m.st.Box.Width(m.boxWidth()).Render(body) + "\n"
}

// accountLine reports who is logged in on the welcome screen.
func (m *Model) accountLine() string {
	if !m.sess.Bound() {
		return ""
	}
	user := m.sess.Username()
	line := "Logged in as " + user + ". Progress is saved to your account.\n"
	if m.sess.IsNewAccount() {
		line = "Account created for " + user + ". Progress is saved.\n"
	}
	if m.sess.IsAdmin() {
		line += "You are an admin: type admin for server statistics.\n"
	}
	return line + "\n"
}

func (m *Model) menuView() string {
	cat := m.sess.Catalog()
	n := len(cat)
	lines := m.menuLines()
	m.syncMenu()
	page := m.menuPage()

	who := m.sess.Username()
	if m.sess.IsAdmin() {
		who += " · admin"
	}
	var b strings.Builder
	b.WriteString(m.st.Title.Render("Missions") + "  " +
		m.st.Dim.Render(who) + "  " +
		m.st.Dim.Render(fmt.Sprintf("XP: %d", m.sess.Progress().XP())) + "\n\n")
	end := m.menuTop + page
	if end > len(lines) {
		end = len(lines)
	}
	if m.menuTop > 0 {
		b.WriteString(m.st.Dim.Render(fmt.Sprintf("\u2191 %d more", m.menuTop)) + "\n")
	}
	titleMax := m.boxWidth() - 38
	if titleMax < 8 {
		titleMax = 8
	}
	diffWidth := 0
	for _, mi := range cat {
		if w := len([]rune(mi.Difficulty)); w > diffWidth {
			diffWidth = w
		}
	}
	for _, l := range lines[m.menuTop:end] {
		if l.header != "" {
			done, total, xpDone, xpTotal := m.tierProgress(l.header)
			b.WriteString(m.st.Accent.Render(fmt.Sprintf("\u2500\u2500 %s \u2500\u2500  %d/%d · %d/%d XP",
				l.header, done, total, xpDone, xpTotal)) + "\n")
			continue
		}
		mi := cat[l.mission]
		status := "[ ]"
		if m.sess.Progress().IsCompleted(mi.ID) {
			status = m.st.Ok.Render("[x]")
		}
		row := fmt.Sprintf("%2d. %s  %s  %s", l.mission+1,
			padRight(truncateRunes(mi.Title, titleMax), titleMax),
			m.st.Dim.Render(padRight(mi.Difficulty, diffWidth)),
			m.st.Dim.Render(fmt.Sprintf("%d XP", mi.XP)))
		if l.mission == m.menuIdx {
			b.WriteString(m.st.Selected.Render("> ") + status + " " + row + "\n")
		} else {
			b.WriteString("  " + status + " " + row + "\n")
		}
	}
	if end < len(lines) {
		b.WriteString(m.st.Dim.Render(fmt.Sprintf("\u2193 %d more", len(lines)-end)) + "\n")
	}
	b.WriteString("\n" + m.st.Dim.Render(
		fmt.Sprintf("%d/%d  ·  \u2191/\u2193 or j/k to choose  ·  Enter to start  ·  q to quit", m.menuIdx+1, n)))
	if m.notice != "" {
		b.WriteString("\n" + m.st.Err.Render(m.notice))
	}
	return "\n" + m.st.Box.Width(m.boxWidth()).Render(b.String()) + "\n"
}

// syncMenu keeps the selected row visible (call after navigation or resize).
// menuLine is one rendered menu row: a tier header or a mission.
type menuLine struct {
	header  string // tier name for section headers, "" for missions
	mission int    // catalog index, -1 for headers
}

// tierOrder lists difficulty tiers in play order; unknown tiers follow in
// first-seen order.
func (m *Model) menuLines() []menuLine {
	cat := m.sess.Catalog()
	byTier := map[string][]int{}
	var tiers []string
	for i, mi := range cat {
		if _, ok := byTier[mi.Difficulty]; !ok {
			tiers = append(tiers, mi.Difficulty)
		}
		byTier[mi.Difficulty] = append(byTier[mi.Difficulty], i)
	}
	fixed := []string{"Beginner", "Intermediate", "Advanced"}
	ordered := make([]string, 0, len(tiers))
	seen := map[string]bool{}
	for _, f := range fixed {
		if _, ok := byTier[f]; ok {
			ordered = append(ordered, f)
			seen[f] = true
		}
	}
	for _, o := range tiers {
		if !seen[o] {
			ordered = append(ordered, o)
		}
	}
	var lines []menuLine
	for _, tier := range ordered {
		lines = append(lines, menuLine{header: tier, mission: -1})
		for _, i := range byTier[tier] {
			lines = append(lines, menuLine{mission: i})
		}
	}
	return lines
}

// tierProgress counts a tier's completions and XP: done/total missions and
// earned/available XP.
func (m *Model) tierProgress(tier string) (done, total, xpDone, xpTotal int) {
	for _, mi := range m.sess.Catalog() {
		if mi.Difficulty != tier {
			continue
		}
		total++
		xpTotal += mi.XP
		if m.sess.Progress().IsCompleted(mi.ID) {
			done++
			xpDone += mi.XP
		}
	}
	return done, total, xpDone, xpTotal
}

// syncMenu keeps the selected mission's row visible (call after navigation,
// resize, or menu return).
func (m *Model) syncMenu() {
	lines := m.menuLines()
	sel := 0
	for i, l := range lines {
		if l.mission == m.menuIdx {
			sel = i
			break
		}
	}
	page := m.menuPage()
	if sel < m.menuTop {
		m.menuTop = sel
	}
	if sel >= m.menuTop+page {
		m.menuTop = sel - page + 1
	}
	if m.menuTop > len(lines)-page {
		m.menuTop = len(lines) - page
	}
	if m.menuTop < 0 {
		m.menuTop = 0
	}
}

// menuPage is how many mission rows fit on screen.
func (m *Model) menuPage() int {
	if p := m.height - 12; p > 1 {
		return p
	}
	return 1
}

func (m *Model) styleLine(l session.Line) string {
	switch l.Kind {
	case session.Error:
		return m.st.Err.Render(l.Text)
	case session.Success:
		return m.st.Ok.Render(l.Text)
	case session.Hint:
		return m.st.Hint.Render(l.Text)
	case session.Info:
		return m.st.Dim.Render(l.Text)
	}
	return l.Text
}

func (m *Model) missionView() string {
	run := m.sess.Run()
	mainW := m.width
	if m.showSidebar() {
		mainW = m.width - sideColWidth
	}
	progFull := fmt.Sprintf("   %d/%d objectives · XP %d", run.DoneCount(), len(run.Mission.Objectives), m.sess.Progress().XP())
	prog := progFull
	if len([]rune(run.Mission.Title))+len([]rune(progFull))+1 > mainW {
		prog = fmt.Sprintf(" %d/%d", run.DoneCount(), len(run.Mission.Objectives))
	}
	titleMax := mainW - len([]rune(prog)) - 1
	if titleMax < 8 {
		titleMax = 8
	}
	header := m.st.Title.Render(truncateRunes(run.Mission.Title, titleMax)) + m.st.Dim.Render(prog)

	footer := pickFooter(mainW)

	avail := m.height - 4
	if avail < 3 {
		avail = 3
	}
	main := header + "\n" + m.st.Dim.Render(strings.Repeat("─", clamp(mainW-1, 10, 200))) + "\n" +
		m.scrollbackW(avail, mainW-1) + "\n" + m.promptLineW(mainW) + "\n" + m.st.Dim.Render(footer)
	if !m.showSidebar() {
		return main
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, main, m.sideBar(avail))
}

// sideColWidth is the objectives column width on screen: box content +
// padding + borders + gap. Roomy enough for one detailed line per objective.
const sideColWidth = 40

// pickFooter returns the richest footer that fits in the given width.
func pickFooter(mainW int) string {
	footers := []string{
		"help · hint · stats · top · mission · missions · clear · exit",
		"help · hint · stats · exit",
		"help · exit",
	}
	for _, f := range footers {
		if len([]rune(f)) <= mainW-1 {
			return f
		}
	}
	return footers[len(footers)-1]
}

// sideBar renders the live objectives panel pinned beside the terminal.
// sideBar renders the live objectives panel pinned beside the terminal.
// Every objective is one line in one shared style; only the [x]/[ ] mark
// changes. Lines are truncated, never wrapped, so rows stay aligned.
func (m *Model) sideBar(avail int) string {
	run := m.sess.Run()
	if run == nil {
		return ""
	}
	inner := sideColWidth - 8 // borders, padding, gap, and mark indent
	if inner < 8 {
		inner = 8
	}
	var b strings.Builder
	b.WriteString(m.st.Title.Render("Objectives:") + "\n")
	for _, o := range run.Mission.Objectives {
		mark := "[ ]"
		if run.IsDone(o.ID) {
			mark = "[x]"
		}
		line := mark + " " + truncateRunes(o.Text, inner-len(mark)-1)
		b.WriteString(m.styleLine(session.Line{Kind: session.Info, Text: line}) + "\n")
	}
	return m.st.Box.MarginLeft(2).Width(sideColWidth - 6).Render(strings.TrimRight(b.String(), "\n"))
}

// promptLine renders the input line, scrolling horizontally when the prompt
// plus input exceeds the screen width (e.g. a pasted SSH key). The cursor
// stays visible; trimmed sides show ◀ ▶ markers.
func (m *Model) promptLine() string {
	return m.promptLineW(m.width)
}

func (m *Model) promptLineW(width int) string {
	avail := width - 1
	if avail < 10 {
		avail = 10
	}
	base := []rune(m.sess.Prompt() + " ")
	total := len(base) + len(m.input)
	cur := len(base) + m.cursor // cursor position in the joined sequence

	at := func(i int) rune {
		if i < len(base) {
			return base[i]
		}
		return m.input[i-len(base)]
	}
	isBase := func(i int) bool { return i < len(base) }

	start := 0
	if total > avail {
		start = cur - avail + 2
		if start < 0 {
			start = 0
		}
		if start > total-avail {
			start = total - avail
		}
	}
	end := start + avail
	if end > total {
		end = total
	}

	// Visible runes, with markers overwriting trimmed edges.
	vis := make([]rune, 0, avail)
	visBase := make([]bool, 0, avail)
	for i := start; i < end; i++ {
		vis = append(vis, at(i))
		visBase = append(visBase, isBase(i))
	}
	if start > 0 && len(vis) > 0 {
		vis[0] = '◀'
		visBase[0] = false
	}
	if end < total && len(vis) > 0 {
		vis[len(vis)-1] = '▶'
		visBase[len(vis)-1] = false
	}
	curPos := cur - start
	if curPos < 0 {
		curPos = 0
	}
	if curPos >= len(vis) && len(vis) > 0 {
		curPos = len(vis) - 1
	}

	// Style runs: prompt part green-bold, cursor reversed.
	var b strings.Builder
	rev := lipgloss.NewStyle().Reverse(true)
	j := 0
	for j < len(vis) {
		if j == curPos {
			b.WriteString(rev.Render(string(vis[j])))
			j++
			continue
		}
		k := j
		for k < len(vis) && k != curPos && visBase[k] == visBase[j] {
			k++
		}
		seg := string(vis[j:k])
		allBase := visBase[j]
		if allBase {
			b.WriteString(m.st.Prompt.Render(seg))
		} else {
			b.WriteString(seg)
		}
		j = k
	}
	return b.String()
}

// showSidebar reports whether the screen is wide enough to pin the live
// objectives panel beside the terminal instead of printing it inline.
func (m *Model) showSidebar() bool {
	return m.width >= 76 && m.sess.Run() != nil
}

// scrollback renders the last n lines of output, padded to exactly n lines.
func (m *Model) scrollback(n int) string {
	return m.scrollbackW(n, m.width-1)
}

// scrollbackW renders into the given content width (narrower beside a sidebar).
func (m *Model) scrollbackW(n, width int) string {
	wrapped := wrapLines(m, width)
	if len(wrapped) > n {
		wrapped = wrapped[len(wrapped)-n:]
	}
	for len(wrapped) < n {
		wrapped = append([]string{""}, wrapped...)
	}
	return strings.Join(wrapped, "\n")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wrapLines word-wraps scrollback to the terminal width and applies styling.
func wrapLines(m *Model, width int) []string {
	if width < 10 {
		width = 10
	}
	var out []string
	for _, l := range m.lines {
		for _, w := range hardWrap(l.Text, width) {
			out = append(out, m.styleLine(session.Line{Kind: l.Kind, Text: w}))
		}
	}
	return out
}

func hardWrap(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		r := []rune(para)
		if len(r) == 0 {
			out = append(out, "")
			continue
		}
		indent := len(para) - len(strings.TrimLeft(para, " "))
		for len(r) > width {
			cut := width
			for i := width; i > indent+1; i-- {
				if r[i-1] == ' ' {
					cut = i
					break
				}
			}
			out = append(out, strings.TrimRight(string(r[:cut]), " "))
			r = []rune(strings.Repeat(" ", indent) + strings.TrimLeft(string(r[cut:]), " "))
		}
		out = append(out, string(r))
	}
	return out
}

func (m *Model) completeView() string {
	run := m.sess.Run()
	var b strings.Builder
	b.WriteString(m.st.Ok.Render("Mission complete!") + "  " + m.st.Title.Render(run.Mission.Title) + "\n\n")
	if m.lastFirst {
		b.WriteString(m.st.Ok.Render(fmt.Sprintf("+%d XP", m.lastXP)) + "\n")
	} else {
		b.WriteString(m.st.Dim.Render("Replay complete. XP is only awarded the first time.") + "\n")
	}
	b.WriteString(fmt.Sprintf("Total XP this session: %d\n\n", m.sess.Summary().XP))
	for _, o := range run.Mission.Objectives {
		b.WriteString(m.st.Ok.Render("[x] ") + o.Text + "\n")
	}
	b.WriteString("\n" + m.st.Dim.Render("Enter or m: mission menu  ·  r: replay  ·  q: quit"))
	box := m.st.Box.Width(m.boxWidth()).Render(b.String())
	// Keep the final command output (e.g. the clue text) visible above the box.
	room := m.height - strings.Count(box, "\n") - 2
	if room < 2 {
		return box
	}
	if room > 8 {
		room = 8
	}
	return m.scrollback(room) + "\n" + box
}

func (m *Model) summaryView() string {
	sum := m.sess.Summary()
	var b strings.Builder
	b.WriteString(m.st.Title.Render("Session summary") + "\n\n")
	b.WriteString(fmt.Sprintf("XP earned:         %d\n", sum.XP))
	b.WriteString(fmt.Sprintf("Missions complete: %d\n", len(sum.Completed)))
	for _, t := range sum.Completed {
		b.WriteString("  " + m.st.Ok.Render("[x] ") + t + "\n")
	}
	b.WriteString(fmt.Sprintf("Commands entered:  %d\n\n", sum.Commands))
	if sum.LifetimeXP != sum.XP {
		b.WriteString(fmt.Sprintf("Lifetime XP (saved to your account): %d\n\n", sum.LifetimeXP))
		b.WriteString(m.st.Dim.Render("Progress is saved. Come back anytime! (press any key)"))
	} else {
		b.WriteString(m.st.Dim.Render("Thanks for playing! (press any key)"))
	}
	return "\n" + m.st.Box.Width(m.boxWidth()).Render(b.String()) + "\n"
}
