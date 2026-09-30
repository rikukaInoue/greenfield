// Command mcpadmin は admin API を MCP ツールとして公開する最小の MCP サーバ(#14 / 5.4)。
//
// 主眼は「自律エージェントに human-in-the-loop が構造的に強制される」ことの実演:
// delete_account ツールはエージェントのトークン(ADMIN_TOKEN)で admin API を叩くが、
// 危険操作は AAL2 を要求する。エージェントのトークンは aal1 までしか持てず(agent は
// public client で、acr_values=aal2 の非対話グラントを OP が拒む)、ツールは 401 の
// RFC 9470 チャレンジをそのまま結果として返す。エージェントはこれ以上進めず、
// **人間が TOTP で再認証したトークンを渡すまで成功しない**。
//
// MCP は JSON-RPC 2.0 over stdio。依存を増やさないため最小実装(initialize /
// tools/list / tools/call だけ)。プロトコルの網羅でなく、ツール呼び出しが認可で
// 止まることの再現が目的。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	in := bufio.NewReader(os.Stdin)
	out := os.Stdout
	for {
		var req rpcRequest
		if err := readMessage(in, &req); err != nil {
			if err == io.EOF {
				return
			}
			continue
		}
		resp := dispatch(req)
		if req.ID == nil {
			continue // notification: 応答しない
		}
		writeMessage(out, resp)
	}
}

func dispatch(req rpcRequest) rpcResponse {
	base := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		base.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "mcpadmin", "version": "0.1.0"},
		}
	case "tools/list":
		base.Result = map[string]any{"tools": []any{
			map[string]any{
				"name":        "delete_account",
				"description": "指定した subject の写真アカウントを削除する(危険操作: AAL2 が要る)",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"subject": map[string]any{"type": "string"}},
					"required":   []string{"subject"},
				},
			},
		}}
	case "tools/call":
		base.Result = callTool(req.Params)
	default:
		base.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return base
}

func callTool(params json.RawMessage) map[string]any {
	var p struct {
		Name      string `json:"name"`
		Arguments struct {
			Subject string `json:"subject"`
		} `json:"arguments"`
	}
	_ = json.Unmarshal(params, &p)
	if p.Name != "delete_account" {
		return toolText(true, "unknown tool: "+p.Name)
	}

	adminURL := envOr("ADMIN_URL", "http://localhost:8082")
	token := os.Getenv("ADMIN_TOKEN") // エージェントに渡されたトークン
	url := fmt.Sprintf("%s/accounts/%s:delete", adminURL, p.Arguments.Subject)
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return toolText(true, "admin API に到達できない: "+err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		return toolText(false, "削除しました: "+string(body))
	case http.StatusUnauthorized:
		// **human-in-the-loop の要**: エージェントはこの先へ進めない。
		// チャレンジをそのまま返し、人間の再認証を促す(エージェントは TOTP を持てない)。
		challenge := resp.Header.Get("WWW-Authenticate")
		return toolText(true, "この操作には人間の再認証(step-up)が必要です。エージェントは"+
			"自力で昇格できません。人間が TOTP で aal2 トークンを取得して再実行してください。\n"+
			"WWW-Authenticate: "+challenge)
	case http.StatusForbidden:
		return toolText(true, "権限がありません(operator でない): "+string(body))
	default:
		return toolText(true, fmt.Sprintf("失敗(%d): %s", resp.StatusCode, string(body)))
	}
}

func toolText(isError bool, text string) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isError,
	}
}

// readMessage / writeMessage は改行区切り JSON(この検証で使う最小形)。
// 本物の MCP stdio は Content-Length フレーミングだが、検証は行区切りで足りる。
func readMessage(r *bufio.Reader, v any) error {
	line, err := r.ReadBytes('\n')
	if err != nil {
		if len(bytes.TrimSpace(line)) == 0 {
			return err
		}
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return readMessage(r, v)
	}
	return json.Unmarshal(line, v)
}

func writeMessage(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "%s\n", b)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
