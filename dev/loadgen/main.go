// loadgen は photo の external API へ連続して読み書きを流し、失敗を数える。
// オンラインマイグレーションの最中にエラーが出ないことを確かめるための観測手段。
//
//	go run ./dev/loadgen -duration 60s -rps 20
//
// 終了時に結果を JSON で出す。エラーが1件でもあれば exit 1。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
)

type counters struct {
	Requests atomic.Int64
	Errors   atomic.Int64
	// ByStatus はステータスコードごとの件数。
	byStatus sync.Map
	// Failures は失敗の例（先頭数件）。
	failures []string
	mu       sync.Mutex
}

func (c *counters) record(status int, body string) {
	c.Requests.Add(1)
	v, _ := c.byStatus.LoadOrStore(status, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
	// 2xx 以外は失敗として扱う。改名の最中に 5xx / 4xx が出ないことが要件
	if status < 200 || status >= 300 {
		c.Errors.Add(1)
		c.mu.Lock()
		if len(c.failures) < 10 {
			c.failures = append(c.failures, fmt.Sprintf("status=%d body=%s", status, truncate(body, 200)))
		}
		c.mu.Unlock()
	}
}

func main() {
	var (
		base     = flag.String("base", "http://localhost:8080", "external リスナーの URL")
		duration = flag.Duration("duration", 30*time.Second, "流す時間")
		rps      = flag.Int("rps", 10, "1秒あたりのリクエスト数")
		users    = flag.String("users", "alice,bob,carol", "使うユーザー（カンマ区切り）")
		writes   = flag.Int("write-every", 5, "何リクエストごとに書き込みを混ぜるか。0 で読み取りのみ")
		quiet    = flag.Bool("quiet", false, "進捗を出さない")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()

	subjects := splitComma(*users)
	tokens := make([]string, len(subjects))
	for i, s := range subjects {
		tokens[i] = devtoken.Mint(devtoken.Claims{Subject: s})
	}

	c := &counters{}
	client := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(time.Second / time.Duration(*rps))
	defer ticker.Stop()

	var wg sync.WaitGroup
	var n int64
	start := time.Now()
	progress := time.NewTicker(5 * time.Second)
	defer progress.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-progress.C:
			if !*quiet {
				fmt.Fprintf(os.Stderr, "  %.0fs requests=%d errors=%d\n",
					time.Since(start).Seconds(), c.Requests.Load(), c.Errors.Load())
			}
		case <-ticker.C:
			i := atomic.AddInt64(&n, 1)
			token := tokens[int(i)%len(tokens)]
			write := *writes > 0 && i%int64(*writes) == 0
			wg.Add(1)
			go func() {
				defer wg.Done()
				if write {
					createPhoto(client, *base, token, c)
					return
				}
				listPhotos(client, *base, token, c)
			}()
		}
	}
	wg.Wait()

	byStatus := map[string]int64{}
	c.byStatus.Range(func(k, v any) bool {
		byStatus[fmt.Sprint(k)] = v.(*atomic.Int64).Load()
		return true
	})
	out, _ := json.MarshalIndent(map[string]any{
		"duration_seconds": time.Since(start).Round(time.Millisecond).Seconds(),
		"requests":         c.Requests.Load(),
		"errors":           c.Errors.Load(),
		"by_status":        byStatus,
		"failures":         c.failures,
	}, "", "  ")
	fmt.Println(string(out))
	if *writes > 0 {
		fmt.Fprintln(os.Stderr, "書き込みが pending_upload の行を残している。mise run reclaim:all で回収する")
	}
	if c.Errors.Load() > 0 {
		os.Exit(1)
	}
}

// listPhotos は一覧を読む。改名対象のカラムは応答に含まれる。
func listPhotos(client *http.Client, base, token string, c *counters) {
	req, _ := http.NewRequest(http.MethodGet, base+"/photos", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	do(client, req, c)
}

// createPhoto は投稿する。画像は上げないので pending_upload のまま残り、回収ジョブの対象になる。
// 溜まった行は `mise run reclaim:all` で回収する（オブジェクト → 行 → タプルの順に削除される）。
func createPhoto(client *http.Client, base, token string, c *counters) {
	body, _ := json.Marshal(map[string]any{
		"caption":      fmt.Sprintf("loadgen %d", time.Now().UnixNano()),
		"content_type": "image/png",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/photos", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	do(client, req, c)
}

func do(client *http.Client, req *http.Request, c *counters) {
	resp, err := client.Do(req)
	if err != nil {
		c.record(0, err.Error())
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	c.record(resp.StatusCode, string(b))
}

func splitComma(s string) []string {
	var out []string
	for _, v := range bytes.Split([]byte(s), []byte(",")) {
		if t := string(bytes.TrimSpace(v)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
