package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bkenks/gitdeck/internal/gh"
	"github.com/bkenks/gitdeck/internal/git"
	. "github.com/bkenks/gitdeck/internal/theme"
)

func sameStyle(a, b lipgloss.Style) bool { return a.Render("x") == b.Render("x") }

func TestStateOf(t *testing.T) {
	cases := []struct {
		name         string
		it           gh.Item
		glyph, label string
		style        lipgloss.Style
	}{
		{"open issue", gh.Item{Kind: gh.Issue, State: gh.StateOpen}, "○", "Open", T.Green},
		{"closed issue", gh.Item{Kind: gh.Issue, State: gh.StateClosed}, "✔", "Closed", T.Magenta},
		{"unknown issue", gh.Item{Kind: gh.Issue}, "○", "Open", T.Green},
		{"open pr", gh.Item{Kind: gh.PR, State: gh.StateOpen}, "●", "Open", T.Green},
		{"draft pr", gh.Item{Kind: gh.PR, State: gh.StateOpen, Draft: true}, "◌", "Draft", T.Dim},
		{"merged pr", gh.Item{Kind: gh.PR, State: gh.StateMerged, Draft: true}, "◆", "Merged", T.Magenta},
		{"closed pr", gh.Item{Kind: gh.PR, State: gh.StateClosed, Draft: true}, "✕", "Closed", T.Red},
		{"unknown pr", gh.Item{Kind: gh.PR}, "●", "Open", T.Green},
	}
	for _, c := range cases {
		g, l, st := stateOf(c.it)
		if g != c.glyph || l != c.label || !sameStyle(st, c.style) {
			t.Errorf("%s: got %q %q", c.name, g, l)
		}
	}
}

func TestChecksSeg(t *testing.T) {
	cases := []struct {
		state gh.CheckState
		text  string
		style lipgloss.Style
	}{
		{gh.CheckSuccess, "✓", T.Green},
		{gh.CheckFailure, "✗", T.Red},
		{gh.CheckError, "✗", T.Red},
		{gh.CheckPending, "●", T.Yellow},
		{gh.CheckExpected, "●", T.Yellow},
		{"", " ", T.Fg},
		{"weird", " ", T.Fg},
	}
	for _, c := range cases {
		got := checksSeg(c.state)
		if got.Text != c.text || !sameStyle(got.Style, c.style) {
			t.Errorf("checksSeg(%q) = %q", c.state, got.Text)
		}
	}
}

func TestReviewSeg(t *testing.T) {
	cases := []struct {
		decision gh.ReviewDecision
		text     string
		style    lipgloss.Style
	}{
		{gh.DecisionApproved, "approved ", T.Green},
		{gh.DecisionChangesRequested, "changes ", T.Red},
		{gh.DecisionReviewRequired, "review ", T.Yellow},
	}
	for _, c := range cases {
		got := reviewSeg(c.decision)
		if len(got) != 1 || got[0].Text != c.text || !sameStyle(got[0].Style, c.style) {
			t.Errorf("reviewSeg(%q) = %v", c.decision, got)
		}
	}
	for _, d := range []gh.ReviewDecision{"", "weird"} {
		if got := reviewSeg(d); got != nil {
			t.Errorf("reviewSeg(%q) = %v, want nil", d, got)
		}
	}
}

func TestPRBadge(t *testing.T) {
	cases := []struct {
		pr    gh.BranchPR
		style lipgloss.Style
	}{
		{gh.BranchPR{Number: 7, State: gh.StateOpen}, T.Green},
		{gh.BranchPR{Number: 7, State: gh.StateOpen, Draft: true}, T.Dim},
		{gh.BranchPR{Number: 7, State: gh.StateMerged}, T.Magenta},
		{gh.BranchPR{Number: 7, State: gh.StateClosed}, T.Red},
		{gh.BranchPR{Number: 7, State: gh.StateClosed, Draft: true}, T.Dim},
	}
	for _, c := range cases {
		got := prBadge(c.pr)
		if len(got) != 1 || got[0].Text != "#7" || !sameStyle(got[0].Style, c.style) {
			t.Errorf("prBadge(%+v) = %v", c.pr, got)
		}
	}
}

func TestOpenPRCount(t *testing.T) {
	g := &ghState{branchPRs: map[string]map[string]gh.BranchPR{
		"o/r": {
			"a": {State: gh.StateOpen}, "b": {State: gh.StateOpen, Draft: true},
			"c": {State: gh.StateMerged}, "d": {State: gh.StateClosed},
		},
	}}
	repo := &git.Repo{}
	repo.Stats.Slug = "o/r"
	if got := g.openPRCount(repo); got != 2 {
		t.Errorf("openPRCount = %d, want 2", got)
	}
	if got := g.openPRCount(nil); got != 0 {
		t.Errorf("openPRCount(nil) = %d, want 0", got)
	}
}

func TestChecksSection(t *testing.T) {
	if got := checksSection(nil, 60); got != nil {
		t.Errorf("no checks: got %q", got)
	}
	checks := []gh.Check{
		{Name: "build", Conclusion: "SUCCESS"},
		{Name: "lint", Workflow: "CI", Conclusion: "FAILURE"},
		{Name: "e2e", Status: "IN_PROGRESS"},
		{Name: "docs", Conclusion: "SKIPPED"},
	}
	got := ansi.Strip(strings.Join(checksSection(checks, 60), "\n"))
	for _, want := range []string{"✓ 1  ✗ 1  ● 1", "  ✗ lint  CI", "  ● e2e"} {
		if !strings.Contains(got, want) {
			t.Errorf("checksSection missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "docs") || strings.Contains(got, "build") {
		t.Errorf("checksSection lists passing or skipped checks:\n%s", got)
	}
	passed := ansi.Strip(strings.Join(checksSection(checks[:1], 60), "\n"))
	if !strings.Contains(passed, "All checks passed") {
		t.Errorf("all passing: got\n%s", passed)
	}
}

func TestReviewsSection(t *testing.T) {
	if got := reviewsSection(gh.Detail{}, 60); got != nil {
		t.Errorf("no reviews: got %q", got)
	}
	d := gh.Detail{
		Reviews: []gh.Review{
			{Author: "ann", State: gh.ReviewApproved},
			{Author: "bob", State: gh.ReviewChangesRequested},
			{Author: "cat", State: gh.ReviewDismissed},
			{Author: "dan", State: "SOME_NEW_STATE"},
		},
		ReviewRequests: []string{"eve"},
	}
	got := ansi.Strip(strings.Join(reviewsSection(d, 60), "\n"))
	for _, want := range []string{
		"✓ ann approved", "✗ bob requested changes", "○ cat dismissed",
		"● dan some new state", "○ eve review requested",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reviewsSection missing %q in:\n%s", want, got)
		}
	}
}
