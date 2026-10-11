// Package admin は社内オペレータ向けの管理画面（SSR）。
//
// 画面はサーバが html/template で描き、photo の admin API はサーバが operator 本人の
// トークンで代理呼び出しする。トークンはサーバ側セッションにしか無く、ブラウザへは
// セッション ID だけが渡る（check #17 と同じ原則。web と違い JS を一切使わないので、
// CSP は script-src 'none' にできる）。
//
// 認可は API 側の判定に委ねる: operator でなければ API が 403、保証レベルが足りなければ
// 401 + insufficient_user_authentication（RFC 9470）を返し、画面はそれを「再認証へ誘導」
// に変換するだけで、自前の権限表を持たない。
package admin

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
	"github.com/rikukaInoue/greenfield/core/logger"
	"github.com/rikukaInoue/greenfield/core/middleware"
	"github.com/rikukaInoue/greenfield/core/runtimeenv"
	adminapi "github.com/rikukaInoue/greenfield/services/photo-client/admin"
)

// Version は管理画面のバージョン（ログの service.version）。
const Version = "0.1.0"

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Config は管理画面の設定。
type Config struct {
	// PhotoAdminURL は photo の admin リスナー。到達できるのはこのサーバだけという前提。
	PhotoAdminURL string
	// SessionSecret はセッション Cookie（ID）の署名鍵。
	SessionSecret string
	// SecureCookie は Cookie に Secure を付ける（TLS の内側で動くとき）。
	SecureCookie bool
	// DevLogin は devtoken による開発用ログインを有効にする。許可リストの環境以外では拒否される。
	DevLogin bool
	// HTTPClient は admin API を呼ぶクライアント。nil なら http.DefaultClient。
	HTTPClient *http.Client
}

// Server は管理画面。
type Server struct {
	cfg      Config
	photo    *adminapi.ClientWithResponses
	sessions *sessionStore
	pages    map[string]*template.Template
}

// New は管理画面を組み立てる。ログインの手段が1つも無い設定は起動を拒む（fail closed）。
func New(cfg Config) (*Server, error) {
	if !cfg.DevLogin {
		return nil, errors.New("admin: ログイン手段が無い（OIDC は未実装。開発環境では DevLogin を有効にする）")
	}
	if err := runtimeenv.RequireDevelopment("admin の開発用ログイン（devtoken）"); err != nil {
		return nil, err
	}
	if cfg.SessionSecret == "" {
		return nil, errors.New("admin: SessionSecret が空")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	photo, err := adminapi.NewClientWithResponses(cfg.PhotoAdminURL, adminapi.WithHTTPClient(hc))
	if err != nil {
		return nil, fmt.Errorf("admin: photo client: %w", err)
	}
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, photo: photo, sessions: newSessionStore(cfg.SessionSecret), pages: pages}, nil
}

// parsePages は layout と各ページを組み合わせる。ページごとに layout を複製するのは、
// 同名の "content" ブロックを持つページを1つの template 集合に混ぜられないため。
func parsePages() (map[string]*template.Template, error) {
	layout, err := template.ParseFS(templateFS, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, name := range []string{"login", "photos", "confirm", "result"} {
		t, err := layout.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := t.ParseFS(templateFS, "templates/"+name+".html"); err != nil {
			return nil, fmt.Errorf("admin: template %s: %w", name, err)
		}
		pages[name] = t
	}
	return pages, nil
}

// Handler はルータを組む。基盤スタック（相関ID→アクセスログ→締め切り→パニック復帰）は
// API と同じ core/middleware を使い、その内側にこの画面固有のヘッダとセッションを積む。
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	for _, m := range middleware.Base(s.renderPanic) {
		r.Use(m)
	}
	r.Use(securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	static, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServerFS(static)))

	r.Get("/login", s.loginForm)
	r.With(sameSiteOnly).Post("/login", s.login)
	r.With(sameSiteOnly).Post("/logout", s.logout)

	r.Group(func(r chi.Router) {
		r.Use(s.requireSession)
		r.Get("/", s.listPhotos)
		r.Get("/accounts/{subject}/delete", s.confirmDelete)
		r.With(sameSiteOnly).Post("/accounts/{subject}:delete", s.deleteAccount)
	})
	return r
}

// securityHeaders は SSR 応答の防御ヘッダ。JS を一切配らないので script-src は 'none'。
// web（React Router）で妥協した 'unsafe-inline' は、ここでは最初から要らない。
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// sameSiteOnly は状態を変える POST をクロスサイトから受け付けない。
// Cookie が SameSite=Lax なので届かないのが本線で、これは Fetch Metadata による二重化。
func sameSiteOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
			http.Error(w, "cross-site request", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

// requireSession はセッションを ctx に積み、無ければログインへ送る。
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := s.sessionFrom(r)
		if sess == nil {
			http.Redirect(w, r, "/login?returnTo="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

func (s *Server) sessionFrom(r *http.Request) *Session {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	return s.sessions.lookup(c.Value)
}

func sessionOf(r *http.Request) *Session {
	sess, _ := r.Context().Value(ctxKey{}).(*Session)
	return sess
}

// bearer は admin API 呼び出しに operator 本人のトークンを付ける（代理呼び出し。M2M ではない）。
func bearer(sess *Session) adminapi.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+sess.AccessToken)
		return nil
	}
}

// ---- ログイン ----

type loginPage struct {
	Viewer   *Session
	Subject  string
	AAL      int
	ReturnTo string
	StepUp   bool
	Error    string
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := loginPage{ReturnTo: safeReturnTo(q.Get("returnTo")), StepUp: q.Get("stepup") == "1", AAL: 1}
	if p.StepUp {
		p.AAL = 2 // 再認証の誘導なので AAL2 を選んだ状態で出す
		if sess := s.sessionFrom(r); sess != nil {
			p.Subject = sess.Subject
		}
	}
	s.render(w, http.StatusOK, "login", p)
}

// login は開発用ログイン。subject と AAL から devtoken を mint してサーバ側セッションに置く。
// 本番の OIDC（confidential client）に差し替えるときも、この関数の外側（セッションと
// Cookie の契約）は変わらない。
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	subject := strings.TrimSpace(r.PostForm.Get("subject"))
	aal, _ := strconv.Atoi(r.PostForm.Get("aal"))
	returnTo := safeReturnTo(r.PostForm.Get("returnTo"))
	if subject == "" || (aal != 1 && aal != 2) {
		s.render(w, http.StatusBadRequest, "login", loginPage{ReturnTo: returnTo, AAL: 1, Error: "subject と保証レベルを入力してください"})
		return
	}
	// 既存セッションは破棄して作り直す（ステップアップは「同じ人が高い AAL で入り直す」なので
	// 前のトークンを残す理由がない）
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.destroy(c.Value)
	}
	token := devtoken.Mint(devtoken.Claims{Subject: subject, AAL: aal})
	value, err := s.sessions.create(&Session{Subject: subject, AAL: aal, AccessToken: token})
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "セッションを作れませんでした")
		return
	}
	http.SetCookie(w, s.cookie(value, int(sessionTTL.Seconds())))
	logger.FromContext(r.Context()).Info("admin login", "subject", subject, "aal", aal)
	// G710: 戻り先は safeReturnTo が同一オリジンの相対パスに限定済み（returnto_test.go で固定）
	http.Redirect(w, r, returnTo, http.StatusSeeOther) //nolint:gosec // 上記

}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.destroy(c.Value)
	}
	http.SetCookie(w, s.cookie("", -1))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// safeReturnTo はログイン後の戻り先を同一オリジンの相対パスに限定する（オープンリダイレクト対策）。
func safeReturnTo(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.ContainsAny(v, "\\\r\n") {
		return "/"
	}
	if u, err := url.Parse(v); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return v
}

// ---- 写真一覧 ----

type photosPage struct {
	Viewer  *Session
	OwnerID string
	Limit   int
	Photos  []adminapi.AdminPhoto
	Error   string
}

func (s *Server) listPhotos(w http.ResponseWriter, r *http.Request) {
	sess := sessionOf(r)
	q := r.URL.Query()
	p := photosPage{Viewer: sess, OwnerID: strings.TrimSpace(q.Get("owner_id")), Limit: 50}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n >= 1 && n <= 200 {
		p.Limit = n
	}
	limit := int64(p.Limit)
	params := &adminapi.AdminListPhotosParams{Limit: &limit}
	if p.OwnerID != "" {
		params.OwnerId = &p.OwnerID
	}
	res, err := s.photo.AdminListPhotosWithResponse(r.Context(), params, bearer(sess))
	if err != nil {
		s.renderError(w, r, http.StatusBadGateway, "photo の admin API に到達できませんでした")
		return
	}
	switch res.StatusCode() {
	case http.StatusOK:
		if res.JSON200 != nil && res.JSON200.Photos != nil {
			p.Photos = *res.JSON200.Photos
		}
		s.render(w, http.StatusOK, "photos", p)
	case http.StatusUnauthorized:
		// トークンが API に拒まれた。セッションごと捨ててログインへ
		s.expireSession(w, r)
	case http.StatusForbidden:
		s.renderError(w, r, http.StatusForbidden, "この subject は platform operator ではありません（authz に operator タプルが無い）")
	default:
		s.renderError(w, r, http.StatusBadGateway, fmt.Sprintf("photo の admin API が %d を返しました", res.StatusCode()))
	}
}

// ---- アカウント削除 ----

type confirmPage struct {
	Viewer  *Session
	Subject string
}

func (s *Server) confirmDelete(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "confirm", confirmPage{Viewer: sessionOf(r), Subject: chi.URLParam(r, "subject")})
}

type resultPage struct {
	Viewer  *Session
	Title   string
	Message string
	OK      bool
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	sess := sessionOf(r)
	subject := chi.URLParam(r, "subject")
	res, err := s.photo.AdminDeleteAccountWithResponse(r.Context(), subject, bearer(sess))
	if err != nil {
		s.renderError(w, r, http.StatusBadGateway, "photo の admin API に到達できませんでした")
		return
	}
	switch res.StatusCode() {
	case http.StatusOK:
		n := 0
		if res.JSON200 != nil {
			n = int(res.JSON200.DeletedPhotos)
		}
		logger.FromContext(r.Context()).Info("admin account deleted", "target", subject, "operator", sess.Subject, "deleted_photos", n)
		s.render(w, http.StatusOK, "result", resultPage{Viewer: sess, OK: true, Title: "削除しました",
			Message: fmt.Sprintf("%s のアカウントを削除しました（写真 %d 件）", subject, n)})
	case http.StatusUnauthorized:
		if isStepUpChallenge(res.HTTPResponse) {
			// RFC 9470: 保証レベル不足。元の操作（確認画面）へ戻れるように returnTo を持たせて再認証へ
			back := "/accounts/" + url.PathEscape(subject) + "/delete"
			http.Redirect(w, r, "/login?stepup=1&returnTo="+url.QueryEscape(back), http.StatusSeeOther)
			return
		}
		s.expireSession(w, r)
	case http.StatusForbidden:
		s.renderError(w, r, http.StatusForbidden, "この subject は platform operator ではありません")
	case http.StatusNotFound:
		s.renderError(w, r, http.StatusNotFound, "対象のアカウントが見つかりません")
	default:
		s.renderError(w, r, http.StatusBadGateway, fmt.Sprintf("photo の admin API が %d を返しました", res.StatusCode()))
	}
}

// isStepUpChallenge は 401 が「保証レベル不足」（RFC 9470）かを WWW-Authenticate で見分ける。
// 単なる無効トークンの 401 と混ぜると、失効したトークンでも再認証ループに入ってしまう。
func isStepUpChallenge(res *http.Response) bool {
	if res == nil {
		return false
	}
	return strings.Contains(res.Header.Get("WWW-Authenticate"), "insufficient_user_authentication")
}

// ---- 描画 ----

func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.pages[page].ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("admin: template", "page", page, "err", err)
	}
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.render(w, status, "result", resultPage{Viewer: sessionOf(r), Title: http.StatusText(status), Message: message})
}

// expireSession はトークンが API に拒まれたときの後始末。セッションを消してログインへ。
func (s *Server) expireSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.destroy(c.Value)
	}
	http.SetCookie(w, s.cookie("", -1))
	http.Redirect(w, r, "/login?returnTo="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
}

// renderPanic は middleware.Recover が呼ぶ 500 の描画（problem+json ではなく HTML）。
func (s *Server) renderPanic(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("<!doctype html><title>Internal Server Error</title><p>内部エラー。相関IDでログを確認してください。"))
}
