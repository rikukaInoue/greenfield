// Package internalapi は internal リスナー（:8081、サービス間（client_credentials）。プライベートネットワークのみ）のハンドラ。
// 呼び出し主体ごとにリスナーを分けるのは、要求するAAL・レート制限・監査・到達経路が異なるため
// （conventions/api-design.md §3.2）。パスプレフィックスによる分離は採らない。
// ディレクトリ名を internal にしないのは、Goの internal パッケージ規則（親配下からしかimport不可）と衝突し
// app/ から配線できなくなるため（規約 internal-01 の handler/internal/ からの意図的な逸脱）。
package internalapi

import "net/http"

// New は internal リスナーのルートハンドラを返す。
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}
