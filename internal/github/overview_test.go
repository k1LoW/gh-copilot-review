package github

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCopilotReviewOverviewNotReadyToApprove(t *testing.T) {
	o := parseCopilotReviewOverview(readTestdata(t, "review_not_ready_to_approve.md"))

	if got, want := o.Assessment, "🟡 Not ready to approve"; got != want {
		t.Errorf("Assessment: got %q, want %q", got, want)
	}
	if !o.NotReadyToApprove {
		t.Error("NotReadyToApprove: got false, want true")
	}
	if !strings.HasPrefix(o.Summary, "There are a few correctness/robustness issues") {
		t.Errorf("Summary: got %q", o.Summary)
	}
	if strings.Contains(o.Summary, "Once you've addressed") {
		t.Errorf("Summary must not include the trailing boilerplate: got %q", o.Summary)
	}
	if !o.NeedsAttention() {
		t.Error("NeedsAttention: got false, want true")
	}

	if got, want := len(o.SuppressedComments), 3; got != want {
		t.Fatalf("SuppressedComments: got %d, want %d", got, want)
	}
	first := o.SuppressedComments[0]
	if got, want := first.Path, "internal/service/reconcile.go"; got != want {
		t.Errorf("SuppressedComments[0].Path: got %q, want %q", got, want)
	}
	if got, want := first.Line, 207; got != want {
		t.Errorf("SuppressedComments[0].Line: got %d, want %d", got, want)
	}
	if !strings.HasPrefix(first.Body, "This switch has no default case.") {
		t.Errorf("SuppressedComments[0].Body: got %q", first.Body)
	}
	if !strings.Contains(first.Body, "This issue also appears on line 317 of the same file.") {
		t.Errorf("SuppressedComments[0].Body must keep every prose paragraph: got %q", first.Body)
	}
	if strings.Contains(first.Body, "case runner.ResultSucceeded") {
		t.Errorf("SuppressedComments[0].Body must not include the quoted code block: got %q", first.Body)
	}

	last := o.SuppressedComments[2]
	if got, want := last.Line, 109; got != want {
		t.Errorf("SuppressedComments[2].Line: got %d, want %d", got, want)
	}
	if strings.Contains(last.Body, "Files reviewed") {
		t.Errorf("SuppressedComments[2].Body must stop at the review metadata: got %q", last.Body)
	}
	for _, sc := range o.SuppressedComments {
		if strings.Contains(sc.Path, "mock_store.go") {
			t.Errorf("the \"Files not reviewed\" list must not be parsed as a suppressed comment: got %q", sc.Path)
		}
	}
}

func TestParseCopilotReviewOverviewWithoutSuppressedComments(t *testing.T) {
	o := parseCopilotReviewOverview(readTestdata(t, "review_not_ready_no_suppressed.md"))

	if got, want := o.Assessment, "🟡 Not ready to approve"; got != want {
		t.Errorf("Assessment: got %q, want %q", got, want)
	}
	if !o.NotReadyToApprove {
		t.Error("NotReadyToApprove: got false, want true")
	}
	if !strings.HasPrefix(o.Summary, "The new cancel RPC can incorrectly downgrade") {
		t.Errorf("Summary: got %q", o.Summary)
	}
	if got := len(o.SuppressedComments); got != 0 {
		t.Errorf("SuppressedComments: got %d, want 0", got)
	}
	if !o.NeedsAttention() {
		t.Error("NeedsAttention: got false, want true (the assessment alone is actionable)")
	}
}

func TestParseCopilotReviewOverviewApproved(t *testing.T) {
	body := strings.Join([]string{
		"### ✅ Ready to approve",
		"",
		"*This review doesn't count toward merge requirements.*",
		"",
		"No blocking issues found.",
	}, "\n")

	o := parseCopilotReviewOverview(body)

	if got, want := o.Assessment, "✅ Ready to approve"; got != want {
		t.Errorf("Assessment: got %q, want %q", got, want)
	}
	if o.NotReadyToApprove {
		t.Error("NotReadyToApprove: got true, want false")
	}
	if got, want := o.Summary, "No blocking issues found."; got != want {
		t.Errorf("Summary: got %q, want %q", got, want)
	}
	if o.NeedsAttention() {
		t.Error("NeedsAttention: got true, want false")
	}
}

func TestParseCopilotReviewOverviewLegacyBody(t *testing.T) {
	body := "Copilot reviewed 3 out of 3 changed files in this pull request and generated no comments."

	o := parseCopilotReviewOverview(body)

	if o.Assessment != "" {
		t.Errorf("Assessment: got %q, want empty", o.Assessment)
	}
	if o.NotReadyToApprove {
		t.Error("NotReadyToApprove: got true, want false")
	}
	if o.NeedsAttention() {
		t.Error("NeedsAttention: got true, want false")
	}
}

func TestNeedsAttentionChangesRequested(t *testing.T) {
	o := &CopilotReviewOverview{State: "CHANGES_REQUESTED"}
	if !o.NeedsAttention() {
		t.Error("NeedsAttention: got false, want true")
	}

	var nilOverview *CopilotReviewOverview
	if nilOverview.NeedsAttention() {
		t.Error("NeedsAttention on nil: got true, want false")
	}
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
