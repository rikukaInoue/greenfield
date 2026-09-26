# 認証認可の差し込み口（core/authz）と先行開発

<!-- 区分: 内部設計 / ステータス: Draft / 元: マルチドメインAPI モノレポ設計書（2026-09-26版）から分割 -->
<!-- 本文中の §N.N 参照は docs/README.md の対応表でファイルへ解決する -->

## 7. 差し込み口設計（core/authz）

認証・認可・保証レベル検査はすべてinterfaceとして `core/authz` に定義し、実装はサブパッケージおよび外部アダプタが提供する。`services/*` がimportしてよいのはinterface側のみで、実装パッケージのimportは `cmd/*` に限る。SDK・基盤側の型をusecase/handlerに漏らさないことで、基盤の変更（SDKメジャーアップ、エンジン差し替え、認証方式変更）の影響を配線部に閉じ込める。

interfaceは4系統に分ける。

```go
// 認可判定（裏: authzサービス / OpenFGA）
type Authorizer interface {
    Can(ctx context.Context, req Request) (Result, error)
}
type Lister interface {
    ListAccessible(ctx context.Context, action, resourceType string) ([]string, error)
}

// 関係書き込み（裏: authzサービス / OpenFGA）
type RelationWriter interface {
    WriteRelations(ctx context.Context, tuples []Tuple) error
    DeleteRelations(ctx context.Context, tuples []Tuple) error
}

// 認証（裏: Hydra JWT検証）
type Authenticator interface {
    Middleware() func(http.Handler) http.Handler
}

// 保証レベル（裏: Hydra/Kratos）
type AssuranceChecker interface {
    Require(ctx context.Context, a Assurance) error
    RequireAAL(ctx context.Context, level AAL) error
}
```

AuthorizerとAssuranceCheckerを分けるのは、前者が「何ができるか」（OpenFGA）、後者が「どれだけ確かにその人か」（Hydra/Kratos）であり、裏にいるシステムと差し替え単位が異なるためである。

認証済み主体は共通型Principalで表し、エンドユーザーとサービスを同一の枠組みで扱う。ミドルウェアが検証結果をctxに積み、ハンドラ・usecaseは `PrincipalFrom(ctx)` のみを参照する。ミドルウェアはnet/http標準形式で提供し、Echoへは `echo.WrapMiddleware` で載せる（フレームワーク非依存の維持）。

タプルのsubject/object表記（`user:<sub>` 等）は文字列連結を各所に書かず、`core/authz` のヘルパー関数に集約する。

認可の要求レベル（Assurance）はミドルウェアのパスマッピングではなくハンドラに明示的に記述する。パス設計の変更で認可が静かに壊れる事故を避けるためである。

ローカル開発・テスト用にAllowAll / Fixed / NopWriter / NopAssurance / StaticAuthenticatorを用意する。本番環境での誤配線は起動時ガード（ENV検査）で防ぐ。

実装スケルトン（interface一式 + hydraauthnアダプタ + introspectionクライアント）は authz-skeleton として作成済み。

**先行開発の条件**。認証認可基盤（V0）の完成を待たずにプロダクト側の開発を先行できる。差し込み口の目的はまさにこれである。ただし「後で差し込む」が成立するには、今固定するものと後回しにするものの線引きを守る必要がある。

今固定するもの（後から変えると全体に波及する）は4つ。第一に呼び出し箇所そのもの: usecaseは最初から `Can` / `ListAccessible` / `WriteRelations` を、handlerは `RequireAAL` を呼んで書く。実装がローカル用でも呼び出しは本物とし、「認可は後で入れるから今は書かない」を禁止する。第二に語彙: `Request` のaction名とresource type名は後にFGAモデルのrelationへマッピングされる契約であり、ドメインごとに一覧化して管理する（authzサービス側のaction→relationマッピングの入力になる）。第三にPrincipalの意味: `Subject` はKratos identity IDになるため、ローカル実装でも不透明な識別子として扱い、解析や別の値（email等）の代入をしない。第四にAAL要求の所在: どのハンドラが `RequireAAL` を持つかは今決めてコードに残す。

後回しにできるものは、Hydraアダプタ、authzhttpアダプタ、token hook、FGAモデルの実体、ステップアップのフロント側であり、いずれも `cmd/*` の配線とアダプタパッケージに閉じる。差し替え時にusecase・handlerを触らないことをV1参照実装で検証する。

**ローカル実装は「緩い」より「本物らしく厳しい」に寄せる**。差し込み口の実装は3段階を区別する。

```
テスト用モック (Fixed / Nop)     決め打ち応答。usecaseの分岐検証用
ローカル実装 (擬似ReBAC)         本物の振る舞いを簡易再現。開発の日常で使う ← 先行開発の主役
本番アダプタ (hydraauthn / authzhttp)  後で差し込む
```

`AllowAll` で長期間開発すると、認可のある前提のコードが検証されないまま蓄積し、本番アダプタを差した日に一斉に壊れる。特に `ListAccessible` が空を返す環境では一覧が空になり、開発者が「Repositoryで全件取る」へ逃げる誘因になる。したがって先行開発では擬似ReBACのローカル実装を用意する。所有者テーブル1枚で「Subjectが所有者であるリソースのみ許可・列挙」し、`WriteRelations` はそのテーブルへ書く程度のもので足りる（100行規模）。これによりタプル書き込みの呼び忘れや `ListAccessible` の迂回が開発中に目に見えて壊れる。より忠実にしたい場合は開発環境にローカルのOpenFGAを立て、簡易モデルで本物の挙動にする。

検証ビルドでは基盤をV0で先に建てるため、既存認証との橋渡しは存在しない。V0完了前にプロダクト側を書く場合のみローカル実装（擬似ReBAC）で進め、V0後に本番アダプタへ差し替えて「usecase・handler無変更」を確認する。
