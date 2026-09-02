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
	if o.Approving {
		t.Error("Approving: got true, want false")
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
	if o.Approving {
		t.Error("Approving: got true, want false")
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
	if !o.Approving {
		t.Error("Approving: got false, want true")
	}
	if got, want := o.Summary, "No blocking issues found."; got != want {
		t.Errorf("Summary: got %q, want %q", got, want)
	}
	if o.NeedsAttention() {
		t.Error("NeedsAttention: got true, want false")
	}

	for _, assessment := range []string{"✅ Approved", "🟢 LGTM", "🟢 Looks good to me", "Approved with minor comments", "🟢 Approval recommended"} {
		t.Run(assessment, func(t *testing.T) {
			o := parseCopilotReviewOverview("### " + assessment + "\n\nNothing blocking.")

			if !o.Approving {
				t.Error("Approving: got false, want true")
			}
			if o.NeedsAttention() {
				t.Error("NeedsAttention: got true, want false")
			}
		})
	}
}

func TestParseCopilotReviewOverviewLegacyBody(t *testing.T) {
	tests := map[string]string{
		"no heading at all": "Copilot reviewed 3 out of 3 changed files in this pull request and generated no comments.",
		"prose first, headings only inside details": strings.Join([]string{
			"Copilot reviewed 3 out of 5 changed files in this pull request and generated no comments.",
			"",
			"<details>",
			"<summary>Review details</summary>",
			"",
			"### Files not reviewed (2)",
			"",
			"* **internal/store/mock/mock_store.go**: Generated file",
			"* **internal/runner/mock/mock_runner.go**: Generated file",
			"</details>",
		}, "\n"),
		"structural heading first": strings.Join([]string{
			"## Pull request overview",
			"",
			"Copilot reviewed 8 out of 8 changed files in this pull request and generated no new comments.",
		}, "\n"),
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			o := parseCopilotReviewOverview(body)

			// A body with no leading heading carries no assessment, however
			// many headings it buries in its <details> blocks.
			if o.Assessment != "" {
				t.Errorf("Assessment: got %q, want empty", o.Assessment)
			}
			if o.Summary != "" {
				t.Errorf("Summary: got %q, want empty", o.Summary)
			}
			if o.Approving {
				t.Error("Approving: got true, want false")
			}
			if o.NeedsAttention() {
				t.Error("NeedsAttention: got true, want false")
			}
		})
	}
}

func TestParseCopilotReviewOverviewLegacyOverviewWithSuppressedComments(t *testing.T) {
	o := parseCopilotReviewOverview(readTestdata(t, "review_legacy_overview_suppressed.md"))

	// "Pull request overview" is Copilot's own section title, not a verdict.
	if o.Assessment != "" {
		t.Errorf("Assessment: got %q, want empty", o.Assessment)
	}

	// Here the section is titled by a <summary>, not by a Markdown heading.
	if got, want := len(o.SuppressedComments), 1; got != want {
		t.Fatalf("SuppressedComments: got %d, want %d", got, want)
	}
	sc := o.SuppressedComments[0]
	if got, want := sc.Path, "internal/parser/overview.go"; got != want {
		t.Errorf("SuppressedComments[0].Path: got %q, want %q", got, want)
	}
	if got, want := sc.Line, 36; got != want {
		t.Errorf("SuppressedComments[0].Line: got %d, want %d", got, want)
	}
	if !strings.HasPrefix(sc.Body, "NeedsAttention() only flags negative assessments") {
		t.Errorf("SuppressedComments[0].Body: got %q", sc.Body)
	}
	if strings.Contains(sc.Body, "o.NotReadyToApprove") {
		t.Errorf("SuppressedComments[0].Body must not include the quoted code block: got %q", sc.Body)
	}
	if !o.NeedsAttention() {
		t.Error("NeedsAttention: got false, want true")
	}
}

func TestParseCopilotReviewOverviewUnknownNonApprovingAssessment(t *testing.T) {
	// The exact wording Copilot uses to decline is not fixed, so anything that
	// does not explicitly approve must still count as needing attention —
	// including wording that embeds an approving keyword.
	for _, assessment := range []string{
		"🔴 Changes needed",
		"🟡 Needs work before approval",
		"🟠 Blocked",
		"🔴 Cannot approve",
		"🔴 Can't approve yet",
		"🟡 Won't approve until tests pass",
		"🟡 Doesn't look good",
		"🟡 Not approved",
		"🟡 Approval pending",
	} {
		t.Run(assessment, func(t *testing.T) {
			o := parseCopilotReviewOverview("### " + assessment + "\n\nSomething to fix.")

			if got := o.Assessment; got != assessment {
				t.Errorf("Assessment: got %q, want %q", got, assessment)
			}
			if o.Approving {
				t.Error("Approving: got true, want false")
			}
			if !o.NeedsAttention() {
				t.Error("NeedsAttention: got false, want true")
			}
		})
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
