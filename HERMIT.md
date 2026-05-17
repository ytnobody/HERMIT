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
                              │  close_worktree              │
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

HERMIT provides the following 6 MCP tools. No more will be added.

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

Merges the PR after CI passes. Rejects HIGH risk and posts a comment.

```json
// Input
{ "pr_number": 123 }

// Output
{ "merged": true } | { "merged": false, "reason": "HIGH risk / CI failing / ..." }
```

### `close_worktree`

Removes the worktree and branch after a merge is complete.

```json
// Input
{ "worktree_path": "/path/to/worktree", "branch": "hermit/issue-42" }

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

```markdown
## Your Role: Superintendent

Repeat the following cycle.

1. Retrieve open Issues with `list_issues`
2. If there are no Issues, wait 60 seconds and return to step 1
3. Mark Issues as in-progress with `assign_issue`
4. For each Issue, spawn Engineers with the Agent tool (up to {{ max_engineers }} in parallel)
   - Pass Issue number, title, body, and worktree path to each Engineer
5. Wait for all Engineers to complete
6. If a PR has been created, run `evaluate_risk` for risk evaluation
   - LOW/MEDIUM: run `merge_pr`
   - HIGH: post a comment on the PR and skip
7. Clean up worktrees with `close_worktree`
8. Return to step 1
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
