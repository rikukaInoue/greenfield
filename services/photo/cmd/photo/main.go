// photo サービスのバイナリ。3リスナー（external / internal / admin）を起動する。
// 差し込み口（core/authz 等）の実装パッケージをimportしてよいのはこのパッケージだけ。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rikukaInoue/greenfield/services/photo/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, app.ConfigFromEnv()); err != nil {
		slog.Error("photo exited", "err", err)
		os.Exit(1)
	}
}
