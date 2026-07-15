# HERMIT 要件定義書

本書は HERMIT (Harness for Engineer Role Management via Interactive Tasks) の要件定義書です。
設計文書 [HERMIT.md](./HERMIT.md) に記述された内容を「あるべき状態 (desired state)」として要件化し、あわせて現行コードベースに対する現状把握 (実装状況の棚卸し) を行ったものです。Issue #124 への回答として整備されました。

## 本書のフォーマットについて

- 各要件は `## REQ-xxx: <タイトル>` 形式の見出しブロックで記述します。HERMIT の要件リコンサイルスイープ (`run_requirements_sweep`) はこの形式をパースします。
- 各ブロックには次のフィールドを記述できます。
  - `- 受け入れ条件:` — その要件が満たされていると判断できる検証可能な条件
  - `- verify: test | manual` — `test` (既定) はスイープが `harness.toml` の `[requirements].test_command` を実行して判定、`manual` は自動検証対象外
  - `- 実装状況:` — 本書作成時点 (2026-07) のコードベース調査に基づく実装状態 (実装済み / 一部実装 / 未実装) と根拠ファイル。スイープのパース対象外の参考情報
- `verify: test` の要件を自動検証するには、`harness.toml` に例えば次のような設定を追加します(テスト関数名は `TestREQ001_...` のように REQ ID からハイフンを除いた接頭辞で命名する規約)。

  ```toml
  [requirements]
  test_command = "go test ./... -run \"^Test$(printf '%s' '{req_id}' | tr -d '-')\" -v"
  ```

---

## 現状把握サマリ (2026-07 時点)

全 14 要件のうち **実装済み 13 件 / 一部実装 1 件 (REQ-011) / 未実装 0 件**。設計文書 HERMIT.md の骨格はすべて実装されており、多くの領域で設計を超えて拡張されています。一方、HERMIT.md 自体が実装に追従しておらず、以下の差分 (設計文書の記述と実際の実装の乖離) があります。

| # | 差分 | 設計 (HERMIT.md) | 実装 (現状) |
|---|---|---|---|
| 1 | MCP ツール数 | 図中「6 ツール」、仕様は 12 ツール | 17 ツール (`internal/mcp/tools.go`)。`get_default_branch` / `get_issue_comments` / `check_ci_status` / `get_recent_pr_comments` / `run_requirements_sweep` が追加 |
| 2 | ブランチ命名 | `hermit/issue-42` | `<branch_prefix>/issue-<N>` (例: `hermit/ytnobody/issue-124`)。旧形式は後方互換で検出 (`internal/git/worktree.go`) |
| 3 | `get_config` の返却値 | `owner` / `repo` / `max_engineers` / `loop_interval` | `loop_interval` / `risk` / `model` (+`risk_overrides`) を返し、`owner` / `repo` / `max_engineers` は返さない (`internal/mcp/tools.go`) |
| 4 | `hermit install` の登録方法 | `~/.claude/settings.json` の `mcpServers` に直接追記 | 公式サポートの `claude mcp add` コマンド経由で登録 (`cmd/hermit/main.go`) |
| 5 | リリースバイナリ名 | `hermit_${OS}_${ARCH}` (アンダースコア) | `hermit-${OS}-${ARCH}` (ハイフン、`install.sh`) |
| 6 | ディレクトリ構成 | トップレベル `templates/` | `cmd/hermit/templates/` に配置し `go:embed` で埋め込み |
| 7 | 設定項目 | `[github] owner/repo`、`[agent] max_engineers/language` のみ | 加えて `rate_limit_threshold` / `default_branch` / `loop_interval` / `[model]` / `[readiness]` / `[risk]` / `[notification]` / `[requirements]` 等 (`cmd/hermit/main.go`) |

設計になかった主な拡張: Issue readiness 判定 (`internal/readiness`)、要件ヒアリング Issue と要件リコンサイルスイープ (`internal/requirements`)、CI 履歴 (`internal/cihistory`)、lessons スコアリング (`internal/lessons`)、通知 (`internal/notification`)、マルチリポジトリ対応、warm-up モード (`[risk].require_human_approval`)、`doctor` / `upgrade` / dry-run サブコマンド。

残ギャップ: `harness.toml` に `[requirements].test_command` が未設定のためリコンサイルスイープは現状スキップされる。また `verify: test` の各要件に対応する REQ-ID 命名のテストは未整備 (スイープ導入時に「テスト未実装」として Issue 化される想定)。

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
- 実装状況: 実装済み — Go 実装は MCP サーバ (`internal/mcp/`) と GitHub/Git/リスク評価等のドメイン操作 (`internal/github/`, `internal/git/`, `internal/risk/` ほか) のみで構成され、LLM API 呼び出し・エージェンティックループの実装は存在しない

## REQ-002: MCP サーバとしてのツール提供

`hermit serve` は stdio ベースの MCP サーバとして起動し、少なくとも以下のツールを提供する:
`list_issues`, `assign_issue`, `create_worktree`, `evaluate_risk`, `merge_pr`, `add_issue_comment`, `close_issue`, `list_prs`, `get_lessons`, `get_config`, `review_pr`, `notify`。

- 受け入れ条件: MCP サーバのツール登録一覧に上記全ツールが含まれ、各ツールが HERMIT.md 記載の入出力スキーマに従うこと
- verify: test
- 実装状況: 実装済み — `internal/mcp/tools.go` で上記 12 ツールすべてに加え、設計後に追加された `get_default_branch` / `get_issue_comments` / `check_ci_status` / `get_recent_pr_comments` / `run_requirements_sweep` の計 17 ツールを登録 (HERMIT.md 側が未追従。ただし `get_config` の返却値は設計と乖離 — REQ-011 参照)

## REQ-003: list_issues は未着手のオープン Issue を返す

`list_issues` は、まだ着手されていないオープンな GitHub Issue の一覧 (number / title / body / labels) を返す。オプションでラベルによる絞り込みができる。

- 受け入れ条件: in-progress 相当の Issue が結果から除外され、`label` 指定時は該当ラベルの Issue のみが返ること
- verify: test
- 実装状況: 実装済み — `internal/mcp/tools.go` の `list_issues` ハンドラ。設計を超えて、readiness 判定 (`internal/readiness`、情報不足 Issue には `needs-clarification` ラベルを付与し除外)、要件ヒアリング Issue (`requirements` ラベル) の除外、マルチリポジトリ対応、trigger_comment フィルタが追加されている

## REQ-004: assign_issue による着手宣言

`assign_issue` は指定 Issue を「作業中」として印付けする (ラベル付与 + 指定 assignee へのアサイン)。

- 受け入れ条件: 実行後、対象 Issue にラベルとアサインが付与され、`{"success": true}` が返ること
- verify: test
- 実装状況: 実装済み — `internal/mcp/tools.go` の `assign_issue` ハンドラ (`internal/github/client.go` 経由)。owner/repo 指定によるマルチリポジトリ対応が追加されている

## REQ-005: create_worktree による Issue 単位の作業環境分離

`create_worktree` は Issue ごとにブランチと git worktree を作成し、`worktree_path` と `branch` を返す。`base_branch` を起点とし、ブランチ名は `hermit/` 接頭辞に Issue 番号を含む命名とする。これにより Engineer は互いに独立して並列作業できる。

- 受け入れ条件: 実行後、指定 base_branch から分岐した新規ブランチとそれをチェックアウトした worktree がファイルシステム上に存在し、返却値の path / branch と一致すること
- verify: test
- 実装状況: 実装済み — `internal/git/worktree.go` の `CreateWorktree`。命名は設計の `hermit/issue-<N>` ではなく `<branch_prefix>/issue-<N>` (例: `hermit/ytnobody/issue-124`) に変更され、旧形式ブランチは後方互換で検出される

## REQ-006: evaluate_risk による PR リスク評価

`evaluate_risk` は PR の変更量・影響範囲に基づき LOW / MEDIUM / HIGH のリスクレベルと理由 (`reasons`) を返す。判定基準 (既定値) は以下とする。

| 条件 | リスクレベル |
|---|---|
| 変更ファイル 20 以上 / 変更行 500 以上 / `cmd/`・`go.mod`・`.github/` の変更 | HIGH |
| 変更ファイル 10 以上 / 変更行 200 以上 / `internal/` コアの変更 | MEDIUM |
| 上記以外 | LOW |

- 受け入れ条件: 上記の各しきい値条件を与えたとき、対応するリスクレベルが返ること (境界値を含むテストで検証)
- verify: test
- 実装状況: 実装済み — `internal/risk/evaluator.go`。`DefaultConfig()` のしきい値・パスは設計表と完全一致。設計を超えて、`harness.toml` の `[risk]` セクションによるしきい値カスタマイズ、`exclude_paths` (例: `cmd/hermit/templates/` の除外)、リポジトリ別オーバーライド、warm-up モード (`require_human_approval`) が追加されている

## REQ-007: merge_pr の CI ゲーティングと HIGH リスク拒否

`merge_pr` は CI がパスした PR のみをマージする。HIGH リスクの PR はマージを拒否し、その旨をコメントとして PR に投稿する。マージ不可の場合は `{"merged": false, "reason": "..."}` で理由を返す。

- 受け入れ条件: CI 失敗中または HIGH リスクの PR に対して merge_pr がマージを実行せず、reason 付きで拒否を返すこと
- verify: test
- 実装状況: 実装済み — `internal/mcp/tools.go` の `merge_pr` ハンドラ。HIGH 時のコメント投稿 (`⚠️ HERMIT: HIGH risk detected.`) と `merged: false` 返却、`CIPassing` でない場合の拒否を実装。設計を超えて `force` フラグと warm-up モード (リスクレベルに関わらず自動マージをブロック) が追加されている

## REQ-008: merge_pr 成功後の worktree 自動クリーンアップ

`merge_pr` に `worktree_path` と `branch` が渡された場合、マージ成功後に該当 worktree とブランチを削除する。

- 受け入れ条件: マージ成功後に worktree ディレクトリと作業ブランチが残存しないこと (マージ失敗時は削除しないこと)
- verify: test
- 実装状況: 実装済み — `merge_pr` ハンドラがマージ成功後にのみ `git.CloseWorktree` (`internal/git/worktree.go`) を呼び出す。あわせて設計外の lessons スコアリング (`internal/lessons`) と CI 失敗履歴のクリア (`internal/cihistory`) も実行される

## REQ-009: Superintendent ロール — Issue 駆動の自律サイクル

Superintendent サイクルはフォアグラウンドを塞がない。`/hermit` の起動 (ユーザー入力または cron トリガー) はフォアグラウンドで「ディスパッチ」(cron ジョブの存在確認と、Agent tool `run_in_background: true` によるバックグラウンド Superintendent サブエージェントの生成) のみを行い、直ちにプロンプトを返す。サイクル本体はバックグラウンドサブエージェントが 1 パスずつ実行する (Issue #147)。

1. `list_issues` でオープン Issue を取得する (なければパスを終了し、次の cron トリガーを待つ)
2. Issue を `assign_issue` で着手中にし、`create_worktree` で作業環境を用意する
3. Agent tool で Engineer を並列に生成し、Issue 番号・タイトル・本文・worktree_path・branch を渡す (サブエージェントのネストが不可能な場合は準備済み Issue を報告し、メインセッションが Engineer を生成する)
4. 全 Engineer の完了を待ち、PR に対して CI 確認とリスク評価を行う
5. LOW / MEDIUM は `merge_pr` でマージ、HIGH はレビューコメントを残して人間の判断を待つ

- 受け入れ条件: `hermit init` が生成する CLAUDE.md テンプレートに、フォアグラウンドディスパッチ (`run_in_background: true` によるバックグラウンドサブエージェント生成) と上記のバックグラウンドサイクルが含まれること
- verify: manual
- 実装状況: 実装済み — `cmd/hermit/templates/CLAUDE.md.tmpl` の Superintendent セクション (Foreground dispatch / Background cycle / Engineer fallback)。設計の 7 ステップから大幅に拡張され、`.hermit-quit` / `.hermit-paused` の検出 (バックグラウンド側で実施)、要件ヒアリング Issue の分岐、Issue 粒度チェック、PR/Issue コメント検出、HIGH リスク時の実質レビュー実施などが追加されている

## REQ-010: Engineer ロール — worktree 内での独立した実装フロー

Engineer は Superintendent から受け取った Issue を、指定された worktree 内で実装する。

1. 指定 `worktree_path` に移動して作業する
2. Issue の要件を実装し、テストを書いてパスさせる
3. 指定 `branch` でコミットし PR を作成する
4. 完了時に `worktree_path` / `branch` / PR 番号を Superintendent に報告する

- 受け入れ条件: `hermit init` が生成する CLAUDE.md テンプレートに上記フローが含まれること
- verify: manual
- 実装状況: 実装済み — `cmd/hermit/templates/CLAUDE.md.tmpl` の Engineer セクション。設計を超えて `[Clarification Needed]` / `[Split Suggested]` による作業中断プロトコル、要件ヒアリング担当 (Requirements Analyst 相当) の役割が追加されている

## REQ-011: Engineer の並列数上限

Superintendent が同時に生成する Engineer の数は `harness.toml` の `[agent] max_engineers` (既定: 4) を上限とする。上限を超える Issue は次サイクルに繰り越す。

- 受け入れ条件: `get_config` が harness.toml の max_engineers 値を返し、CLAUDE.md テンプレートが並列上限としてこの値を参照していること
- verify: test
- 実装状況: 一部実装 — テンプレート側は実装済み (`cmd/hermit/templates/CLAUDE.md.tmpl` が `{{ .MaxEngineers }}` を展開し、超過分の繰り越しも記述)。一方 `get_config` (`internal/mcp/tools.go`) は `loop_interval` / `risk` / `model` のみを返し、HERMIT.md が定める `owner` / `repo` / `max_engineers` を返さないため、実行時に上限値を MCP 経由で参照できない

## REQ-012: harness.toml による設定と GITHUB_TOKEN の非保存

設定は `harness.toml` で管理する (`[github] owner / repo`、`[agent] max_engineers / language` など)。`harness.toml` はチーム共有を意図し `.gitignore` に追加しない。GitHub トークンは環境変数 `GITHUB_TOKEN` からのみ受け取り、**toml には決して書かない**。

- 受け入れ条件: 設定ローダーが GITHUB_TOKEN を環境変数から読むこと。harness.toml およびそのテンプレートにトークン項目が存在しないこと
- verify: test
- 実装状況: 実装済み — `cmd/hermit/main.go` が harness.toml をロードし、トークンは環境変数のみ (toml にトークン項目なし。`hermit doctor` が `GITHUB_TOKEN` の存在を検査)。設定項目は設計より大幅に増えている (`rate_limit_threshold` / `default_branch` / `loop_interval` (既定 270 秒) / `[model]` / `[readiness]` / `[risk]` / `[notification]` / `[requirements]` 等)

## REQ-013: ワンライナーによるインストールとセットアップ

セットアップは単一コマンドで完結する。

- `install.sh` は OS / アーキテクチャを検出し、最新リリースのバイナリを checksum 検証のうえ配置し、`hermit install` を実行する
- `hermit install` は Claude Code に hermit MCP サーバを登録する
- `hermit init` はプロジェクトディレクトリで対話的に `harness.toml` を生成し、テンプレートから `CLAUDE.md` を生成する
- 受け入れ条件: クリーンな環境で `curl | sh` → `hermit init` の手順のみで Superintendent を起動できる状態になること
- verify: manual
- 実装状況: 実装済み — `install.sh` (OS/ARCH 検出、sha256 検証、`~/.local/bin` へ配置、末尾で `hermit install` 実行)。バイナリ名は設計の `hermit_${OS}_${ARCH}` ではなく `hermit-${OS}-${ARCH}`。`hermit install` は設計の settings.json 直接編集ではなく `claude mcp add` 経由で登録し、`.claude/commands/` へのスラッシュコマンド配置も行う。`hermit init` は `cmd/hermit/main.go` がテンプレート (`cmd/hermit/templates/`、go:embed) から harness.toml / CLAUDE.md / .claude/settings.json を生成

## REQ-014: 非目標 (Non-Goals)

MADFLOW の教訓 (HERMIT.md §1, §9) に基づき、HERMIT では以下を行わない。

- Go バイナリが Claude Code をサブプロセスとして起動・管理する「外側から包む」アーキテクチャ
- エージェンティックループ・LLM API 呼び出しの Go による再実装
- チャットログ (ファイル) 経由のエージェント間通信 — Agent tool の入出力で代替する
- タイマーによるコンテキストリセット等のコンテキスト/プロセスライフサイクル管理 — Claude Code に委譲する
- 受け入れ条件: 上記に該当する実装がコードベースに追加されていないこと (レビューで担保)
- verify: manual
- 実装状況: 実装済み (遵守) — 現行コードベースに該当実装は存在しない。`cmd/hermit` のサブコマンドは serve / install / init / doctor / upgrade / version 等の CLI に留まり、Claude Code のプロセス管理や LLM 呼び出しは行っていない
