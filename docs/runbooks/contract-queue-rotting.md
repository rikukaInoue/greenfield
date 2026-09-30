# ContractQueueRotting — 未適用の contract が3日以上残っている

**意味**: 「フラグ 100% → 旧経路削除 → contract 実行」の最後の一歩が忘れられている。
壊れてはいない(だから severity=ticket)が、放置すると旧カラムと新コードの距離が開き続ける。

**手順**
1. `photo migrate status`(または gear)で未適用の contract を確認
2. 前提ゲート2つを確認: sqlc に旧カラムへの参照が残っていない(コンパイルで機械確認)、
   対応する Release フラグが削除済み(internal-05 §22)
3. 通ったら contract を実行(one-off タスク。デプロイ不要)

**根拠**: #200(このメトリクスを足した経緯: フラグには期限+通知があるのに contract には腐敗検知が無かった)。
