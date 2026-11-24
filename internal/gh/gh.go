// Package gh talks to GitHub through the gh CLI, so gitdeck uses whatever
// account and host gh is logged in with and never stores a token itself.
package gh

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bkenks/gitdeck/internal/git"
	"github.com/bkenks/gitdeck/internal/proc"
)

type Kind int

const (
	PR Kind = iota
	Issue
)

func (k Kind) String() string {
	if k == PR {
		return "pr"
	}
	return "issue"
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Item is a PR or issue row.
type Item struct {
	Kind           Kind
	Number         int
	Title          string
	URL            string
	State          string // OPEN, CLOSED, MERGED
	Draft          bool
	Repo           string // owner/name
	Author         string
	Created        time.Time
	Updated        time.Time
	Additions      int
	Deletions      int
	Comments       int
	Labels         []Label
	ReviewDecision string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
	Checks         string // SUCCESS, FAILURE, ERROR, PENDING, EXPECTED
	Head, Base     string
	Assignees      []string
}

func (i Item) Key() string { return fmt.Sprintf("%s#%d", i.Repo, i.Number) }

// Available reports whether gh is installed.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// env keeps gh from prompting and its output plain. GIT_TERMINAL_PROMPT
// covers the git that gh runs for clone and checkout.
var env = []string{
	"GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GH_NO_UPDATE_NOTIFIER=1", "GIT_TERMINAL_PROMPT=0",
}

func read(args ...string) ([]byte, error) {
	out, err := proc.Read("", env, "gh", args...)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func run(dir string, args ...string) proc.Result {
	return proc.Run(dir, env, "gh", args...)
}

// User is the login gh is authenticated as.
func User() (string, error) {
	out, err := read("api", "user", "--jq", ".login")
	return strings.TrimSpace(string(out)), err
}

const searchQuery = `query($q: String!, $n: Int!) {
  search(query: $q, type: ISSUE, first: $n) {
    issueCount
    nodes {
      ... on PullRequest {
        number title url state isDraft createdAt updatedAt additions deletions
        headRefName baseRefName reviewDecision
        author { login }
        repository { nameWithOwner }
        comments { totalCount }
        labels(first: 8) { nodes { name color } }
        assignees(first: 4) { nodes { login } }
        commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
      }
      ... on Issue {
        number title url state createdAt updatedAt
        author { login }
        repository { nameWithOwner }
        comments { totalCount }
        labels(first: 8) { nodes { name color } }
        assignees(first: 4) { nodes { login } }
      }
    }
  }
}`

type searchNode struct {
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	URL            string    `json:"url"`
	State          string    `json:"state"`
	IsDraft        bool      `json:"isDraft"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Additions      int       `json:"additions"`
	Deletions      int       `json:"deletions"`
	HeadRefName    string    `json:"headRefName"`
	BaseRefName    string    `json:"baseRefName"`
	ReviewDecision string    `json:"reviewDecision"`
	Author         struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Comments struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
	Labels struct {
		Nodes []Label `json:"nodes"`
	} `json:"labels"`
	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// Search runs a GitHub search for PRs or issues. The filters are plain
// GitHub search syntax; "is:pr"/"is:issue" is added when missing.
func Search(kind Kind, filters string, limit int) ([]Item, int, error) {
	q := strings.TrimSpace(filters)
	if !strings.Contains(q, "is:"+kind.String()) {
		q = "is:" + kind.String() + " " + q
	}
	if !strings.Contains(q, "sort:") {
		q += " sort:updated-desc"
	}
	out, err := read("api", "graphql", "-f", "query="+searchQuery, "-f", "q="+q, "-F", "n="+strconv.Itoa(limit))
	if err != nil {
		return nil, 0, err
	}
	var resp struct {
		Data struct {
			Search struct {
				IssueCount int          `json:"issueCount"`
				Nodes      []searchNode `json:"nodes"`
			} `json:"search"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, 0, err
	}
	items := make([]Item, 0, len(resp.Data.Search.Nodes))
	for _, n := range resp.Data.Search.Nodes {
		if n.Number == 0 {
			continue
		}
		it := Item{
			Kind: kind, Number: n.Number, Title: n.Title, URL: n.URL, State: n.State, Draft: n.IsDraft,
			Repo: n.Repository.NameWithOwner, Author: n.Author.Login, Created: n.CreatedAt, Updated: n.UpdatedAt,
			Additions: n.Additions, Deletions: n.Deletions, Comments: n.Comments.TotalCount,
			Labels: n.Labels.Nodes, ReviewDecision: n.ReviewDecision, Head: n.HeadRefName, Base: n.BaseRefName,
		}
		for _, a := range n.Assignees.Nodes {
			it.Assignees = append(it.Assignees, a.Login)
		}
		if len(n.Commits.Nodes) > 0 && n.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
			it.Checks = n.Commits.Nodes[0].Commit.StatusCheckRollup.State
		}
		items = append(items, it)
	}
	return items, resp.Data.Search.IssueCount, nil
}

// ─── Detail ─────────────────────────────────────────────────────────────────

type Check struct {
	Name       string
	Workflow   string
	Status     string // COMPLETED, IN_PROGRESS, QUEUED...
	Conclusion string // SUCCESS, FAILURE, SKIPPED, NEUTRAL, CANCELLED...
}

// Outcome folds CheckRuns and StatusContexts into one word:
// pass, fail, pending or skip.
func (c Check) Outcome() string {
	switch strings.ToUpper(c.Conclusion) {
	case "SUCCESS":
		return "pass"
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return "fail"
	case "SKIPPED", "NEUTRAL", "STALE":
		return "skip"
	}
	switch strings.ToUpper(c.Status) {
	case "SUCCESS":
		return "pass"
	case "FAILURE", "ERROR":
		return "fail"
	}
	return "pending"
}

type Review struct {
	Author string
	State  string
	At     time.Time
}

type Comment struct {
	Author string
	Body   string
	At     time.Time
}

type Detail struct {
	Body           string
	Checks         []Check
	Reviews        []Review // latest per reviewer
	ReviewRequests []string
	Comments       []Comment
	ChangedFiles   int
}

type actor struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
}

func (a actor) String() string {
	if a.Login != "" {
		return a.Login
	}
	if a.Slug != "" {
		return a.Slug
	}
	return a.Name
}

type rawComment struct {
	Author    actor     `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

func comments(raw []rawComment) []Comment {
	var out []Comment
	for _, c := range raw {
		out = append(out, Comment{Author: c.Author.String(), Body: c.Body, At: c.CreatedAt})
	}
	return out
}

// GetDetail loads the body, checks, reviews and comments for the preview.
func GetDetail(it Item) (Detail, error) {
	var d Detail
	if it.Kind == Issue {
		out, err := read("issue", "view", num(it), "-R", it.Repo, "--json", "body,comments")
		if err != nil {
			return d, err
		}
		var raw struct {
			Body     string       `json:"body"`
			Comments []rawComment `json:"comments"`
		}
		if err := json.Unmarshal(out, &raw); err != nil {
			return d, err
		}
		d.Body, d.Comments = raw.Body, comments(raw.Comments)
		return d, nil
	}
	out, err := read("pr", "view", num(it), "-R", it.Repo, "--json",
		"body,comments,reviews,reviewRequests,statusCheckRollup,changedFiles")
	if err != nil {
		return d, err
	}
	var raw struct {
		Body     string       `json:"body"`
		Comments []rawComment `json:"comments"`
		Reviews  []struct {
			Author      actor     `json:"author"`
			State       string    `json:"state"`
			SubmittedAt time.Time `json:"submittedAt"`
		} `json:"reviews"`
		ReviewRequests    []actor `json:"reviewRequests"`
		StatusCheckRollup []struct {
			Typename     string `json:"__typename"`
			Name         string `json:"name"`
			Context      string `json:"context"`
			WorkflowName string `json:"workflowName"`
			Status       string `json:"status"`
			Conclusion   string `json:"conclusion"`
			State        string `json:"state"`
		} `json:"statusCheckRollup"`
		ChangedFiles int `json:"changedFiles"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return d, err
	}
	d.Body, d.ChangedFiles, d.Comments = raw.Body, raw.ChangedFiles, comments(raw.Comments)
	latest := map[string]int{}
	for _, r := range raw.Reviews {
		if r.State == "COMMENTED" || r.State == "PENDING" {
			continue
		}
		rv := Review{Author: r.Author.String(), State: r.State, At: r.SubmittedAt}
		if i, ok := latest[rv.Author]; ok {
			d.Reviews[i] = rv
			continue
		}
		latest[rv.Author] = len(d.Reviews)
		d.Reviews = append(d.Reviews, rv)
	}
	for _, r := range raw.ReviewRequests {
		d.ReviewRequests = append(d.ReviewRequests, r.String())
	}
	for _, c := range raw.StatusCheckRollup {
		ch := Check{Name: c.Name, Workflow: c.WorkflowName, Status: c.Status, Conclusion: c.Conclusion}
		if c.Typename == "StatusContext" {
			ch.Name, ch.Status = c.Context, c.State
		}
		d.Checks = append(d.Checks, ch)
	}
	return d, nil
}

// Diff is the PR's patch.
func Diff(it Item) (string, error) {
	out, err := read("pr", "diff", num(it), "-R", it.Repo, "--color=never")
	return string(out), err
}

// BranchPR is the newest PR for a branch, shown beside it in the branches
// panel.
type BranchPR struct {
	Number int
	State  string
	Draft  bool
	URL    string
}

// BranchPRs maps head branch name to its most recent PR in repo.
func BranchPRs(slug string) (map[string]BranchPR, error) {
	out, err := read("pr", "list", "-R", slug, "--state", "all", "--limit", "60",
		"--json", "number,headRefName,state,isDraft,url")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number      int    `json:"number"`
		HeadRefName string `json:"headRefName"`
		State       string `json:"state"`
		IsDraft     bool   `json:"isDraft"`
		URL         string `json:"url"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	m := map[string]BranchPR{}
	for _, r := range raw { // newest first
		if _, ok := m[r.HeadRefName]; !ok {
			m[r.HeadRefName] = BranchPR{Number: r.Number, State: r.State, Draft: r.IsDraft, URL: r.URL}
		}
	}
	return m, nil
}

// ─── Actions ────────────────────────────────────────────────────────────────

func num(it Item) string { return strconv.Itoa(it.Number) }

// Clone clones slug into dest with gh, so gh's auth and protocol apply.
func Clone(slug, dest string) proc.Result {
	return run(filepath.Dir(dest), "repo", "clone", slug, dest)
}

// Checkout checks a PR out with gh. Once a merged PR's branch is deleted gh
// can't find it, so fall back to GitHub's permanent refs/pull/N/head.
func Checkout(dir string, it Item) proc.Result {
	r := run(dir, "pr", "checkout", num(it), "-R", it.Repo)
	if r.Err == nil || !strings.Contains(r.Output, "couldn't find remote ref") {
		return r
	}
	branch := it.Head
	if branch == "" {
		branch = "pr-" + num(it)
	}
	if git.BranchExists(dir, branch) {
		return git.Checkout(dir, branch)
	}
	if f := git.Run(dir, "fetch", "origin", "pull/"+num(it)+"/head:"+branch); f.Err != nil {
		return f
	}
	return git.Checkout(dir, branch)
}

func Approve(it Item) proc.Result {
	return run("", "pr", "review", num(it), "-R", it.Repo, "--approve")
}

func CommentOn(it Item, body string) proc.Result {
	return run("", it.Kind.String(), "comment", num(it), "-R", it.Repo, "--body", body)
}

// Merge merges with method "merge", "squash" or "rebase".
func Merge(it Item, method string) proc.Result {
	return run("", "pr", "merge", num(it), "-R", it.Repo, "--"+method)
}

func Close(it Item) proc.Result {
	return run("", it.Kind.String(), "close", num(it), "-R", it.Repo)
}

func Reopen(it Item) proc.Result {
	return run("", it.Kind.String(), "reopen", num(it), "-R", it.Repo)
}

func MarkReady(it Item) proc.Result {
	return run("", "pr", "ready", num(it), "-R", it.Repo)
}
