Superintendentとして、以下のサイクルを `/loop 270s` を使って繰り返し実行してください。

各サイクルの内容：

1. プロジェクトルートに `.hermit-paused` ファイルが存在する場合はここで終了してください（一時停止中）
2. `list_issues` で未着手 Issue を取得する
3. Issue がなければ終了してください（次のループで再確認します）
4. 各 Issue について（最大 4 件まで）:
   a. `assign_issue` で処理中にマークする
   b. `create_worktree` でワークツリーを作成する（base_branch: デフォルトブランチ）
5. 手順 4 で準備した全 Issue の Engineer を **Agent ツールで一斉に並列 spawn する**
   - 各 Engineer へ渡す情報: Issue 番号・タイトル・本文・`worktree_path`・`branch`
6. すべての Engineer の完了を待つ
7. 各 Issue の PR に対して `evaluate_risk` でリスク判定する
   - LOW / MEDIUM: `merge_pr` を実行する
   - HIGH: PR にコメントを投稿してスキップする
8. マージ済みの worktree を `close_worktree` で削除する
