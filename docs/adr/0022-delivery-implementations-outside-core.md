# 0022. 配送基盤の実装は core に置かない(差し込み口だけ core)

## 背景

`core/eventbus`(#162、Eventual の outbox / relay / inbox)は SNS FIFO / SQS FIFO の実装で、
これが core の direct require に AWS SDK(`service/sns` / `service/sqs`)を持ち込んでいた。

- [ADR 0019](0019-httpapi-in-core.md) は「core に direct require を足す PR には対応する ADR 番号を書く」と定め、
  許可済みの依存を表にしていたが、AWS SDK は表にも ADR にも無かった。
  [ADR 0018](0018-keep-localstack-for-sns-sqs.md) は LocalStack を使い続ける判断で、core に配る判断ではない
- `eventbus` を使うのは photo / gear だけ。`platform/authz` は使わないのに、core を使うだけで
  AWS SDK を依存グラフに抱えていた(module graph・脆弱性スキャン・依存更新の対象が全員に乗る)
- 配送基盤は将来 SNS / SQS から EventBridge 等へ替える可能性がある。実装が core にあると、
  差し替えのたびに core が動き、[ADR 0015](0015-ci-affected-from-module-graph.md) により全モジュールの CI が走る

## 決定

**差し込み口は core、配送基盤の実装は別モジュールに置く。** [ADR 0011](0011-flag-provider-on-aws.md)
(`core/flags` と `flagprovider/appconfig`)と同じ形にそろえる。

- 送信口 `consistency.Bus` と受信の処理 `consistency.Handler` は core(配送基盤の語彙を持たない)
- SNS / SQS の実装は `eventbus/snssqs`(#270 で `core/eventbus` から移動。挙動・依存の版は変えていない)
- どの実装を使うかは各サービスの合成ルート(`app/relay.go`・`app/consume.go`)だけが知る。
  EventBridge にするときは `eventbus/eventbridge` のような実装モジュールを足し、合成ルートを差し替える。
  usecase と core は変わらない

一般化すると、**クラウドの SDK や配送基盤のように差し替えうる実装は core に置かない**。
core には「全サービスで同一でなければならない横断的関心事」(ADR 0019 の httpapi など)だけを置く。

## 代替案

- **core に残し、ADR 0019 の表に AWS SDK を追記して済ませる**: 記録の漏れは埋まるが、使わない
  利用者に AWS SDK を配り続け、差し替えのたびに core が動く問題は残る。不採用
- **core 内のサブパッケージのまま build tag で切り替える**: Go の module graph は go.mod 単位なので、
  タグで切っても依存は全利用者に配られる。不採用

## 影響

- `core/go.mod` の direct require から AWS SDK が消えた。残りは huma / chi / OpenFeature /
  mysql(テスト専用)で、ADR 0019 の表と一致する
- 新モジュール `eventbus/snssqs` が増えた(`go.work`・photo / gear / dev の `go.mod`・
  photo / gear の Dockerfile・dependabot に追加。漏れは `lint:replaces` が検出する)
- **core の direct require は許可リスト `core/deps.allow` と突き合わせる**(`lint:core-deps`、`check` に含む)。
  リストに無い依存・使われなくなった許可・テスト専用の依存の本体からの import を失敗にする。
  ADR 0019 の運用(1)(2)を、文章から機械に移した

## 還流

プロダクション版にも同じ形を持ち帰る: (1) 配送基盤・クラウド SDK の実装は core 外のモジュールに置き、
差し込み口だけを core に置く、(2) core の direct require は許可リスト + CI で止める
(許可リストの各行に根拠の ADR を書く)。
