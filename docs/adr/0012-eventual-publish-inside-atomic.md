# 0012. `Eventual.Publish` は `Atomic.Do` の中で呼ぶ

## 背景

`conventions/internal-03-consistency.md` の2箇所が食い違っている。

**処理の配置ルール（§2.2）とそのコード例**:

```
Doの前:  他サービスからの読み取り・検証、ドメインオブジェクトの生成
Doの中:  ローカルDB操作 + Atomic判定済みの外部書き込みのみ
Doの後:  Eventual（Publish）と BestEffort（TryXxx）、および補償を伴う直接呼び出し
```

```go
err := u.atomic.Do(ctx, func(ctx context.Context) error { ... })
// ...
_ = u.eventual.Publish(ctx, OrderCreatedEvent{...}) // 到達すればよい
```

**Outbox の原子性の主張（§2.3）**:

> 自サービスのDBに `outbox` テーブルを設け、業務データの書き込みと**同一トランザクション**で
> イベントレコード（イベント種別・ペイロード・イベントID・集約ID）をINSERTする。これにより
> 「業務データがある ∧ 送信予定がある」か「両方ない」の2状態のみが**ローカルトランザクションの保証**
> として成立する。

`Publish` を `Do` の後に呼ぶと、outbox への INSERT は業務データとは別のトランザクションになる。
業務データをコミットした直後にプロセスが落ちれば「業務データはあるが送信予定が無い」状態が作れ、
**イベントは永久に失われる**。§2.3 が主張する2状態の保証は成立せず、Outbox パターンの存在理由が消える。

`Eventual` は「窓は許すが欠落は許さない」クラスとして定義されているので、欠落しうる実装は
クラスの定義に反する。

## 決定

**`Eventual.Publish` は `Atomic.Do` の中で呼ぶ。**

- outbox への INSERT は ctx が運ぶトランザクションに参加する（Repository と同じ仕組み、`consistency.TxFrom`）
- 配置ルールは次のように読み替える

```
Doの前:  他サービスからの読み取り・検証、ドメインオブジェクトの生成
Doの中:  ローカルDB操作（outbox への記録を含む）+ Atomic判定済みの外部書き込み
Doの後:  BestEffort（TryXxx）、および補償を伴う直接呼び出し（同期コマンド）
```

outbox への INSERT は自DBへの書き込みであって「外部書き込み」ではないため、
「Doの中: ローカルDB操作」とは矛盾しない。規約の誤りは `Publish` を「Doの後」に置いた点にある。

**実装時のガード**: `Publish` は `consistency.TxFrom(ctx)` が取れなければエラーを返す。
`Do` の外で呼ぶ誤用を、コンパイル時ではないが最初の実行で確実に落とす。
`Eventual` の interface（`Publish(ctx context.Context, event Event) error`）は変更不要で、
ctx がトランザクションを運ぶ。

**「Doの後」に残るもの**: 相手の結果が今の分岐を決める同期コマンド（pending 状態パターン）。
これは outbox を経由せず、tx 外で冪等キー付きの HTTP を直接発行し、結果を別の `Atomic` で反映する。
`BestEffort`（`TryXxx`）も従来どおり「Doの後」。

## 別サービスへの書き込みの分岐（確認）

この決定は分岐そのものを変えない。記録として整理しておく。

```
別ドメインの状態を変えたい
 ├ 相手の結果が今の分岐を決める → 同期コマンド + pending状態 + 冪等キー（Doの後、outbox を通さない）
 └ 起きればよい
    ├ 相手が何をするか知らなくてよい → イベント（Eventual.Publish）… 既定
    └ 特定の相手に確実にやらせる責任がこちらにある → コマンド（Eventual経由）
```

本ビルドの photo / gear 間は3本あり、Eventual を使うのは2本だけである。

| 相互作用 | 分岐 | クラス |
|---|---|---|
| photo → gear 「使用機材の紐付け」 | 結果が今の分岐を決める | 同期コマンド + pending 状態 + 冪等キー |
| gear → photo 「GearPublished / GearRenamed」 | 起きればよい・相手の行動を知らない | イベント（Eventual） |
| photo → gear 「PhotoPublished」 | 起きればよい | イベント（Eventual） |

## 影響

- Phase 4.2（Eventual の実装）の形が決まる。`outbox` テーブルは photo の DB 内に置き、
  `Eventual` の実装は `consistency.TxFrom` を使う
- 既に宣言済みの `usecase.Eventual` / `usecase.Event`（`services/photo/usecase/consistency.go`）は
  シグネチャを変えずに使える
- 現時点で `Eventual` の実装・呼び出しは存在しないため、既存コードへの影響はない
  （選べる整合性クラスは実質 `Atomic` だけの状態）
- `Do` の中に入るものが増えるので、`Do` の中の処理時間は短く保つ規律がより重要になる。
  outbox への INSERT は自DBの1行追加なので影響は小さい

## 還流

`conventions/internal-03-consistency.md` §2.2 の配置ルールとコード例が §2.3 の Outbox の原子性と矛盾している。
`Publish` を「Doの後」に置くと Outbox の2状態保証が成立せず、Eventual の「欠落は許さない」という定義にも反する。

修正案:

1. 配置ルールを「Doの中: ローカルDB操作（outbox への記録を含む）+ Atomic判定済みの外部書き込み」
   「Doの後: BestEffort、および補償を伴う直接呼び出し」に改める
2. §2.2 のコード例から `Publish` を `Do` の中へ移す
3. 実装規約に「`Publish` はトランザクションの中でしか呼べない（ctx に tx が無ければエラー）」を追加する

この矛盾は Eventual を実装する段階まで表に出ない。宣言だけ先に置く（差し込み口を固定する）方針は
規約の意図どおりだが、**宣言と配置ルールの整合は宣言した時点で確認する必要がある**。
