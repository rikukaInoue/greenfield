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
	"slices"
	"syscall"
	"time"

	coreauthz "github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/oidcauthn"
	"github.com/rikukaInoue/greenfield/core/problem"
	"github.com/rikukaInoue/greenfield/core/runtimeenv"

	"github.com/rikukaInoue/greenfield/platform/authz"
	"github.com/rikukaInoue/greenfield/platform/authz/fga"
	"github.com/rikukaInoue/greenfield/telemetry"
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
	var handler http.Handler = authz.NewServer(client, authz.DefaultMapping)

	// 昇格シグナル: authz レイテンシ(#59)。認可の往復に払っている時間を面ごとに測る。
	// /metrics は契約・認証の面と混ぜず専用ポート(未設定なら出さない)
	if maddr := os.Getenv("METRICS_ADDR"); maddr != "" {
		reg := telemetry.New("authz")
		handler = reg.HTTPLatency("internal")(handler)
		reg.Serve(ctx, maddr)
	}
	// サービス間認証。プライベートネットワークを理由とした無認証は試作でも採らない
	// （docs/03-platform.md）。OIDC_ISSUER が無い起動は開発用の許可リスト環境のみ許す。
	if issuer := os.Getenv("OIDC_ISSUER"); issuer != "" {
		authn, err := oidcauthn.New(ctx, issuer)
		if err != nil {
			return fmt.Errorf("oidcauthn: %w", err)
		}
		handler = authn.Middleware()(requireScope("internal:platform")(handler))
		slog.Info("authn enabled", "issuer", issuer, "scope", "internal:platform")
	} else if err := runtimeenv.RequireDevelopment("authz の無認証待ち受け"); err != nil {
		return fmt.Errorf("%w（本番は OIDC_ISSUER を設定する）", err)
	}
	slog.Info("authz ready", "addr", addr, "openfga", fgaURL, "store_id", client.StoreID())

	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
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

// requireScope は Principal が scope を持たなければ 403 を返す（/healthz は素通し）。
func requireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}
			p, ok := coreauthz.PrincipalFrom(r.Context())
			if !ok {
				problem.Write(w, r, problem.New(http.StatusUnauthorized, problem.CodeUnauthenticated, "認証が必要"))
				return
			}
			if !slices.Contains(p.Scopes, scope) {
				problem.Write(w, r, problem.New(http.StatusForbidden, problem.CodeForbidden, "スコープ "+scope+" が必要"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
