package ui

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bkenks/gitdeck/internal/config"
	"github.com/bkenks/gitdeck/internal/gh"
	"github.com/bkenks/gitdeck/internal/git"
	. "github.com/bkenks/gitdeck/internal/theme"
)

type section struct {
	title, query string
}

type sectionData struct {
	items   []gh.Item
	total   int
	err     error
	loading bool
	at      time.Time
}

type ghPanel struct {
	kind gh.Kind
	tab  int
	list list
}

type detailData struct {
	d           gh.Detail
	err         error
	loading     bool
	at          time.Time
	diff        []string
	diffErr     error
	diffLoading bool
}

type ghState struct {
	cfg             config.GitHub
	available       bool
	user, userErr   string
	prs, issues     ghPanel
	data            map[string]*sectionData
	details         map[string]*detailData
	preview         pager
	previewFor      string
	showDiff        bool
	search          map[gh.Kind]string
	repoSlug        string
	branchPRs       map[string]map[string]gh.BranchPR
	branchPRsAt     map[string]time.Time
	selectAfterScan string
	version         int
	pendingDetail   string
}

type (
	ghUserMsg struct {
		user string
		err  error
	}
	ghSectionMsg struct {
		key   string
		items []gh.Item
		total int
		err   error
	}
	ghDetailMsg struct {
		key string
		d   gh.Detail
		err error
	}
	ghDetailDebounceMsg struct{ key string }
	ghDiffMsg           struct {
		key   string
		lines []string
		err   error
	}
	branchPRsMsg struct {
		slug string
		m    map[string]gh.BranchPR
	}
	ghCheckoutMsg struct {
		res    git.Result
		path   string
		cloned bool
		lazy   bool
		item   gh.Item
	}
)

func newGHState(cfg config.Config) ghState {
	return ghState{
		cfg:         cfg.GitHub,
		available:   gh.Available(),
		prs:         ghPanel{kind: gh.PR},
		issues:      ghPanel{kind: gh.Issue},
		data:        map[string]*sectionData{},
		details:     map[string]*detailData{},
		search:      map[gh.Kind]string{},
		branchPRs:   map[string]map[string]gh.BranchPR{},
		branchPRsAt: map[string]time.Time{},
	}
}

func (g *ghState) sections(k gh.Kind) []section {
	base := g.cfg.PRSections
	if k == gh.Issue {
		base = g.cfg.IssueSections
	}
	var out []section
	for _, s := range base {
		out = append(out, section{s.Title, s.Filters})
	}
	if g.repoSlug != "" {
		_, name, _ := strings.Cut(g.repoSlug, "/")
		out = append(out, section{"⌂ " + name, "is:open repo:" + g.repoSlug})
	}
	if q := g.search[k]; q != "" {
		out = append(out, section{"Search", q})
	}
	return out
}

func (g *ghState) current(p *ghPanel) (section, bool) {
	secs := g.sections(p.kind)
	if len(secs) == 0 {
		return section{}, false
	}
	if p.tab >= len(secs) {
		p.tab = len(secs) - 1
	}
	return secs[p.tab], true
}

func dataKey(k gh.Kind, q string) string { return k.String() + "|" + q }

func (g *ghState) currentData(p *ghPanel) *sectionData {
	s, ok := g.current(p)
	if !ok {
		return nil
	}
	return g.data[dataKey(p.kind, s.query)]
}

func (g *ghState) items(p *ghPanel) []gh.Item {
	if d := g.currentData(p); d != nil {
		return d.items
	}
	return nil
}

func (g *ghState) refilter(prs bool) {
	p := &g.issues
	if prs {
		p = &g.prs
	}
	items := g.items(p)
	p.list.refilter(len(items), func(i int) string {
		it := items[i]
		return it.Repo + " #" + itoa(it.Number) + " " + it.Title + " " + it.Author + " " + it.Head
	})
}

func (g *ghState) invalidatePreview() { g.previewFor = "" }

func (g *ghState) branchPR(r *git.Repo, branch string) (gh.BranchPR, bool) {
	if r == nil || r.Stats.Slug == "" {
		return gh.BranchPR{}, false
	}
	m := g.branchPRs[r.Stats.Slug]
	pr, ok := m[branch]
	return pr, ok
}

func (g *ghState) openPRCount(r *git.Repo) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, pr := range g.branchPRs[r.Stats.Slug] {
		if pr.State == "OPEN" {
			n++
		}
	}
	return n
}

// ─── Loading ────────────────────────────────────────────────────────────────

func (a *App) ghInit() tea.Cmd {
	if !a.gh.available {
		a.gh.userErr = "gh not installed"
		return nil
	}
	return tea.Batch(
		func() tea.Msg {
			u, err := gh.User()
			return ghUserMsg{u, err}
		},
		a.ghFetch(&a.gh.prs, false),
		a.ghFetch(&a.gh.issues, false),
	)
}

func fetchSection(k gh.Kind, q string, limit int) tea.Cmd {
	return func() tea.Msg {
		items, total, err := gh.Search(k, q, limit)
		return ghSectionMsg{key: dataKey(k, q), items: items, total: total, err: err}
	}
}

func (a *App) ghFetch(p *ghPanel, force bool) tea.Cmd {
	if !a.gh.available {
		return nil
	}
	s, ok := a.gh.current(p)
	if !ok {
		return nil
	}
	key := dataKey(p.kind, s.query)
	d := a.gh.data[key]
	if d != nil && (d.loading || (!force && time.Since(d.at) < 3*time.Minute)) {
		return nil
	}
	if d == nil {
		d = &sectionData{}
		a.gh.data[key] = d
	}
	d.loading = true
	return fetchSection(p.kind, s.query, a.gh.cfg.Limit)
}

// ghEnsure loads what GitHub mode is about to show.
func (a *App) ghEnsure() tea.Cmd {
	slug := ""
	if r := a.selectedRepo(); r != nil {
		slug = r.Stats.Slug
	}
	if slug != a.gh.repoSlug {
		a.gh.repoSlug = slug
		a.gh.refilter(true)
		a.gh.refilter(false)
	}
	return tea.Batch(a.ghFetch(&a.gh.prs, false), a.ghFetch(&a.gh.issues, false), a.ghEnsureDetail())
}

func (a *App) ghRefresh(force bool) tea.Cmd {
	if it := a.ghItem(); it != nil {
		delete(a.gh.details, it.Key())
	}
	return tea.Batch(a.ghFetch(&a.gh.prs, force), a.ghFetch(&a.gh.issues, force), a.ghEnsureDetail())
}

// previewPanel is the list whose selection the preview follows.
func (a *App) previewPanel() *ghPanel {
	p := a.focus
	if p == pPreview {
		p = a.back
	}
	if p == pIssues {
		return &a.gh.issues
	}
	return &a.gh.prs
}

func (a *App) ghItem() *gh.Item {
	p := a.previewPanel()
	items := a.gh.items(p)
	i := p.list.sel()
	if i < 0 || i >= len(items) {
		return nil
	}
	return &items[i]
}

func (a *App) ghEnsureDetail() tea.Cmd {
	it := a.ghItem()
	if it == nil {
		return nil
	}
	key := it.Key()
	d := a.gh.details[key]
	var cmds []tea.Cmd
	if d == nil || (!d.loading && time.Since(d.at) > 2*time.Minute) {
		a.gh.pendingDetail = key
		cmds = append(cmds, tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return ghDetailDebounceMsg{key} }))
	}
	if a.gh.showDiff && it.Kind == gh.PR && (d == nil || (d.diff == nil && !d.diffLoading && d.diffErr == nil)) {
		if d == nil {
			d = &detailData{}
			a.gh.details[key] = d
		}
		d.diffLoading = true
		item := *it
		cmds = append(cmds, func() tea.Msg {
			out, err := gh.Diff(item)
			return ghDiffMsg{key: key, lines: colorDiff(out), err: err}
		})
	}
	return tea.Batch(cmds...)
}

func fetchDetail(it gh.Item) tea.Cmd {
	return func() tea.Msg {
		d, err := gh.GetDetail(it)
		return ghDetailMsg{key: it.Key(), d: d, err: err}
	}
}

func (a *App) ghBranchPRs(r *git.Repo) tea.Cmd {
	if r == nil || r.Stats.Slug == "" || !a.gh.available || a.gh.userErr != "" {
		return nil
	}
	slug := r.Stats.Slug
	if t, ok := a.gh.branchPRsAt[slug]; ok && time.Since(t) < 2*time.Minute {
		return nil
	}
	a.gh.branchPRsAt[slug] = time.Now()
	return func() tea.Msg {
		m, err := gh.BranchPRs(slug)
		if err != nil {
			return nil
		}
		return branchPRsMsg{slug, m}
	}
}

func (a *App) ghUpdate(msg tea.Msg) tea.Cmd {
	g := &a.gh
	switch msg := msg.(type) {
	case ghUserMsg:
		if msg.err != nil {
			g.userErr = msg.err.Error()
			return nil
		}
		g.user, g.userErr = msg.user, ""
		return a.ghBranchPRs(a.selectedRepo())
	case ghSectionMsg:
		d := g.data[msg.key]
		if d == nil {
			d = &sectionData{}
			g.data[msg.key] = d
		}
		d.loading, d.at, d.err = false, time.Now(), msg.err
		if msg.err == nil {
			d.items, d.total = msg.items, msg.total
		}
		g.refilter(true)
		g.refilter(false)
		g.invalidatePreview()
		if a.mode == modeGitHub {
			return a.ghEnsureDetail()
		}
	case ghDetailDebounceMsg:
		it := a.ghItem()
		if it == nil || it.Key() != msg.key || msg.key != g.pendingDetail {
			return nil
		}
		d := g.details[msg.key]
		if d == nil {
			d = &detailData{}
			g.details[msg.key] = d
		}
		d.loading = true
		return fetchDetail(*it)
	case ghDetailMsg:
		d := g.details[msg.key]
		if d == nil {
			d = &detailData{}
			g.details[msg.key] = d
		}
		d.d, d.err, d.loading, d.at = msg.d, msg.err, false, time.Now()
		g.version++
	case ghDiffMsg:
		d := g.details[msg.key]
		if d == nil {
			d = &detailData{}
			g.details[msg.key] = d
		}
		d.diff, d.diffErr, d.diffLoading = msg.lines, msg.err, false
		g.version++
	case branchPRsMsg:
		g.branchPRs[msg.slug] = msg.m
	case ghCheckoutMsg:
		return a.onGHCheckout(msg)
	}
	return nil
}

// ─── Rows ───────────────────────────────────────────────────────────────────

func stateOf(it gh.Item) (glyph, label string, st lipgloss.Style) {
	if it.Kind == gh.Issue {
		if it.State == "CLOSED" {
			return "✔", "Closed", T.Magenta
		}
		return "○", "Open", T.Green
	}
	switch {
	case it.State == "MERGED":
		return "◆", "Merged", T.Magenta
	case it.State == "CLOSED":
		return "✕", "Closed", T.Red
	case it.Draft:
		return "◌", "Draft", T.Dim
	}
	return "●", "Open", T.Green
}

func checksSeg(state string) Seg {
	switch state {
	case "SUCCESS":
		return S("✓", T.Green)
	case "FAILURE", "ERROR":
		return S("✗", T.Red)
	case "PENDING", "EXPECTED":
		return S("●", T.Yellow)
	}
	return S(" ", T.Fg)
}

func reviewSeg(decision string) []Seg {
	switch decision {
	case "APPROVED":
		return []Seg{S("approved ", T.Green)}
	case "CHANGES_REQUESTED":
		return []Seg{S("changes ", T.Red)}
	case "REVIEW_REQUIRED":
		return []Seg{S("review ", T.Yellow)}
	}
	return nil
}

func (a *App) ghRow(p *ghPanel) func(i int) Row {
	items := a.gh.items(p)
	s, _ := a.gh.current(p)
	repoScoped := strings.Contains(s.query, "repo:")
	return func(i int) Row {
		it := items[i]
		g, _, st := stateOf(it)
		left := []Seg{S(g+" ", st)}
		if !repoScoped {
			_, name, _ := strings.Cut(it.Repo, "/")
			left = append(left, S(name+" ", T.Cyan))
		}
		left = append(left, S("#"+itoa(it.Number)+" ", T.Dim), S(it.Title, T.Fg))
		var right []Seg
		if it.Kind == gh.PR {
			right = append(right, reviewSeg(it.ReviewDecision)...)
			right = append(right, checksSeg(it.Checks), S(" ", T.Fg),
				S("+"+itoa(it.Additions), T.Green), S(" -"+itoa(it.Deletions)+" ", T.Red))
		} else {
			for j, l := range it.Labels {
				if j == 2 {
					break
				}
				right = append(right, S(l.Name+" ", T.Hue(l.Color)))
			}
			if it.Comments > 0 {
				right = append(right, S("↩"+itoa(it.Comments)+" ", T.Dim))
			}
		}
		right = append(right, S(fit(ago(it.Updated), 3), T.Dim))
		return Row{Left: left, Right: right}
	}
}

// ─── Preview ────────────────────────────────────────────────────────────────

func rule(title string, width int) string {
	head := T.Dim.Render("── ") + T.Bold.Render(title) + " "
	return head + T.Dim.Render(strings.Repeat("─", max(0, width-lipgloss.Width(head))))
}

func (a *App) previewLines(width int) []string {
	it := a.ghItem()
	if it == nil {
		return a.ghEmptyPreview()
	}
	d := a.gh.details[it.Key()]
	key := it.Key() + "|" + itoa(width) + "|" + itoa(a.gh.version)
	if a.gh.showDiff {
		key += "|diff"
	}
	if d != nil && d.loading {
		key += "|loading"
	}
	if key == a.gh.previewFor {
		return a.gh.preview.lines
	}
	keep := strings.HasPrefix(a.gh.previewFor, it.Key()+"|")
	a.gh.previewFor = key
	var lines []string
	if a.gh.showDiff && it.Kind == gh.PR {
		switch {
		case d == nil || d.diffLoading:
			lines = []string{a.spin.View() + T.Dim.Render(" loading diff…")}
		case d.diffErr != nil:
			lines = []string{T.Red.Render(d.diffErr.Error())}
		default:
			lines = d.diff
		}
	} else {
		lines = renderItem(*it, d, width, a.spin.View())
	}
	a.gh.preview.set(lines, keep)
	return lines
}

func (a *App) ghEmptyPreview() []string {
	if !a.gh.available {
		return []string{"", T.Bold.Render("GitHub needs the gh CLI"), "",
			"Install it from " + T.Blue.Render("https://cli.github.com"), "then run " + T.Cyan.Render("gh auth login") + "."}
	}
	if a.gh.userErr != "" {
		return []string{"", T.Bold.Render("gh isn't signed in"), "", "Run " + T.Cyan.Render("gh auth login") + ", then press R.", "", T.Dim.Render(a.gh.userErr)}
	}
	return []string{T.Dim.Render("Nothing selected.")}
}

func renderItem(it gh.Item, d *detailData, width int, spin string) []string {
	glyph, label, st := stateOf(it)
	var out []string
	out = append(out, T.Dim.Render(it.Repo+" #"+itoa(it.Number)))
	out = append(out, wrap(T.Bold.Render(it.Title), width)...)
	out = append(out, "")
	meta := st.Render(glyph+" "+label) + "  " + T.Author(it.Author).Render(it.Author)
	if it.Kind == gh.PR {
		meta += T.Dim.Render(" wants to merge ") + T.Cyan.Render(it.Head) + T.Dim.Render(" into ") + T.Cyan.Render(it.Base)
	} else {
		meta += T.Dim.Render(" opened " + agoLong(it.Created))
	}
	out = append(out, wrap(meta, width)...)
	stats := T.Dim.Render("updated " + agoLong(it.Updated))
	if it.Kind == gh.PR {
		stats = T.Green.Render("+"+itoa(it.Additions)) + " " + T.Red.Render("-"+itoa(it.Deletions))
		if d != nil && d.d.ChangedFiles > 0 {
			stats += T.Dim.Render(" in " + plural(d.d.ChangedFiles, "file"))
		}
		stats += T.Dim.Render(" · updated " + agoLong(it.Updated))
	}
	if it.Comments > 0 {
		stats += T.Dim.Render(" · " + plural(it.Comments, "comment"))
	}
	out = append(out, stats)
	if len(it.Labels) > 0 || len(it.Assignees) > 0 {
		var chips []string
		for _, l := range it.Labels {
			chips = append(chips, T.Hue(l.Color).Render("◖"+l.Name+"◗"))
		}
		if len(it.Assignees) > 0 {
			chips = append(chips, T.Dim.Render("→ ")+strings.Join(it.Assignees, ", "))
		}
		out = append(out, wrap(strings.Join(chips, " "), width)...)
	}
	if d == nil || (d.loading && d.at.IsZero()) {
		return append(out, "", spin+T.Dim.Render(" loading…"))
	}
	if d.err != nil {
		return append(out, "", T.Red.Render(d.err.Error()))
	}
	dd := d.d

	if it.Kind == gh.PR && len(dd.Checks) > 0 {
		pass, fail, pend := 0, 0, 0
		var failing, pending []gh.Check
		for _, c := range dd.Checks {
			switch c.Outcome() {
			case "pass":
				pass++
			case "fail":
				fail++
				failing = append(failing, c)
			case "pending":
				pend++
				pending = append(pending, c)
			}
		}
		summary := T.Green.Render("✓ "+itoa(pass)) + "  " + T.Red.Render("✗ "+itoa(fail)) + "  " + T.Yellow.Render("● "+itoa(pend))
		out = append(out, "", rule("Checks", width-lipgloss.Width(summary)-1)+" "+summary)
		show := append(failing, pending...)
		for i, c := range show {
			if i == 8 {
				out = append(out, T.Dim.Render("  … "+itoa(len(show)-8)+" more"))
				break
			}
			g, s := "✗", T.Red
			if c.Outcome() == "pending" {
				g, s = "●", T.Yellow
			}
			line := "  " + s.Render(g) + " " + c.Name
			if c.Workflow != "" {
				line += T.Dim.Render("  " + c.Workflow)
			}
			out = append(out, line)
		}
		if len(show) == 0 {
			out = append(out, T.Green.Render("  All checks passed"))
		}
	}

	if it.Kind == gh.PR && (len(dd.Reviews) > 0 || len(dd.ReviewRequests) > 0) {
		out = append(out, "", rule("Reviews", width))
		for _, r := range dd.Reviews {
			g, s, word := "●", T.Dim, strings.ToLower(strings.ReplaceAll(r.State, "_", " "))
			switch r.State {
			case "APPROVED":
				g, s, word = "✓", T.Green, "approved"
			case "CHANGES_REQUESTED":
				g, s, word = "✗", T.Red, "requested changes"
			case "DISMISSED":
				g, s, word = "○", T.Dim, "dismissed"
			}
			out = append(out, "  "+s.Render(g)+" "+T.Author(r.Author).Render(r.Author)+" "+s.Render(word)+T.Dim.Render(" "+agoLong(r.At)))
		}
		for _, r := range dd.ReviewRequests {
			out = append(out, "  "+T.Yellow.Render("○")+" "+T.Author(r).Render(r)+T.Dim.Render(" review requested"))
		}
	}

	out = append(out, "", rule("Description", width), "")
	out = append(out, markdown(dd.Body, width)...)

	if len(dd.Comments) > 0 {
		out = append(out, "", rule("Comments "+T.Dim.Render(itoa(len(dd.Comments))), width))
		start := max(0, len(dd.Comments)-10)
		if start > 0 {
			out = append(out, T.Dim.Render("  "+plural(start, "earlier comment")+" on GitHub"))
		}
		for _, c := range dd.Comments[start:] {
			out = append(out, "", T.Author(c.Author).Bold(true).Render(c.Author)+T.Dim.Render(" · "+agoLong(c.At)))
			for _, l := range markdown(c.Body, width-2) {
				out = append(out, T.Dim.Render("│ ")+l)
			}
		}
	}
	return out
}

// ─── Layout ─────────────────────────────────────────────────────────────────

func (a *App) ghView(bh int) string {
	lw := max(50, a.w*11/20)
	lw = min(lw, a.w-32)
	rw := a.w - lw
	weights := []int{3, 2}
	if a.focus == pIssues || (a.focus == pPreview && a.back == pIssues) {
		weights = []int{2, 3}
	}
	hs := distribute(bh, weights, []int{5, 5})
	left := []string{a.ghListPanel(&a.gh.prs, pPRs, "1", lw, hs[0]), a.ghListPanel(&a.gh.issues, pIssues, "2", lw, hs[1])}
	a.rects = append(a.rects, rect{0, 1, lw, hs[0], pPRs}, rect{0, 1 + hs[0], lw, hs[1], pIssues})

	logH := 0
	if a.showLog && len(a.cmdLog) > 0 {
		logH = min(8, bh/4+1)
	}
	ph := bh - logH
	right := []string{a.previewPanelView(rw, ph)}
	a.rects = append(a.rects, rect{lw, 1, rw, ph, pPreview})
	if logH > 0 {
		right = append(right, a.logPanel(rw, logH))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(left, "\n"), strings.Join(right, "\n"))
}

func (a *App) ghListPanel(p *ghPanel, pn pane, idx string, w, h int) string {
	focused := a.focus == pn
	secs := a.gh.sections(p.kind)
	var tabs []string
	for _, s := range secs {
		t := s.title
		if d := a.gh.data[dataKey(p.kind, s.query)]; d != nil && d.err == nil && !d.at.IsZero() {
			t += " " + itoa(d.total)
		}
		tabs = append(tabs, t)
	}
	panel := Panel{W: w, H: h, Index: idx, Tabs: tabs, Tab: p.tab, Focused: focused, Searching: focused && a.filtering}
	d := a.gh.currentData(p)
	rows := h - 2
	switch {
	case !a.gh.available:
		panel.Body = []string{T.Dim.Render(" gh CLI not found: https://cli.github.com")}
	case d == nil || (d.loading && d.at.IsZero()):
		panel.Body = []string{" " + a.spin.View() + T.Dim.Render(" loading…")}
	case d.err != nil && len(d.items) == 0:
		panel.Body = wrap(T.Red.Render(" "+d.err.Error()), w-3)
	case len(d.items) == 0:
		what := "pull requests"
		if p.kind == gh.Issue {
			what = "issues"
		}
		panel.Body = []string{T.Dim.Render(" No " + what + " here.")}
	default:
		panel.Body = renderList(&p.list, w-2, rows, focused, a.ghRow(p))
	}
	panel.Footer = p.list.footer()
	if d != nil && d.total > len(d.items) {
		panel.Footer += T.Dim.Render(" (" + itoa(d.total) + ")")
	}
	panel.Status = a.filterStatus(pn, &p.list)
	if panel.Status == "" && d != nil && d.loading && !d.at.IsZero() {
		panel.Status = a.spin.View()
	}
	panel.Total, panel.Offset = p.list.len(), p.list.offset
	return panel.Render()
}

func (a *App) previewPanelView(w, h int) string {
	inner, rows := w-2, h-2
	a.previewLines(inner - 2)
	// view returns a window onto the cached lines; indent a copy.
	view := a.gh.preview.view(rows)
	body := make([]string, len(view))
	for i, l := range view {
		body[i] = " " + l
	}
	p := Panel{W: w, H: h, Index: "0", Focused: a.focus == pPreview, Body: body,
		Total: len(a.gh.preview.lines), Offset: a.gh.preview.offset}
	if it := a.ghItem(); it != nil && it.Kind == gh.PR {
		p.Tabs = []string{"Overview", "Diff"}
		if a.gh.showDiff {
			p.Tab = 1
		}
	} else {
		p.Title = "Preview"
	}
	if p.Total > rows && rows > 0 {
		p.Footer = itoa(min(100, (a.gh.preview.offset+rows)*100/p.Total)) + "%"
	}
	return p.Render()
}

// ─── Keys ───────────────────────────────────────────────────────────────────

func (a *App) ghKey(key string) (bool, tea.Cmd) {
	switch key {
	case "1":
		a.setFocus(pPRs)
		return true, a.ghEnsureDetail()
	case "2":
		a.setFocus(pIssues)
		return true, a.ghEnsureDetail()
	case "0", "enter":
		if a.focus != pPreview {
			a.setFocus(pPreview)
			return true, nil
		}
	case "esc":
		if a.focus == pPreview {
			a.focus = a.back
			return true, nil
		}
		if l := a.listFor(a.focus); l != nil && l.filter != "" {
			a.filterInput = ""
			a.applyFilter()
		}
		return true, a.ghEnsureDetail()
	case "h", "left", "l", "right":
		switch a.focus {
		case pPreview:
			a.focus = a.back
		case pPRs:
			a.focus = pIssues
		default:
			a.focus = pPRs
		}
		return true, a.ghEnsureDetail()
	case "/":
		a.startFilter()
		return true, nil
	case "[", "]":
		p := a.previewPanel()
		n := len(a.gh.sections(p.kind))
		if n == 0 {
			return true, nil
		}
		d := 1
		if key == "[" {
			d = -1
		}
		p.tab = (p.tab + d + n) % n
		p.list.reset()
		p.list.filter = ""
		a.gh.refilter(p.kind == gh.PR)
		return true, tea.Batch(a.ghFetch(p, false), a.ghEnsureDetail())
	case "s":
		a.openGHSearch()
		return true, nil
	case "d":
		a.gh.showDiff = !a.gh.showDiff
		a.gh.invalidatePreview()
		return true, a.ghEnsureDetail()
	}

	if it := a.ghItem(); it != nil {
		if handled, c := a.ghItemKey(key, *it); handled {
			return true, c
		}
	}
	if a.focus == pPreview {
		return a.pagerKey(key, &a.gh.preview, a.panelRows(pPreview)), nil
	}
	if l := a.listFor(a.focus); l != nil && a.navKey(key, l, a.panelRows(a.focus)) {
		return true, a.ghEnsureDetail()
	}
	return false, nil
}

func (a *App) ghItemKey(key string, it gh.Item) (bool, tea.Cmd) {
	num := "#" + itoa(it.Number)
	refresh := func() tea.Cmd {
		p := a.previewPanel()
		s, _ := a.gh.current(p)
		return tea.Batch(fetchDetail(it), fetchSection(p.kind, s.query, a.gh.cfg.Limit))
	}
	ghRun := func(label, ok string, fn func() git.Result) tea.Cmd {
		then := refresh()
		return tea.Sequence(
			func() tea.Msg { return busyMsg{1, label} },
			func() tea.Msg { return resultMsg{res: fn(), ok: ok, then: then} },
			func() tea.Msg { return busyMsg{-1, ""} },
		)
	}
	switch key {
	case "o":
		return true, openURL(it.URL)
	case "y":
		return true, a.copy(it.URL)
	case "c":
		a.modal = newTextArea("Comment on "+num, it.Title, "", func(body string) tea.Cmd {
			if strings.TrimSpace(body) == "" {
				return nil
			}
			return ghRun("Commenting", "Commented on "+num, func() git.Result { return gh.CommentOn(it, body) })
		})
		return true, nil
	case "x":
		if it.State != "OPEN" {
			return true, a.flash(num+" isn't open", true)
		}
		a.confirm("Close "+num+"?", []string{it.Title},
			ghRun("Closing", "Closed "+num, func() git.Result { return gh.Close(it) }))
		a.modal.(*confirmModal).danger = true
		return true, nil
	case "X":
		if it.State != "CLOSED" {
			return true, a.flash(num+" isn't closed", true)
		}
		return true, ghRun("Reopening", "Reopened "+num, func() git.Result { return gh.Reopen(it) })
	}
	if it.Kind != gh.PR {
		return false, nil
	}
	switch key {
	case "space":
		return true, a.ghCheckout(it, false)
	case "L", "ctrl+g":
		return true, a.ghCheckout(it, true)
	case "a":
		a.confirm("Approve "+num+"?", []string{it.Title, T.Dim.Render(it.Repo)},
			ghRun("Approving", "Approved "+num, func() git.Result { return gh.Approve(it) }))
		return true, nil
	case "r":
		if !it.Draft {
			return true, a.flash(num+" isn't a draft", true)
		}
		return true, ghRun("Marking ready", num+" is ready for review", func() git.Result { return gh.MarkReady(it) })
	case "m":
		if it.State != "OPEN" {
			return true, a.flash(num+" isn't open", true)
		}
		merge := func(method string) func() tea.Cmd {
			return func() tea.Cmd {
				return ghRun("Merging", "Merged "+num, func() git.Result { return gh.Merge(it, method) })
			}
		}
		a.modal = &menuModal{title: "Merge " + num, items: []menuItem{
			{"s", "Squash and merge", merge("squash")},
			{"m", "Create a merge commit", merge("merge")},
			{"r", "Rebase and merge", merge("rebase")},
		}}
		return true, nil
	}
	return false, nil
}

func (a *App) openGHSearch() {
	p := a.previewPanel()
	what := "pull requests"
	if p.kind == gh.Issue {
		what = "issues"
	}
	a.modal = newInput("Search "+what, "GitHub search syntax, e.g. repo:charmbracelet/bubbletea is:open label:bug", a.gh.search[p.kind], func(q string) tea.Cmd {
		q = strings.TrimSpace(q)
		a.gh.search[p.kind] = q
		if q == "" {
			p.tab = 0
		} else {
			p.tab = len(a.gh.sections(p.kind)) - 1
		}
		p.list.reset()
		a.gh.refilter(p.kind == gh.PR)
		return tea.Batch(a.ghFetch(p, true), a.ghEnsureDetail())
	})
}

// ─── Checkout: GitHub → local ───────────────────────────────────────────────

// ghCheckout checks a PR out in its local clone, cloning it first if
// needed, then jumps to the local view (or straight into lazygit).
func (a *App) ghCheckout(it gh.Item, lazy bool) tea.Cmd {
	if r := a.repoBySlug(it.Repo); r != nil {
		path := r.Path
		a.touch(path)
		return a.ghCheckoutCmd(it, path, false, lazy)
	}
	root := a.cfg.RepoRoot()
	if root == "" {
		a.openReposDirPrompt()
		return nil
	}
	dest := filepath.Join(root, filepath.FromSlash(it.Repo))
	a.confirm("Clone "+it.Repo+"?", []string{
		"You don't have " + T.Bold.Render(it.Repo) + " locally yet.",
		"Clone it into " + T.Cyan.Render(config.TildePath(dest)) + " and check out #" + itoa(it.Number) + "?",
	}, a.ghCheckoutCmd(it, dest, true, lazy))
	return nil
}

func (a *App) ghCheckoutCmd(it gh.Item, path string, clone, lazy bool) tea.Cmd {
	label := "Checking out #" + itoa(it.Number)
	return tea.Sequence(
		func() tea.Msg { return busyMsg{1, label} },
		func() tea.Msg {
			if clone {
				if r := git.CloneGH(it.Repo, path); r.Err != nil {
					return ghCheckoutMsg{res: r, path: path, item: it}
				}
			}
			return ghCheckoutMsg{res: gh.Checkout(path, it), path: path, cloned: clone, lazy: lazy, item: it}
		},
		func() tea.Msg { return busyMsg{-1, ""} },
	)
}

func (a *App) onGHCheckout(msg ghCheckoutMsg) tea.Cmd {
	a.logCmd(msg.path, msg.res.Cmd, msg.res.Err)
	if msg.res.Err != nil {
		return a.flash(msg.res.Err.Error(), true)
	}
	cmds := []tea.Cmd{a.flash("Checked out #"+itoa(msg.item.Number)+" ("+msg.item.Head+")", false)}
	if a.mode == modeGitHub {
		a.gfocus = a.focus
		a.mode = modeLocal
	}
	a.focus, a.lfocus = pBranches, pBranches
	a.branchTab = 0
	if msg.cloned {
		a.gh.selectAfterScan = msg.path
		cmds = append(cmds, a.scan())
	} else {
		cmds = append(cmds, a.selectRepoPath(msg.path))
	}
	if msg.lazy {
		cmds = append(cmds, a.openLazygit(msg.path, "status"))
	}
	return tea.Batch(cmds...)
}

// selectRepoPath puts the cursor on a repo and loads it.
func (a *App) selectRepoPath(path string) tea.Cmd {
	for i := range a.repos {
		if a.repos[i].Path != path {
			continue
		}
		if !a.reposL.selectIndex(i) {
			a.reposL.filter = ""
			a.refilterRepos()
			a.reposL.selectIndex(i)
		}
		a.cur = nil
		return tea.Batch(a.onRepoCursor(), a.loadStats(path))
	}
	return nil
}
