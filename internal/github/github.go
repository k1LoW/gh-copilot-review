package github

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cli/go-gh/v2"
	graphql "github.com/cli/shurcooL-graphql"

	"github.com/cli/go-gh/v2/pkg/api"
)

func isCopilotUser(login string) bool {
	return strings.EqualFold(login, "copilot-pull-request-reviewer") ||
		strings.EqualFold(login, "copilot")
}

type Client struct {
	rest  *api.RESTClient
	gql   *api.GraphQLClient
	owner string
	repo  string
}

func NewClient(owner, repo string) (*Client, error) {
	rest, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}
	gql, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL client: %w", err)
	}
	return &Client{rest: rest, gql: gql, owner: owner, repo: repo}, nil
}

func (c *Client) IsCopilotReviewRequested(prNumber int) (bool, error) {
	var result struct {
		Users []struct {
			Login string `json:"login"`
		} `json:"users"`
	}
	err := c.rest.Get(fmt.Sprintf("repos/%s/%s/pulls/%d/requested_reviewers", c.owner, c.repo, prNumber), &result)
	if err != nil {
		return false, fmt.Errorf("failed to get requested reviewers: %w", err)
	}
	for _, u := range result.Users {
		if isCopilotUser(u.Login) {
			return true, nil
		}
	}
	return false, nil
}

// CopilotReviewStatus holds the result of checking Copilot review state.
type CopilotReviewStatus struct {
	Pending bool
	Fresh   bool
	// OutdatedReviewIDs are submitted, non-minimized Copilot reviews tied to a
	// commit other than the current head.
	OutdatedReviewIDs []string
	// HeadReviewIDs are submitted, non-minimized Copilot reviews tied to the
	// current head commit.
	HeadReviewIDs []string
}

// copilotReview is a single review node reduced to the fields that decide how
// it is classified.
type copilotReview struct {
	ID          string
	Login       string
	State       string
	IsMinimized bool
	CommitOid   string
}

// collect folds one review node into the status.
// Reviews are bucketed by the commit they are tied to rather than by whether a
// newer review exists, so a caller that finds an up-to-date review can still
// minimize every older one without touching the up-to-date one.
func (s *CopilotReviewStatus) collect(head string, r copilotReview) {
	if !isCopilotUser(r.Login) || r.IsMinimized {
		return
	}
	if r.State == "PENDING" {
		s.Pending = true
		return
	}
	if r.CommitOid == head {
		s.Fresh = true
		s.HeadReviewIDs = append(s.HeadReviewIDs, r.ID)
		return
	}
	s.OutdatedReviewIDs = append(s.OutdatedReviewIDs, r.ID)
}

// CheckCopilotReviewStatus fetches reviews via GraphQL and determines whether
// Copilot has a pending review, whether it has already reviewed the current
// head commit, and which of its reviews are outdated.
// GraphQL is used instead of REST because the REST reviews endpoint
// does not expose PENDING reviews or the IsMinimized field.
func (c *Client) CheckCopilotReviewStatus(prNumber int) (*CopilotReviewStatus, error) {
	var query struct {
		Repository struct {
			PullRequest struct {
				HeadRefOid string `graphql:"headRefOid"`
				Reviews    struct {
					Nodes []struct {
						ID     string `graphql:"id"`
						Author struct {
							Login string
						}
						State       string
						IsMinimized bool `graphql:"isMinimized"`
						Commit      struct {
							Oid string
						}
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"reviews(first: 100, after: $cursor)"`
			} `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]any{
		"owner":  graphql.String(c.owner),
		"repo":   graphql.String(c.repo),
		"number": graphql.Int(int32(prNumber)), //nolint:gosec // PR numbers won't overflow int32
		"cursor": (*graphql.String)(nil),
	}

	status := &CopilotReviewStatus{}
	for {
		err := c.gql.Query("CopilotReviewStatus", &query, variables)
		if err != nil {
			return nil, fmt.Errorf("failed to query review status: %w", err)
		}

		head := query.Repository.PullRequest.HeadRefOid
		if head == "" {
			return nil, fmt.Errorf("failed to resolve head commit of PR #%d", prNumber)
		}

		for _, r := range query.Repository.PullRequest.Reviews.Nodes {
			status.collect(head, copilotReview{
				ID:          r.ID,
				Login:       r.Author.Login,
				State:       r.State,
				IsMinimized: r.IsMinimized,
				CommitOid:   r.Commit.Oid,
			})
		}

		if !query.Repository.PullRequest.Reviews.PageInfo.HasNextPage {
			break
		}
		cursor := graphql.String(query.Repository.PullRequest.Reviews.PageInfo.EndCursor)
		variables["cursor"] = &cursor
	}

	return status, nil
}

// LatestCopilotReviewOverview returns the parsed review overview of the most
// recently submitted, non-minimized Copilot review tied to the current head
// commit, or nil when there is none. The overview carries Copilot's review
// assessment (e.g. "Not ready to approve") and the findings it reported only
// in the body instead of as inline comments, neither of which is visible
// through the inline review threads.
func (c *Client) LatestCopilotReviewOverview(prNumber int) (*CopilotReviewOverview, error) {
	var query struct {
		Repository struct {
			PullRequest struct {
				HeadRefOid string `graphql:"headRefOid"`
				Reviews    struct {
					Nodes []struct {
						Author struct {
							Login string
						}
						Body        string
						URL         string `graphql:"url"`
						State       string
						IsMinimized bool `graphql:"isMinimized"`
						Commit      struct {
							Oid string
						}
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"reviews(first: 100, after: $cursor)"`
			} `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]any{
		"owner":  graphql.String(c.owner),
		"repo":   graphql.String(c.repo),
		"number": graphql.Int(int32(prNumber)), //nolint:gosec // PR numbers won't overflow int32
		"cursor": (*graphql.String)(nil),
	}

	var overview *CopilotReviewOverview
	for {
		err := c.gql.Query("CopilotReviewOverview", &query, variables)
		if err != nil {
			return nil, fmt.Errorf("failed to query review overview: %w", err)
		}

		head := query.Repository.PullRequest.HeadRefOid
		// Reviews come back oldest first, so the last match wins.
		for _, r := range query.Repository.PullRequest.Reviews.Nodes {
			if !isCopilotUser(r.Author.Login) {
				continue
			}
			if r.IsMinimized || r.State == "PENDING" {
				continue
			}
			if r.Commit.Oid != head {
				continue
			}
			o := parseCopilotReviewOverview(r.Body)
			o.URL = r.URL
			o.State = r.State
			overview = o
		}

		if !query.Repository.PullRequest.Reviews.PageInfo.HasNextPage {
			break
		}
		cursor := graphql.String(query.Repository.PullRequest.Reviews.PageInfo.EndCursor)
		variables["cursor"] = &cursor
	}

	return overview, nil
}

// CountUnresolvedCopilotInlineComments returns the number of unresolved
// inline review threads whose originating comment was authored by Copilot
// on a submitted (non-PENDING), non-minimized review tied to the current
// head commit. Each thread corresponds to one inline comment location, so
// this counts distinct unresolved points raised by Copilot on HEAD.
func (c *Client) CountUnresolvedCopilotInlineComments(prNumber int) (int, error) {
	var query struct {
		Repository struct {
			PullRequest struct {
				HeadRefOid   string `graphql:"headRefOid"`
				ReviewThreads struct {
					Nodes []struct {
						IsResolved bool `graphql:"isResolved"`
						Comments   struct {
							Nodes []struct {
								Author struct {
									Login string
								}
								PullRequestReview struct {
									State       string
									IsMinimized bool `graphql:"isMinimized"`
									Commit      struct {
										Oid string
									}
								} `graphql:"pullRequestReview"`
							}
						} `graphql:"comments(first: 1)"`
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"reviewThreads(first: 100, after: $cursor)"`
			} `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]any{
		"owner":  graphql.String(c.owner),
		"repo":   graphql.String(c.repo),
		"number": graphql.Int(int32(prNumber)), //nolint:gosec // PR numbers won't overflow int32
		"cursor": (*graphql.String)(nil),
	}

	count := 0
	for {
		err := c.gql.Query("CopilotUnresolvedInlineCommentCount", &query, variables)
		if err != nil {
			return 0, fmt.Errorf("failed to query inline review threads: %w", err)
		}

		head := query.Repository.PullRequest.HeadRefOid
		for _, t := range query.Repository.PullRequest.ReviewThreads.Nodes {
			if t.IsResolved {
				continue
			}
			if len(t.Comments.Nodes) == 0 {
				continue
			}
			origin := t.Comments.Nodes[0]
			if !isCopilotUser(origin.Author.Login) {
				continue
			}
			review := origin.PullRequestReview
			if review.IsMinimized {
				continue
			}
			if review.State == "PENDING" {
				continue
			}
			if review.Commit.Oid != head {
				continue
			}
			count++
		}

		if !query.Repository.PullRequest.ReviewThreads.PageInfo.HasNextPage {
			break
		}
		cursor := graphql.String(query.Repository.PullRequest.ReviewThreads.PageInfo.EndCursor)
		variables["cursor"] = &cursor
	}

	return count, nil
}

func (c *Client) RequestCopilotReview(prNumber int) error {
	_, _, err := gh.Exec("pr", "edit", fmt.Sprintf("%d", prNumber),
		"--add-reviewer", "@copilot",
		"--repo", fmt.Sprintf("%s/%s", c.owner, c.repo))
	if err != nil {
		return fmt.Errorf("failed to request Copilot review: %w", err)
	}
	return nil
}

func (c *Client) WaitForReviewCompletion(prNumber int, timeout, interval time.Duration) error {
	sawRequestedOrPending := false

	// Immediate check before entering the polling loop
	done, requested, err := c.isReviewComplete(prNumber, sawRequestedOrPending)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	} else if done {
		return nil
	} else if requested {
		sawRequestedOrPending = true
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	consecutiveErrors := 0

	for {
		select {
		case <-deadline:
			return fmt.Errorf("timed out waiting for Copilot review to complete after %s", timeout)
		case <-ticker.C:
			elapsed := time.Since(start).Truncate(time.Second)
			fmt.Fprintf(os.Stderr, "Waiting for Copilot review... (%s elapsed)\n", elapsed)

			done, requested, err := c.isReviewComplete(prNumber, sawRequestedOrPending)
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors >= 3 {
					return fmt.Errorf("failed to check review status: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				continue
			}

			consecutiveErrors = 0

			if requested {
				sawRequestedOrPending = true
			}

			if done {
				return nil
			}
		}
	}
}

// isReviewComplete checks if the Copilot review has completed.
// It returns (done, sawRequestedOrPending, error).
// The !requested && !pending condition is only used as a completion
// signal when Copilot has been observed as requested/pending at least once,
// to avoid false positives from API propagation delays.
func (c *Client) isReviewComplete(prNumber int, sawRequestedOrPending bool) (bool, bool, error) {
	requested, err := c.IsCopilotReviewRequested(prNumber)
	if err != nil {
		return false, false, err
	}

	status, err := c.CheckCopilotReviewStatus(prNumber)
	if err != nil {
		return false, false, err
	}

	observedActive := requested || status.Pending

	if status.Fresh && !status.Pending {
		return true, observedActive, nil
	}
	if sawRequestedOrPending && !requested && !status.Pending {
		return true, observedActive, nil
	}

	return false, observedActive, nil
}

// MinimizeReviews minimizes the given reviews as OUTDATED and returns how many
// were minimized. A review that cannot be minimized is reported as a warning
// and skipped, so one rejected subject does not abort the rest.
func (c *Client) MinimizeReviews(ids []string) int {
	minimized := 0
	for _, id := range ids {
		var mutation struct {
			MinimizeComment struct {
				MinimizedComment struct {
					IsMinimized bool
				}
			} `graphql:"minimizeComment(input: {subjectId: $id, classifier: OUTDATED})"`
		}
		vars := map[string]any{
			"id": graphql.ID(id),
		}
		if err := c.gql.Mutate("MinimizeComment", &mutation, vars); err != nil {
			fmt.Printf("Warning: failed to minimize comment %s: %v\n", id, err)
			continue
		}
		minimized++
	}

	return minimized
}
