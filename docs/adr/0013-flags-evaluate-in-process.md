# 0013. フラグは定義を同期してプロセス内で評価する

## 背景

ADR 0010 で `core/flags`（入口で1回評価して ctx に積む）を決め、プロバイダは flagd を使った。
その実装は flagd の**既定の resolver（`rpc`）**で、flagd 側へ評価を投げる形だった。

ADR 0011 で AWS では AppConfig 用のプロバイダを自作すると決めたが、AppConfig の形は
`StartConfigurationSession` → `GetLatestConfiguration` で**設定ドキュメント全体を受け取り、
評価はアプリのプロセス内で行う**。つまりローカルと本番で評価の場所が違っていた。

flagd の入口を整理すると次のようになる。

| 入口 | 評価の場所 | 通信 |
|---|---|---|
| `:8013` Flag IResolver（`rpc`。従来の設定） | flagd 側 | Connect（gRPC / gRPC-Web / Connect over HTTP） |
| `:8016` OFREP | flagd 側 | HTTP / JSON |
| `:8015` flag sync service（`in-process`） | **アプリのプロセス内** | Connect で定義を同期 |
| ファイル直読み（`file`） | **アプリのプロセス内** | なし |

「AppConfig に合わせるなら REST（OFREP）」は誤りである。**軸はプロトコルではなく評価の場所**で、
OFREP もリモート評価だからである。加えて Go の OFREP プロバイダ（`providers/ofrep@v0.1.7`）は
SSE を実装しておらずキャッシュも持たないため、リクエストごとに HTTP が飛ぶだけになる。

なお「定義を配ってローカルで評価する」形は業界の主流である。

| 製品 | 評価の場所 |
|---|---|
| AWS AppConfig | プロセス内 |
| GrowthBook | プロセス内（features の JSON を取得し SDK が評価。更新は SSE） |
| LaunchDarkly（サーバ SDK） | プロセス内（ruleset をストリームで受ける） |
| Unleash | プロセス内 |
| flagd `rpc` / OFREP | flagd 側 |

## 決定

**評価はアプリのプロセス内で行う。** 定義の取得元は2つを用意し、環境で選ぶ。

| `FLAGS_SOURCE` | 取得元 | 用途 |
|---|---|---|
| `sync`（既定） | flagd の flag sync service（`:8015`） | ローカル開発。compose の flagd から同期する |
| `file` | 定義ファイルを直接読む（`FLAGS_FILE`） | CI とテスト。flagd を立てずに評価経路をそのまま通す |

実装は `services/photo/flagsource`。**`core` には置かない**。プロバイダは grpc / connect を
持ち込むため、規約の「core は軽依存に保つ」に反する（`blobstore` で AWS SDK をサービス側に
閉じたのと同じ扱い）。`core/flags` は評価とミドルウェアだけを持ち、OpenFeature SDK にしか依存しない。

登録は `openfeature.SetNamedProviderWithContextAndWait(ctx, domain, provider)` を使う。
無名のプロバイダはプロセスグローバルで、1プロセスに複数サービスが載る場合（`dev/allinone`）に
互いを上書きする（監査 C-2）。ドメイン名は `flags.NewEvaluator(domain, ...)` と一致させる。

## 影響

- **flagd が停止していても、最後に同期した定義で評価を続ける。** 実測: フラグを ON にしてから
  flagd を止めても 503 を返し続けた。従来の `rpc` では既定値（OFF）へ倒れていた
- 評価経路にネットワークが入らない。`rpc` の LRU キャッシュに頼らずに済む
- CI では flagd を立てない。従来 CI の flagd は `services:` に `command` を渡せず定義を配れないため、
  **立てても何も検査しない飾りだった**（監査 B-2）。`file` で定義を直接読めば、
  プロバイダ登録から評価までの経路がそのまま通る
- compose が公開するポートは `:8013` から **`:8015`**（sync）へ変わる
- Phase 7 で AppConfig プロバイダを差し替えるとき、**評価の場所が変わらない**。
  差し替えは `flagsource` の 1 ケース追加に閉じる

## 実測

| 検査 | 結果 |
|---|---|
| `sync`: 定義ファイルの書き換えが届く | ON へ切り替わる（503） |
| `sync`: flagd を停止した状態 | **ON を保持**（最後に同期した定義で評価） |
| `file`: 定義ファイルの書き換えが届く | ON へ切り替わる（ファイル監視） |
| `file`: flagd を立てずに起動 | 警告・エラーなし。評価経路が通る |
| ドメインが違うプロバイダの分離 | `svc-a` と `svc-b` で別の値を返す（`core/flags` のテスト） |

## 還流

1. **`internal-08` にプロバイダの選び方を書く。** 「OpenFeature 経由なのでプロバイダ交換のみ」は
   正しいが、**評価の場所（プロセス内かリモートか）が変わると障害時の挙動が変わる**。
   ローカルと本番で揃えないと、ローカルで確認した縮退の挙動が本番で再現しない
2. **無名のプロバイダ登録（`SetProvider`）を使わない。** プロセスグローバルで、
   ドメイン名を渡すクライアントを作っても効かない。`SetNamedProvider` を既定とする
3. **CI で「立てただけで何も検査しない依存」を作らない。** 設定を渡せないサービスコンテナは
   飾りである。プロセス内評価にしておけば、定義ファイルを読むだけで経路を検査できる
