// admin は管理画面（社内オペレータ向け SSR）のエントリポイント。
//
//	ADMIN_ADDR       待ち受け（既定 :8200）
//	PHOTO_ADMIN_URL  photo の admin リスナー（既定 http://localhost:8082）
//	SESSION_SECRET   セッション Cookie の署名鍵（開発環境以外では必須）
//	SECURE_COOKIE    true なら Cookie に Secure を付ける（TLS 終端の内側で動くときは true）
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rikukaInoue/greenfield/core/logger"
	"github.com/rikukaInoue/greenfield/core/runtimeenv"

	"github.com/rikukaInoue/greenfield/platform/admin"
)

func main() {
	slog.SetDefault(logger.FromEnv("admin", admin.Version))
	if err := run(); err != nil {
		slog.Error("admin exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		// 開発用の既定値は許可リストの環境でしか使えない（本番で「なんとなく既定の鍵」に落ちない）
		if err := runtimeenv.RequireDevelopment("SESSION_SECRET の開発用既定値"); err != nil {
			return err
		}
		secret = "dev-admin-session-secret" //nolint:gosec // G101: 開発用既定値。直前の RequireDevelopment が本番での使用を拒む
	}
	srv, err := admin.New(admin.Config{
		PhotoAdminURL: envOr("PHOTO_ADMIN_URL", "http://localhost:8082"),
		SessionSecret: secret,
		SecureCookie:  os.Getenv("SECURE_COOKIE") == "true",
		// 開発用ログイン（devtoken）。許可リストの環境以外では New が拒否する
		DevLogin: true,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hs := &http.Server{
		Addr:              envOr("ADMIN_ADDR", ":8200"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       65 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listener ready", "server.address", hs.Addr, "photo_admin", envOr("PHOTO_ADMIN_URL", "http://localhost:8082"))
		errCh <- hs.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
