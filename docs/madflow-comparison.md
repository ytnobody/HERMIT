# HERMIT vs MADFLOW 機能比較レポート

**作成日**: 2026-05-17  
**Issue**: [#1 ytnobody/MADFLOWを確認し、不足している機能のリストアップ](https://github.com/ytnobody/HERMIT/issues/1)

---

## 概要

[MADFLOW](https://github.com/ytnobody/MADFLOW)（Multi-Agent Development Flow）と [HERMIT](https://github.com/ytnobody/HERMIT)（Harness for Engineer Role Management via Interactive Tasks）は、いずれも AI エージェントによる自律的なソフトウェア開発を実現するフレームワークです。ただし実装アーキテクチャが大きく異なります。

| 観点 | MADFLOW | HERMIT |
|------|---------|--------|
| アーキテクチャ | Go バイナリが Claude Code を subprocess として起動・制御 | Claude Code が MCP サーバー経由で HERMIT ツールを呼び出す |
| コード規模 | ~5,000 行（Go） | ~700 行（Go） |
| 役割の重心 | バイナリがオーケストレーションロジックを実装 | Claude Code 自身がオーケストレーションを担当 |
| 設定ファイル | `madflow.toml` | `harness.toml` |

---

## HERMIT が現在提供している機能

| MCP ツール | 説明 |
|------------|------|
| `list_issues` | 未着手の Issue 一覧を返す |
| `assign_issue` | Issue をラベル付与・アサインして処理中にマーク |
| `create_worktree` | `hermit/issue-{N}` ブランチと `/tmp/hermit-{N}` ワークツリーを作成 |
| `evaluate_risk` | PR の変更量・影響範囲から LOW/MEDIUM/HIGH を判定 |
| `merge_pr` | CI 通過確認後にマージ（HIGH リスクは拒否してコメント投稿） |
| `close_worktree` | ワークツリーとブランチを削除 |

サブコマンド: `serve`, `install`, `init`, `pause`, `resume`, `status`

---

## MADFLOW にあって HERMIT にない機能

### 1. レッスン注入機能（自己学習ループ）

**Issue**: [#4](https://github.com/ytnobody/HERMIT/issues/4)

PR マージ後に Issue 指示品質を自動採点し、失敗から教訓を生成してスーパーインテンデントのプロンプトに注入するフィードバックループ。

- 採点基準: 派生 Issue 発生（-30）、Clarification Needed コメント（-20）、直接実装（-20）、PR 複数本（-15）
- 70 点未満の場合に Anthropic API で教訓を生成
- 最大 15 件を管理（LLM でマージ・リスク順トリミング）

### 2. GitHub API レートリミット事前チェック

**Issue**: [#5](https://github.com/ytnobody/HERMIT/issues/5)

API 呼び出し前に残量を確認し、残量不足時は待機またはスキップする保護機構。

- 閾値: デフォルト 10 件未満
- 最大待機時間: 10 分（超過する場合はサイクルをスキップ）
- フェイルオープン設計

### 3. ユーザー名前空間付きブランチ・ワークツリー

**Issue**: [#6](https://github.com/ytnobody/HERMIT/issues/6)

ブランチ名に GitHub ログイン名を含めることで、複数ユーザーの並行稼働時の名前衝突を防止。

- 形式: `madflow/{gh_login}/issue-{ID}` → HERMIT では `hermit/{gh_login}/issue-{N}`
- 自動 GitHub ログイン検出（`gh api user`）
- 後方互換性維持

### 4. hermit upgrade コマンド（自己アップグレード）

**Issue**: [#7](https://github.com/ytnobody/HERMIT/issues/7)

GitHub Releases から最新バイナリをダウンロードして自己更新するコマンド。

- SHA-256 チェックサム検証
- アトミックなバイナリ置き換え
- `hermit version` コマンドも追加

### 5. レガシーリソースの自動クリーンアップ

**Issue**: [#8](https://github.com/ytnobody/HERMIT/issues/8)

旧形式のブランチ名・ワークツリーパスを起動時に検出して自動削除または警告するメカニズム。

- レガシーブランチ: `git branch -d` で安全削除
- レガシーワークツリー: 警告ログ出力
- `hermit cleanup` サブコマンドも追加

### 6. 複数 AI バックエンドのサポート

**Issue**: [#9](https://github.com/ytnobody/HERMIT/issues/9)

Claude Code CLI・Gemini CLI・Anthropic API キーの複数バックエンドをサポートし、プリセットで切り替え可能にする機能。

- HERMIT のアーキテクチャ上、Engineer は Claude Code が担当するため主に Anthropic API キー方式が現実的
- `hermit use <preset>` コマンドで設定を切り替え

### 7. Issue 粒度判断と曖昧な Issue の処理フロー

**Issue**: [#10](https://github.com/ytnobody/HERMIT/issues/10)

Engineer が Issue 受け取り後に粒度チェックと曖昧さ評価を行い、必要に応じて Superintendent に確認するフロー。

- 粒度が大きすぎる場合はサブ Issue 分割を提案
- 曖昧な場合は `[Clarification Needed]` コメントで確認要求
- `add_issue_comment` / `split_issue` MCP ツールの追加

### 8. CI 品質強化（lint・セキュリティ・カバレッジ）

**Issue**: [#11](https://github.com/ytnobody/HERMIT/issues/11)

AI が生成したコードを自動品質ゲートで保証するための CI 強化。

- `golangci-lint`（errcheck, staticcheck, unused 等）
- `govulncheck`（脆弱性スキャン）
- テストカバレッジ閾値チェック
- レースコンディション検出（`-race`）

---

## 優先度評価

| Issue | 機能 | 推奨優先度 | 理由 |
|-------|------|-----------|------|
| #5 | レートリミットチェック | 高 | 長時間運転での即時的なリスク軽減 |
| #11 | CI 品質強化 | 高 | AI 生成コードの品質保証に直結 |
| #4 | レッスン注入 | 中 | 自己改善ループで長期的品質向上 |
| #6 | ユーザー名前空間 | 中 | チーム環境での必須機能 |
| #7 | hermit upgrade | 中 | UX 向上、実装コストが低い |
| #10 | Issue 粒度判断 | 中 | CLAUDE.md 拡張でほぼ実現可能 |
| #8 | レガシークリーンアップ | 低 | #6 実装後に必要になる |
| #9 | 複数バックエンド | 低 | アーキテクチャ制約から効果が限定的 |

---

## HERMIT の強みと差別化ポイント

- **コード量の少なさ**: MADFLOW の約 14% のコード量で同等のコアワークフローを実現
- **Claude Code ネイティブ**: MCP 統合により Claude Code の能力（思考・計画・コンテキスト管理）を最大活用
- **シンプルな導入**: `hermit init` と `hermit serve` だけで使い始められる
- **Claude Code スキル**: `/hermit`, `/loop` などのスキルで自然言語ベースの操作が可能

MADFLOW の多くの高度な機能は、HERMIT ではプロンプトエンジニアリング（CLAUDE.md）と Claude Code 自身の能力で代替可能な部分もあります。実装優先度は実際のユースケースに基づいて決定することを推奨します。
