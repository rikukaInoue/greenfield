// authsweep は認可の総当たり検査（#182）。alice のリソースを実際に作り、
// 全リスナーの全ルートへ「bob のトークン」と「無認証」で当てて、
// 拒否の形（401 / 403 / 404）以外が返ったら落とす。
//
//	mise run authz:sweep   # 前提: allinone（staticauthn）と DB が起動済み
//
// 水平権限昇格（他人の ID を入れると 200 が返る）は、どの既製の道具からも
// 出てこない。ZAP は「そのレスポンスが返っていいのか」を知らないので、
// 200 を正常応答として通過させる。ここは自前で持つしかない（#194 の要点）。
//
// ルート一覧はハンドラ登録の実体（app.APIs → chi の Routes()）から取るので、
// 経路が増えればこの検査の対象も自動で増える。増えた経路のパスパラメータに
// 代入値が未定義なら検査はエラーで止まる——「新しい経路が検査から漏れて
// 素通りする」形を構造的に作らない。
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
	"github.com/rikukaInoue/greenfield/core/httpapi"

	gearapp "github.com/rikukaInoue/greenfield/services/gear/app"
	photoapp "github.com/rikukaInoue/greenfield/services/photo/app"
)

var fail = false

func ng(format string, a ...any) { fmt.Fprintf(os.Stderr, "  NG: "+format+"\n", a...); fail = true }
func ok(format string, a ...any) { fmt.Printf("  ok: "+format+"\n", a...) }

// service は検査対象の1サービス。ルートは app.APIs(nil) から静的に列挙する
// （nil でもルート表の組み立てには足りる。routing_test と同じ使い方）。
type service struct {
	name   string
	bases  map[httpapi.Listener]string
	routes map[httpapi.Listener][]httpapi.Route
	// params はパスパラメータ名 → 代入値。値は「alice が所有する実リソース」を
	// 使う（external の越境検査の本体）。未知のパラメータはエラーで止まる
	params map[string]string
}

// sharedCatalog は「所有の概念が無い」ことを明示した免除。ここに載せた経路は
// bob の 200 を正しい応答として扱う（無認証 401 の検査は免除しない）。
// 免除に理由を書かせるのは、黙って除外した経路が検査済みに見えることを防ぐため。
var sharedCatalog = map[string]string{
	"gear GET /items":      "機材は共有カタログ（全ユーザーが同じ一覧を見る設計）",
	"gear GET /items/{id}": "同上。詳細も公開情報のみ",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if fail {
		os.Exit(1)
	}
}

func run() error {
	alice := devtoken.Mint(devtoken.Claims{Subject: "alice"})
	bob := devtoken.Mint(devtoken.Claims{Subject: "bob"})

	// 実行ごとに一意の印。photo の一覧に alice の行が混ざる検査は ID でなく
	// この印で見る（過去の実行の残骸と衝突しない）
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	marker := "authsweep-" + hex.EncodeToString(b)

	photo := &service{
		name: "photo",
		bases: map[httpapi.Listener]string{
			httpapi.External: envOr("PHOTO_EXTERNAL_URL", "http://localhost:8080"),
			httpapi.Internal: envOr("PHOTO_INTERNAL_URL", "http://localhost:8081"),
			httpapi.Admin:    envOr("PHOTO_ADMIN_URL", "http://localhost:8082"),
		},
		routes: listenerRoutes(photoapp.APIs(nil)),
	}
	gear := &service{
		name: "gear",
		bases: map[httpapi.Listener]string{
			httpapi.External: envOr("GEAR_EXTERNAL_URL", "http://localhost:8090"),
			httpapi.Internal: envOr("GEAR_INTERNAL_URL", "http://localhost:8091"),
			httpapi.Admin:    envOr("GEAR_ADMIN_URL", "http://localhost:8092"),
		},
		routes: listenerRoutes(gearapp.APIs(nil)),
	}

	for _, s := range []*service{photo, gear} {
		for _, l := range httpapi.Listeners {
			if err := waitHealthz(s.bases[l]); err != nil {
				return fmt.Errorf("%s %s に到達できない: %w（mise run run で起動する）", s.name, l, err)
			}
		}
	}

	// --- 種まき: alice のリソースを実際に作る ---
	// Read Model は status='ready' しか返さない（pending の詳細を出さない決定。監査 #85）ため、
	// 越境の的はアップロードを完了させた ready の写真を使う。pending のままだと 404 の理由が
	// 「認可」なのか「状態フィルタ」なのか区別できず、検査として空になる
	fmt.Println("1. alice のリソースを作る（越境検査の的）")
	photoID, upload, err := createPhoto(photo.bases[httpapi.External], alice, marker)
	if err != nil {
		return fmt.Errorf("alice の photo が作れない: %w", err)
	}
	if err := uploadAndCommit(photo.bases[httpapi.External], alice, photoID, upload); err != nil {
		return fmt.Errorf("alice の photo を ready にできない: %w", err)
	}
	pendingID, _, err := createPhoto(photo.bases[httpapi.External], alice, marker)
	if err != nil {
		return fmt.Errorf("alice の pending photo が作れない: %w", err)
	}
	itemID, err := createJSON(gear.bases[httpapi.External]+"/items", alice,
		map[string]any{"kind": "camera", "name": marker})
	if err != nil {
		return fmt.Errorf("alice の gear item が作れない: %w", err)
	}
	ok("photo id=%s（ready）/ pending id=%s / gear item id=%s", photoID, pendingID, itemID)

	photo.params = map[string]string{"id": photoID, "gear_item_id": itemID, "subject": "alice"}
	gear.params = map[string]string{"id": itemID, "gear_item_id": itemID, "key": marker + "-nonexistent"}

	// --- 陽性対照: 検査系自体が全拒否に壊れていないことを先に確かめる ---
	// これが無いと「サーバーが全部 404 を返す壊れ方」でも検査が緑になる
	fmt.Println("2. 陽性対照（alice 自身は見える）")
	// fresh=true: 作成直後は認可判定の反映に窓がある（API 契約にある鮮度指定を使う）
	if st, body := do(http.MethodGet, photo.bases[httpapi.External]+"/v2/photos/"+photoID+"?fresh=true", alice); st != 200 {
		return fmt.Errorf("alice が自分の photo を見られない（status=%d body=%.200s）。検査は空振りしている", st, body)
	}
	if st, _ := do(http.MethodGet, photo.bases[httpapi.External]+"/v2/photos", alice); st != 200 {
		return fmt.Errorf("alice の一覧が %d。検査は空振りしている", st)
	}
	ok("alice は自分の photo の詳細と一覧を見られる")

	// --- 監査 #85 の再発防止: pending の詳細はたとえ他人が ID を知っていても出ない ---
	fmt.Println("3. pending の詳細漏れ（監査 #85 の形）")
	if st, _ := statusIn(http.MethodGet, photo.bases[httpapi.External]+"/v2/photos/"+pendingID, bob,
		http.StatusForbidden, http.StatusNotFound); st != 0 {
		ng("photo external GET /v2/photos/{id}(pending): bob に %d が返った（403/404 であるべき）", st)
	} else {
		ok("pending の詳細は bob に漏れない")
	}

	// --- 総当たり ---
	swept := 0
	for _, s := range []*service{photo, gear} {
		for _, l := range httpapi.Listeners {
			routes := s.routes[l]
			if len(routes) == 0 {
				return fmt.Errorf("%s %s のルートが列挙できない（Routes() が空。検査が空振りしている）", s.name, l)
			}
			fmt.Printf("4. %s %s（%d 経路）\n", s.name, l, len(routes))
			for _, rt := range routes {
				if rt.Path == "/healthz" { // 契約外・認証外
					continue
				}
				if err := sweep(s, l, rt, bob, marker, photoID); err != nil {
					return err
				}
				swept++
			}
		}
	}
	// 経路数のフェイルオープン防止。photo 8 + gear 5 の現状に対し、半分を割ったら
	// 列挙自体を疑う（ルータ差し替えなどで Routes() が痩せても黙って緑にしない）
	if swept < 8 {
		return fmt.Errorf("検査した経路が %d 件しかない。列挙が壊れている", swept)
	}
	fmt.Printf("検査した経路: %d（healthz を除く。無認証 + bob の2面）\n", swept)
	return nil
}

// sweep は1経路に「無認証」と「bob」を当てる。
func sweep(s *service, l httpapi.Listener, rt httpapi.Route, bob, marker, alicePhotoID string) error {
	path, err := fillParams(rt.Path, s.params)
	if err != nil {
		return fmt.Errorf("%s %s %s: %w", s.name, l, rt.Path, err)
	}
	url := s.bases[l] + path
	label := fmt.Sprintf("%s %s %s %s", s.name, l, rt.Method, rt.Path)

	// 無認証は全リスナー共通で 401（素通しのリスナーを作らない）
	if st, _ := do(rt.Method, url, ""); st != http.StatusUnauthorized {
		ng("%s: 無認証で %d（401 であるべき）", label, st)
	} else {
		ok("%s: 無認証 → 401", label)
	}

	reason, shared := sharedCatalog[s.name+" "+rt.Method+" "+rt.Path]
	switch {
	case l == httpapi.Internal:
		// 人間のトークンにはスコープが無い（#27）。403 以外は素通り
		if st, _ := do(rt.Method, url, bob); st != http.StatusForbidden {
			ng("%s: 人間のトークンで %d（403 であるべき）", label, st)
		} else {
			ok("%s: 人間のトークン → 403", label)
		}
	case l == httpapi.Admin:
		// bob はオペレータではない。403 が本線だが、AAL2 要求の経路は RFC 9470 の
		// ステップアップチャレンジ（401 insufficient_user_authentication）が先に返る
		if st, _ := statusIn(rt.Method, url, bob, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound); st != 0 {
			ng("%s: 一般ユーザーのトークンで %d（401/403/404 であるべき）", label, st)
		} else {
			ok("%s: 一般ユーザーのトークン → 拒否", label)
		}
	case shared:
		st, _ := do(rt.Method, url, bob)
		if st < 200 || st >= 300 {
			ng("%s: 共有カタログなのに bob が読めない（%d）", label, st)
		} else {
			ok("%s: bob → %d（免除: %s）", label, st, reason)
		}
	case strings.Contains(rt.Path, "{"):
		// external の越境の本丸: alice のリソース ID に bob のトークン。
		// このリポジトリの決定は「存在を伏せる」なので 404 が本線（403 も拒否として認める）
		if st, body := statusIn(rt.Method, url, bob, http.StatusForbidden, http.StatusNotFound); st != 0 {
			ng("%s: 他人のリソースに %d（403/404 であるべき）body=%.200s", label, st, body)
		} else {
			ok("%s: 他人のリソース → 拒否", label)
		}
	case rt.Method == http.MethodGet:
		// パラメータ無しの読み取り（一覧）: bob には見えてよいが、alice の行が
		// 混ざったら認可フィルタの漏れ
		st, body := do(rt.Method, url, bob)
		switch {
		case st != http.StatusOK:
			ng("%s: bob の一覧が %d（200 であるべき）", label, st)
		case strings.Contains(body, marker) || strings.Contains(body, `"id":`+alicePhotoID+`,`):
			ng("%s: bob の一覧に alice のリソースが混ざっている", label)
		default:
			ok("%s: bob の一覧に alice の行は無い", label)
		}
	default:
		// パラメータ無しの書き込み（作成系）は「自分のリソースを作る」操作で
		// 越境の的が無い。無認証 401 の検査だけで通す
		ok("%s: 作成系（無認証 401 のみ検査）", label)
	}
	return nil
}

// statusIn は許可ステータス以外なら status と body を返し、許可内なら 0 を返す。
func statusIn(method, url, token string, allowed ...int) (int, string) {
	st, body := do(method, url, token)
	for _, a := range allowed {
		if st == a {
			return 0, ""
		}
	}
	return st, body
}

var paramRe = regexp.MustCompile(`\{([^}]+)\}`)

// fillParams はパスパラメータへ代入値を埋める。未知のパラメータは、
// 「新しい経路が増えたのに検査の的が定義されていない」ことなので止める。
func fillParams(path string, params map[string]string) (string, error) {
	var missing []string
	filled := paramRe.ReplaceAllStringFunc(path, func(m string) string {
		name := strings.Trim(m, "{}")
		if v, okv := params[name]; okv {
			return v
		}
		missing = append(missing, name)
		return m
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("パスパラメータ %v の代入値が未定義。authsweep の params に「alice が所有する実リソース」を追記する", missing)
	}
	return filled, nil
}

func listenerRoutes(apis map[httpapi.Listener]httpapi.API) map[httpapi.Listener][]httpapi.Route {
	out := make(map[httpapi.Listener][]httpapi.Route, len(apis))
	for l, api := range apis {
		out[l] = api.Routes()
	}
	return out
}

func do(method, url, token string) (int, string) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return 0, err.Error()
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	return res.StatusCode, string(body)
}

// createPhoto は alice の写真を投稿し、id と署名付きアップロード URL を返す。
func createPhoto(base, token, marker string) (string, string, error) {
	buf, _ := json.Marshal(map[string]any{"caption": marker, "content_type": "image/png"})
	req, err := http.NewRequest(http.MethodPost, base+"/v2/photos", bytes.NewReader(buf))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", "", fmt.Errorf("status=%d body=%.300s", res.StatusCode, body)
	}
	var out struct {
		ID        json.Number `json:"id"`
		UploadURL string      `json:"upload_url"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.ID.String() == "" {
		return "", "", fmt.Errorf("応答から id が取れない: %.300s", body)
	}
	return out.ID.String(), out.UploadURL, nil
}

// uploadAndCommit は署名 URL へ画像を PUT し、:commit で ready にする。
// コンテナ内ホスト名の署名 URL は UPLOAD_URL_REWRITE（"旧=新"）で読み替える
// （例: UPLOAD_URL_REWRITE="http://rustfs:9000=http://localhost:9000"）。
func uploadAndCommit(base, token, id, uploadURL string) error {
	// 署名（SigV4）には Host が含まれるので、URL を書き換えると署名が壊れる。
	// 接続先だけ差し替え、Host ヘッダは署名時の値を送る
	hostOverride := ""
	if rw := os.Getenv("UPLOAD_URL_REWRITE"); rw != "" {
		if old, newer, found := strings.Cut(rw, "="); found && strings.Contains(uploadURL, old) {
			if u, err := neturl.Parse(uploadURL); err == nil {
				hostOverride = u.Host
			}
			uploadURL = strings.Replace(uploadURL, old, newer, 1)
		}
	}
	req, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader([]byte("authsweep-image-bytes")))
	if err != nil {
		return err
	}
	if hostOverride != "" {
		req.Host = hostOverride
	}
	req.Header.Set("Content-Type", "image/png") // content_type と一致させる（署名の条件）
	res, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("画像の PUT: %w", err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("画像の PUT が status=%d", res.StatusCode)
	}
	if st, body := do(http.MethodPost, base+"/v2/photos/"+id+":commit", token); st < 200 || st >= 300 {
		return fmt.Errorf(":commit が status=%d body=%.300s", st, body)
	}
	return nil
}

// createJSON は alice として POST し、応答の id を文字列で返す。
func createJSON(url, token string, payload map[string]any) (string, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("status=%d body=%.300s", res.StatusCode, body)
	}
	var out struct {
		ID json.Number `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.ID.String() == "" {
		return "", fmt.Errorf("応答から id が取れない: %.300s", body)
	}
	return out.ID.String(), nil
}

func waitHealthz(base string) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		res, err := httpClient.Get(base + "/healthz")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("healthz が 200 を返さない")
		}
		time.Sleep(time.Second)
	}
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
