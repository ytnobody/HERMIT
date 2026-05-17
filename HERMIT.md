# HERMIT — 設計書

**HERMIT** (Harness for Engineer Role Management via Interactive Tasks) は、Claude Code のネイティブ機能（Agent ツール・MCP）を活用したシンプルなマルチエージェント開発自動化ハーネスです。

---

## 1. 背景と設計思想

### MADFLOWからの学び

MADFLOWは Go バイナリが Claude Code をサブプロセスとして起動・管理する「外から内」の構造を持つ。これにより以下の問題が生じた：

- `claude -p` / ClaudeStreamProcess の起動コストとプロンプトキャッシュ非効率
- AgenticループをGoで再実装する複雑さ（`anthropic_api.go` など）
- コンテキストリセット・チャットログ・プロセス生命周期管理の重複実装

### HERMITの原則

> **「Claude Codeが主役。HERMITはドメイン操作の道具箱に徹する。」**

- AI推論・オーケストレーション・コンテキスト管理はすべてClaude Codeに委譲
- HERMITはGitHub/Git操作の薄いラッパーをMCPサーバーとして提供するだけ
- SuperintendentとEngineerのロール定義はCLAUDE.mdで記述
- ワンコマンドで導入完了

---

## 2. アーキテクチャ概要

```
┌─────────────────────────────────────────────────┐
│  Claude Code (Superintendent セッション)          │
│                                                 │
│  CLAUDE.md で定義されたロールに従って動作           │
│                                                 │
│  ┌──────────────────────────────────────────┐   │
│  │  Agentツールで Engineer を並列spawn       │   │
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

## 3. ディレクトリ構成

```
hermit/
├── cmd/hermit/
│   └── main.go              # install / init / serve サブコマンド
├── internal/
│   ├── mcp/
│   │   ├── server.go        # stdio MCP サーバー起動
│   │   └── tools.go         # 6ツールのハンドラ定義
│   ├── github/
│   │   └── client.go        # GitHub REST API クライアント
│   ├── git/
│   │   └── worktree.go      # git worktree 操作
│   └── risk/
│       └── evaluator.go     # PRリスク判定ロジック
├── templates/
│   ├── CLAUDE.md.tmpl       # Superintendent / Engineer ロール定義テンプレート
│   └── harness.toml.tmpl    # 設定ファイルテンプレート
├── install.sh               # ワンライナーインストールスクリプト
├── go.mod
└── README.md
```

---

## 4. MCPツール仕様

HERMITが提供するMCPツールは以下の6本。これ以上は追加しない。

### `list_issues`

未着手の GitHub Issue 一覧を返す。

```json
// 入力
{ "label": "string (optional)" }

// 出力
[{ "number": 42, "title": "...", "body": "...", "labels": [...] }]
```

### `assign_issue`

Issue を処理中としてマークする（ラベル付与 + アサイン）。

```json
// 入力
{ "issue_number": 42, "assignee": "string" }

// 出力
{ "success": true }
```

### `create_worktree`

Issue 用のブランチとgitワークツリーを作成する。

```json
// 入力
{ "issue_number": 42, "base_branch": "develop" }

// 出力
{ "worktree_path": "/path/to/worktree", "branch": "hermit/issue-42" }
```

### `evaluate_risk`

PR の変更量・影響範囲からリスクレベルを返す。

```json
// 入力
{ "pr_number": 123 }

// 出力
{ "level": "LOW|MEDIUM|HIGH", "reasons": ["..."] }
```

判定基準：

| 条件 | リスクレベル |
|---|---|
| 変更ファイル20以上 / 変更行500以上 / `cmd/` / `go.mod` / `.github/` に変更あり | HIGH |
| 変更ファイル10以上 / 変更行200以上 / `internal/` コア変更あり | MEDIUM |
| 上記以外 | LOW |

### `merge_pr`

CI通過確認後にPRをマージする。HIGH リスクの場合は拒否してコメントを投稿する。

```json
// 入力
{ "pr_number": 123 }

// 出力
{ "merged": true } | { "merged": false, "reason": "HIGH risk / CI failing / ..." }
```

### `close_worktree`

マージ完了後にワークツリーとブランチを削除する。

```json
// 入力
{ "worktree_path": "/path/to/worktree", "branch": "hermit/issue-42" }

// 出力
{ "success": true }
```

---

## 5. 設定ファイル (`harness.toml`)

```toml
[github]
owner = "your-org"
repo  = "your-repo"

[agent]
max_engineers = 4   # 並列Engineerの最大数
language      = "ja"  # "ja" | "en"
```

**GitHubトークンは `GITHUB_TOKEN` 環境変数で受け取る。tomlには書かない。**

---

## 6. CLAUDE.md テンプレート設計

`hermit init` が生成する CLAUDE.md は以下の2セクションで構成する。

### Superintendent セクション

```markdown
## あなたの役割: Superintendent

以下のサイクルを繰り返してください。

1. `list_issues` で未着手Issueを取得する
2. Issueがなければ60秒待機して1に戻る
3. Issueを `assign_issue` で処理中にマークする
4. 各IssueについてAgentツールでEngineerを起動する（最大 {{ max_engineers }} 並列）
   - EngineerへはIssue番号・タイトル・本文・ワークツリーパスを渡す
5. すべてのEngineerの完了を待つ
6. PRが作成されていれば `evaluate_risk` でリスク判定する
   - LOW/MEDIUM: `merge_pr` を実行する
   - HIGH: PRにコメントを投稿してスキップする
7. `close_worktree` でワークツリーを掃除する
8. 1に戻る
```

### Engineer セクション

```markdown
## あなたの役割: Engineer

Superintendentから受け取ったIssueを実装してください。

1. 指定されたワークツリーパスに移動して作業する
2. Issueの要件を実装する
3. テストを書いて通す
4. コミットしてPRを作成する
5. 完了したらSuperintendentに報告する

### コーディング規約
{{ project_coding_rules }}
```

---

## 7. インストール設計

### `install.sh` の処理フロー

```sh
#!/usr/bin/env sh
set -eu

# 1. OS/アーキテクチャ検出
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
# arm64 / amd64 に正規化

# 2. 最新リリースのバイナリURLを取得
VERSION=$(curl -sSL https://api.github.com/repos/ytnobody/hermit/releases/latest \
  | grep '"tag_name"' | cut -d'"' -f4)

# 3. バイナリとチェックサムをダウンロード
curl -sSL "https://github.com/ytnobody/hermit/releases/download/${VERSION}/hermit_${OS}_${ARCH}" \
  -o /tmp/hermit
curl -sSL "https://github.com/ytnobody/hermit/releases/download/${VERSION}/hermit_${OS}_${ARCH}.sha256" \
  -o /tmp/hermit.sha256

# 4. チェックサム検証
sha256sum -c /tmp/hermit.sha256

# 5. 配置 + 実行権限
install -m 755 /tmp/hermit ~/.local/bin/hermit

# 6. Claude Code への MCP 登録
hermit install
```

### `hermit install` の処理

`~/.claude/settings.json` の `mcpServers` に以下を追加：

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

### `hermit init` の処理（プロジェクトディレクトリで実行）

1. `harness.toml` をインタラクティブに生成（owner / repo / language / max_engineers を入力）
2. `CLAUDE.md` を `templates/CLAUDE.md.tmpl` から生成（harness.toml の値で展開）
3. `.gitignore` に `harness.toml` を**追加しない**（チームで共有する想定）
4. 完了メッセージと次のステップを表示

---

## 8. 実装順序

依存関係の少ない順に実装する。

| ステップ | 内容 | 依存 |
|---|---|---|
| 1 | `go.mod` + プロジェクト雛形 | なし |
| 2 | `internal/github/client.go` | なし |
| 3 | `internal/git/worktree.go` | なし |
| 4 | `internal/risk/evaluator.go` | github |
| 5 | `internal/mcp/tools.go` | github, git, risk |
| 6 | `internal/mcp/server.go` | tools |
| 7 | `cmd/hermit/main.go` (serve) | mcp |
| 8 | `cmd/hermit/main.go` (install) | なし |
| 9 | `cmd/hermit/main.go` (init) | templates |
| 10 | `install.sh` | なし |
| 11 | `templates/` 作成 | なし |

---

## 9. MADFLOWとの対比

| 要素 | MADFLOW | HERMIT |
|---|---|---|
| オーケストレーター | Go バイナリ | Claude Code (CLAUDE.md) |
| AI呼び出し | サブプロセス / REST API | Claude Code ネイティブ |
| エージェント間通信 | チャットログ（ファイル） | Agent ツールの入出力 |
| コンテキストリセット | 8分タイマー（Go実装） | Claude Code に委譲 |
| コード量（概算） | ~5,000行 | ~500行 |
| インストール | `go install` + 手動設定 | `curl \| sh` 1本 |
