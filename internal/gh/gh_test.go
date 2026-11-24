package gh

import (
	"encoding/json"
	"testing"
)

func TestCheckOutcome(t *testing.T) {
	cases := []struct {
		status, conclusion string
		want               Outcome
	}{
		{"COMPLETED", "SUCCESS", OutcomePass},
		{"COMPLETED", "success", OutcomePass},
		{"COMPLETED", "FAILURE", OutcomeFail},
		{"COMPLETED", "ERROR", OutcomeFail},
		{"COMPLETED", "TIMED_OUT", OutcomeFail},
		{"COMPLETED", "CANCELLED", OutcomeFail},
		{"COMPLETED", "ACTION_REQUIRED", OutcomeFail},
		{"COMPLETED", "STARTUP_FAILURE", OutcomeFail},
		{"COMPLETED", "SKIPPED", OutcomeSkip},
		{"COMPLETED", "NEUTRAL", OutcomeSkip},
		{"COMPLETED", "STALE", OutcomeSkip},
		{"COMPLETED", "", OutcomePending},
		{"IN_PROGRESS", "", OutcomePending},
		{"QUEUED", "", OutcomePending},
		{"SUCCESS", "", OutcomePass},
		{"success", "", OutcomePass},
		{"FAILURE", "", OutcomeFail},
		{"ERROR", "", OutcomeFail},
		{"PENDING", "", OutcomePending},
		{"EXPECTED", "", OutcomePending},
		{"", "", OutcomePending},
		{"FAILURE", "SUCCESS", OutcomePass},
		{"SUCCESS", "UNKNOWN_NEW", OutcomePass},
	}
	for _, c := range cases {
		got := Check{Status: c.status, Conclusion: c.conclusion}.Outcome()
		if got != c.want {
			t.Errorf("Outcome(status %q, conclusion %q) = %q, want %q",
				c.status, c.conclusion, got, c.want)
		}
	}
}

func TestKindPlural(t *testing.T) {
	if got := PR.Plural(); got != "pull requests" {
		t.Errorf("PR.Plural() = %q", got)
	}
	if got := Issue.Plural(); got != "issues" {
		t.Errorf("Issue.Plural() = %q", got)
	}
}

func TestSearchNodeDecodesTypedStates(t *testing.T) {
	src := `{"number": 1, "state": "MERGED", "reviewDecision": "CHANGES_REQUESTED",
		"commits": {"nodes": [{"commit": {"statusCheckRollup": {"state": "FAILURE"}}}]}}`
	var n searchNode
	if err := json.Unmarshal([]byte(src), &n); err != nil {
		t.Fatal(err)
	}
	if n.State != StateMerged || n.ReviewDecision != DecisionChangesRequested {
		t.Errorf("state %q, decision %q", n.State, n.ReviewDecision)
	}
	if got := n.Commits.Nodes[0].Commit.StatusCheckRollup.State; got != CheckFailure {
		t.Errorf("rollup %q", got)
	}

	var empty searchNode
	if err := json.Unmarshal([]byte(`{"number": 2, "reviewDecision": null}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.State != "" || empty.ReviewDecision != "" || len(empty.Commits.Nodes) != 0 {
		t.Errorf("missing fields decoded as %+v", empty)
	}
}
