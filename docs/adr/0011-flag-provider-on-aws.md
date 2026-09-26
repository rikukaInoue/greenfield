# 0011. AWS では AppConfig 用の OpenFeature プロバイダを自作する

## 背景

ローカルは flagd（定義はリポジトリ内のファイルを git 管理）で動いている。AWS（Phase 7）でのフラグ基盤を決める必要がある。

`go-sdk-contrib/providers` に AWS AppConfig のプロバイダは**存在しない**。AWS 系は `aws-ssm`（Parameter Store）だけ。

| 選択肢 | 内容 | 評価 |
|---|---|---|
| **A. AppConfig プロバイダを自作** | `appconfigdata` SDK（`StartConfigurationSession` / `GetLatestConfiguration`）を `openfeature.FeatureProvider` として実装 | AppConfig の段階デプロイ・ベイク時間・CloudWatch アラームでの自動ロールバックがそのまま使える |
| B. flagd を S3 sync で動かす | flagd の `--uri` は S3 / GCS / Azure Blob / HTTP を取れる | 追加実装ゼロ。ローカルと同じ flagd だが、%の刻みと自動ロールバックは自前 |
| C. AppConfig SDK を直接使う | OpenFeature を捨てる | 規約（OpenFeature を語彙とする）に反する |

## 決定

**A を採る。** AppConfig 用の OpenFeature プロバイダを自作する。

理由は、AppConfig の段階デプロイ（割合を刻む + ベイク時間 + CloudWatch アラームで自動ロールバック）が、
規約の「展開順と単位（1% → 10% → 50% → 100%）」「切り替え判定はマネージドの閾値アラームから始める」と
そのまま噛み合うこと。flagd の fractional targeting では割合を自前で刻むため、この機能が使えない。

アプリのコードは変わらない。差し替えは合成ルート（`app.LocalDeps` に対する `app.AWSDeps`）で
プロバイダを交換するだけであり、`core/flags` と usecase は無変更のまま。

## 定義の置き場所

規約は「フラグ定義はリポジトリ内のファイルとして git 管理し、変更を PR レビューに載せる
（誰がいつ何%にしたかが履歴に残る）」と定める。AppConfig では定義が AWS 側にあるため、両立させる形を決める。

- 定義（`deploy/flags/*.json`）は git に置く
- **フラグ専用のパイプライン**が AppConfig へ反映する。アプリのデプロイとは独立させる

これで「切替・巻き戻しにデプロイ不要」（#21）と「履歴が残る」を同時に満たす。
デプロイ（ECS）とリリース（フラグ）を2本の独立したレバーとして扱う原則の実装でもある。

## 影響

- Phase 7 の作業に「AppConfig プロバイダの実装」が加わる。200行規模の見込み
- できたプロバイダはプロダクション版への還流物になる（OpenFeature のエコシステムに無いものなので、
  公開する価値もある）
- ローカルは flagd のまま。ローカルと本番でプロバイダが違うため、プロバイダ差し替えで
  usecase / handler が無変更であることを Phase 7 で確認する（#18 と同じ形の検証）

## 還流

AppConfig プロバイダそのもの。および「フラグ定義を git 管理しつつマネージドのフラグ基盤へ反映する」
パイプラインの形（アプリのデプロイと分離する理由を含む）。
