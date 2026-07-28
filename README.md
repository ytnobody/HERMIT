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

> **Version control:** The files generated in `.claude/commands/` (`hermit.md`, `hermit-pause.md`, `hermit-resume.md`, `hermit-quit.md`) **should be committed to git**. They are project-scoped slash commands — similar to `CLAUDE.md` — and allow all contributors using Claude Code on the same project to access `/hermit`, `/hermit-pause`, `/hermit-resume`, and `/hermit-quit` without running `hermit install` themselves.


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
- `.claude/settings.json` — Claude Code permission settings for autonomous operation, plus a recommended `sandbox` block (see "Sandboxing the Engineer" below)

Re-running `hermit init` on an already-initialized project never overwrites an existing `.claude/settings.json` wholesale: any top-level key you've already customized (most importantly `permissions`) is left exactly as-is, and only keys that are entirely missing (e.g. `sandbox`, on a project initialized before it was added) are filled in with hermit's defaults.

Edit the "Coding Guidelines" section in `CLAUDE.md` to match your project.

---

## Security

**Read this before running HERMIT against a public repository.**

- **Issue/comment bodies are injected directly into agent context.** HERMIT passes the raw text of Issues and comments to the Superintendent and Engineer agents so they can act on them. On a public repo, anyone who can open an Issue or leave a comment can attempt to influence that context — including with adversarial or prompt-injection-style instructions. Do not run HERMIT against repositories where you do not trust every person who can author an Issue or comment.
- **`trigger_comment` limits *who can start* processing, not *what the Issue contains*.** Setting `trigger_comment` (e.g. `"/hermit"`) means HERMIT only picks up Issues where someone has posted that exact comment, which narrows the entry point to people who can comment on the repo. It does **not** sanitize, filter, or otherwise mitigate adversarial content already present in the Issue body or in other comments — that content is still handed to the agent once the Issue is picked up.
- **Recommended operating modes for public repositories:**
  - **(a) Restrict Issue authorship to trusted collaborators.** Use repository settings, a bot/label-based intake process, or org membership requirements so that only trusted people can create Issues HERMIT will act on.
  - **(b) Disable auto-merge entirely.** Skip the `merge_pr` step regardless of `evaluate_risk`'s result, and require a human to review and merge every PR HERMIT opens, even ones classified LOW or MEDIUM risk. This bounds the blast radius of any adversarial instruction to "opened a PR," not "got code merged."
  - In practice, many public-repo operators will want both: trusted-author Issues *and* human-reviewed merges.
- **Future work (not yet implemented):** an Issue-author allowlist (e.g. only process Issues from repository collaborators or an explicit list of GitHub usernames) is being considered as a built-in mitigation. Until it exists, use the operating modes above.

### Sandboxing the Engineer

The Engineer runs with `Bash(*)` permission — unrestricted shell access — on the machine running Claude Code, because an allow-list of individual commands was tried and rejected (Issue #138: every time a new tool or command shape showed up that the allow-list hadn't anticipated, the loop either stalled on a confirmation prompt or someone widened the list until it was `Bash(*)` in practice anyway). Instead of enumerating which commands are allowed, `hermit init` generates a `sandbox` block in `.claude/settings.json` that narrows *what the Engineer can reach*, regardless of which command it runs:

```json
{
  "sandbox": {
    "enabled": true,
    "allowUnsandboxedCommands": false,
    "network": {
      "tlsTerminate": {},
      "allowedDomains": ["*.github.com", "proxy.golang.org", "sum.golang.org", "storage.googleapis.com"]
    },
    "credentials": {
      "files": [
        { "path": "~/.ssh", "mode": "deny" },
        { "path": "~/.aws/credentials", "mode": "deny" }
      ],
      "envVars": [
        { "name": "GITHUB_TOKEN", "mode": "mask", "injectHosts": ["api.github.com"] }
      ]
    }
  }
}
```

A few details worth knowing if you edit this block by hand:

- **`allowUnsandboxedCommands` defaults to `true` in Claude Code.** If you omit it (or leave it `true`), the sandbox is effectively optional — a command can simply opt out. `hermit init` always writes it as `false`; keep it that way.
- **`GITHUB_TOKEN` must be `"mode": "mask"` with `injectHosts`, never `"mode": "deny"`.** `gh` (and therefore `gh pr create`, `gh issue comment`, etc., which the Engineer needs to do its job) requires the real token when talking to `api.github.com`. `"deny"` breaks it outright. `"mask"` hides the value everywhere else and only injects the real token on requests to the hosts listed in `injectHosts`; this requires `network.tlsTerminate` to be present (even as `{}`), since Claude Code needs to terminate TLS to inspect the destination host before deciding whether to inject.
- **`network.allowedDomains` must cover your dependency graph, not just GitHub.** For a Go project this means the module proxy/sum/storage hosts (`proxy.golang.org`, `sum.golang.org`, `storage.googleapis.com`) in addition to `*.github.com`, or `go build`/`go test`/`go mod download` will fail under the sandbox. Verify the list by actually running your project's test command with the sandbox enabled — don't assume it's complete.

**Scope and precedence — read this before treating the sandbox as an enforcement mechanism.** Claude Code settings are layered (managed > CLI flags > local project > project > user), and different key *shapes* combine across that stack differently:

- **Boolean keys** (like `sandbox.enabled`, `sandbox.allowUnsandboxedCommands`) resolve by precedence: the highest-precedence scope that sets the key wins outright. A project-scoped `.claude/settings.json` beats a user-scoped `~/.claude/settings.json`.
- **Array keys** (like `sandbox.excludedCommands`, `permissions.allow`, `permissions.deny` — anything that's a list of patterns) are **merged, additively, across every scope** that sets them. Nothing lower in the stack can *remove* an entry a higher scope added, but a lower scope can freely *add* entries a higher scope didn't ask for.

The practical consequence: a `sandbox` block placed in `.claude/settings.json` (project scope, the default `hermit init` target) constrains the Engineer only as long as the Engineer doesn't touch it. But the Engineer's job is to open PRs against this very repository — including PRs that edit `.claude/settings.json` itself, add an entry to `sandbox.excludedCommands`, or otherwise widen the array-merged keys from project scope. Nothing in a project-scoped sandbox config stops the Engineer from proposing (and, if auto-merge is on for LOW/MEDIUM risk PRs, landing) exactly that change. In other words: project settings are a strong default, not an enforcement boundary against the agent they're meant to constrain.

**To actually enforce the sandbox against the Engineer, you need managed settings** — a settings file outside the repository, deployed by an admin/MDM process the Engineer has no write access to, which sits above project scope in the precedence stack. Two managed-settings flags close the remaining loophole in the array-merge behavior described above:

- **`allowManagedReadPathsOnly`** — restricts filesystem reads to paths the managed config explicitly allows, so a project-scope (or Engineer-authored) change can't widen readable paths beyond what the managed config permits.
- **`allowManagedDomainsOnly`** — restricts network access to domains the managed config explicitly allows, closing the equivalent hole for `network.allowedDomains`.

hermit does not currently generate a managed settings file — `hermit init` only ever writes to the project's own `.claude/settings.json`, which (per the above) the Engineer can eventually influence. Automating managed-settings generation, and wiring up `allowManagedReadPathsOnly` / `allowManagedDomainsOnly`, is tracked separately (see Issue #179); until that lands, the project-scope sandbox block here is best understood as raising the cost of an adversarial or buggy Engineer action, not as a hard boundary.

`hermit doctor` checks the generated `.claude/settings.json` for the two most common ways this block ends up not doing anything (`sandbox.enabled` false/missing, `allowUnsandboxedCommands` true/missing) plus a non-empty `sandbox.excludedCommands` (which reopens a hole per command listed there); see `hermit doctor` below.

---

## Usage

Since `hermit install` registers it as an MCP server via `claude mcp add` (project-local scope), **Claude Code automatically starts `hermit serve` on launch**. No need to start it manually in another terminal.

```
Claude Code starts
  └─ hermit serve auto-started (MCP subprocess)
       ↓ list_issues / assign_issue / create_worktree
       ↓ Agent spawn → Engineer × N
       ↓ evaluate_risk / merge_pr (auto-cleans worktree on merge)
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

### Alternative to Step 2: `hermit run` (no Claude Code session required)

`/hermit` keeps the Superintendent loop alive inside a Claude Code session — a terminal running `claude` has to stay open for as long as you want autonomous operation. `hermit run` moves that loop into HERMIT's own long-lived process instead: it owns an internal ticker and, once per tick, launches `claude -p` non-interactively (no permission prompts, `--dangerously-skip-permissions`), waits for it to finish, then waits `[agent].loop_interval` before the next tick. `hermit serve` is already a long-lived process (the MCP server); `hermit run` is the same shape applied to the Superintendent cycle itself.

```sh
cd your-project   # directory where hermit init was run
hermit run
```

`hermit run` and `/hermit` are not mutually exclusive — use whichever fits how you want to keep HERMIT alive:

- `hermit pause` / `hermit resume` / `hermit quit` / `hermit status` all work the same way regardless of which one is driving the loop; `hermit run` checks `.hermit-paused`/`.hermit-quit` itself before every tick.
- `hermit run` sends SIGINT/SIGTERM a graceful shutdown: an in-flight pass is never interrupted — it always finishes, and only then does the loop stop.
- A pass that hangs or takes a long time never causes overlapping ticks: the next tick is only scheduled after the previous one returns.
- If `N` consecutive passes fail, `hermit run` sends a notification via `[notification]` (`[run].failure_notify_threshold`, default 3) so unattended failures don't go unnoticed.
- The three cadence timestamps the Superintendent cycle tracks (last PR-comment check, last Issue-comment check, last requirements sweep) live in `.hermit/superintendent-state.json`, owned by HERMIT's Go code and read/written only through the `get_loop_state`/`update_loop_state` MCP tools — never hand-written by the Superintendent session itself.

See "Running HERMIT Continuously" below for keeping `hermit run` alive across reboots/logouts on each platform.

### Superintendent Cycle

1. Retrieve open Issues with `list_issues`
2. Mark as in-progress with `assign_issue`
3. Spawn Engineers in parallel with the Agent tool (up to `max_engineers`)
4. Risk evaluation with `evaluate_risk`
5. If LOW/MEDIUM, auto-merge with `merge_pr`, passing `worktree_path`/`branch` so the worktree is cleaned up automatically (skip HIGH with comment)
6. Return to step 1

---

## MCP Tools

| Tool | Description |
|---|---|
| `list_issues` | Returns a list of open Issues (supports multi-repo mode) |
| `assign_issue` | Marks an Issue as in-progress by adding a label and assigning |
| `create_worktree` | Creates a branch and git worktree for an Issue |
| `evaluate_risk` | Returns LOW/MEDIUM/HIGH based on PR change volume and impact area |
| `merge_pr` | Merges after CI passes; posts a risk comment, records a lesson, and removes the worktree/branch when `worktree_path`/`branch` are provided |
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
| `now` | Returns the current wall-clock time (RFC3339), authoritative for cadence tracking |
| `get_loop_state` | Returns the cadence timestamps (`pr_comments_since`, `issue_comments_since`, `requirements_sweep_since`) and `hermit run` liveness fields (`last_success_tick`, `consecutive_failures`) from `.hermit/superintendent-state.json` |
| `update_loop_state` | Updates one or more of the three cadence timestamps in `.hermit/superintendent-state.json`; the only supported way to write that file besides `hermit run` itself |
| `run_requirements_sweep` | Reconciles REQUIREMENTS.md against test results, opening Issues for unimplemented/regressed requirements |

### Risk Evaluation Criteria

| Condition | Level |
|---|---|
| 20+ changed files / 500+ changed lines / changes in `cmd/`, `go.mod`, `.github/`, or HERMIT's own control plane (`internal/risk/`, `internal/permissions/`, `internal/readiness/`, `harness.toml`, `.claude/`, `CLAUDE.md`) | HIGH |
| 10+ changed files / 200+ changed lines / changes in `internal/` | MEDIUM |
| Otherwise | LOW |

A PR touching only one of the control-plane paths above is always HIGH, even a single-file, single-line change, and takes priority over the broader `internal/` MEDIUM match. This is deliberate: these paths are the mechanisms that constrain HERMIT itself (including `evaluate_risk`'s own logic and the `harness.toml` `[risk]` section that can override `high_paths`), so a change to them can never be safely self-certified as auto-mergeable — it always needs a human to look at it. The list intentionally includes itself (`internal/risk/`, `harness.toml`) so that a PR attempting to remove a path from `high_paths` is itself HIGH.

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
superintendent = "claude-sonnet-5"   # model used for the Superintendent role
engineer       = "claude-sonnet-5"   # model used for Engineer roles

# [risk]
# high_paths            = ["cmd/", "go.mod", ".github/", "internal/risk/", "internal/permissions/", "internal/readiness/", "harness.toml", ".claude/", "CLAUDE.md"]  # HIGH risk when a changed file matches one of these prefixes; defaults include HERMIT's own control-plane paths so they're always isolated from auto-merge
# medium_paths          = ["internal/"]                   # MEDIUM risk when a changed file matches one of these prefixes
# high_file_threshold   = 20    # HIGH risk when this many or more files changed
# high_line_threshold   = 500   # HIGH risk when this many or more lines changed (additions + deletions)
# medium_file_threshold = 10    # MEDIUM risk when this many or more files changed
# medium_line_threshold = 200   # MEDIUM risk when this many or more lines changed
# require_human_approval = false  # "warm-up mode": when true, merge_pr always blocks auto-merge regardless of risk level, until a human passes force: true

# [readiness]
# min_body_length                = 40                    # minimum non-whitespace chars required in the Issue body
# skip_acceptance_criteria_check = false                  # if true, don't require an "Acceptance Criteria" / "受け入れ条件" section
# label                          = "needs-clarification"  # label applied to Issues judged not ready; also excludes them from list_issues

# [security]
# trusted_author_associations = ["OWNER", "MEMBER", "COLLABORATOR"]  # default shown; other possible values: "CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "NONE"

# [notification]
# webhook_url = "https://hooks.slack.com/services/..."  # Slack, Discord, or generic webhook
# type        = "slack"   # "slack" | "discord" | "generic" (auto-detected from URL if omitted)

# [run]
# failure_notify_threshold = 3  # `hermit run`: consecutive failed passes before a webhook notification (default: 3)
```

**Pass `GITHUB_TOKEN` as an environment variable. Do not write it in `harness.toml`.**

### Rate Limit Handling

HERMIT automatically monitors the GitHub API rate limit before each operation. When remaining calls drop to `rate_limit_threshold` (default: 10), it waits until the limit resets. If the reset time is more than 10 minutes away, the current cycle is skipped rather than blocking.

### Trigger Comment Mode

When `trigger_comment` is set in `harness.toml`, the Superintendent will only pick up Issues that have at least one comment containing the specified string (case-insensitive). This lets you control which Issues HERMIT handles without relying solely on labels or assignment.

Example: set `trigger_comment = "/hermit"` and comment `/hermit` on any Issue you want HERMIT to process.

### Issue Readiness Checks

Before an Issue reaches the Superintendent's queue, `list_issues` runs a deterministic (non-LLM) readiness check against each Issue body — no guessing whether the requirements are clear enough, and no relying on the model to remember to ask. An Issue is judged **not ready** when:

- the body is empty,
- the body is shorter than `min_body_length` (default: 40 non-whitespace characters), or
- the body has no acceptance-criteria-like section (an "Acceptance Criteria" / "受け入れ条件" heading), unless `skip_acceptance_criteria_check = true`.

When an Issue is judged not ready, HERMIT:

1. Posts a structured hearing comment asking for the four things needed to safely implement it: purpose (目的), scope (スコープ), acceptance criteria (受け入れ条件), and explicit non-goals (やらないこと). This comment is only ever posted once per Issue — a hidden marker is used to detect it on later cycles.
2. Applies the `needs-clarification` label (configurable via `label`).
3. Excludes the Issue from the `list_issues` result until a human removes the label (or triggers a fresh check, e.g. by posting the trigger comment).

Once the requirements are filled in and the label is removed, the Issue returns to the normal queue on the next cycle.

### Trusted Issue Authors (`[security]`)

HERMIT can run against public repositories, where anyone can open an Issue. Since the Superintendent hands Issue bodies to an Engineer that runs locally with broad tool access (including `Bash(*)`), an Issue from an untrusted stranger must never be treated as an instruction.

To prevent this, `list_issues` (`ListOpenIssues` / `ListAllIssues`) filters every Issue by the GitHub-computed `author_association` between its author and the repository, keeping only Issues from authors in the `trusted_author_associations` allowlist:

```toml
[security]
trusted_author_associations = ["OWNER", "MEMBER", "COLLABORATOR"]  # default shown
```

- **Default and fallback**: `OWNER`, `MEMBER`, and `COLLABORATOR` only. This is applied both when `[security]` is omitted from `harness.toml` entirely and when `trusted_author_associations` is present but left empty — there is no configuration that resolves to "allow everyone." `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, and `NONE` (the associations any GitHub account, including a first-time stranger, can have on a public repo) are excluded by default.
- **Custom allowlist**: set `trusted_author_associations` to any subset (or superset) of GitHub's `author_association` values to widen or narrow the allowlist for your project.
- **Visibility**: Issues excluded by this filter are never silently dropped — each exclusion is logged (`security: excluding issue #N in owner/repo ...: author_association="..." is not in the trusted allowlist`) so an operator watching HERMIT's logs can notice. Excluded Issues also never reach `assign_issue` / `create_worktree`, since the Superintendent only acts on what `list_issues` returns.

### Model Selection

The `[model]` section lets you specify which Claude model each role uses. You can also apply a preset with `hermit use`:

```sh
hermit use claude        # Sonnet for both Superintendent and Engineer (balanced)
hermit use claude-cheap  # Sonnet for Superintendent, Haiku for Engineers (cost-optimized)
```

### Risk Policy

The `[risk]` section controls how `evaluate_risk` and `merge_pr` classify a PR as LOW, MEDIUM, or HIGH risk: which path prefixes are considered high/medium risk, and the file-count/line-count thresholds for each level. Any field left unset falls back to HERMIT's built-in default (shown commented out above), so omitting `[risk]` entirely preserves the original hardcoded behavior. This is especially useful for non-Go projects or repos with a different directory layout than `cmd/`, `internal/`, `go.mod`. In multi-repo mode, each `[[repos]]` entry may define its own `[repos.risk]` sub-table to override `[risk]` for that repo only (see below).

#### Warm-Up Mode (`require_human_approval`)

Structural risk heuristics (changed file count, line count, risky paths) can underestimate small-but-critical changes — a one-line fix to auth logic or error handling can come out LOW and auto-merge, while a large mechanical rename gets flagged HIGH. Setting `require_human_approval = true` in `[risk]` puts HERMIT into "warm-up mode": `merge_pr` always blocks auto-merge — for LOW, MEDIUM, and HIGH risk PRs alike — via the same gate already used for HIGH-risk PRs, until a human reviews the PR and re-runs `merge_pr` with `force: true`. It does not change `evaluate_risk`'s own computed level or reasons; it only changes `merge_pr`'s gating decision. This is a single global flag with no per-repo override, even in multi-repo mode — it applies uniformly across every `[[repos]]` entry regardless of any `[repos.risk]` sub-table.

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

# [repos.risk]                 # optional: overrides the top-level [risk] section for repo-two only
# high_file_threshold = 10
```

When `[[repos]]` is present, `list_issues` queries all configured repositories and returns issues from all of them in a single call.

---

## Pausing, Resuming, and Quitting Autonomous Operation

```sh
hermit pause    # pause autonomous operation (creates .hermit-paused)
hermit resume   # resume autonomous operation (removes .hermit-paused)
hermit quit     # terminate the /loop entirely (creates .hermit-quit)
hermit status   # check current state (running / paused / quit requested)
```

The Superintendent checks for `.hermit-paused` at the start of each cycle. Running `hermit pause` stops it after the current cycle completes; `hermit resume` resumes it immediately.

`hermit pause` / `hermit resume` only suspend and resume the Issue-processing part of the cycle — the underlying `/loop` (and its recurring `ScheduleWakeup` reschedule) keeps running, waking up every cycle to re-check the pause flag. Use `hermit quit` (or the `/hermit-quit` skill) when you want to stop the `/loop` itself: it creates `.hermit-quit`, and the Superintendent cycle checks for it first, before the pause check. When present, the cycle stops without calling `ScheduleWakeup` again, ending the loop for good. Unlike pause, quitting is **not** resumable with `hermit resume` — start `/hermit` again to begin a fresh loop.

| | `hermit pause` | `hermit quit` |
|---|---|---|
| Flag file | `.hermit-paused` | `.hermit-quit` |
| Effect | Suspends Issue processing; `/loop` keeps ticking | Stops the `/loop` itself (no more `ScheduleWakeup`) |
| Resumable? | Yes, via `hermit resume` | No — run `/hermit` again to restart |

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
hermit run       # Start the Superintendent tick loop as a standalone long-lived process (no Claude Code session needed)
hermit install   # Register MCP server via `claude mcp add` and install slash commands
hermit init      # Initialize a project (generate harness.toml, CLAUDE.md, issue template, settings)
hermit pause     # Pause autonomous operation (resumable)
hermit resume    # Resume autonomous operation
hermit quit      # Terminate the Superintendent /loop entirely (not resumable)
hermit status    # Show autonomous operation status (running / paused / quit requested)
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
- `.claude/settings.json` sandbox configuration (warnings, non-fatal — see "Sandboxing the Engineer" above): `sandbox.enabled` is true, `allowUnsandboxedCommands` is false, `sandbox.excludedCommands` is empty

Exits with a non-zero status if any check fails. The sandbox checks are warnings and never cause a non-zero exit on their own, so `hermit doctor` still passes on projects initialized before the sandbox recommendation existed.

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

## Running HERMIT Continuously

`hermit run` (see "Alternative to Step 2" above) is a plain foreground process: it runs until it receives SIGINT/SIGTERM, a `.hermit-quit` file appears, or it's killed. To keep it running unattended across reboots, logouts, or crashes, use your platform's normal process-supervision tooling — HERMIT intentionally does not generate or install any of these for you (see REQ-019/REQ-014: HERMIT stays a thin toolbox, not a process manager).

### systemd (Linux)

Create a user service, e.g. `~/.config/systemd/user/hermit-run.service`:

```ini
[Unit]
Description=HERMIT Superintendent loop

[Service]
WorkingDirectory=/path/to/your-project
ExecStart=/path/to/hermit run
Restart=on-failure
Environment=GITHUB_TOKEN=...

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now hermit-run.service
journalctl --user -u hermit-run -f   # follow logs
```

### launchd (macOS)

Create `~/Library/LaunchAgents/com.hermit.run.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.hermit.run</string>
  <key>ProgramArguments</key>
  <array>
    <string>/path/to/hermit</string>
    <string>run</string>
  </array>
  <key>WorkingDirectory</key><string>/path/to/your-project</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/tmp/hermit-run.log</string>
  <key>StandardErrorPath</key><string>/tmp/hermit-run.err</string>
</dict>
</plist>
```

```sh
launchctl load ~/Library/LaunchAgents/com.hermit.run.plist
```

### Windows Service

Use a lightweight wrapper such as [WinSW](https://github.com/winsw/winsw) or [NSSM](https://nssm.cc/) to register `hermit.exe run` (with `WorkingDirectory` set to your project) as a Windows service — HERMIT does not register itself as one.

### tmux / screen

The simplest option for a single long-running session:

```sh
tmux new -d -s hermit 'cd /path/to/your-project && hermit run'
tmux attach -t hermit   # to check on it later
```

### Docker

```dockerfile
FROM golang:1-alpine AS build
# ... build the hermit binary ...

FROM alpine
RUN apk add --no-cache git github-cli nodejs npm \
 && npm install -g @anthropic-ai/claude-code
COPY --from=build /out/hermit /usr/local/bin/hermit
WORKDIR /project
ENTRYPOINT ["hermit", "run"]
```

```sh
docker run -d --name hermit-run \
  -e GITHUB_TOKEN=... -e ANTHROPIC_API_KEY=... \
  -v /path/to/your-project:/project \
  your-hermit-image
docker logs -f hermit-run
```

---

## License

MIT
