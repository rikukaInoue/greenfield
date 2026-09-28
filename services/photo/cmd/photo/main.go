// photo サービスのバイナリ。
//
//	photo                                  3リスナー（external / internal / admin）を起動
//	photo migrate expand|contract|status   スキーママイグレーション（デプロイ前ステップ / キュー消化）
//	photo reclaim                          アップロードが完了しないまま残った写真を回収する
//
// 差し込み口（core/authz 等）の実装パッケージをimportしてよいのはこのパッケージだけ。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rikukaInoue/greenfield/core/logger"
	"github.com/rikukaInoue/greenfield/services/photo/app"
	"github.com/rikukaInoue/greenfield/services/photo/migrations"
)

func main() {
	// ロガーは**プロセスの入口で1回だけ**組む。ライブラリ側（app.LocalDeps）でやると、
	// 1プロセスに複数サービスを載せる dev/allinone で後から呼ばれた方の service.name が
	// 全ログに付く。既定ロガーを差し替えるのはプロセスを所有している側の責務。
	//
	// SetDefault しておけば基盤ミドルウェアの logger.FromContext が既定に落ちて
	// 相関IDを載せられる（context を全経路に通す必要がない）。
	slog.SetDefault(logger.FromEnv("photo", app.Version))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch {
	case len(os.Args) >= 2 && os.Args[1] == "migrate":
		err = runMigrate(ctx, os.Args[2:])
	case len(os.Args) >= 2 && os.Args[1] == "reclaim":
		err = runReclaim(ctx, os.Args[2:])
	default:
		err = app.Run(ctx, app.ConfigFromEnv())
	}
	if err != nil {
		slog.Error("photo exited", "err", err)
		os.Exit(1)
	}
}

// runMigrate は PHOTO_MIGRATE_DSN（migrate ユーザー。自database内のDDLのみ許可）で系統を適用する。
// アプリ起動時の自動適用は行わない（conventions/internal-05）。
func runMigrate(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: photo migrate expand|contract|status")
	}
	dsn := os.Getenv("PHOTO_MIGRATE_DSN")
	if dsn == "" {
		dsn = "photo_migrate:photo_migrate@tcp(127.0.0.1:3306)/photo"
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

// runReclaim はアップロードが完了しないまま残った写真を、オブジェクトごと削除する。
// 定期実行を前提とし、1回の実行で処理する件数に上限を置く。
func runReclaim(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("reclaim", flag.ContinueOnError)
	olderThan := fs.Duration("older-than", time.Hour, "この時間を超えて未完了の写真を対象にする")
	limit := fs.Int("limit", 100, "1回で処理する上限")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := app.ConfigFromEnv()
	deps, err := app.LocalDeps(ctx, cfg)
	if err != nil {
		return err
	}
	defer deps.Close()

	n, err := deps.Commands.Reclaim(ctx, *olderThan, *limit)
	if err != nil {
		return err
	}
	slog.Info("reclaim finished", "reclaimed", n, "older_than", olderThan.String())
	return nil
}
