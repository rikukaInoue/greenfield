# Runbooks

常設アラート(deploy/compose/prometheus/alerts.yml)と1対1。**手順の無いアラートを作らない**
(無視される訓練にしかならない)。各 Runbook は「まず見る → 切り分け → 対処 → 根拠」の順。
アラートの発火条件は promtool の単体テスト(`mise run lint:alerts`)で機械検証されている。

- [DBPoolSaturated](db-pool-saturated.md)
- [AuthzLatencyHigh](authz-latency-high.md)
- [OutboxStalled](outbox-stalled.md)
- [MetricUnreadable](metric-unreadable.md)
- [ContractQueueRotting](contract-queue-rotting.md)
