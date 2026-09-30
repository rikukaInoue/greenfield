# 2026-09-30 — 排水（グレースフルシャットダウン）を実装して実測（#222 / 10.8）

環境: ローカル（mysql 13306 + LocalStack）。`dev/scripts/shutdown-drill.sh` で再現可能。

## 直す前に何が壊れていたか（コードで確認）

- **serve**: `Shutdown` のエラーを握り潰し（`_ =`）、上限5秒固定。排水しきれても
  失敗しても同じ見た目（fail open）。DB は閉じず、メトリクスは排水開始と同時に消える
- **relay**: `RunOnce` に SIGTERM の ctx をそのまま渡していた。停止指示が
  処理中のバッチを中断し、「SNS へは送れたのに published_at を記録できない」＝
  次回起動時に**自作の重複配送**を生む形（inbox が吸収するとはいえ避けられる重複）
- **consumer**: 同じく処理・削除が ctx で中断される。「業務処理は済んだのに削除
  できない」＝可視性タイムアウト後の再配信を自作する形
- **relay / consumer とも SIGTERM で exit 1**（`ctx.Err()` を返すため）。ECS の
  タスク停止が毎回「異常終了」として記録される

## 実装

- serve: 排水上限を `SHUTDOWN_TIMEOUT_SECONDS`（既定25秒）に。排水失敗は警告、
  完了は `排水完了 clean=true` をログに。リスナー → DB の順で閉じ、メトリクスは
  排水後に停止
- relay / consumer: 処理中のバッチ・受信済みメッセージは `context.WithoutCancel` +
  `DrainTimeout`（既定20秒）で完走させる。relay は停止時に未送信残数を記録。
  正常停止は nil（exit 0）
- 検証用に `PHOTO_FAULT=<point>=<duration>` で**遅延**を注入できるようにした
  （処理中のリクエストを意図的に作るため）

## 実測（shutdown-drill.sh）

| 確認 | 結果 |
|---|---|
| serve: SIGTERM 後、コミット直前で3秒待つ処理中8リクエスト | **全部 201 で完走**、`排水完了 clean=true` |
| serve: SIGKILL（対照実験＝排水が守っているものの実証） | **8/8 が切断** |
| relay: 150件シード、1バッチ目（100件）途中で SIGTERM | バッチ完走・**送信100＋未送信50の帳尻一致**・`unsent=50` を記録・context canceled ゼロ・exit 0 |
| consumer: 処理中に SIGTERM | 受信済みを完走・context canceled ゼロ・exit 0 |
| 全量ドレイン後 | inbox **150/150**（取り残しゼロ）・**重複スキップ0**（排水が再配信を自作していない） |

## 気づき

- outbox / inbox があるから「排水しなくても壊れない」は正しい。ただし排水しないと
  **停止のたびに at-least-once の再配達を自分で作り出す**。排水は損失対策ではなく、
  重複と中断の削減＋停止の可視化（unsent の記録、clean の真偽）のためにある
- SIGTERM で exit 1 を返す常駐プロセスは、オーケストレータ側の記録を毎回汚す。
  「正常な停止は 0」は排水と独立に直す価値があった
