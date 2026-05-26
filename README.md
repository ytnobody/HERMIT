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

# 3. Start Claude Code and launch the Superintendent loop
claude
# Inside Claude Code:
# /hermit

# 4. Create a GitHub Issue — HERMIT picks it up automatically and handles everything
```

Once `/hermit` is running, just open GitHub Issues in your repository. HERMIT will automatically:
- Pick up the Issue
- Spawn an Engineer agent to implement it
- Create a Pull Request
- Evaluate risk and merge if safe

> **Note:** `install.sh` calls `hermit install` automatically, so the MCP server registration happens as part of the one-liner install command.

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

Generated files:

- `harness.toml` — Project configuration (shared with the team)
- `CLAUDE.md` — Role definitions for Superintendent / Engineer

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
| `list_issues` | Returns a list of open Issues |
| `assign_issue` | Marks an Issue as in-progress by adding a label and assigning |
| `create_worktree` | Creates a `hermit/issue-{N}` branch and `/tmp/hermit-{N}` worktree |
| `evaluate_risk` | Returns LOW/MEDIUM/HIGH based on PR change volume and impact area |
| `merge_pr` | Merges after CI passes (rejects HIGH risk with a comment) |
| `close_worktree` | Removes the worktree and branch |

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
owner = "your-org"
repo  = "your-repo"

[agent]
max_engineers = 4   # maximum number of parallel Engineers
language      = "en"  # "ja" | "en"
```

**Pass `GITHUB_TOKEN` as an environment variable. Do not write it in `harness.toml`.**

---

## Pausing and Resuming Autonomous Operation

```sh
hermit pause    # pause autonomous operation (creates .hermit-paused)
hermit resume   # resume autonomous operation (removes .hermit-paused)
hermit status   # check current state (running / paused)
```

The Superintendent checks for `.hermit-paused` at the start of each cycle. Running `hermit pause` stops it after the current cycle completes; `hermit resume` resumes it immediately.

---

## Subcommand Reference

```
hermit serve    # Start the MCP server (stdio) — Claude Code auto-starts this, manual execution normally not needed
hermit install  # Register MCP server in ~/.claude/settings.json
hermit init     # Initialize a project (generate harness.toml + CLAUDE.md)
hermit pause    # Pause autonomous operation
hermit resume   # Resume autonomous operation
hermit status   # Show autonomous operation status
```

---

## License

MIT
