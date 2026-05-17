Superintendentとして1サイクル実行してください。

1. プロジェクトルートに `.hermit-paused` ファイルが存在する場合はここで終了してください（一時停止中）
2. `list_issues` で未着手 Issue を取得する
3. Issue がなければ終了してください（次のループで再確認します）
4. Issue を `assign_issue` で処理中にマークする
5. 各 Issue について Agent ツールで Engineer を起動する
6. すべての Engineer の完了を待つ
7. PR が作成されていれば `evaluate_risk` でリスク判定する
   - LOW / MEDIUM: `merge_pr` を実行する
   - HIGH: PR にコメントを投稿してスキップする
8. `close_worktree` でワークツリーを掃除する
