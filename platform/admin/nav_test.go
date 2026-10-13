package admin_test

import (
	"net/http"
	"strings"
	"testing"
)

// サイドバー: ログイン前は出ない、ログイン後は現在地が1つだけハイライトされ、
// 準備中の項目はリンクを持たない。
func TestSidebar(t *testing.T) {
	h := newHarness(t)
	if got := body(t, h.get("/login")); strings.Contains(got, `class="sidebar"`) {
		t.Fatal("ログイン画面にサイドバーが出ている")
	}
	got := body(t, h.login("carol", "1", "/"))
	for _, want := range []string{`class="sidebar"`, `<li class="active" aria-current="page"><a href="/">写真</a>`, `<a href="/accounts">アカウント</a>`, `<li class="disabled"><span>機材</span>`} {
		if !strings.Contains(got, want) {
			t.Errorf("一覧画面に %q が無い", want)
		}
	}
	if strings.Count(got, `aria-current="page"`) != 1 {
		t.Errorf("現在地のハイライトが1つでない")
	}
	if strings.Contains(got, `<a href="#">機材`) || strings.Contains(got, `href="/gear"`) {
		t.Errorf("準備中の項目にリンクがある")
	}
	got = body(t, h.get("/accounts"))
	if !strings.Contains(got, `<li class="active" aria-current="page"><a href="/accounts">アカウント</a>`) {
		t.Errorf("アカウント画面で現在地が移っていない")
	}
}

// アカウント画面: subject を入れると確認画面へ、空や / 入りは弾く（GET のみで状態は変えない）。
func TestAccountsFormLeadsToConfirm(t *testing.T) {
	h := newHarness(t)
	body(t, h.login("carol", "1", "/"))
	got := body(t, h.get("/accounts/go?subject=alice"))
	if !strings.Contains(got, "削除を実行する") || !strings.Contains(got, "<code>alice</code>") {
		t.Fatalf("確認画面に遷移しない: %.300s", got)
	}
	res := h.get("/accounts/go?subject=a/b")
	if got := body(t, res); res.StatusCode != http.StatusBadRequest || !strings.Contains(got, "使えません") {
		t.Fatalf("不正な subject が弾かれない: status=%d", res.StatusCode)
	}
	if len(h.api.deleted) != 0 {
		t.Fatalf("GET で削除が走った: %v", h.api.deleted)
	}
}
