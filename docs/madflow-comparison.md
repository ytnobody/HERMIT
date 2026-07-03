# HERMIT vs MADFLOW Feature Comparison Report

**Created**: 2026-05-17
**Issue**: [#1 ytnobody/Review MADFLOW and list missing features](https://github.com/ytnobody/HERMIT/issues/1)

---

## Overview

[MADFLOW](https://github.com/ytnobody/MADFLOW) (Multi-Agent Development Flow) and [HERMIT](https://github.com/ytnobody/HERMIT) (Harness for Engineer Role Management via Interactive Tasks) are both frameworks for autonomous software development with AI agents. However, their implementation architectures differ significantly.

| Aspect | MADFLOW | HERMIT |
|------|---------|--------|
| Architecture | Go binary launches and controls Claude Code as a subprocess | Claude Code calls HERMIT tools via MCP server |
| Code volume | ~5,000 lines (Go) | ~700 lines (Go) |
| Responsibility center | Binary implements orchestration logic | Claude Code itself handles orchestration |
| Configuration file | `madflow.toml` | `harness.toml` |

---

## Features Currently Provided by HERMIT

| MCP Tool | Description |
|------------|------|
| `list_issues` | Returns a list of open Issues |
| `assign_issue` | Marks an Issue as in-progress by adding a label and assigning |
| `create_worktree` | Creates a `hermit/issue-{N}` branch and `/tmp/hermit-{N}` worktree |
| `evaluate_risk` | Returns LOW/MEDIUM/HIGH based on PR change volume and impact area |
| `merge_pr` | Merges after CI passes (rejects HIGH risk with a comment); removes the worktree and branch when `worktree_path`/`branch` are provided |

Subcommands: `serve`, `install`, `init`, `pause`, `resume`, `quit`, `status`

---

## Features in MADFLOW Not in HERMIT

### 1. Lesson Injection (Self-Learning Loop)

**Issue**: [#4](https://github.com/ytnobody/HERMIT/issues/4)

A feedback loop that automatically scores Issue instruction quality after a PR merge, generates lessons from failures, and injects them into the Superintendent's prompt.

- Scoring criteria: derived Issue created (-30), Clarification Needed comment (-20), direct implementation (-20), multiple PRs (-15)
- Generates a lesson using the Anthropic API for scores below 70
- Manages up to 15 lessons (merged and trimmed by risk using LLM)

### 2. GitHub API Rate Limit Pre-Check

**Issue**: [#5](https://github.com/ytnobody/HERMIT/issues/5)

A protection mechanism that checks the remaining quota before API calls and waits or skips when quota is low.

- Threshold: fewer than 10 remaining by default
- Maximum wait time: 10 minutes (skips the cycle if exceeded)
- Fail-open design

### 3. User-Namespaced Branches and Worktrees

**Issue**: [#6](https://github.com/ytnobody/HERMIT/issues/6)

Prevents naming conflicts when multiple users run concurrently by including the GitHub login name in branch names.

- Format: `madflow/{gh_login}/issue-{ID}` → HERMIT: `hermit/{gh_login}/issue-{N}`
- Automatic GitHub login detection (`gh api user`)
- Backward compatibility maintained

### 4. hermit upgrade Command (Self-Upgrade)

**Issue**: [#7](https://github.com/ytnobody/HERMIT/issues/7)

A command that downloads the latest binary from GitHub Releases and self-updates.

- SHA-256 checksum verification
- Atomic binary replacement
- Also adds the `hermit version` command

### 5. Automatic Cleanup of Legacy Resources

**Issue**: [#8](https://github.com/ytnobody/HERMIT/issues/8)

A mechanism that detects old-format branch names and worktree paths on startup and automatically deletes or warns about them.

- Legacy branches: safely deleted with `git branch -d`
- Legacy worktrees: warning log output
- Also adds the `hermit cleanup` subcommand

### 6. Multiple AI Backend Support

**Issue**: [#9](https://github.com/ytnobody/HERMIT/issues/9)

A feature to support multiple backends (Claude Code CLI, Gemini CLI, Anthropic API key) switchable with presets.

- Due to HERMIT's architecture where Claude Code handles Engineers, the Anthropic API key approach is most practical
- `hermit use <preset>` command switches configurations

### 7. Issue Granularity Evaluation and Handling of Ambiguous Issues

**Issue**: [#10](https://github.com/ytnobody/HERMIT/issues/10)

A flow where the Engineer performs a granularity check and ambiguity assessment after receiving an Issue, and requests confirmation from the Superintendent if needed.

- If too large, suggests splitting into sub-Issues
- If ambiguous, requests clarification with a `[Clarification Needed]` comment
- Adds `add_issue_comment` / `split_issue` MCP tools

### 8. CI Quality Enhancement (lint, security, coverage)

**Issue**: [#11](https://github.com/ytnobody/HERMIT/issues/11)

CI enhancements to guarantee quality of AI-generated code with automatic quality gates.

- `golangci-lint` (errcheck, staticcheck, unused, etc.)
- `govulncheck` (vulnerability scanning)
- Test coverage threshold check
- Race condition detection (`-race`)

---

## Priority Evaluation

| Issue | Feature | Recommended Priority | Reason |
|-------|------|-----------|------|
| #5 | Rate limit check | High | Immediate risk mitigation for long-running operations |
| #11 | CI quality enhancement | High | Directly related to quality assurance for AI-generated code |
| #4 | Lesson injection | Medium | Long-term quality improvement through self-improvement loop |
| #6 | User namespacing | Medium | Essential feature for team environments |
| #7 | hermit upgrade | Medium | UX improvement with low implementation cost |
| #10 | Issue granularity evaluation | Medium | Mostly achievable via CLAUDE.md extension |
| #8 | Legacy cleanup | Low | Needed after #6 implementation |
| #9 | Multiple backends | Low | Limited effect due to architectural constraints |

---

## HERMIT's Strengths and Differentiators

- **Small code volume**: Achieves equivalent core workflow with approximately 14% of MADFLOW's code
- **Claude Code native**: Maximizes Claude Code's capabilities (reasoning, planning, context management) through MCP integration
- **Simple setup**: Ready to use with just `hermit init` and `hermit serve`
- **Claude Code skills**: Natural language-based operation with skills like `/hermit`, `/loop`

Many of MADFLOW's advanced features can be partially substituted in HERMIT through prompt engineering (CLAUDE.md) and Claude Code's own capabilities. We recommend determining implementation priorities based on actual use cases.
