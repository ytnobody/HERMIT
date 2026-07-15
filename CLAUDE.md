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

The Superintendent cycle runs as a **background subagent** so the Claude Code prompt is never blocked. Every `/hermit` invocation — whether typed by the user or fired by the recurring cron trigger — performs only the short **foreground dispatch** below and returns control to the prompt immediately; the actual cycle is executed by the background subagent.

### Foreground dispatch (run inline, then return to the prompt)

1. Ensure the cycle keeps triggering on its own, without depending on the model remembering to do so: call `CronList` to check whether a recurring job invoking `/hermit` (or this cycle) is already scheduled.
   - If no such job is registered, call `CronCreate` to schedule one at the configured interval (e.g. `*/2 * * * *` for the default 120-second cadence; round to the nearest whole minute the cron expression can express)
   - If a matching job is already registered, do nothing
2. Spawn exactly **one** background Superintendent subagent that executes a single pass of the "Background cycle" below: call the Agent tool with `run_in_background: true` (use the model configured as `[model].superintendent` in `harness.toml`, if set), instructing it to perform one full background-cycle pass per this file's "Background cycle (one pass)" section.
   - If a background pass spawned by a previous trigger in this session is still running, do **not** spawn a second one — skip this trigger and return
3. Return control to the user immediately — do **not** wait for the background subagent to finish. When a completed background pass reports Issues it prepared but could not spawn Engineers for, perform the "Engineer fallback" below.

### Background cycle (one pass, executed by the background subagent)

1. If a `.hermit-quit` file exists in the project root, stop entirely: end this pass immediately without doing any work, and do **not** schedule anything (quit). This is a terminal stop, unlike pause — it is not resumed by `hermit resume`; starting `/hermit` again is required to resume autonomous operation.
2. If a `.hermit-paused` file exists in the project root, end this pass immediately without doing any work (paused) — the recurring cron trigger re-checks on the next cycle
3. Retrieve open Issues with `list_issues`
4. Check open PRs for new review comments using `get_recent_pr_comments` with a `since` timestamp set to the last check time (store the current time before calling):
   - If new comments are found on any PR, post a summary comment on that PR acknowledging the feedback (use `add_issue_comment`)
   - Update the stored last-check timestamp to now
5. Check open Issues for new comments using `get_issue_comments` with a `since` timestamp set to the last check time (store the current time before calling; track this timestamp separately from step 4's PR-comment-check timestamp and from step 6's requirements-sweep timestamp):
   - For each open Issue retrieved in step 3, call `get_issue_comments` with that `since` timestamp
   - If new comments are found on an Issue, post a summary comment on that Issue acknowledging receipt (use `add_issue_comment`)
   - Update the stored last-check timestamp to now
6. Run the requirements reconcile sweep roughly once an hour using `run_requirements_sweep`, tracking a separate "last requirements-sweep time" across passes the same way step 4 tracks its own PR-comment-check "since" timestamp (store the current time before calling):
   - Only call `run_requirements_sweep` when at least 3600 seconds have elapsed since the last recorded sweep time; otherwise skip this step for the current pass (do not call the tool early — it runs the configured `test_command` for every requirement and shouldn't be wasted on sub-hourly passes)
   - Update the stored last-sweep timestamp to now after calling
7. If there are no Issues, end this pass — the recurring cron trigger starts the next pass
8. For each Issue (up to 4 at a time):
   a. Mark as in-progress with `assign_issue` (assignee: your own username)
   b. Create a worktree with `create_worktree` (base_branch: default branch)
9. **Spawn all Engineers for the Issues prepared in step 8 in parallel at once using the Agent tool**
   - Information to pass to each Engineer: Issue number, title, body, `worktree_path` and `branch` returned by `create_worktree`
   - If the parallel count exceeds 4, process the first 4 and defer the rest to the next pass
   - **Fallback when subagent nesting is unavailable:** if the Agent tool is not available in your context (you are already a subagent and cannot spawn further subagents), do **not** implement the Issues yourself — end this pass and include in your completion report, for each prepared Issue: number, title, body, `worktree_path`, and `branch`, so the main session can spawn the Engineers (see "Engineer fallback")
10. Wait for all Engineers to complete
11. Run `check_ci_status` on the PR for each Issue (including PRs opened by Engineers spawned via the Engineer fallback on an earlier pass — use `list_prs` to find open HERMIT PRs still awaiting evaluation)
    - If CI is failing: the tool automatically posts an investigation comment listing the failing checks; skip merging and wait for fixes
    - If CI is passing: run `evaluate_risk`
      - LOW / MEDIUM: run `merge_pr` with `worktree_path` and `branch` so the worktree is cleaned up automatically after a successful merge
      - HIGH: `evaluate_risk` auto-posts a generic risk comment (`⚠️ HERMIT: HIGH risk detected.\nReasons: [...]`) restating the `risk_reasons`. That comment is not a review — before skipping, perform a substantive review of the PR yourself:
        - Read the actual diff (not just the file paths / line counts in `risk_reasons`)
        - Assess correctness, test coverage of the changed behavior, and consistency with the linked Issue's requirements
        - Check whether the branch is stale relative to the base branch in a way that could hide semantic conflicts, not just textual `mergeable` conflicts
        - Post your findings as a separate PR comment via `add_issue_comment`: a short summary of what changed, anything concerning, and an explicit recommendation (e.g. "looks safe to merge pending approval" vs. "found X, should be fixed first")
        - Skip merging and wait for a human decision
12. End the pass with a short report of what was done — do **not** loop back to step 1 yourself; the recurring cron job fires the next pass

### Engineer fallback (performed by the main session)

If a completed background pass reports Issues it prepared (assigned + worktree created) but could not spawn Engineers for, the main session spawns them itself: for each reported Issue, call the Agent tool with `run_in_background: true` (use the model configured as `[model].engineer` in `harness.toml`, if set), passing the Issue number, title, body, `worktree_path`, and `branch`. The resulting PRs are picked up for CI/risk evaluation by a later background pass (background-cycle step 11).

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
