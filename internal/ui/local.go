package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bkenks/gitdeck/internal/config"
	"github.com/bkenks/gitdeck/internal/gh"
	"github.com/bkenks/gitdeck/internal/git"
	. "github.com/bkenks/gitdeck/internal/theme"
)

// repoData is everything the detail panels show for the selected repo.
type repoData struct {
	path     string
	status   git.Status
	branches []git.Branch
	remotes  []git.Branch
	tags     []git.Tag
	commits  []git.Commit
	stashes  []git.Stash
	err      error
	loaded   bool
}

// fileRow is a line in the files tree: a directory or a file.
type fileRow struct {
	depth int
	name  string
	path  string
	dir   bool
	file  git.File
	files []git.File // every file at or under this row
}

var branchTabs = []string{"Local branches", "Remotes", "Tags"}

// ─── Loading ────────────────────────────────────────────────────────────────

func (a *App) scan() tea.Cmd {
	root := a.cfg.RepoRoot()
	cwd := a.opts.Cwd
	a.scanning = true
	return func() tea.Msg {
		var repos []git.Repo
		var err error
		if root != "" {
			repos, err = git.Scan(root)
		}
		if cwd != "" {
			found := false
			real, _ := filepath.EvalSymlinks(cwd)
			for _, r := range repos {
				if rp, _ := filepath.EvalSymlinks(r.Path); rp == real {
					found = true
					break
				}
			}
			if !found {
				repos = append(repos, git.Repo{
					Name: filepath.Base(cwd), Key: config.TildePath(cwd), Path: cwd, Cwd: true,
					Modified: git.Modified(cwd),
				})
			}
		}
		return scannedMsg{repos, err}
	}
}

func (a *App) onScanned(msg scannedMsg) tea.Cmd {
	a.scanning = false
	a.scanErr = msg.err
	prev := ""
	if r := a.selectedRepo(); r != nil {
		prev = r.Path
	} else if a.opts.Cwd != "" {
		prev = a.opts.Cwd
	}
	if a.gh.selectAfterScan != "" {
		prev, a.gh.selectAfterScan = a.gh.selectAfterScan, ""
	}
	old := map[string]git.Repo{}
	for _, r := range a.repos {
		old[r.Path] = r
	}
	a.repos = msg.repos
	var paths []string
	for i := range a.repos {
		if o, ok := old[a.repos[i].Path]; ok && o.Loaded {
			a.repos[i].Stats, a.repos[i].Loaded = o.Stats, true
		}
		paths = append(paths, a.repos[i].Path)
	}
	a.sortRepos(prev)
	return tea.Batch(a.loadAllStats(paths), a.onRepoCursor(), a.syncMain())
}

func (a *App) loadAllStats(paths []string) tea.Cmd {
	if len(paths) == 0 {
		return nil
	}
	return func() tea.Msg {
		out := statsMsg{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, p := range paths {
			wg.Add(1)
			sem <- struct{}{}
			go func(p string) {
				defer func() { <-sem; wg.Done() }()
				s := git.LoadStats(p)
				mu.Lock()
				out[p] = s
				mu.Unlock()
			}(p)
		}
		wg.Wait()
		return out
	}
}

func (a *App) loadStats(path string) tea.Cmd {
	return func() tea.Msg { return statsMsg{path: git.LoadStats(path)} }
}

func (a *App) onStats(msg statsMsg) tea.Cmd {
	for i := range a.repos {
		if s, ok := msg[a.repos[i].Path]; ok {
			a.repos[i].Stats, a.repos[i].Loaded = s, true
		}
	}
	if len(msg) > 1 {
		prev := ""
		if r := a.selectedRepo(); r != nil {
			prev = r.Path
		}
		a.sortRepos(prev)
	}
	a.refilterRepos()
	// The selected repo's GitHub slug may only now be known.
	return a.ghBranchPRs(a.selectedRepo())
}

func (a *App) loadRepo(path string) tea.Cmd {
	return func() tea.Msg {
		d := &repoData{path: path, loaded: true}
		var wg sync.WaitGroup
		wg.Add(6)
		go func() { defer wg.Done(); d.status, d.err = git.GetStatus(path) }()
		go func() { defer wg.Done(); d.branches, _ = git.Branches(path) }()
		go func() { defer wg.Done(); d.remotes, _ = git.RemoteBranches(path) }()
		go func() { defer wg.Done(); d.tags, _ = git.Tags(path) }()
		go func() { defer wg.Done(); d.commits, _ = git.Log(path, 300) }()
		go func() { defer wg.Done(); d.stashes, _ = git.Stashes(path) }()
		wg.Wait()
		return repoMsg{d}
	}
}

// onRepoCursor loads the detail panels when the selected repo changes.
func (a *App) onRepoCursor() tea.Cmd {
	r := a.selectedRepo()
	if r == nil {
		a.cur = nil
		return nil
	}
	if a.cur != nil && a.cur.path == r.Path {
		return nil
	}
	a.cur = &repoData{path: r.Path}
	a.filesL.reset()
	a.branchesL.reset()
	a.commitsL.reset()
	a.stashL.reset()
	a.filesL.filter, a.branchesL.filter, a.commitsL.filter, a.stashL.filter = "", "", "", ""
	a.collapsed = map[string]bool{}
	a.rebuildDetail()
	return tea.Batch(a.loadRepo(r.Path), a.ghBranchPRs(r))
}

func (a *App) onRepo(d *repoData) tea.Cmd {
	if a.cur == nil || a.cur.path != d.path {
		return nil
	}
	a.cur = d
	for i := range a.repos {
		if a.repos[i].Path == d.path && a.repos[i].Loaded {
			s := &a.repos[i].Stats
			s.Branch, s.Detached = d.status.Branch, d.status.Detached
			s.Upstream, s.Ahead, s.Behind = d.status.Upstream != "", d.status.Ahead, d.status.Behind
			s.Dirty = len(d.status.Files)
		}
	}
	a.rebuildDetail()
	return a.syncMain()
}

func (a *App) rebuildDetail() {
	a.rebuildFiles()
	a.refilterBranches()
	a.refilterCommits()
	a.refilterStash()
}

// ─── Repos ──────────────────────────────────────────────────────────────────

func (a *App) selectedRepo() *git.Repo {
	i := a.reposL.sel()
	if i < 0 || i >= len(a.repos) {
		return nil
	}
	return &a.repos[i]
}

func (a *App) repoByPath(path string) *git.Repo {
	for i := range a.repos {
		if a.repos[i].Path == path {
			return &a.repos[i]
		}
	}
	return nil
}

// repoBySlug finds a local clone of a GitHub repo.
func (a *App) repoBySlug(slug string) *git.Repo {
	slug = strings.ToLower(slug)
	for i := range a.repos {
		r := &a.repos[i]
		if strings.ToLower(r.Stats.Slug) == slug {
			return r
		}
	}
	for i := range a.repos {
		if strings.EqualFold(a.repos[i].Key, slug) || strings.HasSuffix(strings.ToLower(a.repos[i].Key), "/"+slug) {
			return &a.repos[i]
		}
	}
	return nil
}

func (a *App) recency(r git.Repo) time.Time {
	t := r.Modified
	if r.Stats.LastCommit.After(t) {
		t = r.Stats.LastCommit
	}
	if u := a.state.LastUsed[r.Path]; u.After(t) {
		t = u
	}
	return t
}

var sortModes = []string{"recent", "name", "namespace"}

func (a *App) sortRepos(keep string) {
	mode := a.sortMode
	sort.SliceStable(a.repos, func(i, j int) bool {
		ri, rj := a.repos[i], a.repos[j]
		if ri.Cwd != rj.Cwd {
			return ri.Cwd
		}
		switch mode {
		case "name", "name-asc":
			return strings.ToLower(ri.Name) < strings.ToLower(rj.Name)
		case "namespace":
			return strings.ToLower(ri.Key) < strings.ToLower(rj.Key)
		default:
			return a.recency(ri).After(a.recency(rj))
		}
	})
	a.refilterRepos()
	if keep != "" {
		for i := range a.repos {
			if a.repos[i].Path == keep {
				a.reposL.selectIndex(i)
				break
			}
		}
	}
}

func (a *App) refilterRepos() {
	a.reposL.refilter(len(a.repos), func(i int) string { return a.repos[i].Key + " " + a.repos[i].Stats.Slug })
}

func (a *App) touch(path string) {
	a.state.LastUsed[path] = time.Now()
	// Save a copy: the map keeps changing on the UI goroutine.
	snap := config.State{LastUsed: make(map[string]time.Time, len(a.state.LastUsed))}
	for k, v := range a.state.LastUsed {
		snap.LastUsed[k] = v
	}
	go snap.Save()
}

func (a *App) repoRow(i int) Row {
	r := a.repos[i]
	var left []Seg
	if r.Cwd {
		left = append(left, S("⌂ ", T.Cyan), S(r.Name, T.Bold), S(" "+r.Key, T.Dim))
	} else {
		if ns := r.Namespace(); ns != "" {
			left = append(left, S(ns+"/", T.Dim))
		}
		left = append(left, S(r.Name, T.Fg))
	}
	if !r.Loaded {
		return Row{Left: left, Right: []Seg{S("…", T.Dim)}}
	}
	s := r.Stats
	var right []Seg
	if s.Dirty > 0 {
		right = append(right, S("~"+itoa(s.Dirty)+" ", T.Unstaged))
	}
	if s.Ahead > 0 {
		right = append(right, S("↑"+itoa(s.Ahead), T.Yellow))
	}
	if s.Behind > 0 {
		right = append(right, S("↓"+itoa(s.Behind), T.Yellow))
	}
	if s.Ahead > 0 || s.Behind > 0 {
		right = append(right, S(" ", T.Fg))
	}
	right = append(right, S(s.Branch, T.Dim))
	if s.Dirty == 0 && s.Ahead == 0 && s.Behind == 0 && s.Upstream {
		right = append(right, S(" ✓", T.Green))
	} else if !s.Upstream && s.Branch != "" {
		right = append(right, S(" ◌", T.Dim))
	}
	return Row{Left: left, Right: right}
}

// ─── Files ──────────────────────────────────────────────────────────────────

func (a *App) rebuildFiles() {
	prev := ""
	if i := a.filesL.sel(); i >= 0 && i < len(a.fileRows) {
		prev = a.fileRows[i].path
	}
	a.fileRows = a.fileRows[:0]
	if a.cur == nil {
		a.filesL.view = a.filesL.view[:0]
		return
	}
	q := strings.ToLower(a.filesL.filter)
	var files []git.File
	for _, f := range a.cur.status.Files {
		if q == "" || fuzzy(strings.ToLower(f.Path), q) {
			files = append(files, f)
		}
	}
	a.fileRows = buildTree(files, a.collapsed)
	a.filesL.view = a.filesL.view[:0]
	for i := range a.fileRows {
		a.filesL.view = append(a.filesL.view, i)
		if a.fileRows[i].path == prev {
			a.filesL.cursor = i
		}
	}
	a.filesL.clamp()
}

type treeNode struct {
	name     string
	path     string
	children map[string]*treeNode
	file     *git.File
}

// buildTree turns paths into lazygit's file tree: directories first, single
// child directories folded into one row ("a/b/c").
func buildTree(files []git.File, collapsed map[string]bool) []fileRow {
	root := &treeNode{children: map[string]*treeNode{}}
	for i := range files {
		f := &files[i]
		parts := strings.Split(strings.TrimSuffix(f.Path, "/"), "/")
		n := root
		for j, p := range parts {
			child, ok := n.children[p]
			if !ok {
				child = &treeNode{name: p, path: strings.Join(parts[:j+1], "/"), children: map[string]*treeNode{}}
				n.children[p] = child
			}
			n = child
		}
		n.file = f
	}
	var rows []fileRow
	var walk func(n *treeNode, depth int) []git.File
	walk = func(n *treeNode, depth int) []git.File {
		var dirs, leaves []*treeNode
		for _, c := range n.children {
			if c.file == nil {
				dirs = append(dirs, c)
			} else {
				leaves = append(leaves, c)
			}
		}
		sort.Slice(dirs, func(i, j int) bool { return dirs[i].name < dirs[j].name })
		sort.Slice(leaves, func(i, j int) bool { return leaves[i].name < leaves[j].name })
		var all []git.File
		for _, d := range dirs {
			name := d.name
			for len(d.children) == 1 {
				var only *treeNode
				for _, c := range d.children {
					only = c
				}
				if only.file != nil {
					break
				}
				name += "/" + only.name
				d = only
			}
			at := len(rows)
			rows = append(rows, fileRow{depth: depth, name: name, path: d.path, dir: true})
			var sub []git.File
			if collapsed[d.path] {
				sub = collectFiles(d)
			} else {
				sub = walk(d, depth+1)
			}
			rows[at].files = sub
			all = append(all, sub...)
		}
		for _, l := range leaves {
			name := l.name
			if l.file.Orig != "" {
				name = filepath.Base(l.file.Orig) + " → " + l.name
			}
			rows = append(rows, fileRow{depth: depth, name: name, path: l.path, file: *l.file, files: []git.File{*l.file}})
			all = append(all, *l.file)
		}
		return all
	}
	walk(root, 0)
	return rows
}

func collectFiles(n *treeNode) []git.File {
	var out []git.File
	if n.file != nil {
		out = append(out, *n.file)
	}
	for _, c := range n.children {
		out = append(out, collectFiles(c)...)
	}
	return out
}

func (a *App) selectedFileRow() *fileRow {
	i := a.filesL.sel()
	if i < 0 || i >= len(a.fileRows) {
		return nil
	}
	return &a.fileRows[i]
}

func (a *App) fileRowView(i int) Row {
	r := a.fileRows[i]
	indent := strings.Repeat("  ", r.depth)
	if r.dir {
		arrow := "▼ "
		if a.collapsed[r.path] {
			arrow = "▶ "
		}
		st := T.Fg
		if allStaged(r.files) {
			st = T.Green
		}
		return Row{Left: []Seg{S(indent, T.Fg), S(arrow, T.Dim), S(r.name, st)}}
	}
	f := r.file
	x, y := T.Green, T.Unstaged
	if f.Untracked() {
		x = T.Unstaged
	}
	if f.Conflicted() {
		x, y = T.Red.Bold(true), T.Red.Bold(true)
	}
	name := T.Fg
	switch {
	case f.Conflicted():
		name = T.Red
	case f.FullyStaged():
		name = T.Green
	}
	return Row{Left: []Seg{S(indent, T.Fg), S(string(f.X), x), S(string(f.Y), y), S(" "+r.name, name)}}
}

func allStaged(fs []git.File) bool {
	if len(fs) == 0 {
		return false
	}
	for _, f := range fs {
		if !f.FullyStaged() {
			return false
		}
	}
	return true
}

// ─── Branches, commits, stash ──────────────────────────────────────────────

func (a *App) refilterBranches() {
	if a.cur == nil {
		a.branchesL.view = a.branchesL.view[:0]
		return
	}
	switch a.branchTab {
	case 0:
		a.branchesL.refilter(len(a.cur.branches), func(i int) string { return a.cur.branches[i].Name })
	case 1:
		a.branchesL.refilter(len(a.cur.remotes), func(i int) string { return a.cur.remotes[i].Name })
	default:
		a.branchesL.refilter(len(a.cur.tags), func(i int) string { return a.cur.tags[i].Name + " " + a.cur.tags[i].Subject })
	}
}

func (a *App) refilterCommits() {
	if a.cur == nil {
		a.commitsL.view = a.commitsL.view[:0]
		return
	}
	a.commitsL.refilter(len(a.cur.commits), func(i int) string {
		c := a.cur.commits[i]
		return c.Short + " " + c.Author + " " + c.Subject + " " + strings.Join(c.Tags, " ")
	})
}

func (a *App) refilterStash() {
	if a.cur == nil {
		a.stashL.view = a.stashL.view[:0]
		return
	}
	a.stashL.refilter(len(a.cur.stashes), func(i int) string { return a.cur.stashes[i].Message })
}

func branchColor(name string) lipgloss.Style {
	switch {
	case strings.HasPrefix(name, "feature/"), strings.HasPrefix(name, "feat/"):
		return T.Green
	case strings.HasPrefix(name, "bugfix/"), strings.HasPrefix(name, "fix/"):
		return T.Yellow
	case strings.HasPrefix(name, "hotfix/"):
		return T.Red
	case strings.HasPrefix(name, "release/"):
		return T.Cyan
	}
	return T.Fg
}

func recencyCol(t time.Time) string {
	s := ago(t)
	if s == "now" {
		s = "0m"
	}
	return s + strings.Repeat(" ", max(0, 4-len(s)))
}

func (a *App) branchRow(i int) Row {
	switch a.branchTab {
	case 1:
		b := a.cur.remotes[i]
		remote, name, _ := strings.Cut(b.Name, "/")
		return Row{
			Left:  []Seg{S(recencyCol(b.Date), T.Dim), S(remote+"/", T.Dim), S(name, branchColor(name))},
			Right: []Seg{S(b.Subject, T.Dim)},
		}
	case 2:
		t := a.cur.tags[i]
		return Row{Left: []Seg{S(recencyCol(t.Date), T.Dim), S(t.Name, T.Magenta), S(" "+t.Subject, T.Dim)}}
	}
	b := a.cur.branches[i]
	rec := S(recencyCol(b.Date), T.Dim)
	if b.Head {
		rec = S("*   ", T.Green)
	}
	left := []Seg{rec, S(b.Name, branchColor(b.Name))}
	switch {
	case b.Gone:
		left = append(left, S(" upstream gone", T.Red))
	case b.Upstream == "":
	case b.Ahead == 0 && b.Behind == 0:
		left = append(left, S(" ✓", T.Green))
	default:
		t := " "
		if b.Ahead > 0 {
			t += "↑" + itoa(b.Ahead)
		}
		if b.Behind > 0 {
			t += "↓" + itoa(b.Behind)
		}
		left = append(left, S(t, T.Yellow))
	}
	var right []Seg
	if pr, ok := a.gh.branchPR(a.selectedRepo(), b.Name); ok {
		right = append(right, prBadge(pr)...)
	}
	return Row{Left: left, Right: right}
}

func prBadge(pr gh.BranchPR) []Seg {
	st := T.Green
	switch {
	case pr.Draft:
		st = T.Dim
	case pr.State == "MERGED":
		st = T.Magenta
	case pr.State == "CLOSED":
		st = T.Red
	}
	return []Seg{S("#"+itoa(pr.Number), st)}
}

func (a *App) commitRow(i int) Row {
	c := a.cur.commits[i]
	hash := T.Yellow
	if c.Unpushed {
		hash = T.Red
	}
	glyph := "○ "
	if c.Merge {
		glyph = "◎ "
	}
	left := []Seg{
		S(c.Short+" ", hash),
		S(initials(c.Author)+" ", T.Author(c.Author)),
		S(glyph, T.Blue),
	}
	for _, t := range c.Tags {
		left = append(left, S(t+" ", T.Magenta.Bold(true)))
	}
	left = append(left, S(c.Subject, T.Fg))
	return Row{Left: left, Right: []Seg{S(ago(c.Date), T.Dim)}}
}

func (a *App) stashRow(i int) Row {
	s := a.cur.stashes[i]
	return Row{Left: []Seg{S(itoa(i)+" ", T.Dim), S(recencyCol(s.Date), T.Dim), S(s.Message, T.Fg)}}
}

// ─── Main panel ─────────────────────────────────────────────────────────────

// wantMain says what the main panel should show for the current selection:
// a cache key, a title, and how to load it.
func (a *App) wantMain() (key, title string, load func() []string) {
	if a.mode != modeLocal || a.cur == nil {
		return "", "", nil
	}
	dir := a.cur.path
	p := a.focus
	if p == pMain {
		p = a.back
	}
	switch p {
	case pRepos:
		return "repo:" + dir, "Repository", func() []string {
			out, err := git.Graph(dir, "HEAD", 120)
			if err != nil {
				return []string{T.Dim.Render("No commits yet.")}
			}
			return splitANSI(out)
		}
	case pFiles:
		r := a.selectedFileRow()
		if r == nil {
			return "files-empty:" + dir, "Status", func() []string {
				return []string{T.Dim.Render("Working tree clean.")}
			}
		}
		f, isDir := r.file, r.dir
		if isDir {
			f = git.File{Path: r.path, X: ' ', Y: 'M'}
		}
		title := "Unstaged changes"
		switch {
		case !isDir && f.FullyStaged():
			title = "Staged changes"
		case !isDir && f.HasStaged() && f.HasUnstaged():
			title = "Changes"
		case isDir:
			title = "Diff"
		}
		return "file:" + dir + ":" + r.path + ":" + f.Code(), title, func() []string {
			staged, unstaged := git.FileDiff(dir, f, isDir)
			var lines []string
			if unstaged != "" {
				lines = append(lines, colorDiff(unstaged)...)
			}
			if staged != "" {
				if len(lines) > 0 {
					lines = append(lines, "", T.Green.Bold(true).Render("── Staged ──"), "")
				}
				lines = append(lines, colorDiff(staged)...)
			}
			if len(lines) == 0 {
				return []string{T.Dim.Render("No textual changes.")}
			}
			return lines
		}
	case pBranches:
		i := a.branchesL.sel()
		if i < 0 {
			return "", "", nil
		}
		var ref string
		switch a.branchTab {
		case 0:
			ref = a.cur.branches[i].Name
		case 1:
			ref = a.cur.remotes[i].Name
		default:
			ref = a.cur.tags[i].Name
		}
		return "ref:" + dir + ":" + ref, "Log", func() []string {
			out, err := git.Graph(dir, ref, 200)
			if err != nil {
				return []string{T.Red.Render(err.Error())}
			}
			return splitANSI(out)
		}
	case pCommits:
		i := a.commitsL.sel()
		if i < 0 {
			return "", "", nil
		}
		h := a.cur.commits[i].Hash
		return "commit:" + dir + ":" + h, "Patch", func() []string {
			out, err := git.Show(dir, h)
			if err != nil {
				return []string{T.Red.Render(err.Error())}
			}
			return colorDiff(out)
		}
	case pStash:
		i := a.stashL.sel()
		if i < 0 {
			return "", "", nil
		}
		ref := a.cur.stashes[i].Ref
		return "stash:" + dir + ":" + ref + ":" + a.cur.stashes[i].Message, "Stash", func() []string {
			out, err := git.StashShow(dir, ref)
			if err != nil {
				return []string{T.Red.Render(err.Error())}
			}
			return colorDiff(out)
		}
	}
	return "", "", nil
}

// syncMain schedules a main panel load when the selection changed. Loads
// are debounced so holding j through a long log stays smooth.
func (a *App) syncMain() tea.Cmd {
	key, _, load := a.wantMain()
	if load == nil {
		if key == "" && a.mode == modeLocal {
			a.main.set(nil, false)
			a.mainKey, a.mainTitle = "", ""
		}
		return nil
	}
	if key == a.mainKey || key == a.mainPending {
		return nil
	}
	a.mainPending = key
	return tea.Tick(35*time.Millisecond, func(time.Time) tea.Msg { return mainDebounceMsg{key} })
}

func (a *App) loadMain(key string) tea.Cmd {
	k, title, load := a.wantMain()
	if k != key || load == nil {
		return nil
	}
	return func() tea.Msg { return mainMsg{key: key, title: title, lines: load()} }
}

// repoSummary heads the main panel while the repo list is focused.
func (a *App) repoSummary(width int) []string {
	r := a.selectedRepo()
	if r == nil {
		return nil
	}
	label := func(s string) string { return T.Dim.Render(fit(s, 12)) }
	lines := []string{
		T.Bold.Render(r.Name) + "  " + T.Dim.Render(config.TildePath(r.Path)),
		"",
	}
	if !r.Loaded {
		return append(lines, T.Dim.Render("Loading…"))
	}
	s := r.Stats
	br := T.Cyan.Render(s.Branch)
	if a.cur != nil && a.cur.status.Upstream != "" {
		br += T.Dim.Render(" → ") + a.cur.status.Upstream
	}
	switch {
	case !s.Upstream:
		br += T.Dim.Render("  no upstream")
	case s.Ahead == 0 && s.Behind == 0:
		br += T.Green.Render("  ✓ up to date")
	default:
		parts := []string{}
		if s.Ahead > 0 {
			parts = append(parts, T.Yellow.Render("↑"+itoa(s.Ahead)+" to push"))
		}
		if s.Behind > 0 {
			parts = append(parts, T.Yellow.Render("↓"+itoa(s.Behind)+" to pull"))
		}
		br += "  " + strings.Join(parts, " ")
	}
	lines = append(lines, label("Branch")+br)
	if s.Slug != "" {
		lines = append(lines, label("GitHub")+T.Blue.Render(s.Slug))
	} else if s.RemoteURL != "" {
		lines = append(lines, label("Remote")+s.RemoteURL)
	} else {
		lines = append(lines, label("Remote")+T.Dim.Render("none"))
	}
	changes := T.Green.Render("clean")
	if s.Dirty > 0 {
		changes = T.Unstaged.Render(plural(s.Dirty, "changed file"))
	}
	lines = append(lines, label("Changes")+changes)
	refs := plural(s.Branches, "branch")
	if s.Branches != 1 {
		refs = itoa(s.Branches) + " branches"
	}
	if s.Unpushed > 0 {
		refs += T.Red.Render("  " + plural(s.Unpushed, "commit") + " on no remote")
	}
	lines = append(lines, label("Branches")+refs)
	if !s.LastCommit.IsZero() {
		lines = append(lines, label("Last commit")+agoLong(s.LastCommit))
	}
	if a.cur != nil && a.cur.loaded {
		if prs := a.gh.openPRCount(r); prs > 0 {
			lines = append(lines, label("Open PRs")+T.Green.Render(itoa(prs)))
		}
	}
	lines = append(lines, "", T.Dim.Render(strings.Repeat("─", max(0, width))))
	return lines
}

// ─── Layout ─────────────────────────────────────────────────────────────────

func (a *App) sideWidth() int {
	w := a.w / 3
	if w < 34 {
		w = 34
	}
	return min(w, a.w-30)
}

func (a *App) localView(bh int) string {
	lw := a.sideWidth()
	rw := a.w - lw
	order := []pane{pRepos, pFiles, pBranches, pCommits, pStash}
	weights := []int{3, 3, 2, 3, 0}
	mins := []int{3, 3, 3, 3, 3}
	for i, p := range order {
		if p == a.focus || (a.focus == pMain && p == a.back) {
			if p == pStash {
				weights[i] = 2
			} else {
				weights[i]++
			}
		}
	}
	hs := distribute(bh, weights, mins)
	var left []string
	y := 1
	for i, p := range order {
		left = append(left, a.localPanel(p, lw, hs[i]))
		a.rects = append(a.rects, rect{0, y, lw, hs[i], p})
		y += hs[i]
	}

	logH := 0
	if a.showLog {
		logH = min(9, bh/4+1)
	}
	mainH := bh - logH
	right := []string{a.mainPanel(rw, mainH)}
	a.rects = append(a.rects, rect{lw, 1, rw, mainH, pMain})
	if logH > 0 {
		right = append(right, a.logPanel(rw, logH))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		strings.Join(left, "\n"),
		strings.Join(right, "\n"))
}

func (a *App) localPanel(p pane, w, h int) string {
	inner, rows := w-2, h-2
	focused := a.focus == p
	pn := Panel{W: w, H: h, Focused: focused, Searching: focused && a.filtering}
	switch p {
	case pRepos:
		pn.Index, pn.Title = "1", "Repos"
		if root := a.cfg.RepoRoot(); root != "" {
			pn.Title = "Repos " + T.Dim.Render(config.TildePath(root))
		}
		if a.sortMode != "" && a.sortMode != "recent" {
			pn.Title += T.Dim.Render(" · " + a.sortMode)
		}
		switch {
		case a.scanning && len(a.repos) == 0:
			pn.Body = []string{" " + a.spin.View() + T.Dim.Render(" scanning…")}
		case a.scanErr != nil && len(a.repos) == 0:
			pn.Body = append([]string{T.Red.Render(" " + a.scanErr.Error())}, T.Dim.Render(" Set reposDir in "+config.TildePath(a.cfg.File())))
		case len(a.repos) == 0:
			pn.Body = []string{T.Dim.Render(" No repos yet. Press c to clone one.")}
		default:
			pn.Body = renderList(&a.reposL, inner, rows, focused, a.repoRow)
		}
		pn.Footer, pn.Status = a.reposL.footer(), a.filterStatus(p, &a.reposL)
		pn.Total, pn.Offset = a.reposL.len(), a.reposL.offset
	case pFiles:
		pn.Index, pn.Title = "2", "Files"
		if a.cur != nil {
			if n := len(a.cur.status.Files); n > 0 {
				staged := 0
				for _, f := range a.cur.status.Files {
					if f.HasStaged() {
						staged++
					}
				}
				if staged > 0 {
					pn.Title += T.Green.Render(" " + itoa(staged) + " staged")
				}
			}
		}
		if a.cur != nil && a.cur.loaded && len(a.fileRows) == 0 && a.filesL.filter == "" {
			pn.Body = []string{T.Dim.Render(" Working tree clean")}
		} else {
			pn.Body = renderList(&a.filesL, inner, rows, focused, a.fileRowView)
		}
		pn.Footer, pn.Status = a.filesL.footer(), a.filterStatus(p, &a.filesL)
		pn.Total, pn.Offset = a.filesL.len(), a.filesL.offset
	case pBranches:
		pn.Index, pn.Tabs, pn.Tab = "3", branchTabs, a.branchTab
		pn.Body = renderList(&a.branchesL, inner, rows, focused, a.branchRow)
		pn.Footer, pn.Status = a.branchesL.footer(), a.filterStatus(p, &a.branchesL)
		pn.Total, pn.Offset = a.branchesL.len(), a.branchesL.offset
	case pCommits:
		pn.Index, pn.Title = "4", "Commits"
		pn.Body = renderList(&a.commitsL, inner, rows, focused, a.commitRow)
		pn.Footer, pn.Status = a.commitsL.footer(), a.filterStatus(p, &a.commitsL)
		pn.Total, pn.Offset = a.commitsL.len(), a.commitsL.offset
	case pStash:
		pn.Index, pn.Title = "5", "Stash"
		pn.Body = renderList(&a.stashL, inner, rows, focused, a.stashRow)
		pn.Footer, pn.Status = a.stashL.footer(), a.filterStatus(p, &a.stashL)
		pn.Total, pn.Offset = a.stashL.len(), a.stashL.offset
	}
	return pn.Render()
}

func (a *App) mainPanel(w, h int) string {
	inner, rows := w-2, h-2
	pn := Panel{W: w, H: h, Index: "0", Title: a.mainTitle, Focused: a.focus == pMain}
	var body []string
	if a.focus == pRepos || (a.focus == pMain && a.back == pRepos) {
		pn.Title = "Repository"
		body = append(body, a.repoSummary(inner-2)...)
	}
	for i := range body {
		body[i] = " " + body[i]
	}
	rows -= len(body)
	body = append(body, a.main.view(max(0, rows))...)
	pn.Body = body
	pn.Total, pn.Offset = len(a.main.lines), a.main.offset
	if pn.Total > rows && rows > 0 {
		pn.Footer = itoa(min(100, (a.main.offset+rows)*100/pn.Total)) + "%"
	}
	if a.cur == nil && len(a.repos) == 0 && !a.scanning {
		pn.Body = a.welcome(inner)
	}
	return pn.Render()
}

func (a *App) welcome(width int) []string {
	lines := []string{
		"",
		"  " + T.ActiveBorder.Render("◆ gitdeck"),
		"",
		"  Every repo on your machine, lazygit-style, with GitHub built in.",
		"",
		"  " + T.Options.Render("c") + "    clone a repo (URL or owner/name)",
		"  " + T.Options.Render("tab") + "  switch to your GitHub dashboard",
		"  " + T.Options.Render("?") + "    all keybindings",
	}
	return lines
}

func (a *App) logPanel(w, h int) string {
	rows := h - 2
	var body []string
	start := max(0, len(a.cmdLog)-rows)
	if len(a.cmdLog) == 0 {
		body = []string{
			T.Dim.Render("Commands gitdeck runs show up here. Hide this panel with ") + T.Options.Render("@"),
			"",
			T.Yellow.Render("Tip: ") + tips[int(time.Now().Unix()/3600)%len(tips)],
		}
	}
	for _, e := range a.cmdLog[start:] {
		line := T.Dim.Render(e.at.Format("15:04:05")+" ") + T.Dim.Render(e.repo+" ")
		if e.err != "" {
			line += T.Red.Render(e.cmd) + T.Dim.Render(" → ") + T.Red.Render(e.err)
		} else {
			line += T.Cyan.Render(e.cmd)
		}
		body = append(body, line)
	}
	return Panel{W: w, H: h, Title: "Command log", Body: body}.Render()
}

var tips = []string{
	"Press L anywhere to open lazygit right on the panel you're looking at.",
	"Space on a PR checks it out locally, cloning the repo first if you don't have it.",
	"Branches with a pull request show its number; o opens it in the browser.",
	"gitdeck reads your lazygit theme, so both apps always match.",
	"Press / in any panel to filter it. Esc clears the filter.",
	"U pulls every repo at once with --ff-only, skipping anything that would need a merge.",
	"Custom keybinds in the config run any command in the selected repo.",
}

// ─── Exists check used by prompts ───────────────────────────────────────────

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
