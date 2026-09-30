# 2026-09-30 — [5.4] MCP エージェントに human-in-the-loop を強制する（#58 / check #14）

環境: 5.3 と同じスタック(Keycloak browser-loa + TOTP / authz(OIDC) / photo(OIDCDeps))+
最小 MCP サーバ `dev/mcpadmin`。`mise run mcp:hitl` で再現。

## 主張と結果

エージェント(MCP)から削除ツールを呼ぶと、危険操作は 401 で止まり、**人間の再認証後のみ**成功する。監査に client_id が残る。

| 確認 | 結果 |
|---|---|
| aal1 トークンで delete_account ツール実行 | ツールが **isError** + step-up チャレンジ(`insufficient_user_authentication`, `acr_values="aal2"`)を返す。エージェントは進めない |
| エージェントの自己昇格 | acr_values=aal2 の direct grant は **拒否**(acr は aal1 のまま)= human-in-the-loop の抜け穴が無い |
| 人間が TOTP で aal2 → 同じツール | **成功**(200) |
| 監査ログ | `admin.account.delete.{attempt,challenged,completed}` に `actor.client_id`(=azp)・subject・kind・aal が載る。401 で止まった試行も記録 |

## 実装

- `dev/mcpadmin`: MCP(JSON-RPC over stdio)の最小サーバ。`delete_account` ツールが
  admin API を叩き、**401 のチャレンジをそのまま結果として返す**(エージェントが人間へ
  ハンドオフする契機を機械に持たせる)。プロトコル網羅でなく「ツール呼び出しが認可で
  止まる」ことの再現が目的
- 監査ログを admin 削除ハンドラに追加。**AAL 判定より前**に attempt を出す(401 で
  止まった試行こそ監査したい)。client_id は `azp`(authorized party = 叩いたアプリ)を
  使う——人間の対話トークンにも載るため。`client_id` クレームは純 M2M にしか載らない(実測)

## 気づき（還流）

- **human-in-the-loop は「実装した機能」でなく「OP 設定の帰結」**。エージェント(public client)は
  非対話で AAL2 を取れないので、危険操作は構造的に人間の TOTP を要求する。アプリ側は
  `RequireAAL(AAL2)` を書くだけでよい
- **監査の client は `azp` で取る**。`client_id` クレームは client_credentials にしか
  載らないため、対話フローのアプリ識別子(=どのエージェントが叩いたか)は azp が正しい。
  Principal に `AuthorizedParty` を追加した(還流候補)
- 検証環境の注意: Keycloak を再 import すると署名鍵が回転し、authz/photo のキャッシュ
  JWKS が古くなって M2M 経路が 500 になる。keycloak 再作成時は authz/photo も再起動する
