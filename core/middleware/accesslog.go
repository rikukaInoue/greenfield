package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/rikukaInoue/greenfield/core/logger"
)

// AccessLog は**1リクエストにつき1行**だけ出す（canonical log line）。
//
// 処理中に何行も出すのではなく、完了時に1行へ全部詰める。理由:
//
//   - 検索が1行で完結する。複数行を trace_id で結合しないと全体が見えない形にしない
//   - ログ量が桁で減る = 収集・保存のコストが桁で減る
//   - 「遅いリクエスト」を `http.server.request.duration_ms > 500` で即座に出せる
//
// **後から行数を減らすのは難しいので最初に1行と決める。** 処理中に足したい情報は
// context 経由でロガーに属性として載せ、逐次の実況は Debug に置く。
//
// フィールド名は OTel Semantic Conventions に合わせる。独自語彙（method / path / status）
// で始めると、APM やダッシュボードを入れた時に全部マッピングし直すことになる。
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 相関IDを載せたロガーを下流に渡す。これで**ハンドラが出すログにも自動で
		// trace_id が付く**（呼び出し側が毎回書かなくてよい）。
		l := logger.FromContext(r.Context())
		if c, ok := FromContext(r.Context()); ok {
			l = l.With("trace_id", c.TraceID, "span_id", c.SpanID, "request_id", c.RequestID)
		}
		ctx := logger.WithContext(r.Context(), l)

		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))

		// **Authorization / Cookie / クエリ文字列は出さない。**
		// 出していいものを列挙する側に寄せる（禁止リスト方式は必ず漏れる）。
		// url.path はパスのみで、クエリは入れない（トークンや個人情報が乗りうる）。
		attrs := []slog.Attr{
			slog.String("http.request.method", r.Method),
			slog.String("url.path", r.URL.Path),
			slog.Int("http.response.status_code", rec.status),
			slog.Int64("http.response.body.size", rec.written),
			slog.Int64("http.server.request.duration_ms", time.Since(start).Milliseconds()),
			slog.String("network.protocol.version", protoVersion(r)),
			slog.String("user_agent.original", r.UserAgent()),
		}
		// 冪等キーは載せる（許可リストに明示的に追加した1項目。秘密ではなく操作の識別子）。
		// リトライは試行ごとに trace_id が変わるため、**同じ操作の試行同士を繋げる唯一の
		// 安定した識別子**がこれになる。重複排除(4.2/4.4)が入った後は「2回目は実行せず
		// 同じ応答を返した」ことの検証材料にもなる。
		if k := r.Header.Get("Idempotency-Key"); k != "" {
			attrs = append(attrs, slog.String("http.request.header.idempotency-key", k))
		}
		l.LogAttrs(ctx, slog.LevelInfo, "request", attrs...)
	})
}

// recorder はステータスと本文サイズを覚える。
//
// Flush / Hijack を通さないので、SSE や WebSocket を使うリスナーが増えたら
// http.ResponseController 経由の委譲を足す必要がある（今はどのリスナーも使っていない）。
type recorder struct {
	http.ResponseWriter
	status  int
	written int64
	wrote   bool
}

func (r *recorder) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.wrote = true
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		// 暗黙の 200。WriteHeader を呼ばずに Write した場合。
		r.wrote = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

// Unwrap は http.ResponseController が元の ResponseWriter に到達できるようにする。
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func protoVersion(r *http.Request) string {
	switch {
	case r.ProtoMajor == 2:
		return "2"
	case r.ProtoMajor == 1 && r.ProtoMinor == 1:
		return "1.1"
	default:
		return r.Proto
	}
}
