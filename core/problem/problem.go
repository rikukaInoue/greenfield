// Package problem は RFC 9457（application/problem+json）のエラー応答を定める。
// huma の ErrorModel に、機械可読の拡張フィールド `code` を必ず付ける
// （HTTPステータスのみに意味を持たせない。conventions/api-design.md §3.2）。
// 各サービスは Install() で huma のエラー生成をこの型に差し替え、ドメインエラーは New() で作る。
package problem

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Error は code 付きの problem details。huma.StatusError を満たし、ハンドラから返すとそのまま応答になる。
type Error struct {
	huma.ErrorModel
	// Code は機械可読のエラーコード。`<domain>.<reason>`（例: photo.not_found）または
	// フレームワーク共通の汎用コード（validation_failed 等）。
	Code string `json:"code" example:"photo.not_found" doc:"機械可読のエラーコード"`
}

// 汎用コード。ドメイン固有のコードは各サービスで `<domain>.<reason>` として定義する。
const (
	CodeBadRequest       = "bad_request"
	CodeUnauthenticated  = "unauthenticated"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeValidationFailed = "validation_failed"
	CodeInternal         = "internal"
)

// New はドメイン固有のエラー応答を作る。
func New(status int, code, detail string, errs ...error) *Error {
	e := &Error{Code: code}
	e.Status = status
	e.Title = http.StatusText(status)
	e.Detail = detail
	e.Errors = details(errs)
	return e
}

// Install は huma が生成する全てのエラー（入力検証の422、404等）を code 付きにする。
// huma.Register より前に呼ぶ（OpenAPIのエラースキーマにも反映される）。
func Install() {
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return New(status, codeForStatus(status), msg, errs...)
	}
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthenticated
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeConflict
	case http.StatusUnprocessableEntity:
		return CodeValidationFailed
	default:
		if status >= 500 {
			return CodeInternal
		}
		return CodeBadRequest
	}
}

func details(errs []error) []*huma.ErrorDetail {
	if len(errs) == 0 {
		return nil
	}
	out := make([]*huma.ErrorDetail, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		if d, ok := err.(huma.ErrorDetailer); ok {
			out = append(out, d.ErrorDetail())
			continue
		}
		out = append(out, &huma.ErrorDetail{Message: err.Error()})
	}
	return out
}
