// Package middleware は net/http の Handler だけで書ける横断的関心事を持つ。
//
// **huma / chi に依存させない。** ログと相関IDはルータに依存しない関心事であり、
// httpapi（huma + chi を抱える）に入れると core の全利用者にルータを持ち込む
// （#137 の不一致1と同じ形）。ここは stdlib だけで閉じる。
//
// 適用順は Stack が決める。順番に意味があるので呼び出し側で組み替えさせない。
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

// 相関の主キーは W3C Trace Context の trace_id とする。
//
// **X-Request-Id を主にしない。** デファクトだが標準ではなく、組織の外に出ると繋がらない。
// traceparent は ALB / API Gateway / OTel SDK / 各種 APM が素で理解する。#59（otel）を
// 後で乗せる前提なら、最初から標準側に寄せるほうが安い。
//
// X-Request-Id は**外向けの取っ手として残す**。来ていれば記録し、応答ヘッダに返す
// （サポート問い合わせで「このIDで調べて」が成立する）。捨てずに格下げして両方持つ。
const (
	headerTraceparent = "traceparent"
	headerRequestID   = "X-Request-Id"
)

// Correlation はこのリクエストの相関情報。
type Correlation struct {
	// TraceID は W3C の trace-id（16バイトの hex）。受信した traceparent があれば引き継ぐ。
	TraceID string
	// SpanID はこのサーバ処理の span-id（8バイトの hex）。**受信した span は親になるので必ず新規に作る**。
	SpanID string
	// ParentSpanID は受信した traceparent の span-id。無ければ空。
	ParentSpanID string
	// Sampled は trace-flags の sampled ビット。
	Sampled bool
	// RequestID は X-Request-Id。来ていなければ TraceID を流用する（取っ手を必ず1つ返すため）。
	RequestID string
}

type corrKey struct{}

// FromContext は相関情報を返す。無ければゼロ値と false。
func FromContext(ctx context.Context) (Correlation, bool) {
	c, ok := ctx.Value(corrKey{}).(Correlation)
	return c, ok
}

// Correlate は traceparent / X-Request-Id を読んで context に載せ、応答ヘッダに返す。
//
// OTel SDK は使わない。いま要るのは「ヘッダ形式を守って引き継ぐ」ことだけで、
// SDK を core に入れると全利用者が exporter とその依存を抱える（ADR 0004 の軽さ）。
// #59 [6.1] で計測を入れる時に、ここを SDK の propagator に差し替える。
func Correlate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := parseTraceparent(r.Header.Get(headerTraceparent))
		c.SpanID = randHex(8) // サーバ処理は常に新しい span
		if c.TraceID == "" {
			c.TraceID = randHex(16)
		}
		c.RequestID = r.Header.Get(headerRequestID)
		if c.RequestID == "" {
			c.RequestID = c.TraceID
		}

		// 応答に返す。呼び手（SSR / ブラウザ / 運用者）が同じIDで追える。
		w.Header().Set(headerRequestID, c.RequestID)
		w.Header().Set(headerTraceparent, formatTraceparent(c))

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), corrKey{}, c)))
	})
}

// parseTraceparent は `00-<32hex>-<16hex>-<2hex>` を読む。
//
// **壊れた値は黙って捨てて新規採番する**。不正な traceparent で 400 を返すと、
// 経路上の壊れたプロキシ1つでサービスが落ちる。W3C も未知/不正は無視して
// 新しいトレースを始めるよう定めている。
func parseTraceparent(v string) Correlation {
	parts := strings.Split(v, "-")
	if len(parts) != 4 || parts[0] != "00" {
		return Correlation{}
	}
	traceID, spanID, flags := parts[1], parts[2], parts[3]
	if !isHex(traceID, 32) || !isHex(spanID, 16) || !isHex(flags, 2) {
		return Correlation{}
	}
	// all-zero は仕様で無効。
	if strings.Trim(traceID, "0") == "" || strings.Trim(spanID, "0") == "" {
		return Correlation{}
	}
	return Correlation{TraceID: traceID, ParentSpanID: spanID, Sampled: flags[1]&1 == 1}
}

func formatTraceparent(c Correlation) string {
	flags := "00"
	if c.Sampled {
		flags = "01"
	}
	return "00-" + c.TraceID + "-" + c.SpanID + "-" + flags
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func randHex(n int) string {
	b := make([]byte, n)
	// crypto/rand.Read は Go 1.24 以降エラーを返さない（失敗時は panic する）。
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Base は全リスナーに必ず入る基盤スタックを、外側から内側の順で返す。
//
// **順序に意味があるので呼び出し側で組み替えさせない。**
//
//	Correlate  最も外。これ以降の全ログに trace_id が乗る
//	AccessLog  次。Recover が書いた 500 も「最終ステータス」として記録できる
//	Recover    内。認証・ハンドラのパニックを problem に変換する
//
// Recover を AccessLog の外に置くと、パニック時に AccessLog の記録が走らず
// **落ちたリクエストだけログに出ない**（いちばん見たいものが消える）。
// Correlate を AccessLog の外に置くのは、アクセスログ自身に相関IDを載せるため。
func Base(render RenderError) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		Correlate,
		AccessLog,
		Recover(render),
	}
}
