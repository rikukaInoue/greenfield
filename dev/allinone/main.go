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
	"strconv"
	"syscall"

	"github.com/rikukaInoue/greenfield/core/logger"
)

func main() {
	// 1プロセスに複数サービスが載るので、service.name は "allinone" にする。
	// どのサービスのリスナーが処理したかはアクセスログの listener と server.address で分かる。
	slog.SetDefault(logger.FromEnv("allinone", ""))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, len(services))
	for _, s := range services {
		ext, intl, adm := addrs(s.Base)
		slog.Info("starting", "service", s.Name, "external", ext, "internal", intl, "admin", adm)
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

// addrs は external / internal / admin のアドレスを返す。base は services.go の割当表の値で、
// GREENFIELD_PORT_OFFSET があればそれを足す(worktree ごとにポートの番号帯を分けるため。#302)。
// 割当表そのものは変えないので、scaffold が採番する +10 の規則はそのまま使える。
func addrs(base int) (external, internal, admin string) {
	base += portOffset()
	return fmt.Sprintf(":%d", base), fmt.Sprintf(":%d", base+1), fmt.Sprintf(":%d", base+2)
}

// portOffset は GREENFIELD_PORT_OFFSET を読む。未設定は 0(メインの checkout)。
// 数として読めない値は、黙って 0 にすると別の worktree のポートと衝突するので起動を止める。
func portOffset() int {
	v := os.Getenv("GREENFIELD_PORT_OFFSET")
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		slog.Error("GREENFIELD_PORT_OFFSET は 0 以上の整数", "value", v)
		os.Exit(2)
	}
	return n
}
