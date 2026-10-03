# 2026-10-03 — ログを集めて引けるようにし、トレースと双方向に飛ぶ（#271 / 10.1）

環境: ローカル（allinone を `mise run run:obs` で起動 + compose obs プロファイルの Tempo・Loki・Alloy・Grafana）。

## 何を確かめようとしたか

三本柱のうちログだけ、計装（構造化 JSON・全行に trace_id）はあるのに集約先が無かった。
トレースからログへ、ログからトレースへ飛べない。アプリ側の規約（JSON を stdout へ、
1 ストリーム 1 形式 1 出力先）を変えずに、集める側を足して引けるようにする。

## 設計の要点

- **アプリは何も変えない**。集めるのはインフラの仕事（AWS では ECS の awslogs が同じ役）
- Tier 1（ホストのプロセス）は `run:obs` が stdout を `.logs/allinone.log` にも書き、Alloy が追う。
  Tier 2（compose のコンテナ）は Alloy が docker のログを読む
- ラベルは `service_name` と `level` だけ。`trace_id` / `span_id` / `request_id` は**構造化メタデータ**
  （値ごとに系列を作らない。ラベルにすると系列数が爆発する）
- Grafana: Loki の `trace_id` → Tempo（derived field）、Tempo のスパン → 同じ trace_id のログ（tracesToLogsV2）

## 実測

| 確認 | 結果 |
|---|---|
| Tier 1: allinone のアクセスログが Loki に入る | 入る。`{service_name="allinone"}` |
| ログの trace_id で Loki を絞り込む | `| trace_id="9d1767db…"` で 1 行（構造化メタデータで引ける） |
| 同じ trace_id で Tempo | 200、スパン `GET /v1/photos` |
| トレース → ログのクエリ（tracesToLogsV2 と同じ式） | 1 行返る |
| Grafana 経由で Loki を引く | 200 |
| Tier 2: compose のコンテナのログ | photo・gear のアプリログは `service_name` 付き。他(mysql 等)も `compose_service` と `service_name` で引ける |

途中で直したこと:

- JSON を出さないコンテナが `service_name=unknown_service`（Loki の既定）になった → compose のサービス名で補う
- `level` が `INFO`（アプリの slog）と `info`（他のツール）に割れた → 小文字にそろえる

## ついでに見つけたこと

- 既存の `mise run obs:up` は prometheus と grafana しか立てておらず、Tempo が入っていなかった
  （トレースも `obs:up` だけでは見られなかった）。Tempo・Loki・Alloy を加えた
- `docs/02-architecture.md` の構成表が「otel-collector + jaeger」のままだった（実体は Tempo への直接送信）

## 検証の仕方について

既存の greenfield プロジェクトの観測系（grafana / prometheus）は別の作業で起動中だったため、
`-p gf271` の別プロジェクトにコンテナ名と Grafana のポートをずらして立てて検証し、終了後に落とした。

## やっていないこと

- AWS 側（Phase 7 は destroy 済み。トレースは OTLP の送り先未設定で無効、アプリのメトリクスは未収集のまま）
