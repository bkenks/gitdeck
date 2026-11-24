package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bkenks/gitdeck/internal/config"
)

func TestParseBranchLine(t *testing.T) {
	cases := []struct {
		line             string
		branch, upstream string
		ahead, behind    int
		detached, unborn bool
	}{
		{"main...origin/main [ahead 1, behind 2]", "main", "origin/main", 1, 2, false, false},
		{"main...origin/main", "main", "origin/main", 0, 0, false, false},
		{"feature/x", "feature/x", "", 0, 0, false, false},
		{"No commits yet on main", "main", "", 0, 0, false, true},
		{"HEAD (no branch)", "HEAD", "", 0, 0, true, false},
		{"main...origin/main [gone]", "main", "origin/main", 0, 0, false, false},
	}
	for _, c := range cases {
		var st Status
		parseBranchLine(&st, c.line)
		if st.Branch != c.branch || st.Upstream != c.upstream || st.Ahead != c.ahead ||
			st.Behind != c.behind || st.Detached != c.detached || st.Unborn != c.unborn {
			t.Errorf("%q: got %+v", c.line, st)
		}
	}
}

func TestRemoveRefusesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if r := Remove(root, outside); r.Err == nil {
		t.Fatal("Remove deleted a path outside the root")
	}
	if r := Remove(root, root); r.Err == nil {
		t.Fatal("Remove deleted the root itself")
	}
	repo := filepath.Join(root, "owner", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	r := Remove(root, repo)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Dir != filepath.Join(root, "owner") || r.Cmd != "rm -rf "+config.TildePath(repo) {
		t.Errorf("result: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "owner")); !os.IsNotExist(err) {
		t.Error("empty namespace dir was left behind")
	}
}

// TestStatusAndActions runs the real git binary against a scratch repo.
func TestStatusAndActions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if r := Run(dir, args...); r.Err != nil {
			t.Fatalf("git %v: %v", args, r.Err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "Test User")
	run("config", "commit.gpgsign", "false")

	st, err := GetStatus(dir)
	if err != nil || !st.Unborn || st.Branch != "main" {
		t.Fatalf("fresh repo: %+v %v", st, err)
	}
	if cs, err := Log(dir, 10); err != nil || len(cs) != 0 {
		t.Fatalf("log of an empty repo: %v %v", cs, err)
	}

	write := func(name, body string) {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a\n")
	write("dir/b.txt", "b\n")
	if r := Stage(dir, "a.txt"); r.Err != nil {
		t.Fatal(r.Err)
	}
	st, _ = GetStatus(dir)
	if len(st.Files) != 2 {
		t.Fatalf("want 2 files, got %+v", st.Files)
	}
	for _, f := range st.Files {
		switch f.Path {
		case "a.txt":
			if !f.FullyStaged() {
				t.Errorf("a.txt should be staged: %q", f.Code())
			}
		case "dir/b.txt":
			if !f.Untracked() {
				t.Errorf("dir/b.txt should be untracked: %q", f.Code())
			}
		}
	}
	// Unstage on an unborn branch has no HEAD to reset to.
	if r := Unstage(dir, "a.txt"); r.Err != nil {
		t.Fatalf("unstage on unborn branch: %v", r.Err)
	}
	if r := StageAll(dir); r.Err != nil {
		t.Fatal(r.Err)
	}
	if r := CommitMsg(dir, "first"); r.Err != nil {
		t.Fatal(r.Err)
	}
	cs, err := Log(dir, 10)
	if err != nil || len(cs) != 1 || cs[0].Subject != "first" || !cs[0].Unpushed {
		t.Fatalf("log after commit: %+v %v", cs, err)
	}

	// Discard restores a tracked file and removes an untracked one.
	write("a.txt", "changed\n")
	write("new.txt", "new\n")
	st, _ = GetStatus(dir)
	if r := Discard(dir, st.Files); r.Err != nil {
		t.Fatal(r.Err)
	}
	st, _ = GetStatus(dir)
	if len(st.Files) != 0 {
		t.Fatalf("discard left %+v", st.Files)
	}

	if r := NewBranch(dir, "feature/x"); r.Err != nil {
		t.Fatal(r.Err)
	}
	bs, _ := Branches(dir)
	if len(bs) != 2 || !bs[0].Head || bs[0].Name != "feature/x" {
		t.Fatalf("branches: %+v", bs)
	}

	write("a.txt", "stash me\n")
	if r := StashPush(dir, "wip"); r.Err != nil {
		t.Fatal(r.Err)
	}
	ss, _ := Stashes(dir)
	if len(ss) != 1 || ss[0].Ref != "stash@{0}" {
		t.Fatalf("stashes: %+v", ss)
	}
	if r := StashPop(dir, ss[0].Ref); r.Err != nil {
		t.Fatal(r.Err)
	}
}

// TestReadAndRunErrors pins the messages the footer shows when git fails:
// reads report stderr's first line, actions git's own diagnosis.
func TestReadAndRunErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if r := Run(dir, "init", "-q", "-b", "main"); r.Err != nil {
		t.Fatal(r.Err)
	}
	if _, err := Read(dir, "rev-parse", "--verify", "--quiet", "nope"); err == nil ||
		err.Error() != "exit status 1" {
		t.Errorf("read with empty stderr: %v", err)
	}
	if _, err := Read(dir, "rev-parse", "--verify", "nope"); err == nil ||
		err.Error() != "fatal: Needed a single revision" {
		t.Errorf("read with stderr: %v", err)
	}
	r := Run(dir, "checkout", "nope")
	if r.Err == nil || r.Err.Error() != "error: pathspec 'nope' did not match any file(s) known to git" {
		t.Errorf("run error: %v", r.Err)
	}
	if r.Cmd != "git checkout nope" || r.Dir != dir {
		t.Errorf("run result: %+v", r)
	}
}

func TestPullFFOnlyReportsWhetherHEADMoved(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	upstream, clone := t.TempDir(), filepath.Join(t.TempDir(), "clone")
	git := func(dir string, args ...string) {
		t.Helper()
		if r := Run(dir, args...); r.Err != nil {
			t.Fatalf("git %v: %v", args, r.Err)
		}
	}
	commit := func(msg string) {
		t.Helper()
		git(upstream, "-c", "user.email=t@example.com", "-c", "user.name=Test User",
			"-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", msg)
	}
	git(upstream, "init", "-q", "-b", "main")
	commit("first")
	git(upstream, "clone", "-q", upstream, clone)

	if r, moved := PullFFOnly(clone); r.Err != nil || moved {
		t.Fatalf("pull with nothing new: moved %v, %+v", moved, r)
	}
	commit("second")
	r, moved := PullFFOnly(clone)
	if r.Err != nil || !moved {
		t.Fatalf("pull with a new commit: moved %v, %+v", moved, r)
	}
	if r.Cmd != "git pull --ff-only" || r.Dir != clone {
		t.Errorf("result: %+v", r)
	}

	git(clone, "-c", "user.email=t@example.com", "-c", "user.name=Test User",
		"-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "local")
	commit("third")
	if r, moved := PullFFOnly(clone); r.Err == nil || moved {
		t.Fatalf("pull that can't fast-forward: moved %v, %+v", moved, r)
	}
}
