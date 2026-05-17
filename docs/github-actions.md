# GitHub Actions での HERMIT 自動運転

HERMIT は GitHub Actions ワークフローから ANTHROPIC_API_KEY を使って Claude Code CLI を動かすことができます。

## 前提条件

- リポジトリの Secrets に `ANTHROPIC_API_KEY` を登録済みであること
- リポジトリの Secrets に `GITHUB_TOKEN`（または PAT）を登録済みであること
- `harness.toml` と `CLAUDE.md` がリポジトリに含まれていること

## モデルプリセット

`hermit use <preset>` コマンドで `harness.toml` の `[model]` セクションを切り替えられます。

| プリセット名  | superintendent         | engineer               | 用途                     |
|--------------|------------------------|------------------------|--------------------------|
| `claude`      | claude-sonnet-4-5      | claude-sonnet-4-5      | バランス重視（デフォルト） |
| `claude-cheap`| claude-sonnet-4-5      | claude-haiku-4-5       | コスト最適化             |

```bash
# ローカルでプリセットを適用してからコミットする
hermit use claude-cheap
git add harness.toml && git commit -m "chore: switch to claude-cheap preset"
```

## ワークフロー例

```yaml
name: HERMIT Autonomous Loop

on:
  schedule:
    # 毎時 0 分に起動（GitHub Actions の cron は UTC）
    - cron: "0 * * * *"
  workflow_dispatch:

jobs:
  hermit:
    runs-on: ubuntu-latest
    timeout-minutes: 55   # 1 サイクル分の余裕を持たせる

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

### ポイント

- `ANTHROPIC_API_KEY` を設定すると Claude Code CLI は API キー認証を使います（`gh auth login` 不要）。
- `hermit install` は `~/.claude/settings.json` に MCP サーバーを登録します。Actions ランナーは都度クリーンなため毎回実行が必要です。
- `--dangerously-skip-permissions` を指定することで Claude Code が確認プロンプトなしに自律動作します。
- `timeout-minutes` は GitHub Actions の最大実行時間（6 時間）以内に収まるよう設定してください。

## ANTHROPIC_API_KEY を使う場合のモデル指定

Claude Code CLI は `ANTHROPIC_API_KEY` が設定されている場合、`--model` フラグで利用するモデルを指定できます。
`harness.toml` の `[model]` セクションに記述したモデル名をそのまま渡すことができます。

```bash
# harness.toml から値を取得する例
SUPER_MODEL=$(grep superintendent harness.toml | awk -F'"' '{print $2}')
ENG_MODEL=$(grep engineer harness.toml | awk -F'"' '{print $2}')
```

## セキュリティ上の注意

- `ANTHROPIC_API_KEY` は必ずリポジトリの **Secrets** に保存してください（`.env` ファイルをコミットしないこと）。
- フォークからのプルリクエストには Secrets が渡されません。`pull_request_target` トリガーを使う場合は十分に注意してください。
