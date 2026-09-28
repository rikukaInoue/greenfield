// authz は認可サービスのエントリポイント。
// 起動時にストアとモデルを ensure し（追記専用なので差分があるときだけ書く）、
// 4エンドポイントを待ち受ける。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rikukaInoue/greenfield/platform/authz"
	"github.com/rikukaInoue/greenfield/platform/authz/fga"
)

func main() {
	if err := run(); err != nil {
		slog.Error("authz exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := envOr("AUTHZ_ADDR", ":8100")
	fgaURL := envOr("OPENFGA_URL", "http://localhost:8280")
	store := envOr("OPENFGA_STORE", "greenfield")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := fga.New(fgaURL)
	// OpenFGA の起動を少し待つ（compose で同時に上がる分の吸収。長い障害は起動失敗にする）
	if err := retry(ctx, 30, time.Second, func() error { return client.EnsureStore(ctx, store) }); err != nil {
		return fmt.Errorf("ensure store: %w", err)
	}
	if err := client.EnsureModel(ctx, authz.Model); err != nil {
		return fmt.Errorf("ensure model: %w", err)
	}
	slog.Info("authz ready", "addr", addr, "openfga", fgaURL, "store_id", client.StoreID())

	srv := &http.Server{Addr: addr, Handler: authz.NewServer(client, authz.DefaultMapping), ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func retry(ctx context.Context, n int, wait time.Duration, fn func() error) error {
	var err error
	for i := 0; i < n; i++ {
		if err = fn(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(wait):
		}
	}
	return err
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
