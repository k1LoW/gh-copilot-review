## Pull request overview

Copilot reviewed 8 out of 8 changed files in this pull request and generated no new comments.

<details>
<summary>Suppressed comments (1)</summary>

**internal/parser/overview.go:36**
* NeedsAttention() only flags negative assessments by searching for one exact substring, so other non-approving assessment headings would be treated as clean and the CLI would omit the "did not approve" signal. Consider treating any non-empty assessment that is not explicitly approving as needing attention.
```
	return o.NotReadyToApprove || len(o.SuppressedComments) > 0
}
```
</details>
