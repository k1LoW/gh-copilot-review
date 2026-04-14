# gh-copilot-review

`gh-copilot-review` is a GitHub CLI (`gh`) extension that requests a [Copilot](https://docs.github.com/en/copilot/using-github-copilot/code-review/using-copilot-code-review) code review on a pull request.

It is more than a simple wrapper around `gh pr edit --add-reviewer @copilot`:

- **Duplicate prevention** — Skips the request if Copilot is already assigned as a reviewer, has a pending review, or has already reviewed the current head commit.
- **Outdated review cleanup** — Automatically hides (minimizes as "outdated") previous Copilot review overviews before requesting a new review.
- **Wait for completion** — Optionally polls until Copilot finishes reviewing with `--wait`.

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
2. **Hides old reviews** — Minimizes previous Copilot review overviews as `OUTDATED` via the GraphQL API.
3. **Checks review status** — If Copilot review is already requested, in progress, or already up to date for the current head commit, exits early with a message.
4. **Requests review** — Adds `@copilot` as a reviewer via `gh pr edit --add-reviewer @copilot`.
5. **Waits for completion** (with `--wait`) — Polls until Copilot finishes reviewing.

```console
$ gh copilot-review 42
Minimized 3 outdated Copilot review comment(s)
Copilot review requested on PR #42
```

```console
$ gh copilot-review 42 --wait
Minimized 1 outdated Copilot review comment(s)
Copilot review requested on PR #42
Waiting for Copilot review... (30s elapsed)
Waiting for Copilot review... (1m0s elapsed)
Copilot review completed on PR #42
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
