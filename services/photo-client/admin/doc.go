// Package admin は photo の admin リスナー（社内オペレータ API）の生成クライアント。
// `api/photo/admin.openapi.json` から `dev/scripts/client-gen.sh` が生成する。手書きしない。
//
// 使うのは管理画面（platform/admin）だけで、呼び出しは operator 本人のトークンによる
// 代理呼び出し（M2M ではない）。サービス間連携は internal 側（親パッケージ）を使う。
package admin
