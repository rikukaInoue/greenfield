package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// HeaderRequestTimeout は呼び出し元の残り時間(ミリ秒の整数)を下流へ運ぶヘッダ(#268)。
//
// **相対時間で運ぶ**。絶対時刻だとサーバ間の時計のずれがそのまま締め切りの誤差になる。
// gRPC の grpc-timeout と同じ考え方。HTTP に標準ヘッダは無いので独自名。
// 送り手は core/httpclient。
const HeaderRequestTimeout = "X-Request-Timeout-Ms"

// DefaultRequestDeadline はヘッダが無いときに掛ける自分の上限。サーバの WriteTimeout(30s。
// 10.5)より内側に置く——WriteTimeout は ctx に反映されないため、これが無いとハンドラも
// DB クエリも下流呼び出しも、クライアントが諦めた後も走り続けうる。
const DefaultRequestDeadline = 25 * time.Second

// Deadline はリクエストの ctx に締め切りを付ける(デッドライン伝播の受け側。#268)。
//
// 締め切り = min(受信ヘッダの残り時間, limit)。ヘッダが無い・壊れている場合は limit だけ。
// **壊れた値で 400 を返さない**(traceparent と同じ方針。経路上の壊れた中継1つで落ちない)。
// 残り時間が 0 以下で届いたリクエストは、もう誰も結果を待っていないので即 504 で返す。
func Deadline(limit time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d := limit
			if v := r.Header.Get(HeaderRequestTimeout); v != "" {
				if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
					if ms <= 0 {
						http.Error(w, "deadline exceeded before arrival", http.StatusGatewayTimeout)
						return
					}
					if in := time.Duration(ms) * time.Millisecond; in < d {
						d = in
					}
				}
			}
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
