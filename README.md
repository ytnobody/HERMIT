# HERMIT

**Harness for Engineer Role Management via Interactive Tasks**

Claude Code のネイティブ機能（Agent ツール・MCP）を活用したシンプルなマルチエージェント開発自動化ハーネス。

GitHub Issue を自動的に拾い、Agentを並列起動して実装・PR作成・マージまでを自律的に回します。

---

## 設計思想

> **「Claude Code が主役。HERMIT はドメイン操作の道具箱に徹する。」**

- AI推論・オーケストレーション・コンテキスト管理は **すべて Claude Code に委譲**
- HERMIT は GitHub/Git 操作の薄いラッパーを **MCP サーバー** として提供するだけ
- コード量 ~700 行（参考: 同等の Go バイナリ実装では ~5,000 行）

---

## インストール

```sh
curl -sSL https://raw.githubusercontent.com/ytnobody/hermit/main/install.sh | sh
```

`~/.local/bin/hermit` にバイナリを配置し、Claude Code の MCP サーバーとして自動登録します。

### 前提条件

- [Claude Code](https://claude.ai/code) がインストールされていること
- [gh CLI](https://cli.github.com/) で認証済みであること（`gh auth login`）、または `GITHUB_TOKEN` 環境変数が設定されていること
- `git` コマンドが使えること

---

## プロジェクトへの導入

```sh
cd your-project
hermit init
```

対話形式で以下を入力します：

| 項目 | 説明 |
|---|---|
| GitHub owner | org 名またはユーザー名 |
| GitHub repo | リポジトリ名 |
| Language | `ja` または `en`（Claude への指示言語） |
| Max Engineers | 並列起動する Engineer の最大数（デフォルト: 4） |

生成されるファイル：

- `harness.toml` — プロジェクト設定（チームで共有）
- `CLAUDE.md` — Superintendent / Engineer のロール定義

`CLAUDE.md` の「コーディング規約」セクションをプロジェクトに合わせて編集してください。

---

## 使い方

HERMIT の自動化は **`hermit serve`（MCP サーバー）** と **Claude Code（Superintendent）** の2プロセスが協調して動きます。

```
hermit serve  ───MCP ツールを提供───▶  Claude Code (Superintendent)
                                          ↓ list_issues
                                          ↓ assign_issue
                                          ↓ Agent spawn → Engineer × N
                                          ↓ evaluate_risk
                                          ↓ merge_pr
                                          ↓ close_worktree
                                          ↓ （繰り返し）
```

### ステップ 1: MCP サーバーを起動する（ターミナル A）

```sh
hermit serve
```

`GITHUB_TOKEN` 環境変数が設定されていない場合は `gh auth token` で自動取得します。事前に `gh auth login` で認証しておいてください。

このプロセスは常駐させたままにします。

### ステップ 2: プロジェクトで Claude Code を起動する（ターミナル B）

```sh
cd your-project   # hermit init を実行したディレクトリ
claude
```

`CLAUDE.md` が自動的に読み込まれ、追加の指示なしに Superintendent として動作を開始します。Issue がなければ 60 秒待機して再確認するサイクルを繰り返します。

### 長時間・継続運転する場合

Claude Code はコンテキスト上限に達するとループが止まります。`/loop` と `/hermit` カスタムコマンドを組み合わせると、定期的にコンテキストをリセットしながら継続できます。

Claude Code のプロンプトで以下を入力してください：

```
/loop 270s /hermit
```

これにより 270 秒ごとに `/hermit`（Superintendent 1サイクル）が再起動し、コンテキスト枯渇を防ぎながら Issue を監視し続けます。

`/hermit` は `.claude/commands/hermit.md` で定義されたプロジェクトカスタムコマンドです。1回だけ実行したい場合は単体で使えます：

```
/hermit
```

### Superintendent のサイクル

1. `list_issues` で未着手 Issue を取得
2. `assign_issue` で処理中にマーク
3. Agent ツールで Engineer を並列起動（最大 `max_engineers` 本）
4. `evaluate_risk` でリスク判定
5. LOW/MEDIUM なら `merge_pr` で自動マージ（HIGH はスキップしてコメント投稿）
6. `close_worktree` でワークツリーを掃除
7. 1 に戻る

---

## MCP ツール一覧

| ツール | 説明 |
|---|---|
| `list_issues` | 未着手の Issue 一覧を返す |
| `assign_issue` | Issue をラベル付与・アサインして処理中にマーク |
| `create_worktree` | `hermit/issue-{N}` ブランチと `/tmp/hermit-{N}` ワークツリーを作成 |
| `evaluate_risk` | PR の変更量・影響範囲から LOW/MEDIUM/HIGH を判定 |
| `merge_pr` | CI 通過確認後にマージ（HIGH リスクは拒否してコメント投稿） |
| `close_worktree` | ワークツリーとブランチを削除 |

### リスク判定基準

| 条件 | レベル |
|---|---|
| 変更ファイル 20+ / 変更行 500+ / `cmd/` `go.mod` `.github/` に変更 | HIGH |
| 変更ファイル 10+ / 変更行 200+ / `internal/` に変更 | MEDIUM |
| 上記以外 | LOW |

---

## 設定ファイル (`harness.toml`)

```toml
[github]
owner = "your-org"
repo  = "your-repo"

[agent]
max_engineers = 4   # 並列 Engineer の最大数
language      = "ja"  # "ja" | "en"
```

**`GITHUB_TOKEN` は環境変数で渡します。`harness.toml` には書かないでください。**

---

## 自動運転の一時停止・再開

```sh
hermit pause    # 自動運転を一時停止（.hermit-paused を作成）
hermit resume   # 自動運転を再開（.hermit-paused を削除）
hermit status   # 現在の状態を確認（running / paused）
```

Superintendent は各サイクルの先頭で `.hermit-paused` の有無を確認します。`hermit pause` を実行すると現在のサイクルが完了した後に停止し、`hermit resume` で即座に再開します。

---

## サブコマンド一覧

```
hermit serve    # MCP サーバーを起動（stdio）
hermit install  # ~/.claude/settings.json に MCP サーバーを登録
hermit init     # プロジェクトを初期化（harness.toml + CLAUDE.md 生成）
hermit pause    # 自動運転を一時停止
hermit resume   # 自動運転を再開
hermit status   # 自動運転の状態を表示
```

---

## ライセンス

MIT
