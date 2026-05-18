# Running HERMIT Autonomously with GitHub Actions

HERMIT can drive Claude Code CLI using `ANTHROPIC_API_KEY` from a GitHub Actions workflow.

## Prerequisites

- `ANTHROPIC_API_KEY` registered in repository Secrets
- `GITHUB_TOKEN` (or a PAT) registered in repository Secrets
- `harness.toml` and `CLAUDE.md` included in the repository

## Model Presets

Use `hermit use <preset>` to switch the `[model]` section in `harness.toml`.

| Preset Name   | superintendent         | engineer               | Use Case                  |
|--------------|------------------------|------------------------|---------------------------|
| `claude`      | claude-sonnet-4-5      | claude-sonnet-4-5      | Balanced (default)        |
| `claude-cheap`| claude-sonnet-4-5      | claude-haiku-4-5       | Cost-optimized            |

```bash
# Apply a preset locally and commit
hermit use claude-cheap
git add harness.toml && git commit -m "chore: switch to claude-cheap preset"
```

## Workflow Example

```yaml
name: HERMIT Autonomous Loop

on:
  schedule:
    # Run at minute 0 of every hour (GitHub Actions cron is UTC)
    - cron: "0 * * * *"
  workflow_dispatch:

jobs:
  hermit:
    runs-on: ubuntu-latest
    timeout-minutes: 55   # Allow enough time for one cycle

    env:
      ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install Claude Code CLI
        run: npm install -g @anthropic-ai/claude-code

      - name: Install hermit
        run: |
          curl -fsSL https://raw.githubusercontent.com/ytnobody/HERMIT/main/install.sh | bash
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"

      - name: Register hermit MCP server
        run: hermit install

      - name: Run hermit (one superintendent cycle)
        run: |
          claude --dangerously-skip-permissions \
            --model "$(grep superintendent harness.toml | awk -F'"' '{print $2}')" \
            -p "$(cat CLAUDE.md)"
```

### Key Points

- Setting `ANTHROPIC_API_KEY` causes Claude Code CLI to use API key authentication (no `gh auth login` needed).
- `hermit install` registers the MCP server in `~/.claude/settings.json`. Since Actions runners are clean each run, this must be executed every time.
- `--dangerously-skip-permissions` allows Claude Code to operate autonomously without confirmation prompts.
- Set `timeout-minutes` to stay within the GitHub Actions maximum execution time (6 hours).

## Specifying a Model When Using ANTHROPIC_API_KEY

When `ANTHROPIC_API_KEY` is set, Claude Code CLI can specify the model to use with the `--model` flag.
You can pass the model name written in the `[model]` section of `harness.toml` directly.

```bash
# Example: extract values from harness.toml
SUPER_MODEL=$(grep superintendent harness.toml | awk -F'"' '{print $2}')
ENG_MODEL=$(grep engineer harness.toml | awk -F'"' '{print $2}')
```

## Security Notes

- Always store `ANTHROPIC_API_KEY` in repository **Secrets** (never commit `.env` files).
- Secrets are not passed to pull requests from forks. Exercise caution if using the `pull_request_target` trigger.
