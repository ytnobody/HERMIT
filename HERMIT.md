# HERMIT — Design Document

**HERMIT** (Harness for Engineer Role Management via Interactive Tasks) is a simple multi-agent development automation harness that leverages Claude Code's native features (Agent tool and MCP).

---

## 1. Background and Design Philosophy

### Lessons from MADFLOW

MADFLOW has an "outside-in" architecture where a Go binary launches and manages Claude Code as a subprocess. This caused several problems:

- Startup cost and prompt cache inefficiency with `claude -p` / ClaudeStreamProcess
- Complexity of reimplementing the agentic loop in Go (e.g., `anthropic_api.go`)
- Duplicate implementation of context reset, chat log, and process lifecycle management

### HERMIT's Principles

> **"Claude Code is the star. HERMIT is just the toolbox for domain operations."**

- AI reasoning, orchestration, and context management are fully delegated to Claude Code
- HERMIT only provides a thin wrapper for GitHub/Git operations as an MCP server
- Superintendent and Engineer role definitions are described in CLAUDE.md
- Setup is complete with a single command

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────┐
│  Claude Code (Superintendent session)            │
│                                                 │
│  Operates according to roles defined in CLAUDE.md│
│                                                 │
│  ┌──────────────────────────────────────────┐   │
│  │  Spawn Engineers in parallel via Agent   │   │
│  │  Engineer-1  Engineer-2  Engineer-3      │   │
│  └──────────────────────────────────────────┘   │
│                                                 │
│  MCP tools ─────────────────────────────────┐   │
└─────────────────────────────────────────────┼───┘
                                              │
                              ┌───────────────▼──────────────┐
                              │  HERMIT MCP Server (Go)      │
                              │                              │
                              │  list_issues                 │
                              │  assign_issue                │
                              │  create_worktree             │
                              │  evaluate_risk               │
                              │  merge_pr                    │
                              └──────────────────────────────┘
                                      │           │
                               GitHub API      Git CLI
```

---

## 3. Directory Structure

```
hermit/
├── cmd/hermit/
│   └── main.go              # install / init / serve subcommands
├── internal/
│   ├── mcp/
│   │   ├── server.go        # stdio MCP server startup
│   │   └── tools.go         # handler definitions for 6 tools
│   ├── github/
│   │   └── client.go        # GitHub REST API client
│   ├── git/
│   │   └── worktree.go      # git worktree operations
│   └── risk/
│       └── evaluator.go     # PR risk evaluation logic
├── templates/
│   ├── CLAUDE.md.tmpl       # Superintendent / Engineer role definition template
│   └── harness.toml.tmpl    # configuration file template
├── install.sh               # one-liner install script
├── go.mod
└── README.md
```

---

## 4. MCP Tool Specifications

HERMIT provides the following MCP tools.

### `list_issues`

Returns a list of open GitHub Issues that have not been started.

```json
// Input
{ "label": "string (optional)" }

// Output
[{ "number": 42, "title": "...", "body": "...", "labels": [...] }]
```

### `assign_issue`

Marks an Issue as in-progress (adds label + assigns).

```json
// Input
{ "issue_number": 42, "assignee": "string" }

// Output
{ "success": true }
```

### `create_worktree`

Creates a branch and git worktree for an Issue.

```json
// Input
{ "issue_number": 42, "base_branch": "develop" }

// Output
{ "worktree_path": "/path/to/worktree", "branch": "hermit/issue-42" }
```

### `evaluate_risk`

Returns the risk level based on the PR's change volume and impact area.

```json
// Input
{ "pr_number": 123 }

// Output
{ "level": "LOW|MEDIUM|HIGH", "reasons": ["..."] }
```

Evaluation criteria:

| Condition | Risk Level |
|---|---|
| 20+ changed files / 500+ changed lines / changes in `cmd/`, `go.mod`, `.github/` | HIGH |
| 10+ changed files / 200+ changed lines / core changes in `internal/` | MEDIUM |
| Otherwise | LOW |

### `merge_pr`

Merges the PR after CI passes. Rejects HIGH risk and posts a comment. When `worktree_path` and `branch` are provided, removes the worktree and branch after a successful merge.

```json
// Input
{ "pr_number": 123, "worktree_path": "/path/to/worktree", "branch": "hermit/issue-42" }

// Output
{ "merged": true } | { "merged": false, "reason": "HIGH risk / CI failing / ..." }
```

### `add_issue_comment`

Posts a comment to a GitHub Issue.

```json
// Input
{ "issue_number": 42, "body": "comment text" }

// Output
{ "success": true }
```

### `close_issue`

Closes a resolved GitHub Issue.

```json
// Input
{ "issue_number": 42 }

// Output
{ "success": true }
```

### `list_prs`

Returns a list of open pull requests.

```json
// Input
{ "state": "open|closed|merged (optional, default: open)" }

// Output
[{ "number": 123, "title": "...", "branch": "...", "url": "..." }]
```

### `get_lessons`

Returns lessons learned from past Issues and PRs to guide implementation.

```json
// Input
{}

// Output
{ "lessons": ["..."] }
```

### `get_config`

Returns current harness configuration values.

```json
// Input
{}

// Output
{ "owner": "...", "repo": "...", "max_engineers": 4, "loop_interval": 120 }
```

### `review_pr`

Performs static analysis on a PR and returns a review summary.

```json
// Input
{ "pr_number": 123 }

// Output
{ "summary": "...", "risk_level": "LOW|MEDIUM|HIGH", "suggestions": ["..."] }
```

### `notify`

Sends a notification via configured webhook (Slack, Discord, or generic).

```json
// Input
{ "message": "..." }

// Output
{ "success": true }
```

---

## 5. Configuration File (`harness.toml`)

```toml
[github]
owner = "your-org"
repo  = "your-repo"

[agent]
max_engineers = 4   # maximum number of parallel Engineers
language      = "en"  # "ja" | "en"
```

**The GitHub token is received via the `GITHUB_TOKEN` environment variable. Do not write it in the toml.**

---

## 6. CLAUDE.md Template Design

The CLAUDE.md generated by `hermit init` consists of the following 2 sections.

### Superintendent Section

The Superintendent cycle does not run in the Claude Code foreground. Each `/hermit` invocation (typed by the user or fired by the recurring cron trigger) performs a short **foreground dispatch** — ensure the cron trigger exists, spawn a background Superintendent subagent via the Agent tool with `run_in_background: true`, and return control to the prompt — while the subagent executes one **background-cycle pass** (simplified):

```markdown
## Your Role: Superintendent

### Foreground dispatch (run inline, then return to the prompt)

1. Ensure a recurring cron job invoking `/hermit` exists (CronList / CronCreate)
2. Spawn one background Superintendent subagent (Agent tool, `run_in_background: true`)
   that executes a single pass of the background cycle
3. Return control to the user immediately

### Background cycle (one pass, executed by the background subagent)

1. If `.hermit-quit` or `.hermit-paused` exists, end the pass without doing any work
2. Retrieve open Issues with `list_issues`
3. If there are no Issues, end the pass (the cron trigger starts the next one)
4. Mark Issues as in-progress with `assign_issue` and create worktrees
5. For each Issue, spawn Engineers with the Agent tool (up to {{ max_engineers }} in parallel)
   - Pass Issue number, title, body, and worktree path to each Engineer
   - If subagent nesting is unavailable, report the prepared Issues back so the
     main session can spawn the Engineers instead (Engineer fallback)
6. Wait for all Engineers to complete
7. If a PR has been created, run `evaluate_risk` for risk evaluation
   - LOW/MEDIUM: run `merge_pr` with `worktree_path`/`branch` so the worktree is cleaned up automatically
   - HIGH: post a comment on the PR and skip
8. End the pass — the recurring cron job fires the next one
```

### Engineer Section

```markdown
## Your Role: Engineer

Implement the Issue received from the Superintendent.

1. Move to the specified worktree path and start work
2. Implement the Issue requirements
3. Write tests and make them pass
4. Commit and create a PR
5. Report to the Superintendent when done

### Coding Guidelines
{{ project_coding_rules }}
```

---

## 7. Installation Design

### `install.sh` Process Flow

```sh
#!/usr/bin/env sh
set -eu

# 1. Detect OS/architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
# Normalize to arm64 / amd64

# 2. Get the binary URL for the latest release
VERSION=$(curl -sSL https://api.github.com/repos/ytnobody/hermit/releases/latest \
  | grep '"tag_name"' | cut -d'"' -f4)

# 3. Download binary and checksum
curl -sSL "https://github.com/ytnobody/hermit/releases/download/${VERSION}/hermit_${OS}_${ARCH}" \
  -o /tmp/hermit
curl -sSL "https://github.com/ytnobody/hermit/releases/download/${VERSION}/hermit_${OS}_${ARCH}.sha256" \
  -o /tmp/hermit.sha256

# 4. Verify checksum
sha256sum -c /tmp/hermit.sha256

# 5. Place + set execute permission
install -m 755 /tmp/hermit ~/.local/bin/hermit

# 6. Register as MCP in Claude Code
hermit install
```

### `hermit install` Process

Adds the following to `mcpServers` in `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "hermit": {
      "command": "hermit",
      "args": ["serve"],
      "env": {
        "GITHUB_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  }
}
```

### `hermit init` Process (run in project directory)

1. Interactively generate `harness.toml` (enter owner / repo / language / max_engineers)
2. Generate `CLAUDE.md` from `templates/CLAUDE.md.tmpl` (expanded with harness.toml values)
3. Do **not** add `harness.toml` to `.gitignore` (intended to be shared with the team)
4. Display a completion message and next steps

---

## 8. Implementation Order

Implement in order of fewest dependencies.

| Step | Content | Dependencies |
|---|---|---|
| 1 | `go.mod` + project skeleton | none |
| 2 | `internal/github/client.go` | none |
| 3 | `internal/git/worktree.go` | none |
| 4 | `internal/risk/evaluator.go` | github |
| 5 | `internal/mcp/tools.go` | github, git, risk |
| 6 | `internal/mcp/server.go` | tools |
| 7 | `cmd/hermit/main.go` (serve) | mcp |
| 8 | `cmd/hermit/main.go` (install) | none |
| 9 | `cmd/hermit/main.go` (init) | templates |
| 10 | `install.sh` | none |
| 11 | `templates/` creation | none |

---

## 9. Comparison with MADFLOW

| Element | MADFLOW | HERMIT |
|---|---|---|
| Orchestrator | Go binary | Claude Code (CLAUDE.md) |
| AI invocation | Subprocess / REST API | Claude Code native |
| Inter-agent communication | Chat log (file) | Agent tool input/output |
| Context reset | 8-minute timer (Go implementation) | Delegated to Claude Code |
| Code volume (approx.) | ~5,000 lines | ~500 lines |
| Installation | `go install` + manual setup | `curl \| sh` one-liner |
