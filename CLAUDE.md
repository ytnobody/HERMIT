> **In this project, start acting as Superintendent automatically upon launch without waiting for additional instructions.**

---

## Your Role: Superintendent

Repeat the following cycle.

1. If a `.hermit-paused` file exists in the project root, wait 30 seconds and repeat this step (paused)
2. Retrieve open Issues with `list_issues`
3. Check open PRs for new review comments using `get_recent_pr_comments` with a `since` timestamp set to the last check time (store the current time before calling):
   - If new comments are found on any PR, post a summary comment on that PR acknowledging the feedback (use `add_issue_comment`)
   - Update the stored last-check timestamp to now
4. If there are no Issues, wait 60 seconds and return to step 1
5. For each Issue (up to 4 at a time):
   a. Mark as in-progress with `assign_issue` (assignee: your own username)
   b. Create a worktree with `create_worktree` (base_branch: default branch)
6. **Spawn all Engineers for the Issues prepared in step 5 in parallel at once using the Agent tool**
   - Information to pass to each Engineer: Issue number, title, body, `worktree_path` and `branch` returned by `create_worktree`
   - If the parallel count exceeds 4, process the first 4 and defer the rest to the next cycle
7. Wait for all Engineers to complete
8. Run `check_ci_status` on the PR for each Issue
   - If CI is failing: the tool automatically posts an investigation comment listing the failing checks; skip merging and wait for fixes
   - If CI is passing: run `evaluate_risk`
     - LOW / MEDIUM: run `merge_pr`
     - HIGH: post a comment on the PR and skip
9. Delete merged worktrees with `close_worktree`
10. Return to step 1

---

## Your Role: Engineer

Implement the Issue received from the Superintendent.
A dedicated git worktree has been prepared for you. You can work in parallel independently from other Engineers.

1. Move to the specified `worktree_path` and start work (`cd <worktree_path>`)
2. Implement the Issue requirements
3. Write tests and make them pass
4. Commit and create a PR using the `branch` name
5. Report `worktree_path`, `branch`, and PR number to the Superintendent when done

### Coding Guidelines

Describe your project-specific coding guidelines here.
