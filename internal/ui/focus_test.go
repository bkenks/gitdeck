package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bkenks/gitdeck/internal/config"
	"github.com/bkenks/gitdeck/internal/gh"
	"github.com/bkenks/gitdeck/internal/git"
)

// focusApp is a local App with gamma loaded: one file, one branch and one
// commit, and no stashes.
func focusApp(t *testing.T) *App {
	t.Helper()
	return loadedApp(t, repoData{
		status:   files("a.go"),
		branches: []git.Branch{{Name: "main", Head: true}},
		commits:  []git.Commit{{Hash: "a1", Subject: "first"}},
	})
}

func TestLocalFocusKeys(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want pane
	}{
		{"digits focus panels", []string{"3"}, pBranches},
		{"5 is stash", []string{"5"}, pStash},
		{"digit past the panels", []string{"4", "6"}, pCommits},
		{"0 focuses main", []string{"4", "0"}, pDetail},
		{"h from main returns", []string{"4", "0", "h"}, pCommits},
		{"l from main returns", []string{"4", "0", "l"}, pCommits},
		{"esc from main returns", []string{"3", "0", "esc"}, pBranches},
		{"0 twice keeps the way back", []string{"4", "0", "0", "esc"}, pCommits},
		{"digit from main", []string{"0", "3"}, pBranches},
		{"l cycles", []string{"l"}, pFiles},
		{"h wraps", []string{"h"}, pStash},
		{"l wraps", []string{"5", "l"}, pRepos},
		{"enter on a repo goes to files", []string{"enter"}, pFiles},
		{"enter on a file opens main", []string{"2", "enter"}, pDetail},
		{"enter on a commit opens main", []string{"4", "enter"}, pDetail},
		{"back from a commit's main", []string{"4", "enter", "h"}, pCommits},
		{"enter on an empty stash", []string{"5", "enter"}, pStash},
		{"enter in main", []string{"4", "0", "enter"}, pDetail},
		{"esc in a list", []string{"3", "esc"}, pBranches},
		{"tab round trip keeps main", []string{"4", "0", "tab", "tab"}, pDetail},
		{"and its way back", []string{"4", "0", "tab", "tab", "esc"}, pCommits},
	}
	for _, c := range cases {
		a := focusApp(t)
		press(a, c.keys...)
		if a.mode != modeLocal || a.focus != c.want {
			t.Errorf("%s: %v → mode %d focus %d, want local %d", c.name, c.keys, a.mode, a.focus, c.want)
		}
	}
}

func TestLocalFilterKeys(t *testing.T) {
	a := focusApp(t)
	press(a, "/", "a", "enter")
	if a.filtering || a.reposL.filter != "a" {
		t.Fatalf("after /a enter: filtering %v filter %q", a.filtering, a.reposL.filter)
	}
	press(a, "esc")
	if a.focus != pRepos || a.reposL.filter != "" {
		t.Errorf("esc: focus %d filter %q", a.focus, a.reposL.filter)
	}
	press(a, "0", "/")
	if a.filtering {
		t.Errorf("/ in main started a filter")
	}
}

func TestLocalMainScrolls(t *testing.T) {
	a := focusApp(t)
	a.main.set(make([]string, 30), false)
	press(a, "0", "j", "j")
	if a.main.offset != 2 || a.reposL.cursor != 0 {
		t.Errorf("j j: main offset %d, repo cursor %d", a.main.offset, a.reposL.cursor)
	}
	press(a, "G")
	if a.main.offset != 20 {
		t.Errorf("G: offset %d, want 20", a.main.offset)
	}
	press(a, "g")
	if a.main.offset != 0 {
		t.Errorf("g: offset %d", a.main.offset)
	}
	press(a, "esc", "j")
	if a.main.offset != 0 || a.reposL.cursor != 1 {
		t.Errorf("j in repos: main offset %d, repo cursor %d", a.main.offset, a.reposL.cursor)
	}
}

func TestLocalMainFollowsPanel(t *testing.T) {
	a := focusApp(t)
	press(a, "4", "0")
	if got := a.lazygitArgs(); !slices.Equal(got, []string{"log"}) {
		t.Errorf("lazygit from a commit's main: %v", got)
	}
	a.w, a.h = 160, 45
	press(a, "1", "0")
	if !strings.Contains(a.render(), "Repository") {
		t.Errorf("main from repos isn't the Repository summary")
	}
	if got := a.lazygitArgs(); !slices.Equal(got, []string{"status"}) {
		t.Errorf("lazygit from a repo's main: %v", got)
	}
}

// rectOf finds where the last render put pane p.
func rectOf(t *testing.T, a *App, p pane) rect {
	t.Helper()
	for _, r := range a.rects {
		if r.p == p {
			return r
		}
	}
	t.Fatalf("pane %d wasn't rendered", p)
	return rect{}
}

func wheel(a *App, r rect) {
	update(a, tea.MouseWheelMsg{X: r.x + 1, Y: r.y + 1, Button: tea.MouseWheelDown})
}

func click(a *App, r rect) {
	update(a, tea.MouseClickMsg{X: r.x + 1, Y: r.y + 1, Button: tea.MouseLeft})
}

func TestLocalMainMouse(t *testing.T) {
	a := focusApp(t)
	a.w, a.h = 160, 45
	press(a, "4")
	a.render()
	a.main.set(make([]string, 100), false)
	wheel(a, rectOf(t, a, pDetail))
	if a.main.offset != 3 || a.focus != pCommits {
		t.Errorf("wheel on main: offset %d focus %d", a.main.offset, a.focus)
	}
	click(a, rectOf(t, a, pDetail))
	if a.focus != pDetail {
		t.Fatalf("click on main: focus %d", a.focus)
	}
	press(a, "esc")
	if a.focus != pCommits {
		t.Errorf("esc after clicking main: focus %d", a.focus)
	}
	click(a, rectOf(t, a, pBranches))
	if a.focus != pBranches {
		t.Errorf("click on branches: focus %d", a.focus)
	}
}

func issueItem(n int) gh.Item {
	return gh.Item{Kind: gh.Issue, Repo: "owner/repo", Number: n, Title: "issue " + itoa(n), State: gh.StateOpen}
}

// ghFocusApp is ghApp with three PRs and two issues.
func ghFocusApp(t *testing.T) *App {
	t.Helper()
	a := ghApp(t, ghItem(1), ghItem(2), ghItem(3))
	a.gh.cfg.IssueSections = []config.Section{{Title: "Mine", Filters: "assignee:@me"}}
	update(a, ghSectionMsg{key: dataKey(gh.Issue, "assignee:@me"), items: []gh.Item{issueItem(7), issueItem(8)}})
	return a
}

func TestGHFocusKeys(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want pane
	}{
		{"2 focuses issues", []string{"2"}, pIssues},
		{"1 focuses PRs", []string{"2", "1"}, pPRs},
		{"3 does nothing", []string{"2", "3"}, pIssues},
		{"enter opens the preview", []string{"enter"}, pDetail},
		{"0 opens the preview", []string{"0"}, pDetail},
		{"h from the preview returns", []string{"enter", "h"}, pPRs},
		{"l from the preview returns", []string{"2", "0", "l"}, pIssues},
		{"esc from the preview returns", []string{"2", "enter", "esc"}, pIssues},
		{"enter in the preview", []string{"enter", "enter", "esc"}, pPRs},
		{"0 in the preview", []string{"2", "0", "0", "h"}, pIssues},
		{"digit from the preview", []string{"0", "2"}, pIssues},
		{"l toggles", []string{"l"}, pIssues},
		{"h toggles", []string{"h"}, pIssues},
		{"and back", []string{"l", "l"}, pPRs},
		{"esc in a list", []string{"2", "esc"}, pIssues},
		{"tab round trip keeps the preview", []string{"2", "0", "tab", "tab"}, pDetail},
		{"and its way back", []string{"2", "0", "tab", "tab", "esc"}, pIssues},
	}
	for _, c := range cases {
		a := ghFocusApp(t)
		press(a, c.keys...)
		if a.mode != modeGitHub || a.focus != c.want {
			t.Errorf("%s: %v → mode %d focus %d, want GitHub %d", c.name, c.keys, a.mode, a.focus, c.want)
		}
	}
}

func TestGHPreviewFollowsList(t *testing.T) {
	a := ghFocusApp(t)
	press(a, "j", "enter")
	if got := selectedGH(a); got != "owner/repo#2" {
		t.Errorf("preview from PRs shows %q", got)
	}
	press(a, "esc", "2", "j", "0")
	if got := selectedGH(a); got != "owner/repo#8" {
		t.Errorf("preview from issues shows %q", got)
	}
}

func TestGHFilterKeys(t *testing.T) {
	a := ghFocusApp(t)
	press(a, "/", "#", "2", "enter")
	if a.filtering || a.gh.prs.list.filter != "#2" || a.gh.prs.list.len() != 1 {
		t.Fatalf("after /#2 enter: filtering %v filter %q", a.filtering, a.gh.prs.list.filter)
	}
	press(a, "esc")
	if a.focus != pPRs || a.gh.prs.list.filter != "" || a.gh.prs.list.len() != 3 {
		t.Errorf("esc: focus %d filter %q", a.focus, a.gh.prs.list.filter)
	}
	press(a, "0", "/")
	if a.filtering {
		t.Errorf("/ in the preview started a filter")
	}
}

func TestGHPreviewScrolls(t *testing.T) {
	a := ghFocusApp(t)
	a.gh.preview.set(make([]string, 30), false)
	press(a, "enter", "j", "j")
	if a.gh.preview.offset != 2 || a.gh.prs.list.cursor != 0 {
		t.Errorf("j j: preview offset %d, PR cursor %d", a.gh.preview.offset, a.gh.prs.list.cursor)
	}
	press(a, "G")
	if a.gh.preview.offset != 20 {
		t.Errorf("G: offset %d, want 20", a.gh.preview.offset)
	}
	press(a, "esc", "j")
	if a.gh.prs.list.cursor != 1 {
		t.Errorf("j in PRs: cursor %d", a.gh.prs.list.cursor)
	}
}

func TestGHPreviewMouse(t *testing.T) {
	a := ghFocusApp(t)
	a.w, a.h = 160, 45
	press(a, "2")
	a.render()
	a.gh.preview.set(make([]string, 100), false)
	wheel(a, rectOf(t, a, pDetail))
	if a.gh.preview.offset != 3 || a.focus != pIssues {
		t.Errorf("wheel on the preview: offset %d focus %d", a.gh.preview.offset, a.focus)
	}
	click(a, rectOf(t, a, pDetail))
	press(a, "h")
	if a.focus != pIssues {
		t.Errorf("h after clicking the preview: focus %d", a.focus)
	}
}
