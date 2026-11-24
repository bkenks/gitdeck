package ui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bkenks/gitdeck/internal/config"
	"github.com/bkenks/gitdeck/internal/gh"
	"github.com/bkenks/gitdeck/internal/proc"
)

// newTestApp is an App with nothing loaded. Its usage history can't be
// written: /dev/null isn't a directory, so touch's save fails quietly.
func newTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", "/dev/null")
	a := &App{
		state:     config.State{LastUsed: map[string]time.Time{}},
		collapsed: map[string]bool{},
		gh:        newGHState(config.Config{}),
	}
	a.bindLists()
	return a
}

type wantLog struct{ repo, cmd, err string }

func logOf(a *App) []wantLog {
	var got []wantLog
	for _, e := range a.cmdLog {
		got = append(got, wantLog{e.repo, e.cmd, e.err})
	}
	return got
}

func checkResult(t *testing.T, a *App, log []wantLog, toast string, toastErr bool) {
	t.Helper()
	if got := logOf(a); !slices.Equal(got, log) {
		t.Errorf("command log:\n got %q\nwant %q", got, log)
	}
	if a.toast != toast || a.toastErr != toastErr {
		t.Errorf("toast = %q (err %v), want %q (err %v)", a.toast, a.toastErr, toast, toastErr)
	}
}

func update(a *App, msg tea.Msg) { a.Update(msg) }

func TestActionResult(t *testing.T) {
	ok := proc.Result{Dir: "/src/owner/repo", Cmd: "git fetch --all --prune"}
	bad := proc.Result{Dir: "/src/owner/repo", Cmd: "git pull", Err: errors.New("fatal: no upstream")}
	cases := []struct {
		name     string
		msg      resultMsg
		log      []wantLog
		toast    string
		toastErr bool
	}{
		{"success", resultMsg{results: []proc.Result{ok}, ok: "Fetched", refresh: true},
			[]wantLog{{"repo", "git fetch --all --prune", ""}}, "Fetched", false},
		{"silent success", resultMsg{results: []proc.Result{ok}},
			[]wantLog{{"repo", "git fetch --all --prune", ""}}, "", false},
		{"failure", resultMsg{results: []proc.Result{bad}, ok: "Pulled"},
			[]wantLog{{"repo", "git pull", "fatal: no upstream"}}, "fatal: no upstream", true},
		{"no command", resultMsg{results: []proc.Result{{Err: errors.New("boom")}}}, nil, "boom", true},
		{"no dir", resultMsg{results: []proc.Result{{Cmd: "open https://x.y", Err: errors.New("no opener")}}},
			[]wantLog{{".", "open https://x.y", "no opener"}}, "no opener", true},
		{"several failures", resultMsg{results: []proc.Result{bad, ok, bad}, ok: "Done"},
			[]wantLog{
				{"repo", "git pull", "fatal: no upstream"},
				{"repo", "git fetch --all --prune", ""},
				{"repo", "git pull", "fatal: no upstream"},
			},
			"fatal: no upstream; fatal: no upstream", true},
		{"toast override", resultMsg{results: []proc.Result{bad}, ok: "Done", toast: "Custom"},
			[]wantLog{{"repo", "git pull", "fatal: no upstream"}}, "Custom", false},
		{"nothing ran", resultMsg{ok: "Nothing to do"}, nil, "Nothing to do", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t)
			update(a, c.msg)
			checkResult(t, a, c.log, c.toast, c.toastErr)
		})
	}
}

func TestActionResultFollowUps(t *testing.T) {
	t.Run("rescan", func(t *testing.T) {
		a := newTestApp(t)
		update(a, resultMsg{results: []proc.Result{{Dir: "/src/owner", Cmd: "rm -rf ~/src/owner/repo"}},
			ok: "Deleted repo", rescan: true})
		checkResult(t, a, []wantLog{{"owner", "rm -rf ~/src/owner/repo", ""}}, "Deleted repo", false)
		if !a.scanning {
			t.Error("a delete should rescan the repos dir")
		}
	})
	t.Run("refresh reloads the main panel", func(t *testing.T) {
		a := newTestApp(t)
		a.mainKey = "diff"
		update(a, resultMsg{results: []proc.Result{{Dir: "/src/owner/repo", Cmd: "git add -A"}}, refresh: true})
		if a.mainKey != "" {
			t.Errorf("mainKey = %q, want it cleared", a.mainKey)
		}
	})
	t.Run("refresh without a repo", func(t *testing.T) {
		a := newTestApp(t)
		a.mainKey = "diff"
		update(a, resultMsg{results: []proc.Result{{Cmd: "open https://x.y"}}, refresh: true})
		if a.mainKey != "diff" {
			t.Errorf("mainKey = %q, want it kept", a.mainKey)
		}
	})
	t.Run("then", func(t *testing.T) {
		a := newTestApp(t)
		ran := false
		update(a, resultMsg{then: func() tea.Cmd { ran = true; return nil }})
		if !ran {
			t.Error("then didn't run")
		}
	})
}

func pullResult(name string, err error) proc.Result {
	return proc.Result{Dir: "/src/" + name, Cmd: "git pull --ff-only", Err: err}
}

func TestPullAllResult(t *testing.T) {
	moved := pullResult("moved", nil)
	same := pullResult("same", nil)
	diverged := pullResult("diverged", errors.New("fatal: Not possible to fast-forward, aborting."))
	cases := []struct {
		name     string
		results  []proc.Result
		moved    []bool
		log      []wantLog
		toast    string
		toastErr bool
	}{
		{"nothing to pull", nil, nil, nil, "Pulled 0 repos", false},
		{"all up to date", []proc.Result{same, same}, []bool{false, false}, nil, "Pulled 2 repos", false},
		{"one moved", []proc.Result{same, moved}, []bool{false, true},
			[]wantLog{{"moved", "git pull --ff-only", ""}}, "Pulled 2 repos", false},
		{"one failed", []proc.Result{moved, diverged, same}, []bool{true, false, false},
			[]wantLog{
				{"moved", "git pull --ff-only", ""},
				{"diverged", "git pull --ff-only", "fatal: Not possible to fast-forward, aborting."},
			},
			"Pulled 2 repos, 1 couldn't fast-forward (see command log)", true},
		{"all failed", []proc.Result{diverged}, []bool{false},
			[]wantLog{{"diverged", "git pull --ff-only", "fatal: Not possible to fast-forward, aborting."}},
			"Pulled 0 repos, 1 couldn't fast-forward (see command log)", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t)
			a.mainKey = "diff"
			update(a, a.pulledAll(c.results, c.moved))
			checkResult(t, a, c.log, c.toast, c.toastErr)
			if a.mainKey != "" {
				t.Error("a pull should reload what's on screen")
			}
		})
	}
}

func TestCloneResult(t *testing.T) {
	cloneA := proc.Result{Dir: "/src/owner", Cmd: "gh repo clone owner/a /src/owner/a"}
	cloneB := proc.Result{Dir: "/src/other", Cmd: "git clone https://x.y/other/b /src/other/b"}
	existsB := proc.Result{Err: errors.New("~/src/other/b already exists")}
	failC := proc.Result{Dir: "/src/owner", Cmd: "gh repo clone owner/c /src/owner/c",
		Err: errors.New("repository not found")}
	cases := []struct {
		name     string
		results  []proc.Result
		first    string
		log      []wantLog
		toast    string
		toastErr bool
	}{
		{"all cloned", []proc.Result{cloneA, cloneB}, "/src/owner/a",
			[]wantLog{
				{"owner", "gh repo clone owner/a /src/owner/a", ""},
				{"other", "git clone https://x.y/other/b /src/other/b", ""},
			},
			"Cloned 2 repos", false},
		{"some failed", []proc.Result{cloneA, existsB, failC}, "/src/owner/a",
			[]wantLog{
				{"owner", "gh repo clone owner/a /src/owner/a", ""},
				{"owner", "gh repo clone owner/c /src/owner/c", "repository not found"},
			},
			"~/src/other/b already exists; repository not found", true},
		{"bad url only", []proc.Result{{Err: errors.New("can't tell where to clone x")}}, "",
			nil, "can't tell where to clone x", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t)
			update(a, a.clonedAll(c.results, c.first))
			checkResult(t, a, c.log, c.toast, c.toastErr)
			if a.selectAfterScan != c.first {
				t.Errorf("selectAfterScan = %q, want %q", a.selectAfterScan, c.first)
			}
			if !a.scanning {
				t.Error("cloning should rescan the repos dir")
			}
		})
	}
}

// TestCloneOneWithoutCloning covers the failures caught before git runs.
func TestCloneOneWithoutCloning(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "owner", "repo")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	r, dest := cloneOne(root, "owner/repo")
	if r.Cmd != "" || r.Err == nil || r.Err.Error() != config.TildePath(existing)+" already exists" {
		t.Errorf("existing repo: %+v", r)
	}
	if dest != existing {
		t.Errorf("an existing repo should still be selected, got dest %q", dest)
	}
	r, dest = cloneOne(root, "")
	if r.Cmd != "" || r.Err == nil || dest != "" {
		t.Errorf("unusable url: %+v, dest %q", r, dest)
	}
}

func TestGHCheckoutResult(t *testing.T) {
	it := gh.Item{Kind: gh.PR, Repo: "owner/repo", Number: 12, Head: "feature"}
	path := "/src/owner/repo"
	checkout := proc.Result{Dir: path, Cmd: "gh pr checkout 12 -R owner/repo"}
	logged := []wantLog{{"repo", "gh pr checkout 12 -R owner/repo", ""}}

	t.Run("in an existing clone", func(t *testing.T) {
		a := newTestApp(t)
		a.mode, a.focus, a.branchTab = modeGitHub, pPRs, 2
		update(a, a.checkedOut(it, checkout, false, false))
		checkResult(t, a, logged, "Checked out #12 (feature)", false)
		if a.mode != modeLocal || a.focus != pBranches || a.saved[modeGitHub].focus != pPRs {
			t.Errorf("mode %v focus %v saved GitHub focus %v", a.mode, a.focus, a.saved[modeGitHub].focus)
		}
		if a.branchTab != 0 {
			t.Errorf("branchTab = %d, want local branches", a.branchTab)
		}
		if a.scanning || a.selectAfterScan != "" {
			t.Error("checking out in an existing clone shouldn't rescan")
		}
	})
	t.Run("after cloning", func(t *testing.T) {
		a := newTestApp(t)
		update(a, a.checkedOut(it, checkout, true, false))
		checkResult(t, a, logged, "Checked out #12 (feature)", false)
		if !a.scanning || a.selectAfterScan != path {
			t.Errorf("a fresh clone should rescan and select %q, got %q", path, a.selectAfterScan)
		}
	})
	t.Run("into lazygit", func(t *testing.T) {
		a := newTestApp(t)
		update(a, a.checkedOut(it, checkout, false, true))
		checkResult(t, a, logged,
			"lazygit isn't installed: https://github.com/jesseduffield/lazygit#installation", true)
	})
	t.Run("checkout fails", func(t *testing.T) {
		a := newTestApp(t)
		a.mode, a.focus = modeGitHub, pPRs
		bad := checkout
		bad.Err = errors.New("fatal: couldn't find remote ref")
		update(a, a.checkedOut(it, bad, true, true))
		checkResult(t, a, []wantLog{{"repo", "gh pr checkout 12 -R owner/repo", "fatal: couldn't find remote ref"}},
			"fatal: couldn't find remote ref", true)
		if a.mode != modeGitHub || a.focus != pPRs || a.scanning {
			t.Error("a failed checkout should stay in the GitHub view")
		}
	})
}

func TestExecDoneResult(t *testing.T) {
	t.Run("clean exit", func(t *testing.T) {
		a := newTestApp(t)
		a.mainKey = "diff"
		update(a, a.execDone("/src/owner/repo", "lazygit status")(nil))
		checkResult(t, a, []wantLog{{"repo", "lazygit status", ""}}, "", false)
		if a.mainKey != "" {
			t.Error("returning from another program should reload what's on screen")
		}
	})
	t.Run("failed exit", func(t *testing.T) {
		a := newTestApp(t)
		update(a, a.execDone("/src/owner/repo", "vi README.md")(errors.New("exit status 1")))
		checkResult(t, a, []wantLog{{"repo", "vi README.md", "exit status 1"}}, "vi README.md: exit status 1", true)
	})
}
