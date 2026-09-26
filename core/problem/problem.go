// Package problem は RFC 9457（application/problem+json）のエラー応答を定める。
// 機械可読の code を必ず持たせ、HTTP ステータスだけに意味を持たせない。
package problem

import (
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Error は code 付きの problem details。ハンドラから返すとそのまま応答になる。
type Error struct {
	huma.ErrorModel
	// Code は `<domain>.<reason>`（例 photo.not_found）または下記の汎用コード。
	Code string `json:"code" example:"photo.not_found" doc:"機械可読のエラーコード"`

	// Headers は応答に付ける追加ヘッダ。本文には出さない。
	Headers map[string]string `json:"-"`
}

// GetHeaders は huma.HeadersError を満たす。
func (e *Error) GetHeaders() http.Header {
	if len(e.Headers) == 0 {
		return nil
	}
	h := make(http.Header, len(e.Headers))
	for k, v := range e.Headers {
		h.Set(k, v)
	}
	return h
}

// Write はミドルウェア等、huma のハンドラ外から problem+json を書き出す。
func Write(w http.ResponseWriter, _ *http.Request, e *Error) {
	for k, v := range e.Headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}

// 汎用コード。
const (
	CodeBadRequest       = "bad_request"
	CodeUnauthenticated  = "unauthenticated"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeValidationFailed = "validation_failed"
	CodeInternal         = "internal"
)

// New はエラー応答を作る。
func New(status int, code, detail string, errs ...error) *Error {
	e := &Error{Code: code}
	e.Status = status
	e.Title = http.StatusText(status)
	e.Detail = detail
	e.Errors = details(errs)
	return e
}

// Install は huma が生成するエラーにも code を載せる。huma.Register より前に呼ぶ。
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
