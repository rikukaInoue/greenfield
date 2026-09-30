package appconfig

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/appconfigdata"
	"github.com/open-feature/go-sdk/openfeature"
)

// fakeClient は appconfigdata の偽物。返す定義とエラーを差し替えられる。
type fakeClient struct {
	mu       sync.Mutex
	payloads [][]byte // GetLatestConfiguration が順に返す。尽きたら空(=変更なし)
	fetchErr error    // 設定されている間 GetLatestConfiguration が失敗する
	startErr error

	starts  atomic.Int32
	fetches atomic.Int32
	tokens  []string // 受け取ったトークンの列。トークンの持ち回りを検査する
}

func (f *fakeClient) StartConfigurationSession(_ context.Context, _ *appconfigdata.StartConfigurationSessionInput, _ ...func(*appconfigdata.Options)) (*appconfigdata.StartConfigurationSessionOutput, error) {
	f.starts.Add(1)
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &appconfigdata.StartConfigurationSessionOutput{InitialConfigurationToken: aws.String("t0")}, nil
}

func (f *fakeClient) GetLatestConfiguration(_ context.Context, in *appconfigdata.GetLatestConfigurationInput, _ ...func(*appconfigdata.Options)) (*appconfigdata.GetLatestConfigurationOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches.Add(1)
	f.tokens = append(f.tokens, aws.ToString(in.ConfigurationToken))
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	var body []byte
	if len(f.payloads) > 0 {
		body = f.payloads[0]
		f.payloads = f.payloads[1:]
	}
	return &appconfigdata.GetLatestConfigurationOutput{
		Configuration:              body,
		NextPollConfigurationToken: aws.String("t" + string(rune('1'+len(f.tokens)))),
	}, nil
}

func (f *fakeClient) setFetchErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetchErr = err
}

func (f *fakeClient) push(payload []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads = append(f.payloads, payload)
}

const doc = `{
  "ops_disable_uploads": {"enabled": false},
  "rollout_new_list": {"enabled": true, "variant": "blue", "percent": 25, "limit": 3}
}`

func newReady(t *testing.T, c Client) *Provider {
	t.Helper()
	p, err := New(Config{Application: "app", Environment: "env", Profile: "flags", Client: c, PollInterval: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Init(openfeature.EvaluationContext{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Shutdown)
	return p
}

func TestBooleanEvaluation(t *testing.T) {
	p := newReady(t, &fakeClient{payloads: [][]byte{[]byte(doc)}})

	if d := p.BooleanEvaluation(t.Context(), "rollout_new_list", false, nil); !d.Value || d.Reason != openfeature.StaticReason {
		t.Fatalf("enabled のフラグが true にならない: %+v", d)
	}
	if d := p.BooleanEvaluation(t.Context(), "ops_disable_uploads", true, nil); d.Value || d.Reason != openfeature.DisabledReason {
		t.Fatalf("disabled のフラグは false / DISABLED のはず: %+v", d)
	}
	if d := p.BooleanEvaluation(t.Context(), "no.such_flag", true, nil); !d.Value || d.ResolutionError.Error() == "" {
		t.Fatalf("未知のフラグは既定値 + FLAG_NOT_FOUND のはず: %+v", d)
	}
}

func TestAttributeEvaluations(t *testing.T) {
	p := newReady(t, &fakeClient{payloads: [][]byte{[]byte(doc)}})
	ctx := t.Context()

	if d := p.StringEvaluation(ctx, "rollout_new_list.variant", "def", nil); d.Value != "blue" {
		t.Fatalf("string 属性: %+v", d)
	}
	if d := p.FloatEvaluation(ctx, "rollout_new_list.percent", 0, nil); d.Value != 25 {
		t.Fatalf("float 属性: %+v", d)
	}
	if d := p.IntEvaluation(ctx, "rollout_new_list.limit", 0, nil); d.Value != 3 {
		t.Fatalf("int 属性: %+v", d)
	}
	if d := p.IntEvaluation(ctx, "rollout_new_list.percent", 0, nil); d.Value != 25 {
		t.Fatalf("整数に収まる float は int で読める: %+v", d)
	}
	// 型違いは既定値 + TYPE_MISMATCH
	if d := p.StringEvaluation(ctx, "rollout_new_list.percent", "def", nil); d.Value != "def" || d.ResolutionError.Error() == "" {
		t.Fatalf("型違いが素通りしている: %+v", d)
	}
	// フラグ本体の型付き参照は誤用として弾く
	if d := p.StringEvaluation(ctx, "rollout_new_list", "def", nil); d.Value != "def" || d.ResolutionError.Error() == "" {
		t.Fatalf("フラグ本体の string 参照が素通りしている: %+v", d)
	}
	// disabled のフラグの属性は既定値へ倒す
	if d := p.StringEvaluation(ctx, "ops_disable_uploads.variant", "def", nil); d.Value != "def" || d.Reason != openfeature.DisabledReason {
		t.Fatalf("disabled の属性: %+v", d)
	}
	// Object はフラグ全体(属性 map)
	if d := p.ObjectEvaluation(ctx, "rollout_new_list", nil, nil); d.Value.(map[string]any)["variant"] != "blue" {
		t.Fatalf("object: %+v", d)
	}
}

// 一度も取得できていなければ既定値 + PROVIDER_NOT_READY。
// Init が失敗を返すことも確かめる(静かに既定値で走り出さない)。
func TestNotReady(t *testing.T) {
	c := &fakeClient{startErr: errors.New("届かない")}
	p, err := New(Config{Application: "a", Environment: "e", Profile: "p", Client: c})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Init(openfeature.EvaluationContext{}); err == nil {
		t.Fatal("初回取得の失敗が Init のエラーとして出てこない")
	}
	if d := p.BooleanEvaluation(t.Context(), "rollout_new_list", true, nil); !d.Value || d.ResolutionError.Error() == "" {
		t.Fatalf("未取得なら既定値 + エラーのはず: %+v", d)
	}
}

// 取得失敗後も前回の定義で評価が続く(stale)。復帰後は新しい定義が反映される。
// このテストが fail open でないことは、途中の「失敗中も true のまま」の検査を
// 「false になる」に書き換えると失敗することで確認済み。
func TestStaleKeepsLastKnown(t *testing.T) {
	c := &fakeClient{payloads: [][]byte{[]byte(doc)}}
	p, err := New(Config{Application: "a", Environment: "e", Profile: "p", Client: c, PollInterval: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Init(openfeature.EvaluationContext{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Shutdown)

	// ポーリングを待たず、内部の fetch を直接叩いて失敗→復帰を再現する
	c.setFetchErr(errors.New("一時障害"))
	if err := p.fetch(t.Context()); err == nil {
		t.Fatal("失敗が返らない")
	}
	if d := p.BooleanEvaluation(t.Context(), "rollout_new_list", false, nil); !d.Value {
		t.Fatalf("失敗中は前回の定義で評価が続くはず: %+v", d)
	}

	c.setFetchErr(nil)
	c.push([]byte(`{"rollout_new_list": {"enabled": false}}`))
	if err := p.fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if d := p.BooleanEvaluation(t.Context(), "rollout_new_list", true, nil); d.Value {
		t.Fatalf("復帰後の新しい定義が反映されていない: %+v", d)
	}
}

// 変更なし(空の Configuration)は前回の定義を上書きしない。トークンは進む。
func TestEmptyConfigurationMeansUnchanged(t *testing.T) {
	c := &fakeClient{payloads: [][]byte{[]byte(doc)}}
	p := newReady(t, c)

	if err := p.fetch(t.Context()); err != nil { // payloads が尽きて空が返る
		t.Fatal(err)
	}
	if d := p.BooleanEvaluation(t.Context(), "rollout_new_list", false, nil); !d.Value {
		t.Fatalf("空応答で定義が消えた: %+v", d)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.tokens) < 2 || c.tokens[0] == c.tokens[1] {
		t.Fatalf("トークンが持ち回られていない: %v", c.tokens)
	}
}

func TestConfigValidation(t *testing.T) {
	if _, err := New(Config{Client: &fakeClient{}}); err == nil {
		t.Fatal("識別子なしが通る")
	}
	if _, err := New(Config{Application: "a", Environment: "e", Profile: "p"}); err == nil {
		t.Fatal("Client なしが通る")
	}
	if _, err := New(Config{Application: "a", Environment: "e", Profile: "p", Client: &fakeClient{}, PollInterval: time.Second}); err == nil {
		t.Fatal("下限未満の PollInterval が通る")
	}
}
