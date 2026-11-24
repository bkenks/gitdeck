package ui

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bkenks/gitdeck/internal/config"
	"github.com/bkenks/gitdeck/internal/gh"
	"github.com/bkenks/gitdeck/internal/git"
	"github.com/bkenks/gitdeck/internal/proc"
)

// press sends keys as if typed: named keys by name, anything else as text.
func press(a *App, keys ...string) {
	named := map[string]rune{
		"esc": tea.KeyEscape, "enter": tea.KeyEnter, "backspace": tea.KeyBackspace,
	}
	for _, k := range keys {
		if code, ok := named[k]; ok {
			update(a, tea.KeyPressMsg{Code: code})
			continue
		}
		update(a, tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
	}
}

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func testRepo(name string, age int) git.Repo {
	return git.Repo{Name: name, Key: "owner/" + name, Path: "/src/owner/" + name,
		Modified: epoch.Add(-time.Duration(age) * time.Hour)}
}

// scannedApp is an App whose scan found alpha (oldest), beta and gamma
// (newest), sorted most recent first.
func scannedApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newTestApp(t)
	a.cfg.SortMode = "recent"
	update(a, scannedMsg{repos: []git.Repo{testRepo("alpha", 3), testRepo("beta", 2), testRepo("gamma", 1)}})
	return a
}

func visibleRepos(a *App) []string {
	var names []string
	for _, i := range a.reposL.view {
		names = append(names, a.repos[i].Name)
	}
	return names
}

func checkRepo(t *testing.T, a *App, name string, cursor int) {
	t.Helper()
	r := a.selectedRepo()
	if r == nil || r.Name != name || a.reposL.cursor != cursor {
		got := "<none>"
		if r != nil {
			got = r.Name
		}
		t.Errorf("selected %s at %d, want %s at %d (visible %v)", got, a.reposL.cursor, name, cursor, visibleRepos(a))
	}
}

func TestRepoCursorFollowsRepoAcrossResort(t *testing.T) {
	a := scannedApp(t)
	checkRepo(t, a, "gamma", 0)
	press(a, "S")
	if a.cfg.SortMode != "name" {
		t.Fatalf("sort mode %q, want name", a.cfg.SortMode)
	}
	if got := visibleRepos(a); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("sorted by name: %v", got)
	}
	checkRepo(t, a, "gamma", 2)
}

func TestRepoCursorFollowsRepoAcrossStatsReload(t *testing.T) {
	a := scannedApp(t)
	press(a, "j")
	checkRepo(t, a, "beta", 1)

	// One repo's stats don't re-sort the list, even when its recency changed.
	update(a, statsMsg{"/src/owner/alpha": git.Stats{LastCommit: epoch.Add(time.Hour)}})
	if got := visibleRepos(a); !slices.Equal(got, []string{"gamma", "beta", "alpha"}) {
		t.Errorf("a single stats load re-sorted: %v", got)
	}
	checkRepo(t, a, "beta", 1)

	update(a, statsMsg{
		"/src/owner/alpha": git.Stats{LastCommit: epoch.Add(time.Hour)},
		"/src/owner/beta":  git.Stats{LastCommit: epoch.Add(2 * time.Hour)},
	})
	if got := visibleRepos(a); !slices.Equal(got, []string{"beta", "alpha", "gamma"}) {
		t.Errorf("after stats: %v", got)
	}
	checkRepo(t, a, "beta", 0)
}

func TestRepoCursorAcrossRescan(t *testing.T) {
	a := scannedApp(t)
	press(a, "j")
	update(a, scannedMsg{repos: []git.Repo{testRepo("alpha", 3), testRepo("beta", 2), testRepo("gamma", 1),
		testRepo("delta", 0)}})
	checkRepo(t, a, "beta", 2)

	a.selectAfterScan = "/src/owner/alpha"
	update(a, scannedMsg{repos: []git.Repo{testRepo("alpha", 3), testRepo("beta", 2), testRepo("gamma", 1)}})
	checkRepo(t, a, "alpha", 2)
	if a.selectAfterScan != "" {
		t.Errorf("selectAfterScan = %q, want it used up", a.selectAfterScan)
	}

	// A repo that's gone leaves the cursor where it was.
	update(a, scannedMsg{repos: []git.Repo{testRepo("beta", 2), testRepo("gamma", 1)}})
	checkRepo(t, a, "beta", 1)
}

func TestRepoFilterNarrowsAndClears(t *testing.T) {
	a := scannedApp(t)
	press(a, "/", "a")
	if got := visibleRepos(a); !slices.Equal(got, []string{"gamma", "beta", "alpha"}) {
		t.Errorf("filter a: %v", got)
	}
	checkRepo(t, a, "gamma", 0)

	press(a, "l")
	if got := visibleRepos(a); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("filter al: %v", got)
	}
	checkRepo(t, a, "alpha", 0)
	if a.cur == nil || a.cur.path != "/src/owner/alpha" {
		t.Error("narrowing onto another repo should load it")
	}

	// Widening keeps the repo that was at the top of the narrower view.
	press(a, "backspace")
	checkRepo(t, a, "alpha", 2)

	press(a, "l", "enter")
	if a.filtering || a.reposL.filter != "al" {
		t.Errorf("enter should keep the filter, got %q (filtering %v)", a.reposL.filter, a.filtering)
	}
	press(a, "esc")
	if a.reposL.filter != "" || len(visibleRepos(a)) != 3 {
		t.Errorf("esc should clear the filter, got %q showing %v", a.reposL.filter, visibleRepos(a))
	}
	checkRepo(t, a, "alpha", 2)

	press(a, "/", "z", "z", "z")
	if a.selectedRepo() != nil || a.reposL.footer() != "0 of 0" {
		t.Errorf("no match should select nothing, got footer %q", a.reposL.footer())
	}
	press(a, "esc")
	checkRepo(t, a, "gamma", 0)
}

func TestSelectRepoHiddenByFilter(t *testing.T) {
	a := scannedApp(t)
	press(a, "/", "a", "l", "enter")
	it := gh.Item{Kind: gh.PR, Repo: "owner/gamma", Number: 1, Head: "x"}
	update(a, a.checkedOut(it, proc.Result{Dir: "/src/owner/gamma"}, false, false))
	if a.reposL.filter != "" {
		t.Errorf("filter %q should be cleared to show the repo", a.reposL.filter)
	}
	checkRepo(t, a, "gamma", 0)
	if a.cur == nil || a.cur.path != "/src/owner/gamma" {
		t.Error("the selected repo should load")
	}

	press(a, "1", "/", "a", "enter")
	update(a, a.checkedOut(it, proc.Result{Dir: "/src/owner/beta"}, false, false))
	if a.reposL.filter != "a" {
		t.Errorf("a visible repo shouldn't clear the filter, got %q", a.reposL.filter)
	}
	checkRepo(t, a, "beta", 1)

	update(a, a.checkedOut(it, proc.Result{Dir: "/src/owner/missing"}, false, false))
	if a.reposL.filter != "a" {
		t.Errorf("an unknown repo shouldn't touch the filter, got %q", a.reposL.filter)
	}
	checkRepo(t, a, "beta", 1)
}

// loadedApp is a scanned App whose selected repo (gamma) has loaded.
func loadedApp(t *testing.T, d repoData) *App {
	t.Helper()
	a := scannedApp(t)
	d.path, d.loaded = "/src/owner/gamma", true
	update(a, repoMsg{&d})
	return a
}

func files(paths ...string) git.Status {
	var s git.Status
	for _, p := range paths {
		s.Files = append(s.Files, git.File{Path: p, X: ' ', Y: 'M'})
	}
	return s
}

func fileRowPaths(a *App) []string {
	var paths []string
	for _, i := range a.filesL.view {
		paths = append(paths, a.fileRows[i].path)
	}
	return paths
}

func checkFile(t *testing.T, a *App, path string, cursor int) {
	t.Helper()
	r := a.selectedFileRow()
	if r == nil || r.path != path || a.filesL.cursor != cursor {
		got := "<none>"
		if r != nil {
			got = r.path
		}
		t.Errorf("selected %s at %d, want %s at %d (rows %v)", got, a.filesL.cursor, path, cursor, fileRowPaths(a))
	}
}

func TestFileCursorFollowsFileAcrossReloads(t *testing.T) {
	a := loadedApp(t, repoData{status: files("b.go", "c.go")})
	press(a, "2", "j")
	checkFile(t, a, "c.go", 1)

	update(a, repoMsg{&repoData{path: "/src/owner/gamma", loaded: true, status: files("a.go", "b.go", "c.go")}})
	checkFile(t, a, "c.go", 2)

	// When the file goes, the cursor holds its place.
	update(a, repoMsg{&repoData{path: "/src/owner/gamma", loaded: true, status: files("a.go", "b.go", "d.go")}})
	checkFile(t, a, "d.go", 2)
	update(a, repoMsg{&repoData{path: "/src/owner/gamma", loaded: true, status: files("a.go")}})
	checkFile(t, a, "a.go", 0)
}

func TestFileCursorFollowsDirAcrossCollapse(t *testing.T) {
	a := loadedApp(t, repoData{status: files("src/x.go", "src/y.go", "z.go")})
	press(a, "2")
	checkFile(t, a, "src", 0)
	press(a, "enter")
	if got := fileRowPaths(a); !slices.Equal(got, []string{"src", "z.go"}) {
		t.Errorf("collapsed rows: %v", got)
	}
	checkFile(t, a, "src", 0)
	press(a, "j")
	checkFile(t, a, "z.go", 1)
	press(a, "k", "enter")
	checkFile(t, a, "src", 0)
	press(a, "G")
	checkFile(t, a, "z.go", 3)
}

func TestFileFilterBuildsTreeFromMatches(t *testing.T) {
	a := loadedApp(t, repoData{status: files("src/x.go", "src/y.go", "README.md")})
	press(a, "2", "/", "y")
	if got := fileRowPaths(a); !slices.Equal(got, []string{"src", "src/y.go"}) {
		t.Errorf("filter y: %v", got)
	}
	if r := a.fileRows[0]; len(r.files) != 1 {
		t.Errorf("a filtered dir should only own its matches, has %d files", len(r.files))
	}
	press(a, "j", "esc")
	if got := fileRowPaths(a); !slices.Equal(got, []string{"src", "src/x.go", "src/y.go", "README.md"}) {
		t.Errorf("cleared: %v", got)
	}
	checkFile(t, a, "src", 0)
}

func TestBranchTabSwitchResetsCursorAndFilter(t *testing.T) {
	a := loadedApp(t, repoData{
		branches: []git.Branch{{Name: "main"}, {Name: "dev"}, {Name: "feature/x"}},
		remotes:  []git.Branch{{Name: "origin/main"}, {Name: "origin/dev"}},
		tags:     []git.Tag{{Name: "v1", Subject: "first"}, {Name: "v2", Subject: "second"}},
	})
	press(a, "3", "/", "a", "enter")
	if a.branchesL.len() != 2 {
		t.Errorf("filter a shows %d branches, want 2", a.branchesL.len())
	}
	press(a, "j")
	if name, _ := a.selectedRef(); name != "feature/x" {
		t.Errorf("selected %q, want feature/x", name)
	}

	press(a, "]")
	if a.branchTab != 1 || a.branchesL.filter != "" || a.branchesL.cursor != 0 || a.branchesL.len() != 2 {
		t.Errorf("tab %d filter %q cursor %d len %d", a.branchTab, a.branchesL.filter, a.branchesL.cursor,
			a.branchesL.len())
	}
	if name, _ := a.selectedRef(); name != "origin/main" {
		t.Errorf("selected %q, want origin/main", name)
	}

	press(a, "/", "s", "e", "c")
	if name, _ := a.selectedRef(); name != "" {
		t.Errorf("remotes shouldn't match a tag subject, selected %q", name)
	}
	press(a, "esc", "]", "/", "s", "e", "c")
	if name, _ := a.selectedRef(); name != "v2" {
		t.Errorf("tags match on their subject, selected %q", name)
	}
}

func TestCommitAndStashFilters(t *testing.T) {
	a := loadedApp(t, repoData{
		commits: []git.Commit{
			{Hash: "aaa111", Short: "aaa", Author: "ann", Subject: "add parser"},
			{Hash: "bbb222", Short: "bbb", Author: "bob", Subject: "fix build", Tags: []string{"v1.0"}},
		},
		stashes: []git.Stash{{Ref: "stash@{0}", Message: "wip on main"}, {Ref: "stash@{1}", Message: "experiment"}},
	})
	press(a, "4", "/", "v", "1", "enter")
	if a.commitsL.len() != 1 || a.cur.commits[a.commitsL.sel()].Hash != "bbb222" {
		t.Errorf("filter by tag: %d commits", a.commitsL.len())
	}
	press(a, "5", "/", "x", "p", "enter")
	if a.stashL.len() != 1 || a.cur.stashes[a.stashL.sel()].Ref != "stash@{1}" {
		t.Errorf("filter stash: %d entries", a.stashL.len())
	}
	if a.commitsL.filter != "v1" {
		t.Errorf("each list keeps its own filter, commits has %q", a.commitsL.filter)
	}
}

func TestRepoChangeResetsDetailLists(t *testing.T) {
	a := loadedApp(t, repoData{status: files("a.go", "b.go")})
	press(a, "2", "/", "b", "enter")
	press(a, "1", "j")
	if a.filesL.filter != "" || a.filesL.cursor != 0 || a.filesL.len() != 0 {
		t.Errorf("files after repo change: filter %q cursor %d len %d", a.filesL.filter, a.filesL.cursor,
			a.filesL.len())
	}
}

func TestCommitCursorFollowsCommitAcrossReload(t *testing.T) {
	old := []git.Commit{{Hash: "b2", Subject: "second"}, {Hash: "a1", Subject: "first"}}
	a := loadedApp(t, repoData{commits: old})
	press(a, "4")
	update(a, repoMsg{&repoData{path: "/src/owner/gamma", loaded: true,
		commits: append([]git.Commit{{Hash: "c3", Subject: "third"}}, old...)}})
	if c := a.cur.commits[a.commitsL.sel()]; c.Hash != "b2" || a.commitsL.cursor != 1 {
		t.Errorf("a new commit moved the cursor to %s at %d", c.Hash, a.commitsL.cursor)
	}
}

func TestBranchTabSwitchWithFilterStartsAtTop(t *testing.T) {
	a := loadedApp(t, repoData{
		branches: []git.Branch{{Name: "main"}, {Name: "dev"}},
		remotes:  []git.Branch{{Name: "origin/main"}, {Name: "origin/dev"}},
	})
	press(a, "3", "/", "d", "enter", "]")
	if name, _ := a.selectedRef(); name != "origin/main" {
		t.Errorf("selected %q, want the first remote", name)
	}
}

func ghItem(n int) gh.Item {
	return gh.Item{Kind: gh.PR, Repo: "owner/repo", Number: n, Title: "pr " + itoa(n), State: gh.StateOpen}
}

// ghApp is an App in the GitHub view with two PR sections, the first holding
// items.
func ghApp(t *testing.T, items ...gh.Item) *App {
	t.Helper()
	a := newTestApp(t)
	a.gh.cfg.PRSections = []config.Section{{Title: "Mine", Filters: "author:@me"}, {Title: "All", Filters: "is:open"}}
	a.mode, a.focus = modeGitHub, pPRs
	update(a, ghSectionMsg{key: dataKey(gh.PR, "author:@me"), items: items})
	return a
}

func selectedGH(a *App) string {
	if it := a.ghItem(); it != nil {
		return it.Key()
	}
	return ""
}

func TestGHCursorFollowsItemAcrossReload(t *testing.T) {
	a := ghApp(t, ghItem(1), ghItem(2), ghItem(3))
	press(a, "j")
	update(a, ghSectionMsg{key: dataKey(gh.PR, "author:@me"), items: []gh.Item{ghItem(4), ghItem(1), ghItem(2)}})
	if got := selectedGH(a); got != "owner/repo#2" || a.gh.prs.list.cursor != 2 {
		t.Errorf("selected %q at %d, want owner/repo#2 at 2", got, a.gh.prs.list.cursor)
	}
}

func TestGHFilterAndTabSwitch(t *testing.T) {
	a := ghApp(t, ghItem(1), ghItem(12), ghItem(3))
	press(a, "/", "#", "1", "enter")
	if a.gh.prs.list.len() != 2 || selectedGH(a) != "owner/repo#1" {
		t.Errorf("filter #1 shows %d, selects %q", a.gh.prs.list.len(), selectedGH(a))
	}
	press(a, "j", "]")
	l := a.gh.prs.list
	if a.gh.prs.tab != 1 || l.filter != "" || l.cursor != 0 || l.len() != 0 {
		t.Errorf("tab %d filter %q cursor %d len %d", a.gh.prs.tab, l.filter, l.cursor, l.len())
	}
	press(a, "[")
	if selectedGH(a) != "owner/repo#1" || a.gh.prs.list.len() != 3 {
		t.Errorf("back on the first tab: %q of %d", selectedGH(a), a.gh.prs.list.len())
	}
}
