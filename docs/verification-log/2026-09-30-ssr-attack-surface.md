# React Router SSR 固有の攻撃面をテストで固定する（#186）

SSR には Go の API には無い攻撃面があり、そのどれも既製の検査（ZAP / govulncheck）
からは出ない。#182（認可の総当たり）と同じ発想で、5つの面をテストに固定した。

## 1. ハイドレーション payload への漏れ — `e2e/hydration-leak.spec.ts`

loader の返り値は HTML に直列化されてブラウザへ出る。トークンをサーバ側ストアに
置いた努力（check #17）は、loader が返してしまえば無効になる。トークン形状の検査は
既存の token-exposure.spec にあるので、ここではその外側を見る:

- **セッションID**（Cookie payload の中身）が HTML/JS に出ない——出れば Cookie を
  盗まずともセッションを指名できる
- `refreshToken` / `accessToken` / `idToken` というフィールド名ごと直列化される形が無い
- 内部リスナーのポート（8080〜8092）が応答に出ない
- **他人の非公開リソースが混ざらない**: bob で alice の写真の詳細と一覧を SSR し、
  alice のキャプションがどの応答にも現れない

## 2. `.server` 境界の漏れ — `e2e/bundle-leak.spec.ts`

`app/.server/` は client bundle から外されるが、間接 import では漏れうる。**ビルドが
通っても漏れる形**なので、ビルド後の `build/client/` を走査する。minify で識別子は
消えても文字列リテラルは残ることを使い、サーバ側にしか無い文字列（環境変数名
`SSR_CLIENT_SECRET` 等・開発既定値・セッションストアのパス・`openid-configuration`）
が成果物に無いことを固定。空バンドル走査で緑になる形はバイト数の下限で防ぐ。

## 3. セキュリティヘッダ — `app/.server/security.ts` + root の middleware

SSR は**ブラウザが実行する HTML** を返すので、CSP が意味を持つのは API でなくこちら。
root.tsx の middleware で全応答に付与し、単体テストで構成を、e2e でヘッダの実在と
**CSP 違反レポートがゼロのまま実画面が動く**ことを確認した。

- `default-src 'self'` / `object-src 'none'` / `base-uri 'self'` / `form-action 'self'` /
  `frame-ancestors 'none'`（+ 保険の `X-Frame-Options: DENY`）、nosniff、Referrer-Policy
- 画像ストレージの origin（`IMAGE_ORIGIN`、既定 localhost:9000）だけを
  `img-src`（表示）と `connect-src`（署名付き URL への PUT）へ追加
- **既知の妥協**: `script-src` の `'unsafe-inline'` は React Router のハイドレーション
  （ストリーミングのインライン script）のため。nonce 化には entry.server の自作が要る。
  妥協はテストに明記してあり、外した日はそのテストが気づかせる

## 4. 状態を変える GET が無い — `e2e/security-headers.spec.ts`

Cookie は sameSite=Lax でクロスサイト POST には付かないが、**Lax はトップレベル GET を
通す**ので、GET で状態が変わる経路だけが CSRF の穴になる。

- `GET /logout` を踏まされてもログアウトしない（loader は redirect のみ。実測で固定）
- resource route（写真の作成・commit）は action のみで、GET は react-router がエラーに
  する（405 でなく 500 系で落ちるのが v8 の実挙動。副作用ゼロを確認）

## 5. loader からの SSRF — `app/.server/fetch-sites.test.ts`

loader はサーバで動くので、ユーザ入力由来の URL を fetch すると SSRF になる。
「宛先を毎回目視する」は続かないので、**fetch の呼び出し箇所そのものを許可リスト化**した。
現状は4箇所（OIDC ディスカバリ/トークン交換 = 設定由来、投稿フォームの2つ = 相対 URL と
API 応答由来）。新しい fetch を書くとテストが落ち、「その URL はどこから来たか」の
レビューを1回だけ強制する。許可リストへの追記 diff がレビューの記録になる。

## 実測

- unit（node --test）: 5 テスト green
- e2e: **21 passed**（OIDC スペックは Keycloak 無しの環境では skip）
- 副産物: ローカルの :3000 が別プロジェクトに使われていると playwright の
  `reuseExistingServer` が**他人のアプリに対して検査を回す**。`E2E_WEB_PORT` で回避
  （バケット CORS は `FRONTEND_ORIGINS` に合わせて追加が要る）
