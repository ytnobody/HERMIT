# HERMIT

**Harness for Engineer Role Management via Interactive Tasks**

A simple multi-agent development automation harness leveraging Claude Code's native features (Agent tool and MCP).

Automatically picks up GitHub Issues, spawns Agents in parallel, and autonomously handles implementation, PR creation, and merging.

**The software is provided "as is", without warranty of any kind.**

---

## Design Philosophy

> **"Claude Code is the star. HERMIT is just the toolbox for domain operations."**

- AI reasoning, orchestration, and context management are **fully delegated to Claude Code**
- HERMIT only provides a thin wrapper for GitHub/Git operations as an **MCP server**
- Code volume ~700 lines (for reference: an equivalent Go binary implementation would be ~5,000 lines)

---

## Installation

```sh
curl -sSL https://raw.githubusercontent.com/ytnobody/HERMIT/refs/heads/main/install.sh | sh
```

Places the binary at `~/.local/bin/hermit` and automatically registers it as a Claude Code MCP server.

### Prerequisites

- [Claude Code](https://claude.ai/code) must be installed
- Authenticated with [gh CLI](https://cli.github.com/) (`gh auth login`), or `GITHUB_TOKEN` environment variable must be set
- `git` command must be available

---

## Quick Start

```sh
# 1. Install HERMIT (downloads the binary and auto-registers it as a Claude Code MCP server)
curl -sSL https://raw.githubusercontent.com/ytnobody/HERMIT/refs/heads/main/install.sh | sh

# 2. Initialize your project
cd your-project
hermit init

# 3. Commit the generated files (including hermit's slash commands) to version control
git add harness.toml CLAUDE.md .github/ISSUE_TEMPLATE/hermit-task.md .claude/
git commit -m "chore: initialize HERMIT"

# 4. Start Claude Code and launch the Superintendent loop
claude
# Inside Claude Code:
# /hermit

# 5. Create a GitHub Issue — HERMIT picks it up automatically and handles everything
```

Once `/hermit` is running, just open GitHub Issues in your repository. HERMIT will automatically:
- Pick up the Issue
- Spawn an Engineer agent to implement it
- Create a Pull Request
- Evaluate risk and merge if safe

> **Note:** `install.sh` calls `hermit install` automatically, so the MCP server registration happens as part of the one-liner install command.

> **Version control:** The files generated in `.claude/commands/` (`hermit.md`, `hermit-pause.md`, `hermit-resume.md`) **should be committed to git**. They are project-scoped slash commands — similar to `CLAUDE.md` — and allow all contributors using Claude Code on the same project to access `/hermit`, `/hermit-pause`, and `/hermit-resume` without running `hermit install` themselves.


---

## Setting Up a Project

```sh
cd your-project
hermit init
```

Enter the following interactively:

| Field | Description |
|---|---|
| GitHub owner | Org name or username |
| GitHub repo | Repository name |
| Language | `ja` or `en` (language for Claude instructions) |
| Max Engineers | Maximum number of Engineers to spawn in parallel (default: 4) |
| Model preset | Claude model combination to use (default: `claude`) |

Generated files:

- `harness.toml` — Project configuration (shared with the team)
- `CLAUDE.md` — Role definitions for Superintendent / Engineer
- `.github/ISSUE_TEMPLATE/hermit-task.md` — GitHub Issue template for well-structured tasks
- `.claude/settings.json` — Claude Code permission settings for autonomous operation

Edit the "Coding Guidelines" section in `CLAUDE.md` to match your project.

---

## Usage

Since `hermit install` registers it as an MCP server in `~/.claude/settings.json`, **Claude Code automatically starts `hermit serve` on launch**. No need to start it manually in another terminal.

```
Claude Code starts
  └─ hermit serve auto-started (MCP subprocess)
       ↓ list_issues / assign_issue / create_worktree
       ↓ Agent spawn → Engineer × N
       ↓ evaluate_risk / merge_pr / close_worktree
       ↓ (repeat)
```

### Step 1: Start Claude Code in your project

```sh
cd your-project   # directory where hermit init was run
claude
```

### Step 2: Start the Superintendent loop

> **This step is required.** Autonomous development does not begin until you run `/hermit` inside Claude Code.

```
/hermit
```

`/hermit` internally calls `/loop 120s`, resetting context every 120 seconds while continuing the Superintendent cycle. If there are no Issues it waits for the next loop; if there are Issues it automatically handles everything through implementation and merge.

### Step 3: Create a GitHub Issue to trigger automatic development

With `/hermit` running, simply open an Issue in your GitHub repository:

```
GitHub Issue created
  └─ HERMIT picks it up on the next cycle
       └─ Engineer agent spawned automatically
            └─ Implementation committed → PR created
                 └─ Risk evaluated → auto-merged (if LOW/MEDIUM)
```

No further action is needed. HERMIT handles the entire development workflow autonomously.

### Superintendent Cycle

1. Retrieve open Issues with `list_issues`
2. Mark as in-progress with `assign_issue`
3. Spawn Engineers in parallel with the Agent tool (up to `max_engineers`)
4. Risk evaluation with `evaluate_risk`
5. If LOW/MEDIUM, auto-merge with `merge_pr` (skip HIGH with comment)
6. Clean up worktrees with `close_worktree`
7. Return to step 1

---

## MCP Tools

| Tool | Description |
|---|---|
| `list_issues` | Returns a list of open Issues (supports multi-repo mode) |
| `assign_issue` | Marks an Issue as in-progress by adding a label and assigning |
| `create_worktree` | Creates a branch and git worktree for an Issue |
| `evaluate_risk` | Returns LOW/MEDIUM/HIGH based on PR change volume and impact area |
| `merge_pr` | Merges after CI passes; posts a risk comment and records a lesson |
| `close_worktree` | Removes the worktree and branch |
| `check_ci_status` | Checks CI/CD status for a PR; auto-posts a comment if checks are failing |
| `add_issue_comment` | Posts a comment on an Issue or PR |
| `get_issue_comments` | Returns comments on an Issue, optionally filtered by timestamp |
| `get_recent_pr_comments` | Returns inline review comments on a PR, optionally filtered by timestamp |
| `close_issue` | Closes a GitHub Issue, optionally posting a comment first |
| `list_prs` | Returns a list of open pull requests, optionally filtered by Issue number |
| `review_pr` | Posts a structured automated review comment based on static diff analysis |
| `get_lessons` | Returns lessons learned from past merges to avoid repeating mistakes |
| `get_config` | Returns current HERMIT configuration values (e.g. `loop_interval`) |
| `notify` | Sends a notification to the configured webhook (Slack, Discord, or generic) |
| `get_default_branch` | Returns the repository's default branch name |

### Risk Evaluation Criteria

| Condition | Level |
|---|---|
| 20+ changed files / 500+ changed lines / changes in `cmd/`, `go.mod`, `.github/` | HIGH |
| 10+ changed files / 200+ changed lines / changes in `internal/` | MEDIUM |
| Otherwise | LOW |

---

## Configuration File (`harness.toml`)

```toml
[github]
owner                = "your-org"
repo                 = "your-repo"
rate_limit_threshold = 10           # pause when remaining API calls drop to this level
default_branch       = "main"       # base branch for new worktrees

[agent]
max_engineers   = 4         # maximum number of parallel Engineers
language        = "en"      # "ja" | "en"
# loop_interval   = 270     # Superintendent cycle interval in seconds (default: 270)
# branch_prefix   = "hermit/your-login"  # defaults to hermit/<gh_login> if omitted
# trigger_comment = "/hermit"            # only process Issues that have this comment

[model]
superintendent = "claude-sonnet-4-5"   # model used for the Superintendent role
engineer       = "claude-sonnet-4-5"   # model used for Engineer roles

# [notification]
# webhook_url = "https://hooks.slack.com/services/..."  # Slack, Discord, or generic webhook
# type        = "slack"   # "slack" | "discord" | "generic" (auto-detected from URL if omitted)
```

**Pass `GITHUB_TOKEN` as an environment variable. Do not write it in `harness.toml`.**

### Rate Limit Handling

HERMIT automatically monitors the GitHub API rate limit before each operation. When remaining calls drop to `rate_limit_threshold` (default: 10), it waits until the limit resets. If the reset time is more than 10 minutes away, the current cycle is skipped rather than blocking.

### Trigger Comment Mode

When `trigger_comment` is set in `harness.toml`, the Superintendent will only pick up Issues that have at least one comment containing the specified string (case-insensitive). This lets you control which Issues HERMIT handles without relying solely on labels or assignment.

Example: set `trigger_comment = "/hermit"` and comment `/hermit` on any Issue you want HERMIT to process.

### Model Selection

The `[model]` section lets you specify which Claude model each role uses. You can also apply a preset with `hermit use`:

```sh
hermit use claude        # Sonnet for both Superintendent and Engineer (balanced)
hermit use claude-cheap  # Sonnet for Superintendent, Haiku for Engineers (cost-optimized)
```

### Webhook Notifications

HERMIT can send notifications to Slack, Discord, or any generic webhook when key events occur (issue assigned, PR merged, high risk detected, etc.). Set `webhook_url` in `[notification]`. The webhook type is auto-detected from the URL; set `type` explicitly if needed.

### Multi-Repo Mode

To monitor multiple repositories from a single HERMIT instance, replace `[github]` with a `[[repos]]` array:

```toml
[[repos]]
owner = "your-org"
repo  = "repo-one"

[[repos]]
owner = "your-org"
repo  = "repo-two"
label = "hermit"   # optional: only pick up Issues with this label in this repo
```

When `[[repos]]` is present, `list_issues` queries all configured repositories and returns issues from all of them in a single call.

---

## Pausing and Resuming Autonomous Operation

```sh
hermit pause    # pause autonomous operation (creates .hermit-paused)
hermit resume   # resume autonomous operation (removes .hermit-paused)
hermit status   # check current state (running / paused)
```

The Superintendent checks for `.hermit-paused` at the start of each cycle. Running `hermit pause` stops it after the current cycle completes; `hermit resume` resumes it immediately.

---

## Lessons System

After each PR is merged, HERMIT scores the Issue's instruction quality (0–100) based on:

- PR risk level (HIGH: −30, MEDIUM: −15)
- CI was failing before merge (−20)
- Multiple PRs were created for the same Issue (−15)
- A clarification comment was present (−20)

When the score drops below 70, a lesson is generated and saved to `.hermit/lessons.txt`. The Superintendent reads these lessons at the start of each cycle via `get_lessons` to avoid repeating the same mistakes. Up to 15 lessons are kept; high-risk lessons are always retained.

---

## Subcommand Reference

```
hermit serve     # Start the MCP server (stdio) — Claude Code auto-starts this, manual execution normally not needed
hermit install   # Register MCP server in ~/.claude/settings.json and install slash commands
hermit init      # Initialize a project (generate harness.toml, CLAUDE.md, issue template, settings)
hermit pause     # Pause autonomous operation
hermit resume    # Resume autonomous operation
hermit status    # Show autonomous operation status (running / paused)
hermit use       # Apply a model preset to harness.toml (e.g. hermit use claude-cheap)
hermit version   # Print the current hermit version
hermit upgrade   # Download and install the latest hermit release
hermit cleanup   # Remove legacy branches and zombie worktrees left by older versions
hermit doctor    # Check prerequisites (git, gh CLI, GITHUB_TOKEN, harness.toml, Claude Code)
hermit dry-run   # Preview what the Superintendent would do without making any changes
```

### `hermit doctor`

Runs a series of environment checks and reports pass/fail for each:

- `git` is available in PATH
- `gh` CLI is installed and authenticated
- `GITHUB_TOKEN` is available (from environment or `gh auth token`)
- `harness.toml` exists with `owner` and `repo` filled in
- `claude` (Claude Code) is installed

Exits with a non-zero status if any check fails.

### `hermit dry-run`

Shows which open Issues the Superintendent would pick up on the next cycle, along with the branch name and worktree path it would create for each. No GitHub API writes are performed and no files are modified.

### `hermit upgrade`

Fetches the latest release from GitHub, verifies the SHA-256 checksum (when a `checksums.txt` asset is present), and replaces the running binary in-place. The binary is written atomically via a temporary file.

### `hermit cleanup`

Detects and removes:

- **Legacy branches** — branches matching the old `hermit/issue-N` format that were not cleaned up
- **Zombie worktrees** — git worktrees whose directories no longer exist on disk

Run this once after upgrading from an older version if you see warnings about legacy resources on `hermit serve` startup.

---

## License

MIT
