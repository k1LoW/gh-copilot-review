---
name: request-copilot-review
description: >
  Requests a Copilot code review on a pull request using gh-copilot-review and waits until it completes.
  Resolves the target PR (from argument or current branch), runs the request, and reports the outcome
  including whether Copilot left any new inline review comments.
  Use when the user wants to ask Copilot to review a PR or re-request a review.
compatibility: Requires gh CLI and gh-copilot-review extension (gh extension install k1LoW/gh-copilot-review)
---

# Request Copilot Code Review

## Phase 1: Resolve the Target PR

1. If the user passed an argument (PR number or URL), use it as `<arg>`. Otherwise, default to the PR for the current branch.
2. Confirm the target PR before requesting a review:
   - With `<arg>`: `gh pr view <arg> --json number,title,url,headRefName`
   - Without `<arg>`: `gh pr view --json number,title,url,headRefName`
3. If no PR is found (e.g., current branch has no open PR), stop and tell the user. Do not proceed.
4. Show the resolved PR (`#<number> <title>`) and ask the user to confirm before proceeding when the PR was auto-detected from the current branch. When the user explicitly passed `<arg>`, skip the confirmation.

## Phase 2: Request the Review

Run `gh copilot-review` with `--wait` so the command polls until Copilot finishes. Choose flags based on user intent:

- **Default**: `gh copilot-review <arg> --wait`
- **User explicitly asks to force a re-request** (e.g., "force", "ignore the existing review"): add `--force`
- **User specifies timeout / interval**: pass through as `--wait-timeout` / `--wait-interval` (formats: `30sec`, `5min`, `1h`)

Notes:

- The command may take several minutes. Use a generous Bash timeout (e.g., 15 minutes when `--wait-timeout` is the default `10min`; longer if the user raised it).
- Without `<arg>`, omit it from the command. `gh copilot-review` auto-detects the PR for the current branch the same way.
- The command itself handles duplicate prevention. With `--wait`, in-progress reviews are polled to completion rather than skipped, so the only early-exit case to handle is when the current head commit already has a fresh Copilot review. In that case the command prints `Copilot review is already up to date for the current head commit` and exits. Surface that message verbatim and ask the user whether to re-run with `--force`.

## Phase 3: Report the Outcome

Parse the command output and present a short summary:

```
## Copilot Review for PR #<number> (<title>)

- Status: <Completed | Skipped (already reviewed) | Skipped (in progress) | Timed out | Failed>
- Outdated reviews minimized: <n> (omit if 0)
- Inline review comments: <n new | none | unknown>
- URL: <pr url>
```

How to fill in **Inline review comments** for a **Completed** review:

- If the output contains `Copilot left N new inline review comment(s)`, report `N new`.
- If the output contains `No new inline review comments from Copilot`, report `none`.
- If neither line is present (e.g., `WaitForReviewCompletion` returned via the propagation fallback because Copilot left without leaving a fresh review), report `unknown` and note that no fresh Copilot review for the current head was detected.

For other statuses, omit the **Inline review comments** line.

Then, depending on status:

- **Completed**: Done. The summary above is the final report.
- **Skipped (already reviewed / in progress)**: Explain why (per the command output) and offer to re-run with `--force` if the user wants to override.
- **Timed out**: Tell the user Copilot did not finish within the timeout. Offer to re-run with a larger `--wait-timeout`, or to check the PR manually.
- **Failed**: Surface the error verbatim. Do not retry automatically.

## Rules

- Never request a Copilot review on a PR the user has not confirmed when it was auto-detected from the current branch.
- Do not pass `--force` unless the user explicitly asks to override the pre-conditions.
- Do not push, merge, or modify code as part of this skill. It only requests a review and reports the outcome.
- Prefer `gh` commands for GitHub data; do not call the REST/GraphQL API directly when an equivalent `gh` command exists.
- If `gh-copilot-review` is not installed, instruct the user to run `gh extension install k1LoW/gh-copilot-review` and stop.
