package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// CheckCopilotReviewStatus fetches reviews once and determines both
// whether Copilot has a pending review and whether it has already
// reviewed the current head commit.
func (c *Client) CheckCopilotReviewStatus(prNumber int) (*CopilotReviewStatus, error) {
	var pr struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	err := c.rest.Get(fmt.Sprintf("repos/%s/%s/pulls/%d", c.owner, c.repo, prNumber), &pr)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	type review struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		State    string `json:"state"`
		CommitID string `json:"commit_id"`
	}
	stdout, _, err := gh.Exec("api", "--paginate", "--jq", ".[]",
		fmt.Sprintf("repos/%s/%s/pulls/%d/reviews?per_page=100", c.owner, c.repo, prNumber))
	if err != nil {
		return nil, fmt.Errorf("failed to get reviews: %w", err)
	}
	var reviews []review
	dec := json.NewDecoder(strings.NewReader(stdout.String()))
	for {
		var r review
		if err := dec.Decode(&r); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to parse reviews: %w", err)
		}
		reviews = append(reviews, r)
	}

	status := &CopilotReviewStatus{}
	for _, r := range reviews {
		if !isCopilotUser(r.User.Login) {
			continue
		}
		if r.State == "PENDING" {
			status.Pending = true
		}
		if r.CommitID == pr.Head.SHA {
			status.Fresh = true
		}
	}

	return status, nil
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

	if status.Fresh {
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
				} `graphql:"reviews(first: 100)"`
			} `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]any{
		"owner":  graphql.String(c.owner),
		"repo":   graphql.String(c.repo),
		"number": graphql.Int(int32(prNumber)), //nolint:gosec // PR numbers won't overflow int32
	}

	err := c.gql.Query("CopilotReviewComments", &query, variables)
	if err != nil {
		return 0, fmt.Errorf("failed to query review comments: %w", err)
	}

	var subjectIDs []string
	for _, review := range query.Repository.PullRequest.Reviews.Nodes {
		if !isCopilotUser(review.Author.Login) {
			continue
		}
		if !review.IsMinimized {
			subjectIDs = append(subjectIDs, review.ID)
		}
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
