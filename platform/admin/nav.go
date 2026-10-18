package admin

// navItem はサイドバーの1項目。メニューを足すときはここに1行足すだけで、
// レイアウト側（layout.html）は Nav を range して描く。
type navItem struct {
	Key   string // 現在地の判定に使う識別子（ページが Active として返す）
	Label string
	Href  string
	// Disabled は「準備中」。リンクを張らず、項目が増えるときの形だけ見せる
	Disabled bool
}

// nav はメニューの定義。順序がそのまま表示順。
var nav = []navItem{
	{Key: "photos", Label: "写真", Href: "/"},
	{Key: "accounts", Label: "アカウント", Href: "/accounts"},
	{Key: "gear", Label: "機材", Disabled: true}, // gear の admin API は未実装（経路なし）
}

// page は全テンプレートが共有する描画データ。Viewer と現在地に Nav を添える。
type page struct {
	Viewer *Session
	Active string
	Nav    []navItem
	Body   any
}

func newPage(viewer *Session, active string, body any) page {
	return page{Viewer: viewer, Active: active, Nav: nav, Body: body}
}
