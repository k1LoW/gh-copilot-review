### 🟡 Not ready to approve

The new cancel RPC can incorrectly downgrade internal errors from `GetExecution` to `NotFound`, masking real failures and returning the wrong status code.

*Once you've addressed the issues Copilot identified, you can request another Copilot review.*

*This review doesn't count toward merge requirements. [Sign up for the private preview](https://forms.example.com/r/xxxxxxxxxx) to control whether Copilot approvals count.*

<details>
<summary>Pull request overview</summary>

This PR adds support for canceling asynchronous executions by `execution_id`, plus reconciliation logic to finalize CANCELING rows to CANCELED.

**Changes:**
- Introduces a private RPC `CancelJobExecution` with status-matrix behavior.
- Extends reconciliation to process CANCELING executions without an age cutoff.
</details>

<details>
<summary>File summaries</summary>

| File | Description |
| ---- | ----------- |
| internal/store/store.go | Extends the store interface to support status-only updates. |
| internal/service/cancel.go | Adds the private `CancelJobExecution` implementation. |
</details>

<details>
<summary>Review details</summary>

### Files not reviewed (2)

* **internal/store/mock/mock_store.go**: Generated file
* **internal/runner/mock/mock_runner.go**: Generated file

- **Files reviewed:** 6/8 changed files
- **Comments generated:** 1
- **Review effort level:** Lite
</details>

We're testing this review assessment. Please use 👍 or 👎 to tell us if it's correct.
