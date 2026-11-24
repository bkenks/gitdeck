// Package ui is gitdeck's Bubble Tea app: a lazygit-style local view of every
// repo, and a gh-dash-style GitHub view, sharing one layout and theme.
package ui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bkenks/gitdeck/internal/config"
	"github.com/bkenks/gitdeck/internal/git"
	"github.com/bkenks/gitdeck/internal/proc"
	. "github.com/bkenks/gitdeck/internal/theme"
)

type mode int

const (
	modeLocal mode = iota
	modeGitHub
)

type pane int

const (
	pRepos pane = iota
	pFiles
	pBranches
	pCommits
	pStash
	pMain
	pPRs
	pIssues
	pPreview
)

// Options are startup settings from main.
type Options struct {
	Version string
	Cwd     string // git toplevel gitdeck was launched in, if any
	GitHub  bool   // start in GitHub mode
}

type logEntry struct {
	at   time.Time
	cmd  string
	err  string
	repo string
}

type rect struct {
	x, y, w, h int
	p          pane
}

// App is the root model.
type App struct {
	cfg   config.Config
	state config.State
	opts  Options
	w, h  int

	mode   mode
	focus  pane // focus within the current mode
	lfocus pane // remembered local focus while in GitHub mode
	gfocus pane // and vice versa
	back   pane // where esc returns to from the main panel

	// Local
	repos    []git.Repo
	reposL   list
	scanning bool
	scanErr  error
	cur      *repoData
	// repoPending is the repo a debounced detail load is waiting on, and
	// selectAfterScan the repo to put the cursor on once a rescan lands.
	repoPending     string
	selectAfterScan string
	filesL          list
	fileRows        []fileRow
	collapsed       map[string]bool
	branchTab       int
	branchesL       list
	commitsL        list
	stashL          list

	main        pager
	mainTitle   string
	mainKey     string
	mainPending string

	cmdLog  []logEntry
	showLog bool

	// GitHub
	gh ghState

	// Chrome
	filtering  bool
	modal      modal
	toast      string
	toastErr   bool
	toastID    int
	busy       int
	busyLabel  string
	spin       spinner.Model
	spinning   bool // a spinner tick is in flight
	blurred    bool // the terminal reported losing focus
	rects      []rect
	hasLazygit bool
}

func New(cfg config.Config, opts Options) *App {
	a := &App{
		cfg:       cfg,
		state:     config.LoadState(),
		opts:      opts,
		focus:     pRepos,
		lfocus:    pRepos,
		gfocus:    pPRs,
		collapsed: map[string]bool{},
		showLog:   true,
		spin:      spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(T.Cyan)),
	}
	_, err := exec.LookPath(cfg.Lazygit.Command)
	a.hasLazygit = err == nil
	a.gh = newGHState(cfg)
	if opts.GitHub {
		a.mode, a.focus = modeGitHub, pPRs
	}
	return a
}

// ─── Messages ───────────────────────────────────────────────────────────────

type (
	scannedMsg struct {
		repos []git.Repo
		err   error
	}
	statsMsg map[string]git.Stats
	repoMsg  struct{ data *repoData }
	mainMsg  struct {
		key, title string
		lines      []string
	}
	mainDebounceMsg struct{ key string }
	repoDebounceMsg struct{ path string }
	resultMsg       struct {
		res     proc.Result
		ok      string // toast on success
		refresh bool   // reload the repo afterwards
		rescan  bool   // rescan the repos dir afterwards
		then    tea.Cmd
	}
	toastMsg    struct{ id int }
	tickMsg     struct{}
	execDoneMsg struct {
		err  error
		dir  string
		what string
	}
	pullAllMsg struct{ results []proc.Result }
	busyMsg    struct {
		delta int
		label string
	}
)

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{tick(), a.scan()}
	if a.mode == modeGitHub || a.gh.available {
		cmds = append(cmds, a.ghInit())
	}
	if a.cfg.RepoRoot() == "" {
		cmds = append(cmds, func() tea.Msg { return askReposDirMsg{} })
	}
	return tea.Batch(cmds...)
}

func tick() tea.Cmd {
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// ─── Update ─────────────────────────────────────────────────────────────────

// Update handles msg, then keeps the spinner ticking only while something
// it stands for is in flight: an idle spinner would redraw the screen at
// its frame rate for nothing.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if spinTick, ok := msg.(spinner.TickMsg); ok {
		if !a.loading() {
			a.spinning = false
			return a, nil
		}
		var c tea.Cmd
		a.spin, c = a.spin.Update(spinTick)
		return a, c
	}
	c := a.update(msg)
	if !a.spinning && a.loading() {
		a.spinning = true
		c = tea.Batch(c, a.spin.Tick)
	}
	return a, c
}

// loading reports whether anything on screen is showing the spinner.
func (a *App) loading() bool {
	if a.scanning || a.busy > 0 {
		return true
	}
	for _, d := range a.gh.data {
		if d.loading {
			return true
		}
	}
	for _, d := range a.gh.details {
		if d.loading || d.diffLoading {
			return true
		}
	}
	return false
}

func (a *App) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.gh.invalidatePreview()
		return a.syncMain()
	case tea.KeyPressMsg:
		return a.handleKey(msg)
	case tea.PasteMsg:
		if a.modal != nil {
			return a.updateModal(msg)
		}
		if a.filtering {
			a.setFilter(a.filterText() + msg.Content)
		}
		return nil
	case tea.MouseWheelMsg:
		return a.handleWheel(msg)
	case tea.MouseClickMsg:
		return a.handleClick(msg)
	case tea.FocusMsg:
		a.blurred = false
		return a.refreshAll()
	case tea.BlurMsg:
		a.blurred = true
		return nil
	case tickMsg:
		// The status fields the repo list shows come back with the repo
		// itself (see onRepo); the rest of Stats refreshes on focus.
		if !a.blurred && a.modal == nil && a.mode == modeLocal && a.cur != nil {
			return tea.Batch(tick(), a.loadRepo(a.cur.path))
		}
		return tick()
	case toastMsg:
		if msg.id == a.toastID {
			a.toast = ""
		}
		return nil
	case busyMsg:
		a.busy += msg.delta
		if msg.label != "" {
			a.busyLabel = msg.label
		}
		return nil
	case askReposDirMsg:
		a.openReposDirPrompt()
		return nil
	case scannedMsg:
		return a.onScanned(msg)
	case statsMsg:
		return a.onStats(msg)
	case clonedMsg:
		return a.onCloned(msg)
	case repoDebounceMsg:
		return a.onRepoDebounce(msg.path)
	case repoMsg:
		return a.onRepo(msg.data)
	case mainDebounceMsg, mainMsg:
		return a.mainUpdate(msg)
	case resultMsg:
		return a.onResult(msg)
	case pullAllMsg:
		return a.onPullAll(msg)
	case execDoneMsg:
		return a.onExecDone(msg)
	case ghUserMsg, ghSectionMsg, ghDetailMsg, ghDetailDebounceMsg, ghDiffMsg, branchPRsMsg, ghCheckoutMsg:
		return a.ghUpdate(msg)
	}
	// Anything else (cursor blinks and the like) belongs to an open modal.
	if a.modal != nil {
		return a.updateModal(msg)
	}
	return nil
}

// updateModal passes msg to the open modal and closes it when it's done.
func (a *App) updateModal(msg tea.Msg) tea.Cmd {
	done, c := a.modal.update(msg, a)
	if done {
		a.modal = nil
	}
	return c
}

func (a *App) mainUpdate(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case mainDebounceMsg:
		if msg.key == a.mainPending {
			return a.loadMain(msg.key)
		}
	case mainMsg:
		if msg.key == a.mainPending || msg.key == a.mainKey {
			keep := msg.key == a.mainKey
			a.main.set(msg.lines, keep)
			a.mainTitle, a.mainKey, a.mainPending = msg.title, msg.key, ""
		}
	}
	return nil
}

func (a *App) onExecDone(msg execDoneMsg) tea.Cmd {
	var flash tea.Cmd
	if msg.err != nil {
		flash = a.flash(msg.what+": "+msg.err.Error(), true)
	}
	a.logCmd(msg.dir, msg.what, msg.err)
	return tea.Batch(flash, a.refreshAll())
}

func (a *App) flash(text string, isErr bool) tea.Cmd {
	a.toastID++
	a.toast, a.toastErr = text, isErr
	id := a.toastID
	d := 4 * time.Second
	if isErr {
		d = 7 * time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return toastMsg{id} })
}

func (a *App) logCmd(dir, cmd string, err error) {
	e := logEntry{at: time.Now(), cmd: cmd, repo: filepath.Base(dir)}
	if err != nil {
		e.err = err.Error()
	}
	a.cmdLog = append(a.cmdLog, e)
	if len(a.cmdLog) > 200 {
		a.cmdLog = a.cmdLog[len(a.cmdLog)-200:]
	}
}

// withBusy runs work as a command with the footer spinner showing label.
func (a *App) withBusy(label string, work func() tea.Msg) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg { return busyMsg{1, label} },
		work,
		func() tea.Msg { return busyMsg{-1, ""} },
	)
}

// run wraps a git action as a busy command that reports back as a resultMsg.
func (a *App) run(label string, refresh bool, ok string, fn func() proc.Result) tea.Cmd {
	return a.withBusy(label, func() tea.Msg { return resultMsg{res: fn(), ok: ok, refresh: refresh} })
}

func (a *App) onResult(msg resultMsg) tea.Cmd {
	r := msg.res
	if r.Cmd != "" {
		a.logCmd(r.Dir, r.Cmd, r.Err)
	}
	var cmds []tea.Cmd
	if r.Err != nil {
		cmds = append(cmds, a.flash(r.Err.Error(), true))
	} else if msg.ok != "" {
		cmds = append(cmds, a.flash(msg.ok, false))
	}
	if msg.refresh && r.Dir != "" {
		cmds = append(cmds, a.loadRepo(r.Dir), a.loadStats(r.Dir))
		a.mainKey = "" // contents may have changed
		cmds = append(cmds, a.syncMain())
	}
	if msg.rescan {
		cmds = append(cmds, a.scan())
	}
	if msg.then != nil && r.Err == nil {
		cmds = append(cmds, msg.then)
	}
	return tea.Batch(cmds...)
}

// refreshAll reloads whatever is on screen.
func (a *App) refreshAll() tea.Cmd {
	cmds := []tea.Cmd{}
	if a.cur != nil {
		cmds = append(cmds, a.loadRepo(a.cur.path), a.loadStats(a.cur.path))
	}
	a.mainKey = ""
	cmds = append(cmds, a.syncMain())
	return tea.Batch(cmds...)
}

// ─── Keys ───────────────────────────────────────────────────────────────────

func (a *App) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if a.modal != nil {
		return a.updateModal(msg)
	}
	if a.filtering {
		return a.handleFilterKey(msg)
	}
	switch key {
	case "ctrl+c", "q":
		a.state.Save()
		return tea.Quit
	case "?":
		a.openHelp()
		return nil
	case "tab", "shift+tab":
		return a.switchMode()
	case "@":
		a.showLog = !a.showLog
		return nil
	case "R":
		if a.mode == modeGitHub {
			return a.ghRefresh()
		}
		return tea.Batch(a.scan(), a.loadAllStats(a.repoPaths()), a.refreshAll())
	}
	var c tea.Cmd
	var handled bool
	if a.mode == modeLocal {
		handled, c = a.localKey(key)
	} else {
		handled, c = a.ghKey(key)
	}
	if handled {
		return tea.Batch(c, a.syncMain())
	}
	if c := a.customKeybind(key); c != nil {
		return c
	}
	return nil
}

func (a *App) switchMode() tea.Cmd {
	if a.mode == modeLocal {
		a.lfocus = a.focus
		a.mode, a.focus = modeGitHub, a.gfocus
		return a.ghEnsure()
	}
	a.gfocus = a.focus
	a.mode, a.focus = modeLocal, a.lfocus
	return a.syncMain()
}

// listFor returns the list behind a pane, if it has one.
func (a *App) listFor(p pane) *list {
	switch p {
	case pRepos:
		return &a.reposL
	case pFiles:
		return &a.filesL
	case pBranches:
		return &a.branchesL
	case pCommits:
		return &a.commitsL
	case pStash:
		return &a.stashL
	case pPRs:
		return &a.gh.prs.list
	case pIssues:
		return &a.gh.issues.list
	}
	return nil
}

// navKey handles movement shared by every list panel.
func navKey(key string, l *list, h int) bool {
	page := max(1, h-2)
	switch key {
	case "j", "down":
		l.move(1)
	case "k", "up":
		l.move(-1)
	case "ctrl+d", "pgdown":
		l.move(page / 2)
	case "ctrl+u", "pgup":
		l.move(-page / 2)
	case "g", "home":
		l.cursor = 0
	case "G", "end":
		l.cursor = l.len() - 1
		l.clamp()
	default:
		return false
	}
	return true
}

// pagerKey scrolls a pager h rows tall.
func pagerKey(key string, p *pager, h int) bool {
	switch key {
	case "j", "down":
		p.scroll(1, h)
	case "k", "up":
		p.scroll(-1, h)
	case "ctrl+d", "pgdown", "space":
		p.scroll(h/2, h)
	case "ctrl+u", "pgup":
		p.scroll(-h/2, h)
	case "g", "home":
		p.offset = 0
	case "G", "end":
		p.scroll(len(p.lines), h)
	default:
		return false
	}
	return true
}

// ─── Filter ─────────────────────────────────────────────────────────────────

func (a *App) startFilter() {
	a.filtering = a.listFor(a.focus) != nil
}

// filterText is the focused list's filter.
func (a *App) filterText() string {
	if l := a.listFor(a.focus); l != nil {
		return l.filter
	}
	return ""
}

func (a *App) handleFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.filtering = false
		a.setFilter("")
	case "enter":
		a.filtering = false
	case "backspace":
		r := []rune(a.filterText())
		if len(r) > 0 {
			r = r[:len(r)-1]
		}
		a.setFilter(string(r))
	case "ctrl+u":
		a.setFilter("")
	case "up", "ctrl+p":
		if l := a.listFor(a.focus); l != nil {
			l.move(-1)
		}
	case "down", "ctrl+n":
		if l := a.listFor(a.focus); l != nil {
			l.move(1)
		}
	default:
		if t := msg.Key().Text; t != "" {
			a.setFilter(a.filterText() + t)
		} else if msg.String() == "space" {
			a.setFilter(a.filterText() + " ")
		}
	}
	return a.afterMove(a.focus)
}

// setFilter replaces the focused list's filter and refilters it.
func (a *App) setFilter(q string) {
	l := a.listFor(a.focus)
	if l == nil {
		return
	}
	l.filter = q
	l.cursor = 0
	switch a.focus {
	case pRepos:
		a.refilterRepos()
	case pFiles:
		a.rebuildFiles()
	case pBranches:
		a.refilterBranches()
	case pCommits:
		a.refilterCommits()
	case pStash:
		a.refilterStash()
	case pPRs:
		a.gh.refilter(&a.gh.prs)
	case pIssues:
		a.gh.refilter(&a.gh.issues)
	}
}

// filterStatus is the bottom-left of a panel: the live filter, if any.
func (a *App) filterStatus(p pane, l *list) string {
	if a.filtering && a.focus == p {
		return T.SearchingBorder.Render("/" + l.filter + "▏")
	}
	if l.filter != "" {
		return T.Dim.Render("filter: ") + T.Cyan.Render(l.filter)
	}
	return ""
}

// ─── Mouse ──────────────────────────────────────────────────────────────────

func (a *App) paneAt(x, y int) (rect, bool) {
	for _, r := range a.rects {
		if x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h {
			return r, true
		}
	}
	return rect{}, false
}

func (a *App) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if a.modal != nil {
		return nil
	}
	m := msg.Mouse()
	r, ok := a.paneAt(m.X, m.Y)
	if !ok {
		return nil
	}
	d := 3
	if m.Button == tea.MouseWheelUp {
		d = -3
	}
	switch r.p {
	case pMain:
		a.main.scroll(d, r.h-2)
		return nil
	case pPreview:
		a.gh.preview.scroll(d, r.h-2)
		return nil
	}
	if l := a.listFor(r.p); l != nil {
		l.move(d / 3)
		if r.p != a.focus {
			a.setFocus(r.p)
		}
		return a.afterMove(r.p)
	}
	return nil
}

func (a *App) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if a.modal != nil {
		return nil
	}
	m := msg.Mouse()
	if m.Y == 0 {
		// Header: the mode tabs.
		if m.X < 26 {
			if (m.X >= 11 && m.X < 18 && a.mode != modeLocal) || (m.X >= 18 && a.mode != modeGitHub) {
				return a.switchMode()
			}
		}
		return nil
	}
	r, ok := a.paneAt(m.X, m.Y)
	if !ok {
		return nil
	}
	a.setFocus(r.p)
	if l := a.listFor(r.p); l != nil {
		row := m.Y - r.y - 1
		if row >= 0 && row < r.h-2 {
			l.cursor = l.offset + row
			l.clamp()
		}
		return a.afterMove(r.p)
	}
	return a.syncMain()
}

func (a *App) setFocus(p pane) {
	if p == pMain || p == pPreview {
		if a.focus != pMain && a.focus != pPreview {
			a.back = a.focus
		}
	}
	a.focus = p
}

// afterMove reacts to the cursor moving in pane p.
func (a *App) afterMove(p pane) tea.Cmd {
	switch p {
	case pRepos:
		return tea.Batch(a.onRepoCursor(), a.syncMain())
	case pPRs, pIssues:
		return a.ghEnsureDetail()
	}
	return a.syncMain()
}

// ─── View ───────────────────────────────────────────────────────────────────

func (a *App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	v.WindowTitle = a.windowTitle()
	return v
}

func (a *App) windowTitle() string {
	if r := a.selectedRepo(); r != nil && a.mode == modeLocal {
		return "gitdeck · " + r.Name
	}
	return "gitdeck"
}

func (a *App) render() string {
	if a.w == 0 || a.h == 0 {
		return ""
	}
	if a.w < 60 || a.h < 15 {
		msg := "Terminal too small\n" + T.Dim.Render(itoa(a.w)+"×"+itoa(a.h)+", need 60×15")
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, msg)
	}
	a.rects = a.rects[:0]
	bh := a.h - 2
	var body string
	if a.mode == modeLocal {
		body = a.localView(bh)
	} else {
		body = a.ghView(bh)
	}
	screen := a.header() + "\n" + body + "\n" + a.footer()
	if a.modal != nil {
		box := a.modal.view(a)
		bw, bhh := lipgloss.Width(box), lipgloss.Height(box)
		x, y := max(0, (a.w-bw)/2), max(0, (a.h-bhh)/2-1)
		return lipgloss.NewCompositor(
			lipgloss.NewLayer(screen),
			lipgloss.NewLayer(box).X(x).Y(y).Z(1),
		).Render()
	}
	return screen
}

// header is the top bar: app name, mode tabs and the repo in focus.
func (a *App) header() string {
	tab := func(label string, on bool) Seg {
		if on {
			return S(" "+label+" ", Over(T.Bold, T.Selected))
		}
		return S(" "+label+" ", T.Dim)
	}
	left := []Seg{
		S(" ◆ ", T.ActiveBorder), S("gitdeck", T.Bold), S("  ", T.Fg),
		tab("Local", a.mode == modeLocal), tab("GitHub", a.mode == modeGitHub),
		S("   ", T.Fg),
	}
	if r := a.selectedRepo(); r != nil {
		left = append(left, S(config.TildePath(r.Path), T.Dim))
		if r.Loaded {
			left = append(left, S("  ", T.Fg))
			left = append(left, branchSegs(r.Stats)...)
		}
	}
	var right []Seg
	if a.gh.user != "" {
		right = append(right, S("● ", T.Green), S(a.gh.user, T.Dim))
	} else if a.gh.userErr != "" {
		right = append(right, S("○ ", T.Red), S("gh: not signed in", T.Dim))
	}
	right = append(right, S(" ", T.Fg))
	return renderRow(Row{Left: left, Right: right}, a.w, nil)
}

func branchSegs(s git.Stats) []Seg {
	segs := []Seg{S("⎇ ", T.Dim), S(s.Branch, T.Cyan)}
	if s.Ahead > 0 {
		segs = append(segs, S(" ↑"+itoa(s.Ahead), T.Yellow))
	}
	if s.Behind > 0 {
		segs = append(segs, S(" ↓"+itoa(s.Behind), T.Yellow))
	}
	return segs
}

// footer is lazygit's options bar, with status on the right.
func (a *App) footer() string {
	var left []Seg
	if a.filtering {
		left = []Seg{S("Filter: ", T.Options), S("type to narrow", T.Dim), S(" | ", T.Fg),
			S("Keep: ", T.Options), S("<enter>", T.Fg), S(" | ", T.Fg), S("Clear: ", T.Options), S("<esc>", T.Fg)}
	} else {
		for i, h := range a.hints() {
			if !h.footer {
				continue
			}
			if i > 0 && len(left) > 0 {
				left = append(left, S(" | ", T.Fg))
			}
			left = append(left, S(h.desc+": ", T.Options), S(h.key, T.Fg))
		}
		if len(left) > 0 {
			left = append(left, S(" | ", T.Fg))
		}
		left = append(left, S("Keybindings: ", T.Options), S("?", T.Fg))
	}
	var right []Seg
	switch {
	case a.toast != "":
		st, icon := T.Green, "✓ "
		if a.toastErr {
			st, icon = T.Red, "✗ "
		}
		right = append(right, S(icon, st), S(truncate(a.toast, max(20, a.w/2)), st))
	case a.busy > 0:
		right = append(right, S(a.spin.View()+" ", T.Fg), S(a.busyLabel, T.Cyan))
	}
	right = append(right, S("  "+a.opts.Version, T.Dim))
	return renderRow(Row{Left: append([]Seg{S(" ", T.Fg)}, left...), Right: right}, a.w, nil)
}

func truncate(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return ansi.Truncate(s, w, "…")
}

// distribute splits total into parts proportional to weights, with each
// part at least its minimum.
func distribute(total int, weights, mins []int) []int {
	out := make([]int, len(weights))
	sumW, rest := 0, total
	for i, w := range weights {
		out[i] = mins[i]
		rest -= mins[i]
		sumW += w
	}
	if rest <= 0 || sumW == 0 {
		return out
	}
	given := 0
	for i, w := range weights {
		add := rest * w / sumW
		out[i] += add
		given += add
	}
	for i := len(out) - 1; given < rest; i-- {
		if weights[i] > 0 {
			out[i]++
			given++
		}
		if i == 0 {
			i = len(out)
		}
	}
	return out
}
