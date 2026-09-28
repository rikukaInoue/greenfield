# アップストリームパッケージ（還流物の一覧）

検証ビルドの副産物をプロダクションへ持ち帰るための一覧（6.3 / #61）。
このリポジトリの docs では従来「還流」と呼んできたもので、英語では upstreaming。

持ち帰り方は3種類に分かれる。**どれに当たるかは、部品がポリシー（組織ごとの判断）を
含むかで決まる。** ポリシーを含むものをバージョン付き依存にすると、調整のたびに
fork か設定の分岐が要る。そういうものはコピーして相手に所有させる。

| 持ち帰り方 | 対象の性質 | 例 |
|---|---|---|
| A. ライブラリ（module として参照） | ドメインもポリシーも含まない | problem / logger / middleware |
| B. コピー（雛形として所有させる） | ポリシーそのもの | scaffold・CI 検査・compose・規約 |
| C. 判断だけ（文書として渡す） | 実装は文脈依存 | ADR・規約の修正点 |

## 還流物リスト（01-scope の成功条件）と所在

### 1. core 一式

| パッケージ | 分類 | 依存 | 検証記録 / 根拠 |
|---|---|---|---|
| `core/problem` | A | huma（応答形式の定義のみ） | RFC 9457 + 機械可読 code。stage-04 |
| `core/logger` | A | stdlib のみ | internal-06 §10.1。#142 |
| `core/middleware` | A | stdlib のみ | traceparent 相関・canonical log line・panic→problem。#142（変異テストで検査の実効性を確認） |
| `core/httpapi` | B寄りのA | huma + chi | リスナー3面の組み立て。**huma+chi という選定ごと持ち帰るか**が分かれ目（ADR 0019） |
| `core/consistency` | A | stdlib のみ | Atomic / Eventual の契約。stage-11、ADR 0012 |
| `core/authz` | A（interface）+ B（ローカル実装） | interface は stdlib のみ | 差し込み口の切り方が資産。localauthz / staticauthn / devtoken は開発用でコピー側。stage-11、3.3 で oidcauthn + authzhttp への差し替えが usecase/handler 無変更で成立（check #18） |
| `core/flags` | A | OpenFeature SDK | 評価とミドルウェアのみ。プロバイダは持たない（ADR 0010 / 0013） |
| `core/runtimeenv` | B | stdlib のみ | 開発用実装の許可リスト（ADR 0016）。ENV の語彙が組織ごとに違うのでコピー |
| `core/httpclient` | —（未実装） | — | #138。実装は 3.3〜4.4 の知見を待って設計する |

抽出コストは低い。core は最初から独自 go.mod の別モジュールで、境界は引き終わっている。
**2人目の実消費者が現れるまで module 公開はしない**（呼び手のいない抽出は投機）。

### 2. scaffold

`dev/scaffold`（`mise run scaffold <name>`）。分類 B。新サービスの骨格
（3リスナー・migrate サブコマンド・sqlc 設定・compose 追記・go.work 追記）を生成する。
stage-20 で gear を実際にこれで生やした。

### 3. compose 一式

`deploy/compose/`。分類 B。mysql（init SQL に database / ユーザー / GRANT の雛形）、
Keycloak、OpenFGA、flagd、RustFS、LocalStack、deploy プロファイル。
GRANT の設計（app / migrate の2ユーザー、他 database へは SELECT すら不可）は
internal-05 の強制層としてセットで持ち帰る。

### 4. FGA 最小モデル

`platform/authz/fga/`。分類 B + C。モデル（user / platform / photo、operator の
継承）と、action→relation マッピングをサービス内に閉じる構造。検証は stage-32
（check #7 本番版: HIGHER_CONSISTENCY で作成直後可視）。

### 5. Keycloak realm 定義と claim mapper の知見

`deploy/compose/keycloak/realm.json`。分類 B。clients 3種（ssr / agent / svc-*）、
claim mapper、TOTP。検証は stage-31（check #13: カスタムクレームが載る）。
realm-as-code の運用規約（手動変更は export して JSON へ戻す）ごと持ち帰る。

### 6. GitHub Actions パイプライン一式

`.github/workflows/` + `.github/actions/prepare`。分類 B。

- ci.yml: 差分検知（affected、ADR 0015）→ モジュール単位ジョブ + 全体検査
  （cross-db-fk は**モジュール単位に相乗りできない**例外。internal-05）
- deploy.yml: migrate → release → healthcheck の**ジョブグラフで順序を強制**
  （stage-23: migrate 失敗でデプロイが始まらないことを実測）、
  concurrency group で同一サービス直列・別サービス並列（stage-24: 2ランナー実測）
- api-docs.yml: 契約から人が読めるドキュメントを生成（#77）

### 7. 規約の修正点

分類 C。検証で見つかった「文書と実物の食い違い」の記録は `docs/audit-2026-09-27.md`
に対処状況ごと残っている。プロダクションの規約へ反映すべき代表例:

- 跨ぐ FK の禁止は文章では守れない → 機械検査を置く（internal-05、#136）
- 「モジュール間のマイグレーションは独立並行できる」の前提条件を明記（同上）
- `core/httpclient` のような**存在しない参照を現在形で書かない**（#137 #138）
- ログ規約一式（internal-06 §10.1。相関ID・canonical log line・許可リスト方式）

### 8. マイグレーションツール比較の ADR 草案

`docs/adr/0020-migration-tool-comparison.md`。分類 C。golang-migrate と goose を
同条件で実測した比較（stage-62）。要点: 設計はツール非依存で成立（check #20）、
差は失敗状態の管理・欠番検出・ロックの既定。

## 検証記録の索引

`docs/verification-log/README.md`（38本、Phase 別）。持ち帰る部品には対応する
ログを添える。**「動く」だけでなく「壊れ方を測ってある」ことがこのパッケージの価値**
で、たとえば middleware は panic 時に EOF になることを、deploy は migrate 失敗時に
release が始まらないことを、それぞれ壊してから直している。

## 持ち帰らないもの

- `services/photo` / `services/gear` の業務コード（検証用の題材。構造だけ scaffold が運ぶ）
- `dev/allinone`（ローカル専用と明記済み）
- `frontend/`（SSR の検証用。トークン非露出の設計判断のみ C として持ち帰る）
- 監査で「誤りだった」と訂正済みの主張（audit の訂正欄が正）
