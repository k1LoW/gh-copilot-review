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
	"github.com/k1LoW/duration"
	"github.com/spf13/cobra"

	ghclient "github.com/k1LoW/gh-copilot-review/internal/github"
	"github.com/k1LoW/gh-copilot-review/version"
)

var (
	waitFlag     bool
	waitTimeout  string
	waitInterval string
)

func init() {
	rootCmd.Flags().BoolVar(&waitFlag, "wait", false, "Wait for Copilot review to complete")
	rootCmd.Flags().StringVar(&waitTimeout, "wait-timeout", "10min", "Timeout for waiting (e.g. 10min, 1h, 30sec)")
	rootCmd.Flags().StringVar(&waitInterval, "wait-interval", "30sec", "Polling interval for waiting (e.g. 10sec, 30sec, 1min)")
}

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

	requested, err := client.IsCopilotReviewRequested(prNumber)
	if err != nil {
		return err
	}

	status, err := client.CheckCopilotReviewStatus(prNumber)
	if err != nil {
		return err
	}
	if status.Fresh {
		fmt.Println("Copilot review is already up to date for the current head commit")
		return nil
	}
	if status.Pending && !waitFlag {
		fmt.Println("Copilot review is in progress")
		return nil
	}

	minimized, err := client.MinimizeCopilotComments(prNumber)
	if err != nil {
		return err
	}
	if minimized > 0 {
		fmt.Printf("Minimized %d outdated Copilot review comment(s)\n", minimized)
	}

	if !status.Pending {
		if requested && !status.Fresh {
			// Copilot is listed as a requested reviewer but has no pending or
			// fresh review. This is a stale request from a previous review
			// cycle, so re-request to trigger a review for the current HEAD.
			if err := client.RequestCopilotReview(prNumber); err != nil {
				return err
			}
			fmt.Printf("Copilot review re-requested on PR #%d (stale request detected)\n", prNumber)
		} else if !requested {
			if err := client.RequestCopilotReview(prNumber); err != nil {
				return err
			}
			fmt.Printf("Copilot review requested on PR #%d\n", prNumber)
		}
	}

	if waitFlag {
		timeout, err := duration.Parse(waitTimeout)
		if err != nil {
			return fmt.Errorf("invalid --wait-timeout value: %w", err)
		}
		if timeout <= 0 {
			return fmt.Errorf("invalid --wait-timeout value: must be greater than 0")
		}
		interval, err := duration.Parse(waitInterval)
		if err != nil {
			return fmt.Errorf("invalid --wait-interval value: %w", err)
		}
		if interval <= 0 {
			return fmt.Errorf("invalid --wait-interval value: must be greater than 0")
		}
		if interval > timeout {
			return fmt.Errorf("invalid wait settings: --wait-interval (%s) must be less than or equal to --wait-timeout (%s)", interval, timeout)
		}
		if err := client.WaitForReviewCompletion(prNumber, timeout, interval); err != nil {
			return err
		}
		fmt.Printf("Copilot review completed on PR #%d\n", prNumber)
	}

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
