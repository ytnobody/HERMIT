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

全 17 要件のうち **実装済み 17 件 / 一部実装 0 件 / 未実装 0 件**。設計文書 HERMIT.md の骨格はすべて実装されており、多くの領域で設計を超えて拡張されています(Issue #181 の `hermit run` (REQ-019) を含む)。一方、HERMIT.md 自体が実装に追従しておらず、以下の差分 (設計文書の記述と実際の実装の乖離) があります。

| # | 差分 | 設計 (HERMIT.md) | 実装 (現状) |
|---|---|---|---|
| 1 | MCP ツール数 | 図中「6 ツール」、仕様は 12 ツール | 17 ツール (`internal/mcp/tools.go`)。`get_default_branch` / `get_issue_comments` / `check_ci_status` / `get_recent_pr_comments` / `run_requirements_sweep` が追加 |
| 2 | ブランチ命名 | `hermit/issue-42` | `<branch_prefix>/issue-<N>` (例: `hermit/ytnobody/issue-124`)。旧形式は後方互換で検出 (`internal/git/worktree.go`) |
| 3 | `get_config` の返却値 | `owner` / `repo` / `max_engineers` / `loop_interval` | `loop_interval` / `max_engineers` / `risk` / `model` (+`risk_overrides`) を返し、`owner` / `repo` は返さない (`internal/mcp/tools.go`) |
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
- 実装状況: 実装済み — `internal/mcp/tools.go` で上記 12 ツールすべてに加え、設計後に追加された `get_default_branch` / `get_issue_comments` / `check_ci_status` / `get_recent_pr_comments` / `run_requirements_sweep` の計 17 ツールを登録。`TestREQ002_RequiredMCPToolsRegistered` はツール登録一覧のみを検証しており「各ツールが HERMIT.md 記載の入出力スキーマに従うこと」は未検証だったため、`TestREQ002_ToolSchemasMatchHERMITDoc` を追加し入出力スキーマの整合性も検証するようにした (Issue #163)。あわせて棚卸しの過程で `list_prs` (`state` ではなく `issue_number` を受け取る)・`notify` (`event` が必須入力、出力は `success` ではなく `sent`/`event`)・`review_pr` (出力は `summary`/`risk_level`/`suggestions` ではなく `pr_number`/`comment_posted`)・`list_issues` (出力キーが小文字ではなく `Number`/`Title`/`Body`/`Labels`) の 4 ツールで HERMIT.md の記述が実装と乖離していたことが判明したため HERMIT.md 側を実装に合わせて修正した。`get_config` の `owner`/`repo` 相違は既知のとおり REQ-011 側の扱いのまま据え置き

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

Superintendent サイクルは `/hermit` の起動 (ユーザー入力または cron トリガー) を受けたコンテキストで、同期的に・インラインで 1 パス丸ごと実行する。以前の設計 (Issue #147) は cron ティックごとにバックグラウンド Superintendent サブエージェントを 1 体生成してフォアグラウンドの占有を避けていたが、長時間ループを継続すると cron ティックの回数だけサブエージェント生成が際限なく積み上がり、セッションの subagent spawn 上限を消費してループがサイレントに停止する不具合が判明した (Issue #171)。インライン実行に戻すことで、パスそのものによる spawn コストはゼロになり、残る spawn は 1 パスあたり上限付き (最大 {{ max_engineers }} 並列) の Engineer/Analyst 生成のみになる。

1. `list_issues` でオープン Issue を取得する (なければパスを終了し、次の cron トリガーを待つ)
2. Issue を `assign_issue` で着手中にし、`create_worktree` で作業環境を用意する
3. Agent tool (`run_in_background: true`) で Engineer を並列に生成し、Issue 番号・タイトル・本文・worktree_path・branch を渡す
4. 全 Engineer の完了を待ち、PR に対して CI 確認とリスク評価を行う
5. LOW / MEDIUM は `merge_pr` でマージ、HIGH はレビューコメントを残して人間の判断を待つ

- 受け入れ条件: `hermit init` が生成する CLAUDE.md テンプレートに、cron トリガーの存在確認と、上記のサイクルをインラインで実行する記述が含まれ、バックグラウンド Superintendent サブエージェントの生成を指示する記述が含まれないこと
- verify: manual
- 実装状況: 実装済み — `cmd/hermit/templates/CLAUDE.md.tmpl` の Superintendent セクション (「Superintendent cycle (one pass)」に統合済み。旧来の Foreground dispatch / Background cycle / Engineer fallback の分離構成は Issue #171 で廃止)。`.hermit-quit` / `.hermit-paused` の検出、要件ヒアリング Issue の分岐、Issue 粒度チェック、PR/Issue コメント検出、HIGH リスク時の実質レビュー実施は維持されている

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
- 実装状況: 実装済み — テンプレート側 (`cmd/hermit/templates/CLAUDE.md.tmpl` が `{{ .MaxEngineers }}` を展開し、超過分の繰り越しも記述) に加え、`get_config` (`internal/mcp/tools.go`) が `[agent].max_engineers` (既定値未設定または 0 以下のときは既定 4、`cmd/hermit/main.go` の `loadConfig`) を `max_engineers` として返すようになった。受け入れ条件の両半分がそれぞれテストで検証されている: `get_config` 側は `internal/mcp/req_test.go` の `TestREQ011_GetConfig_ReturnsMaxEngineers`、CLAUDE.md テンプレート側は `cmd/hermit/inprocess_test.go` の `TestREQ011_ClaudeMdReferencesConfiguredMaxEngineersAsCap` (`hermit init` を任意の max_engineers 値で実行し、生成された CLAUDE.md の並列上限ステップにその値が実際に反映されることを検証。従来はコメントで「テンプレート側は実装済み」と主張するのみでテストが存在しなかった)。`owner`/`repo` を返さない点は HERMIT.md との差分として残るが、この要件の受け入れ条件には含まれない。Issue #174 での再点検: PR #172 で Superintendent サイクルが単一の「Superintendent cycle」ステップ列に統合された後も、上記 2 テストが検証する文字列 (`up to N at a time` / `exceeds N` / `max_engineers = N`) はテンプレート内に維持されており、両テストは変更なしで現行の受け入れ条件を引き続き正しく検証していることを確認した。Issue #183 での再点検: 要件テキスト変更を受け再確認したが、受け入れ条件文言 (`get_config が harness.toml の max_engineers 値を返し、CLAUDE.md テンプレートが並列上限としてこの値を参照していること`) に実質的な変更はなく、`TestREQ011_GetConfig_ReturnsMaxEngineers` と `TestREQ011_ClaudeMdReferencesConfiguredMaxEngineersAsCap` はいずれも現行の受け入れ条件を引き続き正しく検証している (両テストとも green)

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

- Go バイナリが Claude Code をサブプロセスとして起動・管理する「外側から包む」アーキテクチャ (ただし REQ-019 `hermit run` は明示的な例外— 下記注記参照)
- エージェンティックループ・LLM API 呼び出しの Go による再実装
- チャットログ (ファイル) 経由のエージェント間通信 — Agent tool の入出力で代替する
- タイマーによるコンテキストリセット等のコンテキスト/プロセスライフサイクル管理 — Claude Code に委譲する
- 受け入れ条件: 上記に該当する実装 (ただし REQ-019 で明示的に許可された `hermit run` の範囲を除く) がコードベースに追加されていないこと (レビューで担保)
- verify: manual
- 実装状況: 実装済み (遵守、Issue #181 による限定的な例外あり) — `cmd/hermit` のサブコマンドは serve / run / install / init / doctor / upgrade / version 等の CLI に留まり、Claude Code のプロセス管理や LLM 呼び出しの再実装は行っていない。唯一の例外が REQ-019 の `hermit run`: `claude -p` を子プロセスとして起動する「外側から包む」形に見えるが、(1) エージェンティックループ・コンテキスト管理・LLM API 呼び出しの再実装は一切行わず、1 tick = 1 回の `claude -p` 起動を待つだけの薄いラッパーに留まる、(2) これは Issue #147 (Superintendent をバックグラウンドサブエージェント化する設計) の再導入ではなく、Issue #171 で実証された「サブエージェント spawn 上限の枯渇によるループの静かな停止」という失敗モードを構造的に回避するための独立した設計 (3 回目の再設計) である、という理由により、この 1 行に限り本要件の対象から明示的に除外する。詳細は REQ-019 を参照

## REQ-015: 制御面パスの evaluate_risk 既定 HIGH 昇格

HERMIT 自身を制約する制御面 (`internal/risk/`・`internal/permissions/`・`internal/readiness/`・`harness.toml`・`.claude/`・`CLAUDE.md`) への変更は、変更ファイル数・行数に関わらず既定で HIGH と判定し、MEDIUM 自動マージの対象から外す。制御面パスへの変更をその制約自身の判定で自動マージしてよいかは構造的に無効な問いであるため、機械的に隔離する。

- `DefaultConfig()` の `HighPaths` は `cmd/`・`go.mod`・`.github/` に加え、上記 6 つの制御面パスを含む
- `HighPaths` の判定は `MediumPaths` (`internal/`) より優先される。`internal/risk/` のように `HighPaths` と `MediumPaths` の両方に前方一致する変更は HIGH と判定される
- `internal/risk/`・`harness.toml` 自身が `HighPaths` に含まれることで、`HighPaths` からこれらのパスを除外しようとする変更それ自体が HIGH 判定に掛かり、ガードを弱める PR が自動マージされない
- `cmd/hermit/templates/CLAUDE.md.tmpl` および `cmd/hermit/templates/harness.toml.tmpl` は既存の `ExcludePaths` により引き続き除外され、今回の変更で HIGH に巻き込まれない
- `internal/git/` など制御面に該当しない `internal/` 配下の変更は従来通り MEDIUM のまま
- 受け入れ条件: `internal/risk/`・`internal/permissions/`・`internal/readiness/`・`harness.toml`・`.claude/`・`CLAUDE.md` のいずれかのみを 1 ファイル 1 行変更しても HIGH と判定されること。制御面以外の `internal/` 配下の変更や `cmd/hermit/templates/` 配下のみの変更は従来通りの判定 (MEDIUM・LOW) を維持すること
- verify: test
- 実装状況: 実装済み — `internal/risk/evaluator.go` の `DefaultConfig()`。`internal/risk/req_test.go` の `TestREQ015_ControlPlanePathsAreHighRisk` で検証

## REQ-016: review-test のハッシュ判定は仕様(受け入れ条件・verify)のみを対象とする

reconcile sweep の review-test は、要件の「仕様」が変わったときにのみ発火しなければならない。要件ブロック全体 (見出し・説明文・`実装状況` 進捗メモを含む) をハッシュ対象にすると、review-test を解決する作業自体が `実装状況` 行を書き換えるため、次の sweep で再び「テキストが変化した」と判定され review-test が無限に再発火する自己増殖ループになる (Issue #182)。ハッシュは `受け入れ条件` と `verify` の値のみから計算し、`実装状況` を含む残りのブロックは対象外とする。

- 受け入れ条件: `Requirement.Hash` が `受け入れ条件` と `verify` のみから計算され、要件ブロック全体からは計算されないこと。`実装状況` 行のみを変更しても次の sweep で review-test が発火しないこと。`受け入れ条件` の変更、および `verify` の `test` ↔ `manual` の切り替えは従来どおり発火すること。見出しや説明文のみの変更では発火しないこと。ハッシュストアに計算方式のバージョンが記録され、方式変更後の初回 sweep は全件を再計算・保存するのみで Issue を起票しないこと
- verify: test
- 実装状況: 実装済み — `internal/requirements/requirements.go` の `specHash` が `AcceptanceCriteria` と `Verify` のみからハッシュを計算するように変更 (旧 `hashText(block)` を置き換え)。`internal/requirements/hashstore.go` の `HashStore` インターフェースを `Load() (version int, hashes map[string]string, err error)` / `Save(version int, hashes map[string]string) error` に拡張し、`HashSchemeVersion` 定数 (現在値 2) を導入。旧形式 (バージョン無しの素の map) のファイルは version 0 として扱われ後方互換。`internal/requirements/sweep.go` の `Sweep` は読み込んだバージョンが `HashSchemeVersion` と異なる場合 `schemeChanged` として HashChanged 判定を強制的に false にし (review-test を発火させず)、sweep 終了時に現行バージョンでハッシュを保存し直すことで移行を1回のsweepで完了させる。自己増殖ループの回帰テストは `internal/requirements/sweep_test.go` の `TestSweep_ImplementationStatusOnlyChange_DoesNotFireReviewTest`、スキーマ移行の回帰テストは同ファイルの `TestSweep_HashSchemeMigration_DoesNotFireReviewTest_JustRecomputesAndSaves`、ハッシュ計算自体の単体テストは `internal/requirements/requirements_test.go` の `TestParse_HashUnaffectedByImplementationStatusField` / `TestParse_HashUnaffectedByTitleOrDescriptionOnly` / `TestParse_HashChangesWithVerifyMode` で検証。REQ-ID 命名規約に沿った `TestREQ016_ReviewTestHashIgnoresImplementationStatus` を追加
## REQ-017: list_issues は信頼できる author_association の Issue のみを返す

HERMIT は public リポジトリで運用され得るため、第三者が作成した Issue の本文がそのまま Engineer への指示としてローカルで実行されることを防ぐ。`ListOpenIssues` / `ListAllIssues` は GitHub API の `author_association` を参照し、信頼できる association を持つ Issue のみを返す。信頼する association は `harness.toml` の `[security] trusted_author_associations` で設定可能で、既定値は `OWNER` / `MEMBER` / `COLLABORATOR` の 3 種のみ (`CONTRIBUTOR` / `FIRST_TIME_CONTRIBUTOR` / `NONE` は含めない)。

- 受け入れ条件: 信頼できない author_association を持つ Issue が `ListOpenIssues` / `ListAllIssues` の結果から除外されること。`harness.toml` に設定が無い、または空の場合も安全な既定値 (OWNER/MEMBER/COLLABORATOR) が適用され「全員許可」にフォールバックしないこと。除外時にログが出力されること
- verify: test
- 実装状況: 実装済み — `internal/github/client.go` の `listOpenIssuesFromRepo` が各 Issue の `author_association` を `(*Client).isTrustedAuthor` で判定し、非信頼の Issue を除外したうえで `log.Printf` により除外理由 (Issue番号・owner/repo・association) を記録する。信頼リストは `(*Client).SetTrustedAuthorAssociations` で設定でき、未設定または空スライスを渡した場合は `gh.DefaultTrustedAuthorAssociations` (`OWNER`/`MEMBER`/`COLLABORATOR`) にフォールバックする — 「空 = 全員許可」にはならない。`cmd/hermit/main.go` の `Config.Security.TrustedAuthorAssociations` (`harness.toml` の `[security] trusted_author_associations`) が `cmdServe` / `cmdDryRun` で `SetTrustedAuthorAssociations` に渡される。除外された Issue が `assign_issue` / `create_worktree` の対象にならないのは、`list_issues` (`internal/mcp/tools.go`) がこの層より上位で `ListOpenIssues`/`ListAllIssues` の返り値のみをキューとして扱うため自動的に保証される。テストは `internal/github/req_test.go` の `TestREQ017_*`

## REQ-018: Engineer の Bash をサンドボックスで制限する

Engineer は `Bash(*)` 許可でローカルマシン上で動作しており、`permissions.allow` によるコマンド単位の allowlist 方式は不採用 (Issue #138: 新規ツール追加のたびに未更新でループが壊れる失敗モードが実証済み)。代わりに `hermit init` が Claude Code のサンドボックス機能 (`sandbox` ブロック) を推奨設定として生成し、能力を列挙するのではなく届く範囲そのものを狭める。

- `hermit init` は `.claude/settings.json` に `sandbox.enabled: true` / `allowUnsandboxedCommands: false` / `network.tlsTerminate` + `allowedDomains` (`*.github.com`, `proxy.golang.org`, `sum.golang.org`, `storage.googleapis.com`) / `credentials.files` (`~/.ssh`, `~/.aws/credentials` を deny) / `credentials.envVars` (`GITHUB_TOKEN` を `mode: mask` + `injectHosts: ["api.github.com"]`、`deny` にはしない) を書き込む
- `allowUnsandboxedCommands` は Claude Code 側の既定が `true` であり明示的に `false` にしないとサンドボックス自体が実質無効化されるため、生成される設定では必ず `false` にする
- `GITHUB_TOKEN` を `deny` にすると `gh` コマンド (Engineer が PR 作成等に使う) が動作しなくなるため、`mask` + `injectHosts` を用いる
- 既に `.claude/settings.json` が存在するプロジェクトに対して `hermit init` を再実行しても、既存の `permissions` などのトップレベルキーは破壊されず、欠けているキー (`sandbox` など) のみが追加される
- `hermit doctor` は生成済み `.claude/settings.json` の `sandbox.enabled` が false/未設定、`allowUnsandboxedCommands` が true/未設定、`sandbox.excludedCommands` が空でない場合に警告する (警告のみで doctor 全体の pass/fail には影響しない)
- README にスコープごとの precedence (boolean キーは上位スコープが勝つ、配列キー (`excludedCommands` 等) は全スコープでマージされ下位スコープから広げられる) と、project settings 配置では Engineer 自身が PR 経由で無効化・迂回しうる旨、実効的な強制には managed settings と `allowManagedReadPathsOnly` / `allowManagedDomainsOnly` が必要な旨、その自動生成自体は別 Issue #179 のスコープである旨を明記する

- 受け入れ条件: `hermit init` が生成する `.claude/settings.json` に上記構造の `sandbox` ブロックが含まれ、`allowUnsandboxedCommands: false` / `GITHUB_TOKEN` の `mode: mask` + `injectHosts` が満たされていること。生成された設定を適用した状態で `go build ./...` / `go test ./...` が成功し、`gh pr create` 相当の操作が実行できること。`hermit doctor` が上記 3 種の警告を検出すること。既存プロジェクトへの `hermit init` 再実行が既存の `permissions` 設定を破壊しないこと
- verify: test
- 実装状況: 実装済み — `internal/permissions/permissions.go` の `DefaultSandboxSettings` / `MergeDefaultSettings` (再実行時は既存のトップレベルキーを保持し、欠けているキーのみ補完)、`cmd/hermit/main.go` の `writeClaudeSettings` (`MergeDefaultSettings` を経由するよう変更)、`cmd/hermit/doctor.go` の `checkSandboxSettings` (3 種の警告)。テストは `internal/permissions/permissions_test.go` の `TestREQ018_*` 群 (`DefaultSandboxSettings` の `allowUnsandboxedCommands`/`GITHUB_TOKEN`/Go ツールチェーン許可ドメイン、`MergeDefaultSettings` の新規生成・既存 `permissions`/`sandbox` の保持・エラー経路) と `cmd/hermit/doctor_test.go` / `cmd/hermit/unit_test.go` の `TestREQ018_*` 群 (`checkSandboxSettings` の警告条件、`writeClaudeSettings` の再実行時非破壊)。`go build ./...` と `go test ./...` の成功、および `gh pr create` 相当操作の実行可能性は本 Issue #180 の PR 自体 (生成された設定下で `go test ./...` を通し、同じ worktree から `gh pr create` で PR を作成) によって実地検証済み。README の "Sandboxing the Engineer" セクションにスコープ precedence・managed settings・Issue #179 依存の記載を追加

## REQ-019: `hermit run` — Superintendent ループを Claude Code セッションの外に出す

Issue #181。無人運用のために利用者が `claude` を起動し `/hermit` を打ったセッションを永続的に保持し続ける必要がある、という #147→#172 を経てもなお残っていた制約を取り除くため、`hermit run` サブコマンドを追加する。長寿命の Go プロセスが内部 ticker を持ち、`hermit serve` が MCP サーバとして長寿命プロセスであるのと同じ形で、毎 tick `claude -p` を 1 回起動して完了を待つ。

サブエージェント方式 (#147) への回帰は明示的に禁止する — #171 で「バックグラウンドサブエージェントを cron tick ごとに spawn し続けると、長時間運用でセッションの spawn 上限を静かに枯渇させる」という失敗モードが実証済みのため、`hermit run` は Claude Code のサブエージェントを一切 spawn しない設計でなければならない。

- 受け入れ条件: (1) `hermit run` サブコマンドが存在し usage 出力に記載されている、(2) 内部 ticker で周期実行し前のパスが完了してから `[agent].loop_interval` (既定 270 秒) 待って次のパスを開始する、(3) パスが長時間かかっても重複起動しない、(4) 各 tick はプロジェクトルートを cwd として `claude -p` を非対話 (permission prompt でハングしない) で起動する、(5) `.hermit/superintendent-state.json` を Go 側が所有し、PR コメント確認/Issue コメント確認/要件スイープの 3 つの since タイムスタンプは `get_loop_state`/`update_loop_state` MCP ツール経由でのみ読み書きされる、(6) 最終成功 tick 時刻を記録し設定可能な N 回連続失敗で webhook 通知する、(7) `.hermit-paused`/`.hermit-quit` を `hermit run` 自身が検知する、(8) SIGINT/SIGTERM を受けても実行中のパスを中断せずグレースフルに停止する、(9) systemd unit / launchd plist / Windows サービス登録などの OS 固有コードは追加せず常駐化方法は README の案内に留める、(10) Windows amd64 でビルド・動作する、(11) `/hermit` スラッシュコマンドは廃止せず併存させる
- verify: test
- 実装状況: 実装済み — `internal/runloop` (`Run`/`Options`) が ticker・重複起動防止 (1 ループ内で Invoke を直列実行するのみで並行呼び出しの余地がない構造)・グレースフルシャットダウン (shutdown context はパス開始前とインターバル待機中のみ参照し、実行中の Invoke には渡さない)・`.hermit-paused`/`.hermit-quit` 検知・失敗連続カウントに応じた webhook 通知を実装。`internal/state` が `.hermit/superintendent-state.json` の読み書き (load-modify-save、一時ファイル+rename によるアトミック書き込み) を所有。`internal/mcp/tools.go` の `get_loop_state`/`update_loop_state` ツールが 3 つの since タイムスタンプを internal/state 経由で読み書きし、Superintendent サイクルが同ファイルを直接書き込む経路は存在しない。`cmd/hermit/main.go` の `cmdRun`/`newClaudeInvoker`/`buildClaudeRunArgs` が `hermit run` サブコマンド本体・非対話 `claude` 起動 (`--dangerously-skip-permissions`)・SIGINT/SIGTERM (`signal.NotifyContext`) を実装し、`go build`/`go test` を `GOOS=windows GOARCH=amd64` で実行して Windows ビルドを確認済み。systemd/launchd/Windows サービス/tmux/Docker の常駐化案内は README「Running `hermit run` Continuously」節に追記。テストは `internal/runloop/runloop_test.go`・`internal/state/state_test.go`・`internal/mcp/req_test.go`・`cmd/hermit/run_test.go` の `TestREQ019_*`
