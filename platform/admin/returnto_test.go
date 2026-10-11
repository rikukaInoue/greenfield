package admin

import "testing"

// safeReturnTo はログイン後の戻り先を同一オリジンの相対パスに限定する。
// オープンリダイレクト（#185 で web に実際にあった穴）を admin でも作らないことの固定。
func TestSafeReturnTo(t *testing.T) {
	cases := map[string]string{
		"":                               "/",
		"/":                              "/",
		"/accounts/alice/delete":         "/accounts/alice/delete",
		"/?owner_id=alice&limit=10":      "/?owner_id=alice&limit=10",
		"//evil.example/":                "/",
		"https://evil.example/":          "/",
		"javascript:alert(1)":            "/",
		"accounts/alice":                 "/",
		"/\\evil.example":                "/",
		"/x\r\nSet-Cookie: a=b":          "/",
		"/accounts/alice/delete#frag":    "/accounts/alice/delete#frag",
		"/%2F%2Fevil.example":            "/%2F%2Fevil.example", // パスとして解釈される（ホストにならない）
		"http://localhost:8200/accounts": "/",
	}
	for in, want := range cases {
		if got := safeReturnTo(in); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}
