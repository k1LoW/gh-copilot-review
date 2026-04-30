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
}

// CheckCopilotReviewStatus fetches reviews via GraphQL and determines both
// whether Copilot has a pending review and whether it has already
// reviewed the current head commit.
// GraphQL is used instead of REST because the REST reviews endpoint
// does not expose PENDING reviews or the IsMinimized field.
func (c *Client) CheckCopilotReviewStatus(prNumber int) (*CopilotReviewStatus, error) {
	var query struct {
		Repository struct {
			PullRequest struct {
				HeadRefOid string `graphql:"headRefOid"`
				Reviews    struct {
					Nodes []struct {
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
	var headRefOid string
	for {
		err := c.gql.Query("CopilotReviewStatus", &query, variables)
		if err != nil {
			return nil, fmt.Errorf("failed to query review status: %w", err)
		}

		headRefOid = query.Repository.PullRequest.HeadRefOid

		for _, r := range query.Repository.PullRequest.Reviews.Nodes {
			if !isCopilotUser(r.Author.Login) {
				continue
			}
			if r.IsMinimized {
				continue
			}
			if r.State == "PENDING" {
				status.Pending = true
			}
			if r.State != "PENDING" && r.Commit.Oid == headRefOid {
				status.Fresh = true
			}
		}

		if (status.Pending && status.Fresh) || !query.Repository.PullRequest.Reviews.PageInfo.HasNextPage {
			break
		}
		cursor := graphql.String(query.Repository.PullRequest.Reviews.PageInfo.EndCursor)
		variables["cursor"] = &cursor
	}

	return status, nil
}

// CountFreshCopilotInlineComments returns the number of inline review
// comments authored by Copilot on submitted (non-PENDING), non-minimized
// reviews tied to the current head commit.
func (c *Client) CountFreshCopilotInlineComments(prNumber int) (int, error) {
	var query struct {
		Repository struct {
			PullRequest struct {
				HeadRefOid string `graphql:"headRefOid"`
				Reviews    struct {
					Nodes []struct {
						Author struct {
							Login string
						}
						State       string
						IsMinimized bool `graphql:"isMinimized"`
						Commit      struct {
							Oid string
						}
						Comments struct {
							TotalCount int
						} `graphql:"comments(first: 1)"`
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

	count := 0
	for {
		err := c.gql.Query("CopilotInlineCommentCount", &query, variables)
		if err != nil {
			return 0, fmt.Errorf("failed to query inline review comments: %w", err)
		}

		head := query.Repository.PullRequest.HeadRefOid
		for _, r := range query.Repository.PullRequest.Reviews.Nodes {
			if !isCopilotUser(r.Author.Login) {
				continue
			}
			if r.IsMinimized {
				continue
			}
			if r.State == "PENDING" {
				continue
			}
			if r.Commit.Oid != head {
				continue
			}
			count += r.Comments.TotalCount
		}

		if !query.Repository.PullRequest.Reviews.PageInfo.HasNextPage {
			break
		}
		cursor := graphql.String(query.Repository.PullRequest.Reviews.PageInfo.EndCursor)
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

func (c *Client) MinimizeCopilotComments(prNumber int) (int, error) {
	var query struct {
		Repository struct {
			PullRequest struct {
				Reviews struct {
					Nodes []struct {
						ID          string `graphql:"id"`
						IsMinimized bool   `graphql:"isMinimized"`
						Author      struct {
							Login string
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

	var subjectIDs []string
	for {
		err := c.gql.Query("CopilotReviewComments", &query, variables)
		if err != nil {
			return 0, fmt.Errorf("failed to query review comments: %w", err)
		}

		for _, review := range query.Repository.PullRequest.Reviews.Nodes {
			if !isCopilotUser(review.Author.Login) {
				continue
			}
			if !review.IsMinimized {
				subjectIDs = append(subjectIDs, review.ID)
			}
		}

		if !query.Repository.PullRequest.Reviews.PageInfo.HasNextPage {
			break
		}
		cursor := graphql.String(query.Repository.PullRequest.Reviews.PageInfo.EndCursor)
		variables["cursor"] = &cursor
	}

	minimized := 0
	for _, id := range subjectIDs {
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

	return minimized, nil
}
