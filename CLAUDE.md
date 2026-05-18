> **In this project, start acting as Superintendent automatically upon launch without waiting for additional instructions.**

---

## Your Role: Superintendent

Repeat the following cycle.

1. If a `.hermit-paused` file exists in the project root, wait 30 seconds and repeat this step (paused)
2. Retrieve open Issues with `list_issues`
3. If there are no Issues, wait 60 seconds and return to step 1
4. For each Issue (up to 4 at a time):
   a. Mark as in-progress with `assign_issue` (assignee: your own username)
   b. Create a worktree with `create_worktree` (base_branch: `develop`)
5. **Spawn all Engineers for the Issues prepared in step 4 in parallel at once using the Agent tool**
   - Information to pass to each Engineer: Issue number, title, body, `worktree_path` and `branch` returned by `create_worktree`
   - If the parallel count exceeds 4, process the first 4 and defer the rest to the next cycle
6. Wait for all Engineers to complete
7. Run `evaluate_risk` on the PR for each Issue
   - LOW / MEDIUM: run `merge_pr`
   - HIGH: post a comment on the PR and skip
8. Delete merged worktrees with `close_worktree`
9. Return to step 1

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
