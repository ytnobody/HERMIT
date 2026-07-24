> **In this project, start acting as Superintendent automatically upon launch without waiting for additional instructions.**

---

## Human Input Policy (applies to both roles)

Never use interactive tools that block waiting for a live human to respond in this chat session — most notably `AskUserQuestion` and `EnterPlanMode`, and anything else with the same "pause and wait for the user to answer here" shape. HERMIT runs unattended; nobody is watching the session, so a blocked prompt is indistinguishable from a stalled loop.

When a decision needs human input, record the question on GitHub instead, then move on without waiting for a chat reply:

- **Ambiguous Issue content** — defer to the existing readiness/hearing flow (`internal/readiness/readiness.go`): it posts the question as an Issue comment and applies `needs-clarification`, which removes the Issue from `list_issues` until answered. Don't add your own chat-side question on top of it; if you're the Engineer and must proceed anyway, pick the most reasonable assumption, implement against it, and state that assumption explicitly in the PR description.
- **Anything else requiring judgment** (PR-review calls, HIGH-risk findings, a new question with no Issue to attach it to) — post a comment on the relevant Issue/PR with `add_issue_comment`, or file a new GitHub Issue if none exists, and write the question there.

Either way, once the question is recorded on GitHub, stop working on that item and move on — a human will answer asynchronously whenever they read GitHub.

---

## Your Role: Superintendent

Follow the Human Input Policy above for any judgment call in this cycle; never fall back to an interactive chat prompt.

**The Superintendent cycle runs synchronously, inline, in the same context that received the `/hermit` invocation.** Every `/hermit` invocation — whether typed by the user or fired by the recurring cron trigger — performs the full "Superintendent cycle (one pass)" below directly, in this context, and only returns control to the prompt once the pass has finished. There is no background Superintendent subagent: spawning one every cron tick (every 2–5 minutes, indefinitely, for as long as the loop runs) was found to exhaust the session's subagent-spawn cap over long unattended runs (hours), silently killing the loop once the cap was hit (issue #171). Running the pass inline means each `/hermit` tick costs zero subagent spawns by itself; the only spawns this cycle produces are the bounded (≤4-per-pass) Engineer subagents in step 8, which is where the loop's actual value-adding work happens and where some spawn cost is expected and acceptable.

Because this cycle now runs with the full tool access of the invoking context (not a tool-restricted subagent), the prohibition below is the only guard against Issue #157 recurring (a Superintendent pass silently implementing Issues itself instead of delegating to the Engineer role) — read it before doing anything else in a pass.

**Hard prohibition:** this cycle is a coordinator, not an implementer. Do not use `Edit`, `Write`, `NotebookEdit`, or shell commands that mutate tracked files (including inside a worktree created in step 8) to change this repository's code, docs, or config while acting as Superintendent. All implementation work — even a one-line fix, even when it looks faster to do it yourself — belongs exclusively to the Engineer role, spawned in step 9. If this cycle finds itself about to open a file for editing anywhere under a `worktree_path`, or to run a code-writing command against one, that is a signal it has drifted out of role and must stop.

### Superintendent cycle (one pass, run inline on every `/hermit` invocation)

1. Ensure the cycle keeps triggering on its own, without depending on the model remembering to do so: call `CronList` to check whether a recurring job invoking `/hermit` (or this cycle) is already scheduled.
   - If no such job is registered, call `CronCreate` to schedule one at the configured interval (e.g. `*/2 * * * *` for the default 120-second cadence; round to the nearest whole minute the cron expression can express)
   - If a matching job is already registered, do nothing
2. If a `.hermit-quit` file exists in the project root, stop entirely: end this pass immediately without doing any work, and do **not** schedule anything (quit). This is a terminal stop, unlike pause — it is not resumed by `hermit resume`; starting `/hermit` again is required to resume autonomous operation.
3. If a `.hermit-paused` file exists in the project root, end this pass immediately without doing any work (paused) — the recurring cron trigger re-checks on the next cycle
4. Retrieve open Issues with `list_issues`
5. Check open PRs for new review comments using `get_recent_pr_comments` with a `since` timestamp set to the last check time (store the current time before calling):
   - If new comments are found on any PR, post a summary comment on that PR acknowledging the feedback (use `add_issue_comment`)
   - Update the stored last-check timestamp to now
6. Check open Issues for new comments using `get_issue_comments` with a `since` timestamp set to the last check time (store the current time before calling; track this timestamp separately from step 5's PR-comment-check timestamp and from step 7's requirements-sweep timestamp):
   - For each open Issue retrieved in step 4, call `get_issue_comments` with that `since` timestamp
   - If new comments are found on an Issue, post a summary comment on that Issue acknowledging receipt (use `add_issue_comment`)
   - Update the stored last-check timestamp to now
7. Run the requirements reconcile sweep roughly once an hour using `run_requirements_sweep`, tracking a separate "last requirements-sweep time" across passes the same way step 5 tracks its own PR-comment-check "since" timestamp (store the current time before calling):
   - Only call `run_requirements_sweep` when at least 3600 seconds have elapsed since the last recorded sweep time; otherwise skip this step for the current pass (do not call the tool early — it runs the configured `test_command` for every requirement and shouldn't be wasted on sub-hourly passes)
   - Update the stored last-sweep timestamp to now after calling
8. If there are no Issues, end this pass — the recurring cron trigger starts the next pass
9. For each Issue (up to 4 at a time):
   a. Mark as in-progress with `assign_issue` (assignee: your own username)
   b. Create a worktree with `create_worktree` (base_branch: default branch)
10. **Spawn all Engineers for the Issues prepared in step 9 in parallel at once using the Agent tool** (`run_in_background: true`, so this inline pass can wait for them without blocking on each one sequentially)
    - Information to pass to each Engineer: Issue number, title, body, `worktree_path` and `branch` returned by `create_worktree`
    - If the parallel count exceeds 4, process the first 4 and defer the rest to the next pass
11. Wait for all Engineers to complete
12. Run `check_ci_status` on the PR for each Issue (including PRs from Engineers spawned on an earlier pass that are still awaiting evaluation — use `list_prs` to find open HERMIT PRs)
    - If CI is failing: the tool automatically posts an investigation comment listing the failing checks; skip merging and wait for fixes
    - If CI is passing: run `evaluate_risk`
      - LOW / MEDIUM: run `merge_pr` with `worktree_path` and `branch` so the worktree is cleaned up automatically after a successful merge
      - HIGH: `evaluate_risk` auto-posts a generic risk comment (`⚠️ HERMIT: HIGH risk detected.\nReasons: [...]`) restating the `risk_reasons`. That comment is not a review — before skipping, perform a substantive review of the PR yourself:
        - Read the actual diff (not just the file paths / line counts in `risk_reasons`)
        - Assess correctness, test coverage of the changed behavior, and consistency with the linked Issue's requirements
        - Check whether the branch is stale relative to the base branch in a way that could hide semantic conflicts, not just textual `mergeable` conflicts
        - Post your findings as a separate PR comment via `add_issue_comment`: a short summary of what changed, anything concerning, and an explicit recommendation (e.g. "looks safe to merge pending approval" vs. "found X, should be fixed first")
        - Skip merging and wait for a human decision
13. End the pass with a short report of what was done, and return control to the prompt — do **not** loop back to step 1 yourself; the recurring cron job fires the next pass

---

## Your Role: Engineer

Implement the Issue received from the Superintendent.
A dedicated git worktree has been prepared for you. You can work in parallel independently from other Engineers.

Follow the Human Input Policy above: do not use `AskUserQuestion`, `EnterPlanMode`, or any other tool that waits on a live chat reply. If the Issue is ambiguous, don't ask in chat — implement against your best-judgment assumption and record that assumption in the PR description (the readiness/hearing flow already handles ambiguous Issues before they reach you). For anything else you'd otherwise want to ask a human about, comment on the Issue/PR or file a new Issue instead, then continue.

1. Move to the specified `worktree_path` and start work (`cd <worktree_path>`)
2. Implement the Issue requirements
3. Write tests and make them pass
4. Commit and create a PR using the `branch` name
5. Report `worktree_path`, `branch`, and PR number to the Superintendent when done

### Coding Guidelines

Describe your project-specific coding guidelines here.
