# gh-copilot-review

`gh-copilot-review` is a GitHub CLI (`gh`) extension that requests a [Copilot](https://docs.github.com/en/copilot/using-github-copilot/code-review/using-copilot-code-review) code review on a pull request.

It is more than a simple wrapper around `gh pr edit --add-reviewer @copilot`:

- **Duplicate prevention** — Skips the request if Copilot is already assigned as a reviewer, has a pending review, or has already reviewed the current head commit.
- **Outdated review cleanup** — Automatically hides (minimizes as "outdated") every Copilot review overview that is not tied to the current head commit. This runs on every invocation, including the ones that exit early, so reviews do not pile up on repositories where Copilot reviews on push.
- **Wait for completion** — Optionally polls until Copilot finishes reviewing with `--wait`.
- **Review overview findings** — Reports Copilot's review assessment (e.g. `🟡 Not ready to approve`) and the findings it lists only in the review overview as "suppressed comments", not just the unresolved inline comments.

## Usage

```bash
# Request Copilot review on the PR for the current branch
$ gh copilot-review

# Specify a PR number
$ gh copilot-review 123

# Specify a PR URL
$ gh copilot-review https://github.com/owner/repo/pull/123

# Request and wait for Copilot review to complete
$ gh copilot-review --wait

# Customize timeout and polling interval
$ gh copilot-review --wait --wait-timeout 5min --wait-interval 10sec

# Force request, ignoring all pre-conditions
$ gh copilot-review --force
```

### What it does

1. **Resolves the PR** — Uses the argument (number or URL), or auto-detects from the current branch via `gh pr view`.
2. **Hides old reviews** — Minimizes every Copilot review overview tied to a commit other than the current head as `OUTDATED` via the GraphQL API. The review on the current head is kept, unless `--force` is given.
3. **Checks review status** — If Copilot review is already requested, in progress, or already up to date for the current head commit, exits early with a message.
4. **Requests review** — Adds `@copilot` as a reviewer via `gh pr edit --add-reviewer @copilot`.
5. **Waits for completion** (with `--wait`) — Polls until Copilot finishes reviewing.

```console
$ gh copilot-review 42
Minimized 3 outdated Copilot review(s)
Copilot review requested on PR #42
```

```console
$ gh copilot-review 42 --wait
Minimized 1 outdated Copilot review(s)
Copilot review requested on PR #42
Waiting for Copilot review... (30s elapsed)
Waiting for Copilot review... (1m0s elapsed)
Copilot review completed on PR #42
Copilot review assessment: 🟡 Not ready to approve
  There are a few correctness issues that should be addressed before approval.
Review overview: https://github.com/owner/repo/pull/42#pullrequestreview-1234567890
Copilot has 1 suppressed review comment(s) (reported in the review overview, not as inline comments):
  - internal/server/handler.go:207
    This switch has no default case. ...
No unresolved inline review comments from Copilot
Copilot did not approve this pull request; address the review overview above
```

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `--force` | `false` | Force request Copilot review, ignoring all pre-conditions |
| `--wait` | `false` | Wait for Copilot review to complete |
| `--wait-timeout` | `10min` | Timeout for waiting (e.g. `10min`, `1h`, `30sec`) |
| `--wait-interval` | `30sec` | Polling interval for waiting (e.g. `10sec`, `30sec`, `1min`) |

## Install

```bash
$ gh extension install k1LoW/gh-copilot-review
```

## Agent Skill

This repository includes an example [Agent Skill](https://agentskills.io/), [**request-copilot-review**](skills/request-copilot-review/SKILL.md). It uses `gh copilot-review --wait` to request a Copilot review on a pull request, waits until it completes, and reports how many unresolved Copilot inline review comments remain on the current head commit.

You can install it via [skills.sh](https://skills.sh/):

```bash
$ npx skills add k1LoW/gh-copilot-review
```

Or via [`gh skill`](https://cli.github.com/manual/gh_skill) (preview):

```bash
$ gh skill install k1LoW/gh-copilot-review request-copilot-review
```

Or manually copy [`skills/request-copilot-review/SKILL.md`](skills/request-copilot-review/SKILL.md) to a location recognized by your agent (e.g., `.claude/skills/` for [Claude Code](https://docs.anthropic.com/en/docs/claude-code), `.github/skills/` for [GitHub Copilot Coding Agent](https://docs.github.com/en/copilot/using-github-copilot/using-copilot-coding-agent), or wherever your agent discovers skills).
