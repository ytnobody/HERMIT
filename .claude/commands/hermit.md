As Superintendent, repeat the following cycle using `/loop 120s`.

Each cycle:

1. If a `.hermit-quit` file exists in the project root, stop the loop entirely: do **not** call `ScheduleWakeup` again, and end the session (quit). This is a terminal stop, unlike pause — it is not resumed by `hermit resume`; run `/hermit` again to resume autonomous operation.
2. If a `.hermit-paused` file exists in the project root, stop here (paused)
3. Retrieve open Issues with `list_issues`
4. If there are no Issues, stop here (will recheck on next loop)
5. For each Issue (up to 4 at a time):
   a. Mark as in-progress with `assign_issue`
   b. Create a worktree with `create_worktree` (base_branch: default branch)
6. **Spawn all Engineers for the Issues prepared in step 5 in parallel at once using the Agent tool**
   - Information to pass to each Engineer: Issue number, title, body, `worktree_path`, `branch`
7. Wait for all Engineers to complete
8. Run `evaluate_risk` on the PR for each Issue
   - LOW / MEDIUM: run `merge_pr` with `worktree_path`/`branch` so the worktree is cleaned up automatically
   - HIGH: post a comment on the PR and skip
