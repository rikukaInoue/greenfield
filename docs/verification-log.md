# 検証ログ

<!-- 区分: 検証ビルド / 追記専用。各エントリは 04-milestones.md のチェック # または 05-roadmap.md のステージ # に対応する -->

チェックの証跡（実験手順・結果・気づき）を時系列で追記する。規約への修正が必要な発見は「還流」節に集める。

---

## エントリ

1エントリ1ファイルに分けている（`docs/verification-log/`）。EOF への追記が並行作業で必ず競合するため、
**新しいエントリはこのディレクトリに新しいファイルを作り、下の表に1行足す**。既存のファイルは触らない。

| 日付 | 内容 |
|---|---|
| 2026-09-26 | [ステージ 0.1 モジュール骨格（#28） / チェック #3（#3）](verification-log/2026-09-26-stage-01.md) |
| 2026-09-26 | [ステージ 0.2 DB基盤（#29） / チェック #1（#1）](verification-log/2026-09-26-stage-02.md) |
| 2026-09-26 | [ステージ 0.3 sqlc + 整合CI（#30） / チェック #2（#2）](verification-log/2026-09-26-stage-03.md) |
| 2026-09-26 | [ステージ 0.4 API骨格（#31） / チェック #16 前半（#16）](verification-log/2026-09-26-stage-04.md) |
| 2026-09-26 | [ステージ 0.5 開発ループ（#32）](verification-log/2026-09-26-stage-05.md) |
| 2026-09-26 | [ステージ 1.1 Atomic / 1.2 CQS / 1.3 認可呼び出し（#34 #35 #36） / チェック #4 #5 #6 #7](verification-log/2026-09-26-stage-11.md) |
| 2026-09-27 | [ステージ 1.3b 画像ストレージ（#74） / チェック #28 #29](verification-log/2026-09-27-stage-13b.md) |
| 2026-09-27 | [ステージ 1.4 フラグ（#37）](verification-log/2026-09-27-stage-14.md) |
| 2026-09-27 | [ステージ 5.2 SSR（前半: Keycloak 配線前、#56）](verification-log/2026-09-27-stage-52.md) |
| 2026-09-27 | [ステージ 1.5a〜1.5e オンラインマイグレーション（#38〜#42） / チェック #15 #19 #21 #25](verification-log/2026-09-27-stage-15a-15e.md) |
| 2026-09-27 | [ステージ 5.2 ブラウザ E2E（#56）](verification-log/2026-09-27-stage-52-2.md) |
| 2026-09-27 | [監査の修正 1: ゲートが嘘をつく箇所（#83）](verification-log/2026-09-27-audit-fix-1.md) |
| 2026-09-27 | [ステージ 5.2 SSR でのフラグ判定（#56）](verification-log/2026-09-27-stage-52-3.md) |
| 2026-09-27 | [フラグの評価をプロセス内へ移す（ADR 0013）](verification-log/2026-09-27-adr-0013.md) |
| 2026-09-27 | [監査の修正 2: スキーマの二重化（#84）](verification-log/2026-09-27-audit-fix-2.md) |
| 2026-09-27 | [SSR のフラグ評価をやめ、判定を API から受け取る（ADR 0014）](verification-log/2026-09-27-adr-0014.md) |
| 2026-09-27 | [監査の修正 3: import 規律を lint で強制する（#90）](verification-log/2026-09-27-audit-fix-3.md) |
| 2026-09-27 | [監査の修正 4: 運用・設定（#92）](verification-log/2026-09-27-audit-fix-4.md) |
| 2026-09-27 | [ステージ 2.0 第2サービス骨格（gear、#43）](verification-log/2026-09-27-stage-20.md) |
| 2026-09-27 | [監査の修正 5: 誤った主張の訂正（#89）](verification-log/2026-09-27-audit-fix-5.md) |
| 2026-09-27 | [ステージ 2.1 差分検知 CI（#44）](verification-log/2026-09-27-stage-21.md) |
| 2026-09-27 | [監査の修正 6: 主張の裏付けになるテストを置く（#88）](verification-log/2026-09-27-audit-fix-6.md) |
| 2026-09-27 | [検証ログを1エントリ1ファイルに分割](verification-log/2026-09-27-split-log.md) |
| 2026-09-27 | [監査の修正 7: 本番の既定値を fail-closed にする（#87）](verification-log/2026-09-27-audit-fix-7.md) |
| 2026-09-27 | [ステージ 2.2 前半: API のバージョニング規則（ADR 0017、#45 / #16）](verification-log/2026-09-27-stage-22a.md) |
| 2026-09-27 | [ステージ 2.2 photo の v2 を並行提供する（ADR 0017、#45 / #16）](verification-log/2026-09-27-stage-22b.md) |
| 2026-09-27 | [イベント基盤のローカル実装を測る（#75）](verification-log/2026-09-27-event-backend-probe.md) |
| 2026-09-27 | [ステージ 2.2 利用側（frontend）を photo v2 へ移す（#45 / #16）](verification-log/2026-09-27-stage-22c.md) |
| 2026-09-27 | [ステージ 2.2 photo v1 を deprecated にする（廃止予告、#45 / #16）](verification-log/2026-09-27-stage-22d.md) |
| 2026-09-27 | [ステージ 2.2 photo v1 を削除する（並行提供の手順の完了、#16 / #45）](verification-log/2026-09-27-stage-22e.md) |
| 2026-09-27 | [ステージ 2.2 Go の生成クライアント（#45）](verification-log/2026-09-27-stage-22f.md) |
| 2026-09-27 | [status の既定を fail-closed に（監査 D-4 / #122）](verification-log/2026-09-27-status-fail-closed.md) |
| 2026-09-27 | [ステージ 2.3 デプロイ順序（#46 / check #24）](verification-log/2026-09-27-stage-23.md) |
| 2026-09-27 | [ステージ 2.4 並列と直列（#47 / check #26）](verification-log/2026-09-27-stage-24.md) |
| 2026-09-28 | [ステージ 3.1 Keycloak（#48） / チェック #13（#13）](verification-log/2026-09-28-stage-31.md) |
