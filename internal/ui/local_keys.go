package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/bkenks/gitdeck/internal/config"
	"github.com/bkenks/gitdeck/internal/gh"
	"github.com/bkenks/gitdeck/internal/git"
	"github.com/bkenks/gitdeck/internal/proc"
	. "github.com/bkenks/gitdeck/internal/theme"
)

var localOrder = []pane{pRepos, pFiles, pBranches, pCommits, pStash}

func (a *App) panelRows(p pane) int {
	for _, r := range a.rects {
		if r.p == p {
			return r.h - 2
		}
	}
	return 10
}

func (a *App) localKey(key string) (bool, tea.Cmd) {
	// Panel focus, lazygit style.
	switch key {
	case "1", "2", "3", "4", "5":
		a.setFocus(localOrder[key[0]-'1'])
		return true, a.afterMove(a.focus)
	case "0":
		a.setFocus(pMain)
		return true, nil
	case "h", "left", "l", "right":
		if a.focus == pMain {
			a.focus = a.back
			return true, nil
		}
		d := 1
		if key == "h" || key == "left" {
			d = -1
		}
		for i, p := range localOrder {
			if p == a.focus {
				a.focus = localOrder[(i+d+len(localOrder))%len(localOrder)]
				break
			}
		}
		return true, a.afterMove(a.focus)
	case "/":
		a.startFilter()
		return true, nil
	case "esc":
		if a.focus == pMain {
			a.focus = a.back
			return true, nil
		}
		if a.filterText() != "" {
			a.setFilter("")
			return true, a.afterMove(a.focus)
		}
		return true, nil
	}

	repo := a.selectedRepo()
	if repo != nil && a.focus != pMain {
		switch key {
		case "L", "ctrl+g":
			return true, a.openLazygit(repo.Path, a.lazygitArgs()...)
		case "p":
			return true, a.run("Pulling "+repo.Name, true, "Pulled "+repo.Name, func() proc.Result { return git.Pull(repo.Path) })
		case "P":
			return true, a.run("Pushing "+repo.Name, true, "Pushed "+repo.Name, func() proc.Result { return git.Push(repo.Path) })
		case "f":
			return true, a.run("Fetching "+repo.Name, true, "Fetched "+repo.Name, func() proc.Result { return git.Fetch(repo.Path) })
		}
	}
	switch key {
	case "U":
		return true, a.pullAll()
	case "c":
		if a.focus == pRepos || repo == nil {
			a.openClone()
			return true, nil
		}
	}

	switch a.focus {
	case pRepos:
		if handled, c := a.reposKey(key, repo); handled {
			return true, c
		}
	case pFiles:
		if handled, c := a.filesKey(key, repo); handled {
			return true, c
		}
	case pBranches:
		if handled, c := a.branchesKey(key, repo); handled {
			return true, c
		}
	case pCommits:
		if handled, c := a.commitsKey(key, repo); handled {
			return true, c
		}
	case pStash:
		if handled, c := a.stashKey(key, repo); handled {
			return true, c
		}
	case pMain:
		return pagerKey(key, &a.main, a.panelRows(pMain)), nil
	}

	if l := a.listFor(a.focus); l != nil && navKey(key, l, a.panelRows(a.focus)) {
		return true, a.afterMove(a.focus)
	}
	return false, nil
}

// lazygitArgs picks the lazygit panel matching the focused gitdeck panel.
func (a *App) lazygitArgs() []string {
	p := a.focus
	if p == pMain {
		p = a.back
	}
	switch p {
	case pBranches:
		return []string{"branch"}
	case pCommits:
		return []string{"log"}
	case pStash:
		return []string{"stash"}
	}
	return []string{"status"}
}

func (a *App) reposKey(key string, repo *git.Repo) (bool, tea.Cmd) {
	switch key {
	case "enter":
		if repo != nil {
			a.focus = pFiles
		}
		return true, a.syncMain()
	case "s":
		if repo != nil {
			return true, a.openShell(repo.Path)
		}
	case "S":
		i := slices.Index(sortModes, a.cfg.SortMode)
		a.cfg.SortMode = sortModes[(i+1)%len(sortModes)]
		_ = a.cfg.Save()
		keep := ""
		if repo != nil {
			keep = repo.Path
		}
		a.sortRepos(keep)
		return true, a.flash("Sorted by "+a.cfg.SortMode, false)
	case "y":
		if repo != nil {
			return true, a.copy(repo.Path)
		}
	case "e":
		if repo != nil {
			return true, a.openEditor(repo.Path, ".")
		}
	case "o":
		if repo != nil {
			return true, a.openRepoWeb(repo, "")
		}
	case "d":
		if repo != nil {
			a.confirmDelete(*repo)
			return true, nil
		}
	}
	return false, nil
}

func (a *App) filesKey(key string, repo *git.Repo) (bool, tea.Cmd) {
	if repo == nil {
		return false, nil
	}
	dir := repo.Path
	row := a.selectedFileRow()
	switch key {
	case "space":
		if row == nil {
			return true, nil
		}
		path := row.path
		if allStaged(row.files) {
			return true, a.run("Unstaging", true, "", func() proc.Result { return git.Unstage(dir, path) })
		}
		return true, a.run("Staging", true, "", func() proc.Result { return git.Stage(dir, path) })
	case "a":
		if a.cur != nil && allStaged(a.cur.status.Files) {
			return true, a.run("Unstaging all", true, "", func() proc.Result { return git.UnstageAll(dir) })
		}
		return true, a.run("Staging all", true, "", func() proc.Result { return git.StageAll(dir) })
	case "c":
		return true, a.openCommit(repo)
	case "d":
		if row != nil {
			a.confirmDiscard(dir, *row)
		}
		return true, nil
	case "s":
		if a.cur != nil && len(a.cur.status.Files) > 0 {
			a.openStash(dir)
		}
		return true, nil
	case "e":
		if row != nil && !row.dir {
			return true, a.openEditor(dir, row.path)
		}
		return true, a.openEditor(dir, ".")
	case "y":
		if row != nil {
			return true, a.copy(row.path)
		}
	case "enter":
		if row == nil {
			return true, nil
		}
		if row.dir {
			a.collapsed[row.path] = !a.collapsed[row.path]
			a.rebuildFiles()
			return true, nil
		}
		a.setFocus(pMain)
		return true, nil
	}
	return false, nil
}

func (a *App) branchesKey(key string, repo *git.Repo) (bool, tea.Cmd) {
	if repo == nil || a.cur == nil {
		return false, nil
	}
	dir := repo.Path
	switch key {
	case "[", "]":
		d := 1
		if key == "[" {
			d = -1
		}
		a.branchTab = (a.branchTab + d + len(branchTabs)) % len(branchTabs)
		a.branchesL.reset()
		a.branchesL.filter = ""
		a.refilterBranches()
		return true, a.syncMain()
	case "n":
		a.openNewBranch(dir)
		return true, nil
	}
	name, ok := a.selectedRef()
	if !ok {
		return false, nil
	}
	switch key {
	case "space":
		ok := "Checked out " + name
		switch a.branchTab {
		case 1:
			return true, a.run("Checking out", true, ok, func() proc.Result { return git.CheckoutRemote(dir, name) })
		default:
			return true, a.run("Checking out", true, ok, func() proc.Result { return git.Checkout(dir, name) })
		}
	case "d":
		if a.branchTab == 0 {
			return true, a.confirmDeleteBranch(dir, a.cur.branches[a.branchesL.sel()])
		}
		return true, nil
	case "y":
		return true, a.copy(name)
	case "o":
		if pr, ok := a.gh.branchPR(repo, name); ok {
			return true, openURL(pr.URL)
		}
		short := name
		if a.branchTab == 1 {
			_, short, _ = strings.Cut(name, "/")
		}
		return true, a.openRepoWeb(repo, "tree/"+short)
	case "enter":
		a.setFocus(pMain)
		return true, nil
	}
	return false, nil
}

func (a *App) commitsKey(key string, repo *git.Repo) (bool, tea.Cmd) {
	if repo == nil || a.cur == nil {
		return false, nil
	}
	i := a.commitsL.sel()
	if i < 0 {
		return false, nil
	}
	c := a.cur.commits[i]
	switch key {
	case "y":
		return true, a.copy(c.Hash)
	case "o":
		return true, a.openRepoWeb(repo, "commit/"+c.Hash)
	case "enter":
		a.setFocus(pMain)
		return true, nil
	}
	return false, nil
}

func (a *App) stashKey(key string, repo *git.Repo) (bool, tea.Cmd) {
	if repo == nil || a.cur == nil {
		return false, nil
	}
	i := a.stashL.sel()
	if i < 0 {
		return false, nil
	}
	dir, ref := repo.Path, a.cur.stashes[i].Ref
	switch key {
	case "space":
		return true, a.run("Applying stash", true, "Applied "+ref, func() proc.Result { return git.StashApply(dir, ref) })
	case "g":
		return true, a.run("Popping stash", true, "Popped "+ref, func() proc.Result { return git.StashPop(dir, ref) })
	case "d":
		a.confirm("Drop stash?", []string{a.cur.stashes[i].Message, "", T.Dim.Render("This can't be undone.")},
			a.run("Dropping stash", true, "Dropped "+ref, func() proc.Result { return git.StashDrop(dir, ref) }))
		return true, nil
	case "enter":
		a.setFocus(pMain)
		return true, nil
	}
	return false, nil
}

// ─── Multi-step actions ─────────────────────────────────────────────────────

func (a *App) pullAll() tea.Cmd {
	var repos []git.Repo
	for _, r := range a.repos {
		if !r.Loaded || r.Stats.Upstream {
			repos = append(repos, r)
		}
	}
	if len(repos) == 0 {
		return a.flash("Nothing to pull: no repo has an upstream", true)
	}
	return a.withBusy(fmt.Sprintf("Pulling %d repos", len(repos)), func() tea.Msg {
		results := make([]proc.Result, len(repos))
		moved := make([]bool, len(repos))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)
		for i, r := range repos {
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				results[i], moved[i] = git.PullFFOnly(r.Path)
			})
		}
		wg.Wait()
		return a.pulledAll(results, moved)
	})
}

// pulledAll reports a pull of every repo. Pulls that changed nothing stay
// out of the command log.
func (a *App) pulledAll(results []proc.Result, moved []bool) resultMsg {
	var logged []proc.Result
	var paths []string
	failed := 0
	for i, r := range results {
		paths = append(paths, r.Dir)
		if r.Err != nil {
			failed++
		}
		if r.Err != nil || moved[i] {
			logged = append(logged, r)
		}
	}
	text := "Pulled " + plural(len(results)-failed, "repo")
	if failed > 0 {
		text += fmt.Sprintf(", %d couldn't fast-forward (see command log)", failed)
	}
	return resultMsg{results: logged, toast: text, toastErr: failed > 0, then: func() tea.Cmd {
		return tea.Batch(a.loadAllStats(paths), a.refreshAll())
	}}
}

func (a *App) confirmDelete(r git.Repo) {
	if r.Cwd {
		a.modal = newMessage("Can't delete", []string{"This repo is outside your repos dir, so gitdeck won't delete it."})
		return
	}
	body := []string{T.Bold.Render(r.Key), T.Dim.Render(config.TildePath(r.Path)), ""}
	s := r.Stats
	warn := false
	if s.Dirty > 0 {
		body = append(body, T.Red.Render("⚠ "+plural(s.Dirty, "uncommitted file")))
		warn = true
	}
	if s.Unpushed > 0 {
		body = append(body, T.Red.Render("⚠ "+plural(s.Unpushed, "commit")+" not on any remote"))
		warn = true
	}
	if s.RemoteURL == "" {
		body = append(body, T.Red.Render("⚠ no remote: this is the only copy"))
		warn = true
	}
	if !warn {
		body = append(body, T.Green.Render("✓ everything is pushed"))
	}
	body = append(body, "", "The directory is removed from disk.")
	root := a.cfg.RepoRoot()
	path := r.Path
	a.confirmDanger("Delete "+r.Name+"?", body, func() tea.Msg {
		res := git.Remove(root, path)
		return resultMsg{results: []proc.Result{res}, ok: "Deleted " + filepath.Base(path), rescan: true}
	})
}

func (a *App) confirmDiscard(dir string, row fileRow) {
	what := row.path
	if row.dir {
		what += "/ (" + plural(len(row.files), "file") + ")"
	}
	files := row.files
	body := []string{"Throw away all changes to", T.Bold.Render(what), "",
		T.Dim.Render("Staged and unstaged. This can't be undone.")}
	a.confirmDanger("Discard changes?", body,
		a.run("Discarding", true, "Discarded "+row.path, func() proc.Result { return git.Discard(dir, files) }))
}

func (a *App) confirmDeleteBranch(dir string, b git.Branch) tea.Cmd {
	if b.Head {
		return a.flash("You can't delete the branch you're on", true)
	}
	body := []string{T.Bold.Render(b.Name), "",
		T.Dim.Render("Uses git branch -d, so unmerged work is safe.")}
	del := func() proc.Result { return git.DeleteBranch(dir, b.Name) }
	a.confirm("Delete branch?", body, a.run("Deleting branch", true, "Deleted "+b.Name, del))
	return nil
}

func (a *App) openCommit(repo *git.Repo) tea.Cmd {
	staged := 0
	if a.cur != nil {
		staged = a.cur.status.Staged()
	}
	if staged == 0 {
		return a.flash("Nothing staged. Space stages a file, a stages everything", true)
	}
	dir := repo.Path
	a.modal = newTextArea("Commit "+plural(staged, "file")+" to "+repo.Stats.Branch, "Summary on the first line, details below", "", func(msg string) tea.Cmd {
		if strings.TrimSpace(msg) == "" {
			return a.flash("Empty commit message", true)
		}
		return a.run("Committing", true, "Committed", func() proc.Result { return git.CommitMsg(dir, msg) })
	})
	return nil
}

func (a *App) openStash(dir string) {
	a.modal = newInput("Stash changes", "Message (optional)", "", func(msg string) tea.Cmd {
		return a.run("Stashing", true, "Stashed", func() proc.Result { return git.StashPush(dir, msg) })
	})
}

func (a *App) openNewBranch(dir string) {
	a.modal = newInput("New branch", "Branch name, from the current HEAD", "", func(name string) tea.Cmd {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil
		}
		return a.run("Creating branch", true, "On new branch "+name, func() proc.Result { return git.NewBranch(dir, name) })
	})
}

func (a *App) openClone() {
	root := a.cfg.RepoRoot()
	if root == "" {
		a.openReposDirPrompt()
		return
	}
	a.modal = newTextArea("Clone repos", "One per line: URL or owner/name. Lands in "+config.TildePath(root)+"/<owner>/<repo>", "", func(text string) tea.Cmd {
		var urls []string
		for _, l := range strings.Split(text, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				urls = append(urls, l)
			}
		}
		if len(urls) == 0 {
			return nil
		}
		return a.cloneAll(urls, root)
	})
}

func (a *App) cloneAll(urls []string, root string) tea.Cmd {
	return a.withBusy("Cloning "+plural(len(urls), "repo"), func() tea.Msg {
		var first string
		results := make([]proc.Result, len(urls))
		for i, u := range urls {
			var dest string
			results[i], dest = cloneOne(root, u)
			if first == "" {
				first = dest
			}
		}
		return a.clonedAll(results, first)
	})
}

// cloneOne clones url under root. dest is where the repo now is, or "" when
// there's nothing there to select. A failure that ran no command comes back
// as a result with only Err set, which keeps it out of the command log.
func cloneOne(root, url string) (r proc.Result, dest string) {
	dest, slug, err := git.CloneDest(root, url)
	if err != nil {
		return proc.Result{Err: err}, ""
	}
	if dirExists(dest) {
		return proc.Result{Err: errors.New(config.TildePath(dest) + " already exists")}, dest
	}
	_ = os.MkdirAll(filepath.Dir(dest), 0o755)
	if slug != "" {
		r = gh.Clone(slug, dest)
	} else {
		r = git.Clone(url, dest)
	}
	if r.Err != nil {
		return r, ""
	}
	return r, dest
}

// clonedAll reports a batch of clones, selecting first once the rescan
// finds it.
func (a *App) clonedAll(results []proc.Result, first string) resultMsg {
	m := resultMsg{results: results, ok: "Cloned " + plural(len(results), "repo"), rescan: true}
	if first != "" {
		m.then = func() tea.Cmd {
			a.selectAfterScan = first
			a.touch(first)
			return nil
		}
	}
	return m
}

type askReposDirMsg struct{}

func (a *App) openReposDirPrompt() {
	def := "~/Development"
	if a.cfg.ReposDir != "" {
		def = a.cfg.ReposDir
	}
	a.modal = newInput("Where do your repos live?", "gitdeck keeps repos as <dir>/<owner>/<repo>", def, func(dir string) tea.Cmd {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return nil
		}
		if err := os.MkdirAll(config.ExpandHome(dir), 0o755); err != nil {
			return a.flash(err.Error(), true)
		}
		a.cfg.ReposDir = dir
		if err := a.cfg.Save(); err != nil {
			return a.flash(err.Error(), true)
		}
		return a.scan()
	})
}
