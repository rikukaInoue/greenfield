# スコープ

<!-- 区分: 検証ビルド / ステータス: Draft -->

## 目的

1. **設計主張の実証**。プロダクション設計の主要な主張（GRANT分離が誤クエリを実行時に落とす、Atomicのctx伝播、Outboxの回復性、ステップアップのhuman-in-the-loop等）を、動くコードとチェックリスト（04）で検証する。
2. **認可基盤構築の習熟**。authzサービス（action→relationマッピング、consistency hint、FGAモデル）を自作して実機の挙動を確認する。認証は自作せずKeycloak（セルフホスト1コンテナ、realm-as-code）に任せる——試作の主眼はCI/CD分離とオンラインマイグレーションであり、認証は標準に乗るだけでよいため。SaaS（Auth0等）にしない理由は、CIの中で認証込みの結合テストを並列・無制限に回すためにローカル完結（クォータ・ネット依存なし）が要ること。Kratos/Hydra・token hookの構築知見はプロダクション側タスクとして残ることを明記しておく。
3. **雛形の資産化**。core/（authz差し込み口、consistency、httpclient）、scaffold、localauthz、migrateサブコマンド、composeをプロダクションへ還流できる形で作る。

## 検証しないこと

規模・性能（500rpsの負荷試験はしない）。TGW・ingress/egress VPC等のネットワーク統制（構造だけ模擬: 3リスナー、ポート分離）。PCI・決済。マルチチーム運用（CODEOWNERS、差分ビルドの並列調整）。移行系全部（共有DB方式、所有権台帳、identity import）。ステップアップ認証のE2E（`RequireAAL` の呼び出し語彙はコードに固定するが、実フロー検証は任意課題へ格下げ）。

## 成功条件

Phase 6までの完了（クラウド費用ゼロ） + 04のチェックリストの（任意）印を除く全消化。Phase 7（AWS、使い捨て）は任意。副産物として還流物リスト（core一式、scaffold、compose、FGA最小モデル、Keycloak realm定義とclaim mapper設定の知見、GitHub Actionsパイプライン一式、検証で見つかった規約の修正点、マイグレーションツール比較のADR草案）が揃っていること。
