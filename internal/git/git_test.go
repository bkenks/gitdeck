package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
	if err := Remove(root, outside); err == nil {
		t.Fatal("Remove deleted a path outside the root")
	}
	if err := Remove(root, root); err == nil {
		t.Fatal("Remove deleted the root itself")
	}
	repo := filepath.Join(root, "owner", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, repo); err != nil {
		t.Fatal(err)
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
