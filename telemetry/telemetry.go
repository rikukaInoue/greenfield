// Package telemetry は昇格シグナルのメトリクスを Prometheus 形式で公開する(6.1 / #59)。
//
// **core には置かない。** core に入れると全利用者が prometheus クライアントの依存を
// 抱える(internal-06 の「OTel SDK を core へ入れない」と同じ理由)。使ってよいのは
// 各サービスの合成ルート(cmd / app)のみで、usecase / handler からは見えない。
//
// 昇格シグナル(02-architecture)= 「この構成のままでよいか」を判断する材料:
//
//   - プール使用率: DB 接続が飽和し始めたら、インスタンス分離やプールの見直しの合図
//   - authz レイテンシ: 認可の往復が遅くなったら、キャッシュや配置の見直しの合図
//   - outbox 滞留: relay が追いつかなくなったら、配送の並列化やバスの見直しの合図
//
// 可観測性なしでは昇格条件が絵に描いた餅になる、が 6.1 の存在理由。
package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry は1プロセス分のメトリクスの置き場。
type Registry struct {
	reg     *prometheus.Registry
	service string
}

// New はサービス名付きの Registry を作る。Go ランタイムの標準メトリクスも載せる。
func New(service string) *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return &Registry{reg: reg, service: service}
}

// ObservePool は *sql.DB のプール状態を gauge として公開する。
// 値は scrape のたびに db.Stats() から読む(push しない。ズレの窓は scrape 間隔だけ)。
func (r *Registry) ObservePool(name string, db *sql.DB) {
	labels := prometheus.Labels{"service": r.service, "db": name}
	r.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "db_pool_in_use", Help: "使用中の接続数", ConstLabels: labels,
	}, func() float64 { return float64(db.Stats().InUse) }))
	r.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "db_pool_open", Help: "開いている接続数(使用中+アイドル)", ConstLabels: labels,
	}, func() float64 { return float64(db.Stats().OpenConnections) }))
	r.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "db_pool_max", Help: "プール上限(MaxOpenConnections。0=無制限)", ConstLabels: labels,
	}, func() float64 { return float64(db.Stats().MaxOpenConnections) }))
	r.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "db_pool_wait_total", Help: "接続待ちに入った累計回数(増え続けたら飽和の合図)", ConstLabels: labels,
	}, func() float64 { return float64(db.Stats().WaitCount) }))
}

// HTTPLatency は http.Handler の処理時間ヒストグラムを返すミドルウェア。
// authz サービスが自分の面(check / batch-check / ...)に巻く。バケットは
// 「認可の往復に払ってよい時間」の判断用に 1ms〜2.5s。
func (r *Registry) HTTPLatency(name string) func(http.Handler) http.Handler {
	hist := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "http_server_request_duration_seconds",
		Help:        "リクエスト処理時間",
		ConstLabels: prometheus.Labels{"service": r.service, "server": name},
		Buckets:     []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"path", "code"})
	r.reg.MustRegister(hist)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(rec, req)
			// path はテンプレートでなく実パスだが、authz の面は4エンドポイント固定なので
			// カーディナリティは弾けない(一般のAPIに使うならテンプレート化が要る)
			hist.WithLabelValues(req.URL.Path, http.StatusText(rec.code)).Observe(time.Since(start).Seconds())
		})
	}
}

// ObserveGauge は名前付きの汎用 gauge を登録する(outbox 滞留などの SQL 由来の値)。
// fn は scrape のたびに呼ばれる。エラー時は NaN でなく前回値も返せないため、
// fn 側で「読めない」を -1 として返す規約にする(0 と欠測を混同しない)。
func (r *Registry) ObserveGauge(name, help string, fn func(context.Context) float64) {
	r.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: name, Help: help, ConstLabels: prometheus.Labels{"service": r.service},
	}, func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return fn(ctx)
	}))
}

// Serve は /metrics を addr で公開する。ブロックしない。
// 公開リスナー(external/admin/internal)に相乗りさせないのは、メトリクスの面を
// API の契約(OpenAPI)や認証の面と混ぜないため。到達制御はインフラの持ち物。
func (r *Registry) Serve(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics listener exited", "addr", addr, "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
}

type statusRecorder struct {
	http.ResponseWriter
	code  int
	wrote bool
}

func (s *statusRecorder) WriteHeader(c int) {
	if !s.wrote {
		s.wrote, s.code = true, c
	}
	s.ResponseWriter.WriteHeader(c)
}
