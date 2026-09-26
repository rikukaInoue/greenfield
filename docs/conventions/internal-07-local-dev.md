# ローカル開発環境

<!-- 区分: 内部設計 / ステータス: Draft -->

**原則**。アプリケーションはALB非依存とする。アプリが知るのはListenするポートだけであり、TLS・ホスト名・到達制御はインフラの持ち物。呼び先URLは環境変数注入、OIDC issuerは設定値。したがってローカル環境はALBの3責務（TLS終端 / ホスト名振り分け / 到達制御）のうち、必要なものだけを安い代替で置く。

## Tier 1: ポート直（日常の既定）

プロキシなし。各サービスの3リスナーをポート割当表に従って直接叩く。

```
order:     8080 (external) / 8081 (internal) / 8082 (admin)
user:      8090 / 8091 / 8092
（新サービスは +10 で採番。scaffoldが割当表を更新する）
```

`dev/allinone` で全サービスを1プロセス起動できる（通信はlocalhostの各ポートを通すHTTPのまま。in-process呼び出しの近道は作らない）。SSRを動かす場合もAPI base URLにlocalhostポートを注入するだけ。認証は、基盤稼働前はローカル実装（localauthz / StaticAuthenticator）、基盤稼働後はローカルHydraからトークンを取得するヘルパCLI（`dev token --user <name> --aal <n>`。curl・HTTPクライアント用）を用いる。サービス単体の開発ループはこのTierで完結し、ホスト名の概念を持ち込まない。

## Tier 2: ローカルALB（Caddy。結合・認証フローの検証時）

docker composeにCaddyを1つ置き、本番のサブドメイン構成と同型のホストベースルーティングを組む。

```
order.api.localhost → order:8080
user.api.localhost  → user:8090
admin.localhost     → 各サービス:8082
auth.localhost      → Hydra / ログインUI
app.localhost       → SSR
```

`*.localhost` はブラウザがループバックへ解決する（RFC 6761）ため/etc/hosts不要。curl等ホストのCLIから叩く場合のみ `--resolve` またはhosts追記。localhostはブラウザのsecure context扱いのため、TOTP / WebAuthnはTLSなしで動作する。secure cookie等TLSが必要な検証のみmkcertでCaddyに証明書を配る。

**issuer整合（このTierの存在理由）**。OIDCのissuer / JWKS URLは、ブラウザとサーバ側コンテナ（SSR、各サービスのJWKS取得）の両方から同じ名前で到達できなければならない。CaddyをComposeネットワーク内に置き、`auth.localhost` 等を**network aliasとして張る**ことで、コンテナ内の名前解決もCaddyへ向け、issuerを `http://auth.localhost` の1つに固定する。認証フロー・ステップアップのリダイレクト・サブドメインCORS・SSEを通す検証はこのTierで行い、それ以外はTier 1で足りる。

## 模擬しないもの

WAF・adminのIP制限/ZTNA・TGW・egress統制はローカルで模擬しない。到達制御の実効性はネットワーク層ではなくGRANT・scope・JWT検証に置いてあるため（§2, §5）、セキュリティ規約の検証はALBなしでそのまま成立する。

## ルーティング表の同型性

host →（service, port）の対応表を本書の1枚の真実として保持し、Caddyfile（ローカル）とALBリスナールール（IaC）を同じ表から書く（生成の自動化は任意）。ローカルと本番でルーティングの形を乖離させない。ポート割当表・ホスト対応表の変更はこのファイルの更新とセットで行う。
