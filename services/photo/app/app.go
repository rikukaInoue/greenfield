// Package app は photo サービスの組み立てと起動を担う。
// cmd/photo と dev/allinone の両方から同じ Run を呼ぶ（起動手順を二重に持たない）。
package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/rikukaInoue/greenfield/services/photo/handler/admin"
	"github.com/rikukaInoue/greenfield/services/photo/handler/external"
	"github.com/rikukaInoue/greenfield/services/photo/handler/internalapi"
)

// Config はリスナーの待ち受けアドレス。アプリが知るのはListenするポートだけであり、
// TLS・ホスト名・到達制御はインフラの持ち物（conventions/internal-07）。
type Config struct {
	ExternalAddr string
	InternalAddr string
	AdminAddr    string
}

// ConfigFromEnv は PHOTO_{EXTERNAL,INTERNAL,ADMIN}_ADDR から設定を読む。
// 既定はポート割当表のとおり :8080 / :8081 / :8082。
func ConfigFromEnv() Config {
	return Config{
		ExternalAddr: envOr("PHOTO_EXTERNAL_ADDR", ":8080"),
		InternalAddr: envOr("PHOTO_INTERNAL_ADDR", ":8081"),
		AdminAddr:    envOr("PHOTO_ADMIN_ADDR", ":8082"),
	}
}

// Run は3リスナーを起動し、ctxのキャンセルまたはいずれかのリスナーの失敗で全て停止する。
func Run(ctx context.Context, cfg Config) error {
	servers := []*http.Server{
		{Addr: cfg.ExternalAddr, Handler: external.New()},
		{Addr: cfg.InternalAddr, Handler: internalapi.New()},
		{Addr: cfg.AdminAddr, Handler: admin.New()},
	}

	errc := make(chan error, len(servers))
	for _, s := range servers {
		go func() {
			if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
				return
			}
			errc <- nil
		}()
	}

	var cause error
	select {
	case <-ctx.Done():
	case cause = <-errc:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdownCtx)
	}
	return cause
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
