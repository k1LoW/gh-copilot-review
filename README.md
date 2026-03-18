# gh-copilot-review

`gh-copilot-review` is a GitHub CLI (`gh`) extension that requests a [Copilot](https://docs.github.com/en/copilot/using-github-copilot/code-review/using-copilot-code-review) code review on a pull request.

It is more than a simple wrapper around `gh pr edit --add-reviewer @copilot`:

- **Duplicate prevention** — Skips the request if Copilot is already assigned as a reviewer or has a pending review.
- **Outdated review cleanup** — Automatically hides (minimizes as "outdated") previous Copilot review overviews before requesting a new review.

## Usage

```bash
# Request Copilot review on the PR for the current branch
$ gh copilot-review

# Specify a PR number
$ gh copilot-review 123

# Specify a PR URL
$ gh copilot-review https://github.com/owner/repo/pull/123
```

### What it does

1. **Resolves the PR** — Uses the argument (number or URL), or auto-detects from the current branch via `gh pr view`.
2. **Checks review status** — If Copilot review is already requested or in progress, exits early with a message.
3. **Hides old reviews** — Minimizes previous Copilot review overviews as `OUTDATED` via the GraphQL API.
4. **Requests review** — Adds `@copilot` as a reviewer via `gh pr edit --add-reviewer @copilot`.

```console
$ gh copilot-review 42
Minimized 3 outdated Copilot review comment(s)
Copilot review requested on PR #42
```

## Install

```bash
$ gh extension install k1LoW/gh-copilot-review
```
