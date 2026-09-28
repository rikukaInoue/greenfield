package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/rikukaInoue/greenfield/core/logger"
)

// RenderError はパニックを応答に変換する。
//
// **middleware から core/problem を直接呼ばない。** problem は huma に依存しており、
// import すると「middleware は stdlib だけ」という前提が崩れる（huma を抱えた問題は
// #137 の不一致1そのもの）。エラー形式を知っているのは HTTP 境界を組み立てる側なので、
// そちらから注入してもらう。
type RenderError func(w http.ResponseWriter, r *http.Request)

// Recover はハンドラのパニックを RFC 9457 の 500 に変換する。
//
// これが無いと何が起きるかは実測してある（#142）。net/http は接続単位で recover するので
// **プロセスは落ちないが、クライアントが受け取るのは EOF（接続切断）**で、problem 応答に
// ならない。さらにパニックは標準 log パッケージに出る（`http: panic serving`）ため、
// slog の外に落ちて構造化もされず相関IDとも紐づかない。
//
//	実測: Get "http://127.0.0.1:61581/boom": EOF
//
// 応答を書き始めた後のパニックは救えない。ヘッダは送信済みなので、ここでは本文を足さず
// ログだけ残して接続を切る（中途半端な JSON を継ぎ足すと、読む側が壊れた本文を掴む）。
func Recover(render RenderError) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec, _ := w.(*recorder) // AccessLog の下に置かれていれば取れる
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// http.ErrAbortHandler は「意図的に打ち切る」の合図。ログに出さない。
				if v == http.ErrAbortHandler {
					panic(v)
				}

				l := logger.FromContext(r.Context())
				l.LogAttrs(r.Context(), slog.LevelError, "panic",
					slog.Any("panic.value", v),
					slog.String("stack", string(debug.Stack())),
					slog.String("http.request.method", r.Method),
					slog.String("url.path", r.URL.Path),
				)

				if rec != nil && rec.wrote {
					return // ヘッダ送信済み。継ぎ足さない
				}
				render(w, r)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
