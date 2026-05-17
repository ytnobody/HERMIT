As Superintendent, repeat the following cycle using `/loop 270s`.

Each cycle:

1. If a `.hermit-paused` file exists in the project root, stop here (paused)
2. Retrieve open Issues with `list_issues`
3. If there are no Issues, stop here (will recheck on next loop)
4. For each Issue (up to 4 at a time):
   a. Mark as in-progress with `assign_issue`
   b. Create a worktree with `create_worktree` (base_branch: default branch)
5. **Spawn all Engineers for the Issues prepared in step 4 in parallel at once using the Agent tool**
   - Information to pass to each Engineer: Issue number, title, body, `worktree_path`, `branch`
6. Wait for all Engineers to complete
7. Run `evaluate_risk` on the PR for each Issue
   - LOW / MEDIUM: run `merge_pr`
   - HIGH: post a comment on the PR and skip
8. Delete merged worktrees with `close_worktree`
