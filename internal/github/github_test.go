package github

import (
	"slices"
	"testing"
)

const testHead = "aaaaaaaa"

func TestCopilotReviewStatusCollect(t *testing.T) {
	tests := []struct {
		name     string
		reviews  []copilotReview
		pending  bool
		fresh    bool
		outdated []string
		head     []string
	}{
		{
			name: "a review on an older commit is outdated",
			reviews: []copilotReview{
				{ID: "r1", Login: "copilot-pull-request-reviewer", State: "COMMENTED", CommitOid: "bbbbbbbb"},
			},
			outdated: []string{"r1"},
		},
		{
			name: "a review on the head commit is fresh and is not outdated",
			reviews: []copilotReview{
				{ID: "r1", Login: "copilot-pull-request-reviewer", State: "COMMENTED", CommitOid: testHead},
			},
			fresh: true,
			head:  []string{"r1"},
		},
		{
			name: "an up-to-date review does not shield older ones from cleanup",
			reviews: []copilotReview{
				{ID: "r1", Login: "copilot-pull-request-reviewer", State: "COMMENTED", CommitOid: "bbbbbbbb"},
				{ID: "r2", Login: "copilot-pull-request-reviewer", State: "COMMENTED", CommitOid: "cccccccc"},
				{ID: "r3", Login: "copilot-pull-request-reviewer", State: "COMMENTED", CommitOid: testHead},
			},
			fresh:    true,
			outdated: []string{"r1", "r2"},
			head:     []string{"r3"},
		},
		{
			name: "an already minimized review is not minimized again",
			reviews: []copilotReview{
				{ID: "r1", Login: "copilot-pull-request-reviewer", State: "COMMENTED", CommitOid: "bbbbbbbb", IsMinimized: true},
			},
		},
		{
			name: "a pending review is reported but never minimized",
			reviews: []copilotReview{
				{ID: "r1", Login: "copilot-pull-request-reviewer", State: "PENDING", CommitOid: "bbbbbbbb"},
			},
			pending: true,
		},
		{
			name: "reviews by other authors are ignored",
			reviews: []copilotReview{
				{ID: "r1", Login: "k1LoW", State: "COMMENTED", CommitOid: "bbbbbbbb"},
				{ID: "r2", Login: "k1LoW", State: "APPROVED", CommitOid: testHead},
			},
		},
		{
			name: "Copilot is matched case-insensitively under either login",
			reviews: []copilotReview{
				{ID: "r1", Login: "Copilot", State: "COMMENTED", CommitOid: "bbbbbbbb"},
				{ID: "r2", Login: "Copilot-Pull-Request-Reviewer", State: "COMMENTED", CommitOid: "bbbbbbbb"},
			},
			outdated: []string{"r1", "r2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &CopilotReviewStatus{}
			for _, r := range tt.reviews {
				s.collect(testHead, r)
			}

			if got, want := s.Pending, tt.pending; got != want {
				t.Errorf("Pending: got %v, want %v", got, want)
			}
			if got, want := s.Fresh, tt.fresh; got != want {
				t.Errorf("Fresh: got %v, want %v", got, want)
			}
			if got, want := s.OutdatedReviewIDs, tt.outdated; !slices.Equal(got, want) {
				t.Errorf("OutdatedReviewIDs: got %v, want %v", got, want)
			}
			if got, want := s.HeadReviewIDs, tt.head; !slices.Equal(got, want) {
				t.Errorf("HeadReviewIDs: got %v, want %v", got, want)
			}
		})
	}
}
