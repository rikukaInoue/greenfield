// gear サービスのバイナリ。
//
//	gear                                  3リスナー（external / internal / admin）を起動
//	gear migrate expand|contract|status   スキーママイグレーション（デプロイ前ステップ / キュー消化）
//
// 差し込み口（core/authz 等）の実装パッケージをimportしてよいのはこのパッケージだけ。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rikukaInoue/greenfield/services/gear/app"
	"github.com/rikukaInoue/greenfield/services/gear/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch {
	case len(os.Args) >= 2 && os.Args[1] == "migrate":
		err = runMigrate(ctx, os.Args[2:])
	default:
		err = app.Run(ctx, app.ConfigFromEnv())
	}
	if err != nil {
		slog.Error("gear exited", "err", err)
		os.Exit(1)
	}
}

// runMigrate は GEAR_MIGRATE_DSN（migrate ユーザー。自database内のDDLのみ許可）で系統を適用する。
// アプリ起動時の自動適用は行わない（conventions/internal-05）。
func runMigrate(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: gear migrate expand|contract|status")
	}
	dsn := os.Getenv("GEAR_MIGRATE_DSN")
	if dsn == "" {
		dsn = "gear_migrate:gear_migrate@tcp(127.0.0.1:3306)/gear"
	}
	switch args[0] {
	case "expand":
		return migrations.Up(ctx, dsn, migrations.Expand)
	case "contract":
		return migrations.Up(ctx, dsn, migrations.Contract)
	case "status":
		for _, s := range []migrations.Series{migrations.Expand, migrations.Contract} {
			v, dirty, err := migrations.Status(dsn, s)
			if err != nil {
				return err
			}
			fmt.Printf("%-9s version=%d dirty=%v\n", s, v, dirty)
		}
		return nil
	default:
		return fmt.Errorf("unknown migrate command %q (expand|contract|status)", args[0])
	}
}
