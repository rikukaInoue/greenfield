# 0016. 開発用の実装は許可リストで守る（未設定は本番とみなす）

## 背景

開発用の実装を本番へ持ち込まないためのガードが2箇所にあり、どちらも**拒否リスト**だった。

**`staticauthn`**（署名検証のない擬似トークンを受け付ける Authenticator）:

```go
if env := os.Getenv("ENV"); env == "production" || env == "prod" {
    return nil, fmt.Errorf(...)
}
```

`ENV` が未設定、あるいは `prd` / `staging` のような綴りだと**通り抜ける**。
`app.LocalDeps` が唯一の Deps 生成経路なので、出荷される `photo` バイナリに他の認証経路は無い。
つまり `ENV` を設定し忘れた本番デプロイは、**署名検証のない base64 トークンを受け付ける**。

**オブジェクトストレージの資格情報**:

```go
AccessKeyID:     envOr("AWS_ACCESS_KEY_ID", "test"),
SecretAccessKey: envOr("AWS_SECRET_ACCESS_KEY", "testtest"),
Endpoint:        envOr("AWS_ENDPOINT_URL", "http://localhost:9000"),
```

`blobstore` は `AccessKeyID != ""` ならスタティック認証に切り替える。既定値が非空なので、
**IAM のタスクロール運用（環境変数を設定しないのが普通）では AWS の既定の認証チェーンが使われず**、
`test` / `testtest` で接続しに行く。エンドポイントも `localhost:9000` を向く。
危険というより混乱する形の失敗だが、「dev の既定が本番の解決機構を抑止する」形そのものである。

## 決定

**環境の判定を `core/runtimeenv` に一元化し、許可リストにする。**

```go
func Current() Kind                      // 既定は Prod（ENV 未設定も Prod）
func RequireDevelopment(what string) error // dev / test / ci のみ許す
```

- `ENV` が `dev` / `development` / `local` / `test` / `ci` のいずれかでなければ **Prod**
- 開発用の実装は `RequireDevelopment` を通らなければ組み立てられない
- **既定値で開発用の資格情報を置かない。** ローカル用の値は「開発環境であることを確かめた後」に補う

`blobstore` 側にも最低限の検証を入れた（バケット名が空なら起動しない、
`AccessKeyID` があるのに `SecretAccessKey` が無ければ起動しない）。

## 影響

- **`ENV` の設定が必須になる。** 未設定だと `photo` は起動しない
  （`staticauthn: ... ENV=(未設定) では使用できない`）。`mise.toml` に `ENV = "dev"`、
  CI に `ENV: ci` を置いた
- ローカルの S3 既定値（バケット・エンドポイント・資格情報）は開発環境でだけ補われる。
  本番では環境変数が無ければ AWS の既定の認証チェーンが使われる
- Phase 3.3 で `oidcauthn` を配線したあとも、`staticauthn` が本番で組み立てられないことは
  このガードが保証し続ける

## 実測

| `ENV` | 結果 |
|---|---|
| 未設定 | **起動しない**（`ENV=(未設定) では使用できない`） |
| `prd`（綴り違い） | **起動しない** |
| `staging` | 起動しない |
| `dev` | 起動する。投稿が 200 |

`core/runtimeenv` の単体テストで10通りの `ENV` 値について種別と許否を固定した。

## 還流

1. **本番ガードは許可リストにする。** 拒否リストは「知っている本番の名前」しか止められず、
   未設定・綴り違い・新しい環境名（`staging`、`sandbox`）が通り抜ける。
   開発用の実装を持つ設計（差し込み口 + ローカル実装）では、この判定が最後の防波堤になる
2. **開発用の既定値を production コードのパスに置かない。** 「環境変数が無ければ dev の値」は、
   環境変数を設定しない運用（IAM ロール、シークレット注入）を**抑止する**。
   既定値を置くなら「開発環境であることを確かめた後」に限る
3. 環境の判定を1箇所に集める。2箇所に別々の判定があると、片方だけ許可リストに直しても守られない
