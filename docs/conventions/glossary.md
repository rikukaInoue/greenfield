# 用語集

<!-- 区分: 参考 / ステータス: Draft -->

| 用語 | 定義 | 詳細 |
|---|---|---|
| Entity | 業務概念のモデルをメモリ上のオブジェクトとして実装したもの。不変条件と状態遷移のみを持つ。行そのものではない | internal/02 |
| Value Object | IDを持たず内容で等価判定する値。値レベルの制約を型で保証する | internal/02 |
| Read Model | 自ドメインの正から組み立てた、読むためだけの形。Entityを経由しない | internal/02 |
| ReplicaView | 他ドメインの公開データの読み取り専用の複製。正は相手にあり、イベント購読で追従。業務判断に使用禁止 | internal/01 |
| Atomic | 整合性クラス: 原子性。中間状態が観測されない。実装はctx運搬のトランザクション | internal/03 |
| Eventual | 整合性クラス: 結果整合。窓は許すが欠落は許さない（at-least-once）。実装はOutbox+SNS/SQS | internal/03 |
| BestEffort | 整合性クラス: 保証なしの一回試行。TryXxx命名+ログ | internal/03 |
| Outbox | 業務データと同一txで送信予約をDBに刻むテーブル。再生の正でもある | internal/03 |
| inbox | 受信側の処理済みイベントID記録。重複配送の主たる防御 | internal/03 |
| pending状態パターン | 別ドメインへの同期コマンドの整合性の窓を、ドメインの状態として表に出す方式 | internal/03 |
| CQS | コマンド（Entity経由・Atomic内）とクエリ（Read Model直行・tx外）の経路分離 | internal/02 |
| Principal | 認証済み主体（ユーザー/サービス）。Subject = Kratos identity ID（不変条件） | external/04, internal/04 |
| AAL / ACR | 認証保証レベル（NIST SP 800-63B参考の社内定義）とそのOIDC表現 | external/04 |
| ステップアップ | RFC 9470。401+WWW-Authenticateで再認証を要求。エージェントへのhuman-in-the-loopも兼ねる | external/04 |
| タプル | ReBAC（OpenFGA）の関係データ。Atomic適用第一号 | external/05, internal/03 |
| 直接経路台帳 | ブラウザがSSRを介さずAPIを叩く例外の登録簿。CORS/トークン方式が行単位で紐づく | external/03 |
| expand/contract | 後方互換マイグレーションの規約。改名は3リリース | internal/05 |
| フィーチャーフラグ | 時間軸の制御（Release/Ops/Experiment）。主体の権利はauthz管轄（Permission型不採用） | internal/08 |
