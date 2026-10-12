# 全体構成（C4 モデル）

greenfield の構成を C4 モデルの 4 段階で示す。上から順に粒度が細かくなる。
図は Mermaid の C4 記法で、GitHub 上でそのまま描画される。各図の下に、図では
伝わらない判断と制約を書く。正本は `02-architecture.md`・`03-platform.md`・
`conventions/` で、この文書は入口である。

| 段階 | 問い | 対象 |
|---|---|---|
| Context | 誰が、どのシステムと話すか | greenfield と外部システム |
| Container | 何がプロセスとして動くか | SSR・photo・gear・authz・DB・バス |
| Component | プロセスの中はどう分かれているか | photo の内部 |
| Deployment | どこで動くか | Phase 7 の AWS 検証構成（使い捨て） |

## Context — 誰が、どのシステムと話すか

```mermaid
C4Context
  title greenfield — System Context
  Person(user, "利用者", "ブラウザで写真を投稿・閲覧する")
  Person(operator, "プラットフォーム運用者", "管理操作。危険操作はステップアップ認証")
  Person(agent, "自律エージェント", "MCP クライアント。危険操作は人間の再認証が要る")
  Person(dev, "開発者", "dev/ の CLI と compose で検証を回す")

  Enterprise_Boundary(gf, "greenfield") {
    System(app, "greenfield", "写真と機材のマルチドメイン API と SSR。サービスごとに別デプロイ")
  }

  System_Ext(kc, "Keycloak", "OIDC OP。利用者ログイン・ステップアップ・サービス間の M2M トークン")
  System_Ext(fga, "OpenFGA", "ReBAC エンジン。authz からのみ到達")
  System_Ext(db, "MySQL", "サービスごとに database を分ける")
  System_Ext(s3, "オブジェクトストレージ", "画像。ローカルは RustFS、AWS は S3。署名付き URL")
  System_Ext(bus, "SNS + SQS FIFO", "サービス間イベント。ローカルは LocalStack")
  System_Ext(flags, "フィーチャーフラグ", "ローカルは flagd、AWS は AppConfig。評価はプロセス内")
  System_Ext(obs, "観測スタック", "Prometheus / Tempo / Loki / Grafana")

  Rel(user, app, "投稿・閲覧", "HTTPS")
  Rel(operator, app, "管理操作", "HTTPS")
  Rel(agent, app, "admin API を MCP ツール経由で", "HTTPS")
  Rel(dev, app, "起動・検証", "CLI")
  Rel(user, kc, "ログイン・TOTP", "OIDC")
  Rel(app, kc, "JWT 検証・M2M トークン取得", "OIDC / JWKS")
  Rel(app, fga, "判定・一覧", "authz 経由")
  Rel(app, db, "読み書き", "MySQL")
  Rel(app, s3, "署名付き URL で直接アップロード", "S3 API")
  Rel(app, bus, "outbox → 配送 → inbox", "SNS / SQS")
  Rel(app, flags, "定義を同期", "flag sync / AppConfig")
  Rel(app, obs, "メトリクス・トレース・ログ", "Prometheus / OTLP / stdout")
  UpdateLayoutConfig($c4ShapeInRow="4", $c4BoundaryInRow="1")
```

図から読み取れないこと。

- 外部システムはすべて差し替え可能な前提で、つなぎ先は環境変数（URL / ARN / DSN）だけで決まる。コードの中に「同じアカウントにいる」前提は無い。本番系はアカウントを分けて TGW でつなぐ想定で、greenfield は 3 リスナーとポート分離で構造だけを模擬している（01-scope）
- 認証と認可は別のシステムである。Keycloak は「誰か」を決め、OpenFGA は「何をしてよいか」を決める。アプリは OpenFGA を直接呼ばず、authz サービスを経由する（03-platform）
- 自律エージェントは利用者と同じ入口を使うが、持てるトークンが AAL1 までに制限されている。危険操作は構造的に人間の再認証を要求する（check #14）

## Container — 何がプロセスとして動くか

```mermaid
C4Container
  title greenfield — Containers
  Person(user, "利用者", "ブラウザ。Cookie のみ")
  Person(agent, "自律エージェント", "MCP クライアント")

  System_Boundary(gf, "greenfield") {
    Container(ssr, "frontend SSR", "React Router", "confidential client。トークンはサーバ側だけが持つ")
    Container(photo, "photo", "Go", "写真。external :8080 / internal :8081 / admin :8082 / metrics :9091")
    Container(gear, "gear", "Go", "機材。external :8090 / internal :8091 / admin :8092 / metrics :9092")
    Container(authz, "authz", "Go", "認可サービス :8100。判定・一覧・タプル書き込み")
    Container(mcp, "mcpadmin", "Go", "admin API を MCP ツールとして公開。agent トークンで呼ぶ")
    ContainerDb(photodb, "photo DB", "MySQL database", "写真・outbox")
    ContainerDb(geardb, "gear DB", "MySQL database", "機材・inbox")
    ContainerDb(fgadb, "openfga DB", "MySQL database", "タプル。authz 専用")
  }

  System_Ext(kc, "Keycloak", "realm-as-code。clients: ssr / agent / svc-photo / svc-gear")
  System_Ext(fga, "OpenFGA", ":8280")
  System_Ext(s3, "RustFS / S3", "画像")
  ContainerQueue(topic, "SNS FIFO トピック", "photo のイベント", "")
  ContainerQueue(queue, "SQS FIFO キュー", "gear-photo-events", "")
  System_Ext(flagd, "flagd", "flag sync :8015")
  System_Ext(obs, "観測スタック", "Prometheus / Tempo / Loki")

  Rel(user, ssr, "HTML", "HTTPS")
  Rel(user, kc, "ログイン", "OIDC Authorization Code + PKCE")
  Rel(ssr, photo, "v1 / v2 API", "HTTPS :8080")
  Rel(ssr, gear, "API", "HTTPS :8090")
  Rel(agent, mcp, "ツール呼び出し", "MCP")
  Rel(mcp, photo, "admin API。危険操作は 401 で止まる", "HTTPS :8082")
  Rel(photo, gear, "紐付け。冪等キー付き", "HTTPS :8081 → :8091、client_credentials")
  Rel(photo, kc, "JWT 検証・M2M トークン", "JWKS / token endpoint")
  Rel(gear, kc, "JWT 検証", "JWKS")
  Rel(photo, authz, "判定・一覧", "HTTPS :8100")
  Rel(gear, authz, "判定", "HTTPS :8100")
  Rel(authz, fga, "Check / ListObjects / Write", "HTTP :8280")
  Rel(authz, fgadb, "", "MySQL")
  Rel(photo, photodb, "Atomic + outbox", "MySQL")
  Rel(gear, geardb, "inbox + 業務", "MySQL")
  Rel(photo, s3, "署名付き URL を発行。本体はブラウザが直接置く", "S3 API")
  Rel(photo, topic, "relay が outbox を送る", "SNS Publish")
  Rel(topic, queue, "ファンアウト", "raw message delivery")
  Rel(queue, gear, "consumer がポーリング。inbox で重複排除", "SQS Receive")
  Rel(photo, flagd, "定義を同期。評価はプロセス内", "gRPC sync")
  Rel(gear, flagd, "定義を同期", "gRPC sync")
  Rel(photo, obs, "metrics / OTLP / stdout", "")
  Rel(gear, obs, "metrics / OTLP / stdout", "")
  UpdateLayoutConfig($c4ShapeInRow="4", $c4BoundaryInRow="1")
```

図から読み取れないこと。

- 各サービスは external / internal / admin の 3 リスナーを別ポートで持つ。本番では ALB の 3 責務（TLS 終端・ホスト名振り分け・到達制御）に対応し、アプリは Listen するポートしか知らない（internal-07）
- サービス間の同期呼び出しは internal リスナーだけで、Keycloak の client_credentials による M2M トークンを `core/httpclient` が付ける。IAM ロールではつないでいないので、アカウントをまたいでも同じ仕組みで動く
- 非同期の経路は outbox → relay → SNS → SQS → inbox で、at-least-once。重複は受信側の inbox が弾き、relay が止まっても未送信レコードは DB に残る（check #8）。配送基盤の実装は `eventbus/snssqs` にあり、core は差し込み口しか持たない（ADR 0022）
- 画像の本体はアプリを通らない。photo が署名付き URL を発行し、ブラウザがストレージへ直接置く（ADR 0009）
- フラグは定義を同期してプロセス内で評価する。評価のたびに外部を呼ばないので、flagd が落ちても最後の定義で動く（ADR 0013）
- 観測は 3 本とも計装済みで、ログの trace_id とトレースの trace_id は同じ値（internal-06 §10.1）。compose の `obs` プロファイルで Grafana から相互に辿れる

## Component — photo の内部

```mermaid
C4Component
  title photo — Components
  Container_Boundary(photo, "photo") {
    Component(ext1, "handler/external", "huma", "v1 アダプタ。/photos")
    Component(ext2, "handler/external/v2", "huma", "v2 アダプタ。/v2/photos。同じ usecase につながる")
    Component(intl, "handler/internalapi", "huma", "サービス間 API。scope internal:*")
    Component(adm, "handler/admin", "huma", "管理 API。危険操作は AAL2")
    Component(mw, "core/httpapi + middleware", "Go", "相関 ID → アクセスログ → recover → 認証。選択不可で必ず積む")
    Component(uc, "usecase", "Go", "業務の手順。バージョンを持たない")
    Component(dom, "domain", "Go", "業務ルール。書き込み経路だけが通る")
    Component(repo, "repository", "sqlc", "Atomic な書き込み。outbox への追記も同じトランザクション")
    Component(rm, "readmodel", "sqlc", "読み取り専用クエリ")
    Component(rv, "replicaview", "sqlc", "他サービス由来の表示用データ。業務判断には使えない")
    Component(cons, "consistency", "core", "outbox / relay / inbox")
    Component(gl, "gearlink", "gear-client", "gear の internal API を呼ぶ。冪等キー・デッドライン伝播")
    Component(az, "authzhttp", "core", "authz サービスへの判定呼び出し")
    Component(an, "oidcauthn", "core", "JWT 検証。JWKS を Keycloak から取る")
    Component(bs, "blobstore", "S3 SDK", "署名付き URL の発行。差し込み口")
    Component(fs, "flagsource", "OpenFeature", "フラグ定義の同期と評価。差し込み口")
    Component(tel, "telemetry", "OTel / Prometheus", "トレーサとメトリクス。合成ルートだけが触る")
  }
  ContainerDb(db, "photo DB", "MySQL", "")
  System_Ext(gear, "gear internal", ":8091")
  System_Ext(authz, "authz", ":8100")
  System_Ext(kc, "Keycloak", "")
  System_Ext(s3, "RustFS / S3", "")
  System_Ext(sns, "SNS FIFO", "")

  Rel(ext1, mw, "")
  Rel(ext2, mw, "")
  Rel(intl, mw, "")
  Rel(adm, mw, "")
  Rel(mw, an, "認証")
  Rel(mw, uc, "")
  Rel(uc, dom, "")
  Rel(uc, repo, "書き込み")
  Rel(uc, rm, "読み取り")
  Rel(uc, rv, "表示用")
  Rel(uc, az, "認可")
  Rel(uc, gl, "紐付け")
  Rel(uc, bs, "URL 発行")
  Rel(uc, fs, "フラグ評価")
  Rel(repo, db, "")
  Rel(rm, db, "")
  Rel(rv, db, "")
  Rel(cons, db, "outbox を読む")
  Rel(cons, sns, "relay が送る")
  Rel(gl, gear, "")
  Rel(az, authz, "")
  Rel(an, kc, "JWKS")
  Rel(bs, s3, "")
  UpdateLayoutConfig($c4ShapeInRow="4", $c4BoundaryInRow="1")
```

図から読み取れないこと。

- v1 と v2 は同じ usecase につながる別のアダプタで、バージョンを持つのは handler だけ。旧版の廃止はアダプタ 1 つとスペック 1 ファイルの削除で済む（ADR 0017）
- 差し込み口（blobstore / flagsource）と replicaview の使い方は depguard が import 単位で強制する。規約ではなく lint が止める
- `core/*` は横断的関心事だけを持ち、OTel SDK や Prometheus クライアントは `telemetry` モジュールに置く。core を使う全サービスに依存を抱えさせない（internal-06）
- 書き込みは Atomic（1 トランザクション）で、同じトランザクションの中で outbox に追記する。イベントの送信はトランザクションの外で relay が行う（ADR 0012）

## Deployment — Phase 7 の AWS 検証構成

```mermaid
C4Deployment
  title greenfield — Phase 7 deployment（使い捨て検証構成）
  Deployment_Node(aws, "AWS アカウント（単一）", "ap-northeast-1") {
    Deployment_Node(vpc, "VPC", "public subnet のみ。NAT Gateway 無し") {
      Deployment_Node(alb, "ALB", "HTTP / HTTPS。B/G 用に target group を 2 つ") {
        Container(lb, "listener rule", "photo", "")
      }
      Deployment_Node(ecs, "ECS Fargate", "cluster main。public IP、ingress は ALB からのみ") {
        Container(photo, "photo", "Go コンテナ", "ECR のイメージ。awslogs で CloudWatch Logs へ")
        Container(boot, "bootstrap task", "Go", "マイグレーションと初期データ")
      }
      Deployment_Node(rds, "RDS MySQL", "db.t4g.micro / Single-AZ（検証時だけ Multi-AZ）") {
        ContainerDb(db, "photo / localauthz", "database", "")
      }
    }
    Deployment_Node(svc, "リージョンサービス", "") {
      Container(s3, "S3 bucket images", "画像", "")
      Container(ssm, "SSM Parameter Store", "DSN・DB パスワード", "taskdef の environment に平文を置かない")
      Container(cw, "CloudWatch Logs", "保持 1 日", "")
      Container(ac, "AppConfig", "フィーチャーフラグ", "flagprovider/appconfig が同期")
      Container(ecr, "ECR", "photo イメージ", "cosign 署名・SBOM attestation")
    }
  }
  Rel(lb, photo, "HTTP", "target group")
  Rel(photo, db, "MySQL", "")
  Rel(photo, s3, "署名付き URL", "")
  Rel(photo, ssm, "起動時に取得", "task role")
  Rel(photo, cw, "stdout", "awslogs")
  Rel(photo, ac, "定義を同期", "")
  Rel(ecr, photo, "pull", "")
  UpdateLayoutConfig($c4ShapeInRow="3", $c4BoundaryInRow="1")
```

図から読み取れないこと。

- この構成は使い捨てである。`terraform apply` → 検証 → 当日 `destroy` で回し、立てっぱなしにしない。state はローカルで、remote state を持たない。Phase 7 の全検証（7.1〜7.5）で費用は $2.5 未満
- AWS に載せたのは photo だけで、gear と authz は載せていない。Keycloak / OpenFGA / SNS+SQS も AWS 上には無い（SNS/SQS の実機再演は 7.3 で別途）。サービスごとにアカウントを分けるには、Terraform のサービス単位への分割、ネットワーク接続（本番は TGW）、SNS/SQS のアカウント越えポリシー、デプロイの送り先ごとのロールが要る。コード側はつなぎ先が全部環境変数なので変更は要らない
- 観測はログだけ CloudWatch Logs へ送る。トレースは OTLP の送り先が未設定なので無効（no-op）で、アプリのメトリクスは集めていない。ALB の 5xx アラームだけがある
- NAT Gateway を置かず public subnet にタスクを置くのは費用のため。到達制御は security group で行う

## 読み方

- 新しいサービスを足すときは Container 図に 1 つ足し、external / internal / admin の 3 リスナーと DB、authz・Keycloak への線を同じ形で引く。`mise run scaffold <name>` が骨格を作る
- サービスを切り出したり除却したりするときの手順と実測は `verification-log/2026-09-30-boundary-redraw-drill.md`
- 図と実体がずれていないかは、この文書では検査していない。構成を変えた PR でこの文書を更新する
