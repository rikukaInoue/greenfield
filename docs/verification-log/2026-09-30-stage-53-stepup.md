# 2026-09-30 — [5.3] ステップアップ認証を E2E で実測（#57 / check #12）

環境: Keycloak(realm-as-code に browser-loa フロー + alice の TOTP エンロール)、
openfga / mysql / authz(OIDC) / photo(OIDCDeps)。`mise run stepup:check` で再現。

## 主張と結果（RFC 9470）

「aal1 のトークンで AAL2 必須の操作 → 401 + WWW-Authenticate → TOTP のみ追加で昇格 → 成功」を通した。

| 段 | 結果 |
|---|---|
| aal1 ログイン（パスワードのみ） | acr=aal1 のトークン取得 |
| aal1 トークンで削除 API | **401** + `WWW-Authenticate: Bearer error="insufficient_user_authentication", acr_values="aal2"` |
| チャレンジどおり acr_values=aal2 で再認可 | acr=aal2 へ昇格。**追加入力は TOTP のみ**（パスワード再入力なし = 同一 SSO セッションの step-up） |
| aal2 トークンで同じ削除 API | **200** |
| aal1 トークンを使い回し | 引き続き **401**（トークンの再利用で昇格しない） |

## realm への追加（realm-as-code）

- `acr.loa.map = {aal1:1, aal2:2}`（oidcauthn の `acr=="aal2"→AAL2` 判定と揃える）
- browser-loa フロー: LoA1=password / LoA2=+TOTP の条件付きサブフロー
  （`conditional-level-of-authentication` の loa-max-age を LoA2 は 0 にして毎回 TOTP を強制）

## 踏んだ罠（実装ではなく検証手順側。2つとも「無トークンでは見えない」種類）

1. **credentials に otp を混ぜると password ごとインポートが静かに壊れる**
   （`invalid_user_credentials`。エラーは password 側に出るので原因が見えにくい）。
   TOTP を realm に焼くのをやめ、**初回 step-up のセットアップページから secret を拾って
   その場でエンロールする**形にした（エンロール込みの E2E になり、むしろ本物に近い）。
2. **`{subject}:delete` の `{subject}` にコロンを含む値（`user:probe-nobody`）を渡すと
   huma のパス一致が外れて 404**。しかも**無トークンだと認証ミドルウェアが先に 401 を
   返す**ため、ルーティングの穴が 401 に隠れて見えない。トークンを通して初めて 404 が出た。
   subject はコロン無し（`probe-nobody`）で渡す。fail open の親戚（手前の層の応答が奥の層の
   バグを隠す。#142 事例3 と同型）。

## 位置づけ

`simpleassurance` の RFC 9470 応答は 3.3 で実装済みだったが、**実フロー（実 Keycloak の
step-up で acr が本当に上がり、上がったトークンでだけ通る）は未検証**だった。今回それを閉じた。
`RequireAAL` はハンドラに明示する現行の形のまま、OP 側の設定だけで step-up が成立する。
