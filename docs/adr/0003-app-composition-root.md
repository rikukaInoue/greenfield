# 0003. `app/` を合成ルートにし、ハンドラは Deps を受ける

## 背景

`conventions/internal-04` は「差し込み口の実装パッケージを import してよいのは `cmd/*` だけ」と定める。
一方このビルドには `cmd/<service>` と `dev/allinone` の2つの起動経路があり、両方に同じ配線を書くと
起動手順が二重になる（片方だけ直す事故が起きる）。

## 決定

`services/<name>/app` パッケージを合成ルートとする。

- 実装パッケージ（`localauthz` / `staticauthn` / driver 等）を import してよいのは `app/` と `cmd/*`
- `cmd/<name>` と `dev/allinone` はどちらも `app.Run` を呼ぶだけ
- ハンドラは `Deps` 構造体で差し込み口（interface）を受け取る。実装を知らない
- OpenAPI 生成（`dev/genapi`）も `app.APIs` を呼ぶので、配線とスペックが乖離しない

## 影響

「差し替えの diff は配線だけ」という性質（チェック #18）は満たされるが、その配線は `cmd/` ではなく
`app/` にある。#18 の判定はこの前提で行う。

## 還流

`conventions/internal-01` のツリー図に `app/` を追加し、「実装 import は `app/` と `cmd/*` に限る」
と読み替える。ハンドラが `Deps` で差し込み口を受ける形も併せて記載する。

## 追記: 「片方だけ直す事故」は防げていなかった（監査 A-5、#86）

`cmd/<name>` と `dev/allinone` がどちらも `app.Run` を呼ぶ形にしても、**`Config` の組み立ては二重のまま**だった。
`cmd` は `ConfigFromEnv()` を通す一方、allinone はポートだけを詰めた `Config` リテラルを渡しており、
DSN・S3・フラグの設定が空で DB に触れる全エンドポイントが 500 になっていた。scaffold も同じリテラルを生成していた。

合成ルートを1つにしても、その入力（設定）を組み立てる経路が2つあれば同じ事故が起きる。
allinone は `ConfigFromEnv()` を読んだうえでアドレスだけを上書きする形に直し（#82）、scaffold の生成行も揃えた。
curl での確認は `cmd` 経由の photo に対して行っていたため気づかず、allinone を起動するブラウザ E2E で初めて発覚した。
