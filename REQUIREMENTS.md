# HERMIT 要件定義書

本書は HERMIT (Harness for Engineer Role Management via Interactive Tasks) の要件定義書です。
設計文書 [HERMIT.md](./HERMIT.md) に記述された内容を「あるべき状態 (desired state)」として要件化したものであり、Issue #124 への回答として整備されました。

## 本書のフォーマットについて

- 各要件は `## REQ-xxx: <タイトル>` 形式の見出しブロックで記述します。HERMIT の要件リコンサイルスイープ (`run_requirements_sweep`) はこの形式をパースします。
- 各ブロックには次のフィールドを記述できます。
  - `- 受け入れ条件:` — その要件が満たされていると判断できる検証可能な条件
  - `- verify: test | manual` — `test` (既定) はスイープが `harness.toml` の `[requirements].test_command` を実行して判定、`manual` は自動検証対象外
- `verify: test` の要件を自動検証するには、`harness.toml` に例えば次のような設定を追加します(テスト関数名は `TestREQ001_...` のように REQ ID からハイフンを除いた接頭辞で命名する規約)。

  ```toml
  [requirements]
  test_command = "go test ./... -run \"^Test$(printf '%s' '{req_id}' | tr -d '-')\" -v"
  ```

---

## 1. 目的

HERMIT は、Claude Code のネイティブ機能 (Agent tool / MCP) を活用したシンプルなマルチエージェント開発自動化ハーネスである。
GitHub Issue を入力として、Superintendent (監督) と Engineer (実装者) の役割分担により、実装からレビュー・マージまでを自律的に進める。

## REQ-001: 設計原則 — HERMIT はドメイン操作の薄いツールボックスに徹する

「Claude Code が主役。HERMIT はドメイン操作のためのツールボックスにすぎない」を設計原則とする。

- AI の推論・オーケストレーション・コンテキスト管理は Claude Code に完全に委譲する
- HERMIT は GitHub / Git 操作の薄いラッパーを MCP サーバとして提供するのみとする
- Superintendent / Engineer の役割定義は CLAUDE.md に記述する
- 受け入れ条件: HERMIT の Go 実装に、エージェンティックループ・LLM API 呼び出し・コンテキスト管理の再実装が含まれていないこと
- verify: manual

## REQ-002: MCP サーバとしてのツール提供

`hermit serve` は stdio ベースの MCP サーバとして起動し、少なくとも以下のツールを提供する:
`list_issues`, `assign_issue`, `create_worktree`, `evaluate_risk`, `merge_pr`, `add_issue_comment`, `close_issue`, `list_prs`, `get_lessons`, `get_config`, `review_pr`, `notify`。

- 受け入れ条件: MCP サーバのツール登録一覧に上記全ツールが含まれ、各ツールが HERMIT.md 記載の入出力スキーマに従うこと
- verify: test

## REQ-003: list_issues は未着手のオープン Issue を返す

`list_issues` は、まだ着手されていないオープンな GitHub Issue の一覧 (number / title / body / labels) を返す。オプションでラベルによる絞り込みができる。

- 受け入れ条件: in-progress 相当の Issue が結果から除外され、`label` 指定時は該当ラベルの Issue のみが返ること
- verify: test

## REQ-004: assign_issue による着手宣言

`assign_issue` は指定 Issue を「作業中」として印付けする (ラベル付与 + 指定 assignee へのアサイン)。

- 受け入れ条件: 実行後、対象 Issue にラベルとアサインが付与され、`{"success": true}` が返ること
- verify: test

## REQ-005: create_worktree による Issue 単位の作業環境分離

`create_worktree` は Issue ごとにブランチと git worktree を作成し、`worktree_path` と `branch` を返す。`base_branch` を起点とし、ブランチ名は `hermit/` 接頭辞に Issue 番号を含む命名とする。これにより Engineer は互いに独立して並列作業できる。

- 受け入れ条件: 実行後、指定 base_branch から分岐した新規ブランチとそれをチェックアウトした worktree がファイルシステム上に存在し、返却値の path / branch と一致すること
- verify: test

## REQ-006: evaluate_risk による PR リスク評価

`evaluate_risk` は PR の変更量・影響範囲に基づき LOW / MEDIUM / HIGH のリスクレベルと理由 (`reasons`) を返す。判定基準は以下とする。

| 条件 | リスクレベル |
|---|---|
| 変更ファイル 20 以上 / 変更行 500 以上 / `cmd/`・`go.mod`・`.github/` の変更 | HIGH |
| 変更ファイル 10 以上 / 変更行 200 以上 / `internal/` コアの変更 | MEDIUM |
| 上記以外 | LOW |

- 受け入れ条件: 上記の各しきい値条件を与えたとき、対応するリスクレベルが返ること (境界値を含むテストで検証)
- verify: test

## REQ-007: merge_pr の CI ゲーティングと HIGH リスク拒否

`merge_pr` は CI がパスした PR のみをマージする。HIGH リスクの PR はマージを拒否し、その旨をコメントとして PR に投稿する。マージ不可の場合は `{"merged": false, "reason": "..."}` で理由を返す。

- 受け入れ条件: CI 失敗中または HIGH リスクの PR に対して merge_pr がマージを実行せず、reason 付きで拒否を返すこと
- verify: test

## REQ-008: merge_pr 成功後の worktree 自動クリーンアップ

`merge_pr` に `worktree_path` と `branch` が渡された場合、マージ成功後に該当 worktree とブランチを削除する。

- 受け入れ条件: マージ成功後に worktree ディレクトリと作業ブランチが残存しないこと (マージ失敗時は削除しないこと)
- verify: test

## REQ-009: Superintendent ロール — Issue 駆動の自律サイクル

Superintendent は CLAUDE.md に定義されたサイクルに従い、以下を繰り返す。

1. `list_issues` でオープン Issue を取得する (なければ待機して再試行)
2. Issue を `assign_issue` で着手中にし、`create_worktree` で作業環境を用意する
3. Agent tool で Engineer を並列に生成し、Issue 番号・タイトル・本文・worktree_path・branch を渡す
4. 全 Engineer の完了を待ち、PR に対して CI 確認とリスク評価を行う
5. LOW / MEDIUM は `merge_pr` でマージ、HIGH はレビューコメントを残して人間の判断を待つ

- 受け入れ条件: `hermit init` が生成する CLAUDE.md テンプレートに上記サイクルが含まれること
- verify: manual

## REQ-010: Engineer ロール — worktree 内での独立した実装フロー

Engineer は Superintendent から受け取った Issue を、指定された worktree 内で実装する。

1. 指定 `worktree_path` に移動して作業する
2. Issue の要件を実装し、テストを書いてパスさせる
3. 指定 `branch` でコミットし PR を作成する
4. 完了時に `worktree_path` / `branch` / PR 番号を Superintendent に報告する

- 受け入れ条件: `hermit init` が生成する CLAUDE.md テンプレートに上記フローが含まれること
- verify: manual

## REQ-011: Engineer の並列数上限

Superintendent が同時に生成する Engineer の数は `harness.toml` の `[agent] max_engineers` (既定: 4) を上限とする。上限を超える Issue は次サイクルに繰り越す。

- 受け入れ条件: `get_config` が harness.toml の max_engineers 値を返し、CLAUDE.md テンプレートが並列上限としてこの値を参照していること
- verify: test

## REQ-012: harness.toml による設定と GITHUB_TOKEN の非保存

設定は `harness.toml` で管理する (`[github] owner / repo`、`[agent] max_engineers / language` など)。`harness.toml` はチーム共有を意図し `.gitignore` に追加しない。GitHub トークンは環境変数 `GITHUB_TOKEN` からのみ受け取り、**toml には決して書かない**。

- 受け入れ条件: 設定ローダーが GITHUB_TOKEN を環境変数から読むこと。harness.toml およびそのテンプレートにトークン項目が存在しないこと
- verify: test

## REQ-013: ワンライナーによるインストールとセットアップ

セットアップは単一コマンドで完結する。

- `install.sh` は OS / アーキテクチャを検出し、最新リリースのバイナリを checksum 検証のうえ配置し、`hermit install` を実行する
- `hermit install` は Claude Code の設定 (`mcpServers`) に hermit MCP サーバを登録する
- `hermit init` はプロジェクトディレクトリで対話的に `harness.toml` を生成し、テンプレートから `CLAUDE.md` を生成する
- 受け入れ条件: クリーンな環境で `curl | sh` → `hermit init` の手順のみで Superintendent を起動できる状態になること
- verify: manual

## REQ-014: 非目標 (Non-Goals)

MADFLOW の教訓 (HERMIT.md §1, §9) に基づき、HERMIT では以下を行わない。

- Go バイナリが Claude Code をサブプロセスとして起動・管理する「外側から包む」アーキテクチャ
- エージェンティックループ・LLM API 呼び出しの Go による再実装
- チャットログ (ファイル) 経由のエージェント間通信 — Agent tool の入出力で代替する
- タイマーによるコンテキストリセット等のコンテキスト/プロセスライフサイクル管理 — Claude Code に委譲する
- 受け入れ条件: 上記に該当する実装がコードベースに追加されていないこと (レビューで担保)
- verify: manual
