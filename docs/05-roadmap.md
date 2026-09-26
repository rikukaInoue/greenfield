# 段階検証ロードマップ（Phase 0〜6）

<!-- 区分: 検証ビルド / ステータス: Draft（試作） -->

順序の思想は「**独自設計の主張が濃い順**」。フィーチャーフラグ×オンラインマイグレーション（Phase 1）とGitHub Actionsによるサービス単位デプロイ分離（Phase 2）を主役に置き、標準に乗るだけの認証（Keycloak）は後方へ縮小した。各ステージは数時間〜週末1回で閉じる検証単位とし、**確かめること**の#は `04-milestones.md` のチェックリストに対応する。サイズ: S=数時間 / M=1日 / L=週末。

各Phaseの出口は「そこで止まっても学びが残る」状態で切る。

**費用原則**: Phase 0〜6は完全ローカルで行い、クラウド費用をゼロに保つ。CIはGitHub-hostedの無料枠、デプロイ系ジョブは手元マシンのself-hosted runner（分数消費ゼロ）。AWSはPhase 7のみとし、`terraform apply → 検証 → destroy` の短期間使い捨てで回す（立てっぱなし禁止）。

## Phase 0: 骨格 — 境界が構造で守られるリポジトリ

| # | ステージ | 作るもの | 確かめること | サイズ |
|---|---|---|---|---|
| 0.1 | モジュール骨格 | go.work、core/、services/order(+client)、scaffold | #3 越境importがビルド不能 | M |
| 0.2 | DB基盤 | mysql:8 compose、ロール/GRANT、golang-migrate二系統+履歴分離、migrateサブコマンド | #1 他DBへのSELECTが権限エラー | M |
| 0.3 | sqlc+整合CI | sqlc、mysqldump一致検証、テーブル許可リストlint | #2 越境クエリが生成時に落ちる | M |
| 0.4 | API骨格 | huma 3リスナー、RFC 9457+code、api/出力、oasdiff | 3ポート独立、破壊的変更でCI失敗（#16前半） | M |
| 0.5 | 開発ループ | dev/allinone、localauthz、StaticAuthenticator | Tier 1だけで開発が回る。Staticでも認証ミドルウェアとPrincipal伝播は全経路（internal含む）で有効——素通しの直叩きを作らない | S |
| 0.6 | CI初期版 | Actionsワークフローの骨格（Phase 2で拡張する土台） | CIが後付けにならないこと自体 | S |

## Phase 1: 縦切り + ★オンラインマイグレーション完全版

| # | ステージ | 作るもの | 確かめること | サイズ |
|---|---|---|---|---|
| 1.1 | Atomic | ctx運搬tx、queries(ctx)、偽Atomic、CreateOrder(Entity) | #4 fn内2Repo+故意エラーで両方ロールバック | M |
| 1.2 | CQS | Read Model直行の一覧、:verbコマンド | コマンド=Entity経由 / クエリ=tx外の実地 | M |
| 1.3 | 認可呼び出し固定 | Can/ListAccessible/WriteRelations（localauthz） | #5 失敗注入で注文ごと消える、#6 孤児無害、#7（local版） | M |
| 1.4 | フラグ | OpenFeature+flagd、入口ミドルウェア→ctx | OFF/ON切替、基盤停止時に安全側 | S |
| 1.5a | 観測手段 | 負荷スクリプト（連続読み書き）+ 新旧カラム一致チェッカー | 検証の観測手段そのもの | S |
| 1.5b | expand+二重書き | INSTANT/INPLACE確認、旧バイナリ並走（allinone×2） | #15 全段エラーなし | M |
| 1.5c | バックフィル | BGジョブ（分割・冪等・中断→再開）+ 検算 | #25 負荷中に改名一式、エラー0・一致100% | M |
| 1.5d | フラグ切替 | 1%→50%→100%（key固定確認）→ OFF巻き戻しドリル → 削除 | #21 切替・巻き戻しにデプロイ不要 | M |
| 1.5e | contract | sqlc参照ゼロ+フラグ削除の2ゲート→DROP、lock timeout実験 | #19 ロック行列で自分が退く | S |

**出口**: 負荷をかけたまま、デプロイ2回とフラグ操作だけでカラム改名が完走し、いつでも戻れた記録。最重要成果物その1。

## Phase 2: ★デプロイ分離（GitHub Actions）

分離の証明相手として、scaffoldで空の第2サービス（user骨格）を生成する（scaffoldの初仕事を兼ねる）。

| # | ステージ | 作るもの | 確かめること | サイズ |
|---|---|---|---|---|
| 2.1 | 差分検知CI | paths-filterでモジュール単位のbuild/test/migrate検証/lint。依存グラフ発火（core/**→全部、services/order/**→orderのみ、client再生成→利用側） | #22 orderのPRでuserジョブ不実行 / #23 core変更で全発火 | M |
| 2.2 | 契約伝播 | api/コミット→oasdiff→client生成→メジャー連動でconsumerへPR | #16 完成形（/v2並行含む） | M |
| 2.3 | デプロイ順序 | デプロイ先は手元マシンのcompose + self-hosted runner（費用ゼロ。ECSはPhase 7）。`migrate expand → deploy → healthcheck` をジョブグラフで強制 | #24 migrate失敗でデプロイ不開始・無傷 | L |
| 2.4 | 並列と直列 | concurrency group（同一サービス直列・別サービス並列） | #26 相互に待たない・追い越さない | S |

**出口**: モノレポなのにサービスごとに独立して安全に出る、の実証。最重要成果物その2。1.5系と組み合わせると「orderのオンライン改名の最中にuserを普通にデプロイできる」まで示せる。

## Phase 3: 認証・認可（縮小版）

| # | ステージ | 作るもの | 確かめること | サイズ |
|---|---|---|---|---|
| 3.1 | Keycloak | composeへ追加、realm-as-code（clients: ssr/agent/svc-*、claim mapper、TOTP）。CIでも同一realmが立つこと | #13 クレームが載る | M |
| 3.2 | authzサービス | check/batch/list-objects/tuples:write + FGA最小モデル + consistency hint（自作の本丸） | #7 本番版（HIGHER_CONSISTENCYで作成直後可視） | M |
| 3.3 | 差し替え | localauthz/Static → oidcauthn+authzhttp配線。**人間・M2Mの両方**（svc-*のclient_credentials有効化 + core/httpclientのトークン取得・キャッシュ） | #18 usecase/handlerのdiffゼロ、#27 内部APIが無認証で叩けない | S+ |

## Phase 4: 結合（第2コンテキストの実装）

| # | ステージ | 内容 | 確かめること | サイズ |
|---|---|---|---|---|
| 4.1 | サービス間 | user（またはinventory）を実装、client module、M2M+scope、境界での型変換 | 生成→配布→interface受けの一連 | M |
| 4.2 | Eventual | outbox+relay+SNS/SQS(LocalStack)+inbox | #8 relay停止→回復、#9 重複無害化 | L |
| 4.3 | ReplicaView | イベント購読→表示用複製、outbox再生 | #10 再構築一致 | M |
| 4.4 | 同期コマンド | pending状態+冪等キー+回収ジョブ | #11 相手停止→pending→回収で確定 | L |

## Phase 5: 体験層（優先度低・一部任意）

Tier 2（Caddy） / SSR confidential client+loader合成（#17） / （任意）ステップアップE2E（#12） / （任意）admin+MCPのhuman-in-the-loop（#14）。動機が出たときに着手する。

## Phase 6: 運用・還流（ローカル）

otelで昇格シグナルの最小ダッシュボード（プール使用率・authzレイテンシ・outbox滞留） / goose比較1周（#20） / verification-log整理と還流パッケージ化（01の成功条件）。ここまででクラウド費用ゼロのまま、チェックリストの（任意）以外が消化される。

## Phase 7: AWS検証（任意・短期間の使い捨て）

`infra/` のTerraformで最小構成（単一VPC + ALB + ECS + RDS MySQL）を立て、ローカルでは代替だった部分だけを実機で再演し、**当日〜数日でdestroyする**。対象: (1) ECSネイティブB/G・カナリアで#24/#26を再演（2.3のデプロイ先をECSへ差し替えるだけの形にしてある）、(2) 実SNS/SQS FIFOでLocalStackとの挙動差分の確認（#8/#9再演）、(3) （さらに任意）Aurora MySQLでのINSTANT DDL挙動。apply→destroyが素直に繰り返せること自体がIaC（infra/）の検証になる。費用は数日で収まる規模に留め、予算上限を決めてから立てる。
