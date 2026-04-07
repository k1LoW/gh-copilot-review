package github

import (
	"fmt"
	"strings"

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

func (c *Client) HasCopilotPendingReview(prNumber int) (bool, error) {
	var reviews []struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		State string `json:"state"`
	}
	err := c.rest.Get(fmt.Sprintf("repos/%s/%s/pulls/%d/reviews", c.owner, c.repo, prNumber), &reviews)
	if err != nil {
		return false, fmt.Errorf("failed to get reviews: %w", err)
	}
	for _, r := range reviews {
		if isCopilotUser(r.User.Login) && r.State == "PENDING" {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) IsCopilotReviewFresh(prNumber int) (bool, error) {
	// Get the PR head SHA
	var pr struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	err := c.rest.Get(fmt.Sprintf("repos/%s/%s/pulls/%d", c.owner, c.repo, prNumber), &pr)
	if err != nil {
		return false, fmt.Errorf("failed to get PR: %w", err)
	}

	// Check if any Copilot review was submitted on the current head commit
	var reviews []struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		CommitID string `json:"commit_id"`
	}
	err = c.rest.Get(fmt.Sprintf("repos/%s/%s/pulls/%d/reviews?per_page=100", c.owner, c.repo, prNumber), &reviews)
	if err != nil {
		return false, fmt.Errorf("failed to get reviews: %w", err)
	}

	for _, r := range reviews {
		if isCopilotUser(r.User.Login) && r.CommitID == pr.Head.SHA {
			return true, nil
		}
	}

	return false, nil
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
