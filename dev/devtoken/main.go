// devtoken はローカル開発・CI用の擬似アクセストークンを発行するCLI。
//
//	go run ./dev/devtoken --user alice                          # AAL1 のユーザートークン
//	go run ./dev/devtoken --user alice --aal 2                  # ステップアップ済み
//	go run ./dev/devtoken --service svc-gear --scope internal:photo   # サービス間（client_credentials 相当）
//	eval "$(go run ./dev/devtoken --user alice --export)"       # TOKEN 環境変数として取り込む
//
// 本番のOP（Keycloak、Phase 3.1）稼働後は、このCLIを Keycloak からトークンを取得する形に差し替える
// （conventions/internal-07 の dev トークンCLI）。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
)

func main() {
	var (
		user    = flag.String("user", "", "ユーザーのSubject（例: alice）")
		service = flag.String("service", "", "サービスのclient_id（例: svc-gear）。--user と排他")
		scopes  = flag.String("scope", "", "スコープ（カンマ区切り。例: internal:photo）")
		aal     = flag.Int("aal", 1, "認証保証レベル（1 or 2）")
		export  = flag.Bool("export", false, "export TOKEN=... の形で出力する")
	)
	flag.Parse()

	if (*user == "") == (*service == "") {
		fmt.Fprintln(os.Stderr, "devtoken: --user か --service のどちらか一方を指定する")
		flag.Usage()
		os.Exit(2)
	}

	c := devtoken.Claims{AAL: *aal}
	if *service != "" {
		// サービスは client_credentials 相当。Subject には client_id を入れる（人のIDと混ぜない）。
		c.Subject, c.ClientID, c.Service = *service, *service, true
	} else {
		c.Subject = *user
	}
	if *scopes != "" {
		c.Scopes = strings.Split(*scopes, ",")
	}

	token := devtoken.Mint(c)
	if *export {
		fmt.Printf("export TOKEN=%s\n", token)
		return
	}
	fmt.Println(token)
}
