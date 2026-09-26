# 基盤: Keycloak（認証） + platform/authz（認可）

<!-- 区分: 検証ビルド / ステータス: Draft（試作） -->

認証はKeycloak（セルフホスト、realm-as-code）に任せ、認可（authzサービス + OpenFGA）のみ自作する。試作の主眼（CI/CD分離・オンラインマイグレーション）にリソースを寄せるための割り切りであり、Kratos/Hydra・token hookの構築はプロダクション側タスクとして残る。

## Keycloak

認証はKeycloak（セルフホスト、コンテナ1個）に任せ、ログインUI・ユーザー管理・MFAは自作しない。SaaS（Auth0等）を採らない理由は、CIの中で認証込みの結合テストを並列・無制限に回すためにローカル完結が必要なこと（クォータ・レート・ネット依存を持ち込まない）。代償はメモリ1GB弱と起動数十秒であり、試作の単一ホストでは誤差とする。

**realm-as-code**。realm定義JSON（clients・ロール・claim mapper・TOTP設定）を `deploy/compose/keycloak/realm.json` としてgit管理し、`start-dev --import-realm` で毎回同一状態を再現する。CIでも同じコンテナが立つ。手動でのAdmin Console変更はexportして必ずJSONへ戻す。

登録するクライアントは3種: `ssr`（confidential、Authorization Code + PKCE）、`agent`（public、PKCE。任意フェーズ用）、`svc-photo` / `svc-gear` 等（service accounts有効 = client_credentials、scope `internal:*`）。カスタムクレームはprotocol mapper（claim mapper）で注入する（`auth_time` は標準クレーム、amr・org等はmapper）。M2Mトークンのキャッシュ（期限まで再利用）はクォータ理由がなくても `core/httpclient` の規約として維持する。

ステップアップは当面対象外とし、コード側は `RequireAAL` の呼び出し語彙だけを固定して簡易AssuranceCheckerで進める。後日実施する場合、KeycloakはACR↔LoAマッピングとStep-up Authenticationを標準機能として持つため、realm設定の追加のみで任意課題（#12）に着手できる——SaaSに対するもう1つの優位点である。

JWT検証は汎用のOIDC検証（issuer + JWKS）であり、プロダクションのhydraauthnと同じ構造の `oidcauthn` として実装する。issuerの整合は02のとおり（Tier 1はホストプロセス実行で `http://localhost:8180`、Tier 2はCaddy aliasで `auth.localhost`）。


## authzサービス（platform/authz）

API契約はプロダクション版 external/05 と同一: `POST /check`、`POST /batch-check`、`POST /list-objects`、`POST /tuples:write`。action→relationのマッピングはこのサービス内に閉じ、プロダクトには語彙（action名・resource type名）だけを見せる。consistency hint（HIGHER_CONSISTENCY相当）をリクエストからOpenFGAへ透過する。`tuples:write` は同一タプルへの重複適用が無害（自然冪等）であることをAPIの性質として保証する（Eventual化への備え）。

FGA最小モデル:

```
type user
type platform
  relations
    define operator: [user]
    define support: [user]
type photo
  relations
    define parent: [platform]
    define owner: [user]
    define viewer: owner or operator from parent
    define editor: owner or operator from parent
```

オペレータの権限を専用機構なしでReBACに載せる検証（adminリスナーからの `Can` がplatform operatorで通ること）を含む。

## サービス間認証

KeycloakのM2M（client_credentials、service accounts）JWT + scope。`core/httpclient` がトークン取得・キャッシュ・自動付与・`Idempotency-Key`・トレースID伝播を担う。プライベートネットワークを理由とした無認証は試作でも採らない（構造をプロダクションと揃える）。
