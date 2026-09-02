package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"slices"
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
	forceFlag    bool
	waitFlag     bool
	waitTimeout  string
	waitInterval string
)

func init() {
	rootCmd.Flags().BoolVar(&forceFlag, "force", false, "Force request Copilot review, ignoring all pre-conditions")
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

	status, err := client.CheckCopilotReviewStatus(prNumber)
	if err != nil {
		return err
	}

	// Minimizing runs before any early return because a repository that reviews
	// on push already has a fresh review by the time this command runs, and
	// those runs would otherwise leave every earlier review expanded forever.
	targets := status.OutdatedReviewIDs
	if forceFlag {
		// A forced re-request supersedes the review on the current head too.
		targets = append(slices.Clone(targets), status.HeadReviewIDs...)
	}
	if minimized := client.MinimizeReviews(targets); minimized > 0 {
		fmt.Printf("Minimized %d Copilot review(s)\n", minimized)
	}

	if forceFlag {
		if err := client.RequestCopilotReview(prNumber); err != nil {
			return err
		}
		fmt.Printf("Copilot review force-requested on PR #%d\n", prNumber)
	} else {
		requested, err := client.IsCopilotReviewRequested(prNumber)
		if err != nil {
			return err
		}

		if status.Fresh {
			fmt.Println("Copilot review is already up to date for the current head commit")
			if waitFlag {
				if err := reportCopilotFindings(client, prNumber); err != nil {
					return err
				}
			}
			return nil
		}
		if status.Pending && !waitFlag {
			fmt.Println("Copilot review is in progress")
			return nil
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

		// Only report inline comment count when a fresh Copilot review for
		// the current head commit is actually visible. WaitForReviewCompletion
		// can return via the propagation fallback (Copilot left without
		// reviewing), in which case the count is meaningless.
		postStatus, err := client.CheckCopilotReviewStatus(prNumber)
		if err != nil {
			return err
		}
		if postStatus.Fresh {
			if err := reportCopilotFindings(client, prNumber); err != nil {
				return err
			}
		}
	}

	return nil
}

func reportCopilotFindings(client *ghclient.Client, prNumber int) error {
	overview, err := client.LatestCopilotReviewOverview(prNumber)
	if err != nil {
		return err
	}
	if overview != nil {
		if overview.Assessment != "" {
			fmt.Printf("Copilot review assessment: %s\n", overview.Assessment)
			if overview.Summary != "" {
				fmt.Printf("  %s\n", overview.Summary)
			}
		}
		if overview.URL != "" {
			fmt.Printf("Review overview: %s\n", overview.URL)
		}
		if n := len(overview.SuppressedComments); n > 0 {
			fmt.Printf("Copilot has %d suppressed review comment(s) (reported in the review overview, not as inline comments):\n", n)
			for _, sc := range overview.SuppressedComments {
				fmt.Printf("  - %s:%d\n", sc.Path, sc.Line)
				for line := range strings.SplitSeq(sc.Body, "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
		}
	}

	count, err := client.CountUnresolvedCopilotInlineComments(prNumber)
	if err != nil {
		return err
	}
	if count == 0 {
		fmt.Println("No unresolved inline review comments from Copilot")
	} else {
		fmt.Printf("Copilot has %d unresolved inline review comment(s)\n", count)
	}

	if count == 0 && overview.NeedsAttention() {
		fmt.Println("Copilot did not approve this pull request; address the review overview above")
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
