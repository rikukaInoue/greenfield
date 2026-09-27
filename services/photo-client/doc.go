// Package photoclient は photo サービスの生成クライアント（=契約）。
// 他サービスが photo に依存してよいのはこのモジュールに対してのみ。
//
// **現状はまだ空**。`api/photo/internal.openapi.json` から oapi-codegen で生成する
// 計画だが、ツールも CI ステップも生成物も無い（ステージ 2.2 で入れる）。
// 「CI が生成する。手書き禁止」と書いてあったのを現状に合わせた（#91）——
// 無い仕組みを既にあるように書くと、読んだ人が生成物を探して時間を落とす。
package photoclient
