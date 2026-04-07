package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"

	ghclient "github.com/k1LoW/gh-copilot-review/internal/github"
	"github.com/k1LoW/gh-copilot-review/version"
)

var rootCmd = &cobra.Command{
	Use:   "gh-copilot-review [<number> | <url>]",
	Short: "Request a Copilot review on a pull request",
	Args:  cobra.MaximumNArgs(1),
	RunE:         run,
	SilenceUsage: true,
	Version:      version.Version,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	owner, repoName, prNumber, err := resolveTarget(args)
	if err != nil {
		return err
	}

	client, err := ghclient.NewClient(owner, repoName)
	if err != nil {
		return err
	}

	minimized, err := client.MinimizeCopilotComments(prNumber)
	if err != nil {
		return err
	}
	if minimized > 0 {
		fmt.Printf("Minimized %d outdated Copilot review comment(s)\n", minimized)
	}

	requested, err := client.IsCopilotReviewRequested(prNumber)
	if err != nil {
		return err
	}
	if requested {
		fmt.Println("Copilot review is already requested")
		return nil
	}

	pending, err := client.HasCopilotPendingReview(prNumber)
	if err != nil {
		return err
	}
	if pending {
		fmt.Println("Copilot review is in progress")
		return nil
	}

	fresh, err := client.IsCopilotReviewFresh(prNumber)
	if err != nil {
		return err
	}
	if fresh {
		fmt.Println("Copilot review is already up to date (newer than the last commit)")
		return nil
	}

	if err := client.RequestCopilotReview(prNumber); err != nil {
		return err
	}
	fmt.Printf("Copilot review requested on PR #%d\n", prNumber)

	return nil
}

// resolveTarget returns owner, repo, and PR number from args.
func resolveTarget(args []string) (string, string, int, error) {
	if len(args) == 0 {
		return detectCurrentPR()
	}

	arg := args[0]

	// Try as a number — requires current repo context
	if n, err := strconv.Atoi(arg); err == nil {
		repo, err := repository.Current()
		if err != nil {
			return "", "", 0, fmt.Errorf("failed to determine repository: %w", err)
		}
		return repo.Owner, repo.Name, n, nil
	}

	// Try as a URL containing /{owner}/{repo}/pull/{number}
	if u, err := url.Parse(arg); err == nil && u.Host != "" {
		parts := strings.Split(path.Clean(u.Path), "/")
		// parts: ["", owner, repo, "pull", number, ...]
		for i, p := range parts {
			if p == "pull" && i+1 < len(parts) && i >= 3 {
				if n, err := strconv.Atoi(parts[i+1]); err == nil {
					return parts[i-2], parts[i-1], n, nil
				}
			}
		}
	}

	return "", "", 0, fmt.Errorf("invalid PR number or URL: %s", arg)
}

func detectCurrentPR() (string, string, int, error) {
	stdout, _, err := gh.Exec("pr", "view", "--json", "number")
	if err != nil {
		return "", "", 0, fmt.Errorf("no PR found for current branch: %w", err)
	}
	var result struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", "", 0, fmt.Errorf("failed to parse PR info: %w", err)
	}
	if result.Number == 0 {
		return "", "", 0, fmt.Errorf("no PR found for current branch")
	}

	repo, err := repository.Current()
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to determine repository: %w", err)
	}
	return repo.Owner, repo.Name, result.Number, nil
}
