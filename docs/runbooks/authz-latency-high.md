# AuthzLatencyHigh — 認可の p95 が 100ms 超

**まず見る**: authz のトレース(ログの trace_id から)。遅いのが OpenFGA への往復か、authz 自身の DB か。

**切り分け**
1. OpenFGA の Check/ListObjects が遅い → タプル数の伸び・リスト系の使いすぎ(WHERE IN 方式の対象数)
2. authz プロセスの CPU/プール → db_pool 系メトリクスと突き合わせ
3. 全サービスのレイテンシも同時に悪いなら、原因は authz でなく共通基盤(DB/ネットワーク)

**対処**: 認可は全リクエストの前段なので、悪化はサービス全体の p95 に直結する。
consistency hint(HIGHER_CONSISTENCY)の使用箇所が増えていないかを先に疑う(作成直後の可視性にしか使わない規約)。

**根拠**: internal-04(authz 統合)。レイテンシ histogram は #59。
