// allinone は全サービスのリスナーを1プロセスで起動するローカル開発用バイナリ。
// サービス間通信はこの場合もlocalhostの各ポートを通すHTTPであり、
// in-process呼び出しへの近道は作らない。本番では使わない。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, len(services))
	for _, s := range services {
		slog.Info("starting", "service", s.Name, "external", s.Base, "internal", s.Base+1, "admin", s.Base+2)
		go func() { errc <- s.run(ctx) }()
	}

	var failed bool
	for range services {
		if err := <-errc; err != nil {
			slog.Error("service exited", "err", err)
			failed = true
			stop()
		}
	}
	if failed {
		os.Exit(1)
	}
}

func addrs(base int) (external, internal, admin string) {
	return fmt.Sprintf(":%d", base), fmt.Sprintf(":%d", base+1), fmt.Sprintf(":%d", base+2)
}
