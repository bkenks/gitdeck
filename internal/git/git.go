// Package git is gitdeck's plain-git layer: it finds repos, reads their state
// and runs the actions the UI offers. Everything shells out to git.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Read runs a read-only git command. Optional locks are off so a refresh
// never fights with another git process over the index.
func Read(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	Detach(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			err = errors.New(firstLine(msg))
		}
	}
	return out.String(), err
}

// Result is the outcome of a command that changes something. The UI logs
// Cmd in its command log.
type Result struct {
	Dir    string
	Cmd    string
	Output string
	Err    error
}

// Run runs a git command that changes state.
func Run(dir string, args ...string) Result {
	return RunCmd(dir, "git", args...)
}

// RunCmd runs any command in dir, detached from the terminal.
func RunCmd(dir, name string, args ...string) Result {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1")
	Detach(cmd)
	out, err := cmd.CombinedOutput()
	r := Result{Dir: dir, Cmd: Display(name, args...), Output: strings.TrimSpace(string(out))}
	if err != nil {
		msg := lastMeaningfulLine(r.Output)
		if msg == "" {
			msg = err.Error()
		}
		r.Err = errors.New(msg)
	}
	return r
}

// Display renders a command line for the command log.
func Display(name string, args ...string) string {
	parts := []string{name}
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'") {
			a = strconv.Quote(a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func lastMeaningfulLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	// git's own diagnosis beats a wrapper's "exit status 128".
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "fatal: ") || strings.HasPrefix(l, "error: ") {
			return l
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "hint:") {
			continue
		}
		return l
	}
	return ""
}

// ─── Repos ──────────────────────────────────────────────────────────────────

// Repo is one repository on disk.
type Repo struct {
	Name string // directory name
	Key  string // path relative to the repos dir, e.g. "bkenks/gitdeck"
	Path string // absolute path
	Cwd  bool   // the repo gitdeck was launched from, outside the repos dir

	// Modified is when the repo's index last changed: a free recency signal
	// available before Stats load.
	Modified time.Time

	Stats  Stats
	Loaded bool // Stats filled in
}

// Namespace is the key without the repo's own name.
func (r Repo) Namespace() string {
	ns := filepath.ToSlash(filepath.Dir(r.Key))
	if ns == "." {
		return ""
	}
	return ns
}

// Stats is a repo's local state, cheap enough to load for every repo.
type Stats struct {
	Branch      string
	Detached    bool
	Upstream    bool
	Ahead       int
	Behind      int
	Dirty       int
	Branches    int
	Unpushed    int // commits on local branches that no remote has
	LastCommit  time.Time
	Slug        string // owner/name when origin is on GitHub
	RemoteURL   string
	DefaultHead string
}

// Scan walks root and returns every repo under it. Walking stops at a repo,
// so nested namespaces (host/owner/repo) work.
func Scan(root string) ([]Repo, error) {
	if root == "" {
		return nil, errors.New("no repos dir set")
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	var repos []Repo
	err = filepath.WalkDir(real, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() || p == real {
			return nil
		}
		name := d.Name()
		if name == "node_modules" || name == ".git" {
			return filepath.SkipDir
		}
		if _, err := os.Stat(filepath.Join(p, ".git")); err != nil {
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(real, p)
		repos = append(repos, Repo{
			Name:     name,
			Key:      filepath.ToSlash(rel),
			Path:     filepath.Join(root, rel),
			Modified: Modified(p),
		})
		return filepath.SkipDir
	})
	return repos, err
}

// Modified is the mtime of the repo's index, or of .git itself.
func Modified(dir string) time.Time {
	for _, p := range []string{filepath.Join(dir, ".git", "index"), filepath.Join(dir, ".git")} {
		if fi, err := os.Stat(p); err == nil {
			return fi.ModTime()
		}
	}
	return time.Time{}
}

// Toplevel returns the work tree root containing dir, or "".
func Toplevel(dir string) string {
	out, err := Read(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// LoadStats reads a repo's local state without touching the network.
func LoadStats(dir string) Stats {
	var s Stats
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		if st, err := GetStatus(dir); err == nil {
			s.Branch, s.Detached = st.Branch, st.Detached
			s.Upstream, s.Ahead, s.Behind = st.Upstream != "", st.Ahead, st.Behind
			s.Dirty = len(st.Files)
		}
	}()
	go func() {
		defer wg.Done()
		if out, err := Read(dir, "for-each-ref", "--format=%(refname)", "refs/heads"); err == nil {
			s.Branches = countLines(out)
		}
		if out, err := Read(dir, "rev-list", "--count", "--branches", "--not", "--remotes"); err == nil {
			s.Unpushed, _ = strconv.Atoi(strings.TrimSpace(out))
		}
	}()
	go func() {
		defer wg.Done()
		if out, err := Read(dir, "log", "-1", "--format=%ct"); err == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64); err == nil {
				s.LastCommit = time.Unix(n, 0)
			}
		}
	}()
	go func() {
		defer wg.Done()
		s.RemoteURL = RemoteURL(dir)
		s.Slug = GitHubSlug(s.RemoteURL)
	}()
	wg.Wait()
	return s
}

// RemoteURL is origin's URL, or the first remote's.
func RemoteURL(dir string) string {
	if out, err := Read(dir, "remote", "get-url", "origin"); err == nil {
		return strings.TrimSpace(out)
	}
	out, err := Read(dir, "remote")
	if err != nil {
		return ""
	}
	if name := firstLine(strings.TrimSpace(out)); name != "" {
		if u, err := Read(dir, "remote", "get-url", name); err == nil {
			return strings.TrimSpace(u)
		}
	}
	return ""
}

// GitHubSlug extracts owner/name from a github.com remote URL.
func GitHubSlug(url string) string {
	u := strings.TrimSuffix(strings.TrimSpace(url), ".git")
	for _, p := range []string{"git@github.com:", "ssh://git@github.com/", "https://github.com/", "http://github.com/", "git://github.com/"} {
		if strings.HasPrefix(u, p) {
			parts := strings.Split(strings.TrimPrefix(u, p), "/")
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// ─── Status ─────────────────────────────────────────────────────────────────

// File is one changed path. X is the index (staged) status, Y the work tree.
type File struct {
	Path, Orig string
	X, Y       byte
}

func (f File) Untracked() bool   { return f.X == '?' }
func (f File) HasStaged() bool   { return f.X != ' ' && f.X != '?' }
func (f File) HasUnstaged() bool { return f.Y != ' ' || f.X == '?' }
func (f File) Conflicted() bool {
	return f.X == 'U' || f.Y == 'U' || (f.X == 'A' && f.Y == 'A') || (f.X == 'D' && f.Y == 'D')
}
func (f File) Code() string        { return string([]byte{f.X, f.Y}) }
func (f File) FullyStaged() bool   { return f.HasStaged() && !f.HasUnstaged() }
func (f File) NewInIndex() bool    { return f.X == 'A' }
func (f File) DeletedInTree() bool { return f.Y == 'D' }

type Status struct {
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
	Detached bool
	Unborn   bool
	Files    []File
}

// GetStatus parses `git status --porcelain=v1 -b -z`.
func GetStatus(dir string) (Status, error) {
	var st Status
	out, err := Read(dir, "-c", "core.quotepath=false", "status", "--porcelain=v1", "-b", "-z", "--untracked-files=all")
	if err != nil {
		return st, err
	}
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if e == "" {
			continue
		}
		if strings.HasPrefix(e, "## ") {
			parseBranchLine(&st, strings.TrimPrefix(e, "## "))
			continue
		}
		if len(e) < 4 {
			continue
		}
		f := File{X: e[0], Y: e[1], Path: e[3:]}
		if f.X == 'R' || f.X == 'C' {
			if i+1 < len(entries) {
				f.Orig = entries[i+1]
				i++
			}
		}
		st.Files = append(st.Files, f)
	}
	sort.SliceStable(st.Files, func(i, j int) bool { return st.Files[i].Path < st.Files[j].Path })
	return st, nil
}

func parseBranchLine(st *Status, line string) {
	switch {
	case strings.HasPrefix(line, "No commits yet on "):
		st.Branch, st.Unborn = strings.TrimPrefix(line, "No commits yet on "), true
		return
	case strings.HasPrefix(line, "Initial commit on "):
		st.Branch, st.Unborn = strings.TrimPrefix(line, "Initial commit on "), true
		return
	case strings.HasPrefix(line, "HEAD (no branch)"):
		st.Detached = true
		st.Branch = "HEAD"
		return
	}
	track := ""
	if i := strings.Index(line, " ["); i >= 0 {
		track = strings.Trim(line[i+1:], "[]")
		line = line[:i]
	}
	if i := strings.Index(line, "..."); i >= 0 {
		st.Branch, st.Upstream = line[:i], line[i+3:]
	} else {
		st.Branch = line
	}
	st.Ahead, st.Behind, _ = parseTrack(track)
}

// parseTrack reads "ahead 1, behind 2" / "gone".
func parseTrack(s string) (ahead, behind int, gone bool) {
	for _, part := range strings.Split(s, ",") {
		f := strings.Fields(part)
		if len(f) == 1 && f[0] == "gone" {
			gone = true
		}
		if len(f) != 2 {
			continue
		}
		n, _ := strconv.Atoi(f[1])
		switch f[0] {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return
}

// ─── Refs ───────────────────────────────────────────────────────────────────

type Branch struct {
	Name     string
	Head     bool
	Upstream string
	Ahead    int
	Behind   int
	Gone     bool
	Date     time.Time
	Subject  string
	Remote   bool
}

func Branches(dir string) ([]Branch, error) {
	out, err := Read(dir, "for-each-ref", "--sort=-committerdate", "refs/heads",
		"--format=%(HEAD)%00%(refname:short)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(committerdate:unix)%00%(contents:subject)")
	if err != nil {
		return nil, err
	}
	var bs []Branch
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 6 {
			continue
		}
		b := Branch{Head: f[0] == "*", Name: f[1], Upstream: f[2], Date: unix(f[4]), Subject: f[5]}
		b.Ahead, b.Behind, b.Gone = parseTrack(f[3])
		bs = append(bs, b)
	}
	// Current branch first, like lazygit.
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].Head && !bs[j].Head })
	return bs, nil
}

func RemoteBranches(dir string) ([]Branch, error) {
	out, err := Read(dir, "for-each-ref", "--sort=-committerdate", "refs/remotes",
		"--format=%(refname:short)%00%(committerdate:unix)%00%(contents:subject)%00%(symref)")
	if err != nil {
		return nil, err
	}
	var bs []Branch
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 4 || f[3] != "" || !strings.Contains(f[0], "/") {
			continue
		}
		bs = append(bs, Branch{Name: f[0], Date: unix(f[1]), Subject: f[2], Remote: true})
	}
	return bs, nil
}

type Tag struct {
	Name    string
	Date    time.Time
	Subject string
}

func Tags(dir string) ([]Tag, error) {
	out, err := Read(dir, "for-each-ref", "--sort=-creatordate", "refs/tags",
		"--format=%(refname:short)%00%(creatordate:unix)%00%(contents:subject)")
	if err != nil {
		return nil, err
	}
	var ts []Tag
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 3 {
			continue
		}
		ts = append(ts, Tag{Name: f[0], Date: unix(f[1]), Subject: f[2]})
	}
	return ts, nil
}

func unix(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// ─── Log ────────────────────────────────────────────────────────────────────

type Commit struct {
	Hash, Short string
	Author      string
	Date        time.Time
	Tags        []string
	Subject     string
	Merge       bool
	Unpushed    bool
}

// Log reads the current branch's history.
func Log(dir string, limit int) ([]Commit, error) {
	if _, err := Read(dir, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return nil, nil // unborn branch: no history yet
	}
	out, err := Read(dir, "-c", "log.showSignature=false", "log", "-n", strconv.Itoa(limit),
		"--format=%H%x00%h%x00%an%x00%at%x00%D%x00%P%x00%s", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	unpushed := map[string]bool{}
	if up, err := Read(dir, "rev-list", "HEAD", "--not", "--remotes"); err == nil {
		for _, h := range strings.Fields(up) {
			unpushed[h] = true
		}
	}
	var cs []Commit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 7 {
			continue
		}
		c := Commit{Hash: f[0], Short: f[1], Author: f[2], Date: unix(f[3]), Subject: f[6],
			Merge: len(strings.Fields(f[5])) > 1, Unpushed: unpushed[f[0]]}
		for _, d := range strings.Split(f[4], ", ") {
			if strings.HasPrefix(d, "tag: ") {
				c.Tags = append(c.Tags, strings.TrimPrefix(d, "tag: "))
			}
		}
		cs = append(cs, c)
	}
	return cs, nil
}

type Stash struct {
	Ref     string // stash@{0}
	Date    time.Time
	Message string
}

func Stashes(dir string) ([]Stash, error) {
	out, err := Read(dir, "stash", "list", "--format=%gd%x00%ct%x00%gs")
	if err != nil {
		return nil, err
	}
	var ss []Stash
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 3 {
			continue
		}
		ss = append(ss, Stash{Ref: f[0], Date: unix(f[1]), Message: f[2]})
	}
	return ss, nil
}

// ─── Content for the main panel ─────────────────────────────────────────────

// FileDiff returns the staged and unstaged diff for a path (a file or a
// directory). Untracked files diff against /dev/null.
func FileDiff(dir string, f File, isDir bool) (staged, unstaged string) {
	base := []string{"-c", "core.quotepath=false", "-c", "diff.noprefix=false", "diff", "--no-color", "--no-ext-diff", "-M"}
	if !isDir && f.Untracked() {
		out, _ := Read(dir, append(base, "--no-index", "--", os.DevNull, f.Path)...)
		return "", out
	}
	unstaged, _ = Read(dir, append(base, "--", f.Path)...)
	staged, _ = Read(dir, append(base, "--cached", "--", f.Path)...)
	if isDir {
		// Untracked files inside the directory don't show up in git diff.
		out, _ := Read(dir, "ls-files", "--others", "--exclude-standard", "--", f.Path)
		for _, p := range strings.Fields(out) {
			d, _ := Read(dir, append(base, "--no-index", "--", os.DevNull, p)...)
			unstaged += d
		}
	}
	return staged, unstaged
}

func Show(dir, rev string) (string, error) {
	return Read(dir, "-c", "core.quotepath=false", "show", "--no-color", "--no-ext-diff", "--stat", "-p",
		"--format=commit %H%nAuthor: %an <%ae>%nDate:   %ad%n%n%w(0,4,4)%B", rev, "--")
}

func StashShow(dir, ref string) (string, error) {
	return Read(dir, "stash", "show", "--no-color", "--stat", "-p", ref)
}

// Graph is a colored `git log --graph`, the main-panel view for refs. Git's
// default colors are ANSI, so they follow the terminal theme too.
func Graph(dir, ref string, n int) (string, error) {
	return Read(dir, "-c", "log.showSignature=false", "log", "--graph", "--color=always", "-n", strconv.Itoa(n),
		"--format=%C(yellow)%h%C(reset) %C(blue)%<(12,trunc)%an%C(reset) %C(dim)%<(14)%ar%C(reset)%C(auto)%d%C(reset) %s",
		ref, "--")
}

// ─── Actions ────────────────────────────────────────────────────────────────

func Stage(dir, path string) Result    { return Run(dir, "add", "-A", "--", path) }
func StageAll(dir string) Result       { return Run(dir, "add", "-A") }
func UnstageAll(dir string) Result     { return Run(dir, "reset", "-q") }
func CommitMsg(dir, msg string) Result { return Run(dir, "commit", "-m", msg) }
func Checkout(dir, ref string) Result  { return Run(dir, "checkout", ref) }
func NewBranch(dir, n string) Result   { return Run(dir, "checkout", "-b", n) }
func DeleteBranch(dir, n string, force bool) Result {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return Run(dir, "branch", flag, n)
}
func Fetch(dir string) Result           { return Run(dir, "fetch", "--all", "--prune") }
func Pull(dir string) Result            { return Run(dir, "pull") }
func PullFFOnly(dir string) Result      { return Run(dir, "pull", "--ff-only") }
func StashPush(dir, msg string) Result  { return stashPush(dir, msg) }
func StashApply(dir, ref string) Result { return Run(dir, "stash", "apply", ref) }
func StashPop(dir, ref string) Result   { return Run(dir, "stash", "pop", ref) }
func StashDrop(dir, ref string) Result  { return Run(dir, "stash", "drop", ref) }
func Clone(url, dest string) Result     { return RunCmd(filepath.Dir(dest), "git", "clone", url, dest) }
func CloneGH(slug, dest string) Result {
	return RunCmd(filepath.Dir(dest), "gh", "repo", "clone", slug, dest)
}

func stashPush(dir, msg string) Result {
	args := []string{"stash", "push", "--include-untracked"}
	if msg != "" {
		args = append(args, "-m", msg)
	}
	return Run(dir, args...)
}

// Unstage removes a path from the index. On an unborn branch there's no HEAD
// to reset to, so it falls back to rm --cached.
func Unstage(dir, path string) Result {
	r := Run(dir, "reset", "-q", "--", path)
	if r.Err != nil {
		return Run(dir, "rm", "--cached", "-r", "-q", "--", path)
	}
	return r
}

// CheckoutRemote checks out a remote branch as a local tracking branch.
func CheckoutRemote(dir, remoteRef string) Result {
	local := remoteRef
	if i := strings.Index(remoteRef, "/"); i >= 0 {
		local = remoteRef[i+1:]
	}
	if _, err := Read(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+local); err == nil {
		return Run(dir, "checkout", local)
	}
	return Run(dir, "checkout", "-b", local, "--track", remoteRef)
}

// Push pushes, setting an upstream on origin the first time.
func Push(dir string) Result {
	if _, err := Read(dir, "rev-parse", "--abbrev-ref", "@{u}"); err != nil {
		return Run(dir, "push", "-u", "origin", "HEAD")
	}
	return Run(dir, "push")
}

// Discard throws away all changes to files, staged and not.
func Discard(dir string, files []File) Result {
	var last Result
	for _, f := range files {
		switch {
		case f.Untracked():
			last = Run(dir, "clean", "-fd", "--", f.Path)
		case f.NewInIndex():
			last = Run(dir, "rm", "-rf", "--", f.Path)
		default:
			if f.Orig != "" {
				// A rename: drop the new path, restore the old one.
				Run(dir, "rm", "-rf", "--cached", "--", f.Path)
				_ = os.Remove(filepath.Join(dir, f.Path))
				last = Run(dir, "checkout", "HEAD", "--", f.Orig)
				continue
			}
			last = Run(dir, "checkout", "HEAD", "--", f.Path)
		}
		if last.Err != nil {
			return last
		}
	}
	if len(files) > 1 {
		last.Cmd = fmt.Sprintf("discard %d files", len(files))
	}
	return last
}

// Remove deletes a repo and prunes now-empty namespace dirs up to root.
// It refuses anything not strictly inside root.
func Remove(root, path string) error {
	if !inside(root, path) {
		return fmt.Errorf("refusing to delete %s: not inside %s", path, root)
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	for dir := filepath.Dir(path); inside(root, dir); dir = filepath.Dir(dir) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 || os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CloneDest works out where a clone URL (or owner/name) lands under root:
// <root>/<owner>/<name>, like gitkeeper.
func CloneDest(root, url string) (dest string, ghSlug string, err error) {
	u := strings.TrimSpace(url)
	if u == "" {
		return "", "", errors.New("empty URL")
	}
	if s := GitHubSlug(u); s != "" {
		return filepath.Join(root, filepath.FromSlash(s)), "", nil
	}
	// owner/name shorthand goes through gh, which honours its git protocol.
	if parts := strings.Split(u, "/"); len(parts) == 2 && !strings.Contains(u, ":") && parts[0] != "" && parts[1] != "" {
		return filepath.Join(root, parts[0], strings.TrimSuffix(parts[1], ".git")), u, nil
	}
	trim := strings.TrimSuffix(u, ".git")
	trim = strings.TrimSuffix(trim, "/")
	if i := strings.Index(trim, "://"); i >= 0 {
		trim = trim[i+3:]
	}
	if i := strings.Index(trim, "@"); i >= 0 {
		trim = trim[i+1:]
	}
	trim = strings.Replace(trim, ":", "/", 1)
	parts := strings.Split(trim, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("can't tell where %q should go", url)
	}
	ns, name := parts[len(parts)-2], parts[len(parts)-1]
	return filepath.Join(root, ns, name), "", nil
}
