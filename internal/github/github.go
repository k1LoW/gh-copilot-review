package github

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/api"
)

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
		if u.Login == "copilot" {
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
		if r.User.Login == "copilot" && r.State == "PENDING" {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) RequestCopilotReview(prNumber int) error {
	body, err := json.Marshal(map[string][]string{"reviewers": {"copilot"}})
	if err != nil {
		return err
	}
	err = c.rest.Post(fmt.Sprintf("repos/%s/%s/pulls/%d/requested_reviewers", c.owner, c.repo, prNumber), bytes.NewReader(body), nil)
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
						Author struct {
							Login string
						}
						Comments struct {
							Nodes []struct {
								ID          string `graphql:"id"`
								IsMinimized bool   `graphql:"isMinimized"`
							}
						} `graphql:"comments(first: 100)"`
					}
				} `graphql:"reviews(first: 100)"`
			} `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]any{
		"owner":  c.owner,
		"repo":   c.repo,
		"number": prNumber,
	}

	err := c.gql.Query("CopilotReviewComments", &query, variables)
	if err != nil {
		return 0, fmt.Errorf("failed to query review comments: %w", err)
	}

	var commentIDs []string
	for _, review := range query.Repository.PullRequest.Reviews.Nodes {
		if review.Author.Login != "copilot" {
			continue
		}
		for _, comment := range review.Comments.Nodes {
			if !comment.IsMinimized {
				commentIDs = append(commentIDs, comment.ID)
			}
		}
	}

	minimized := 0
	for _, id := range commentIDs {
		var mutation struct {
			MinimizeComment struct {
				MinimizedComment struct {
					IsMinimized bool
				}
			} `graphql:"minimizeComment(input: {subjectId: $id, classifier: OUTDATED})"`
		}
		vars := map[string]any{
			"id": id,
		}
		if err := c.gql.Mutate("MinimizeComment", &mutation, vars); err != nil {
			fmt.Printf("Warning: failed to minimize comment %s: %v\n", id, err)
			continue
		}
		minimized++
	}

	return minimized, nil
}
