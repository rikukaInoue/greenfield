// Package appconfig は AWS AppConfig の feature flags を評価する
// openfeature.FeatureProvider。go-sdk-contrib に AppConfig のプロバイダが
// 存在しない(AWS 系は aws-ssm だけ)ため自作した(docs/adr/0011)。
//
// 形は「定義を同期してプロセス内で評価する」(ADR 0013)。appconfigdata の
// StartConfigurationSession / GetLatestConfiguration をポーリングし、
// 最新の定義をプロセス内に保持して評価する。評価のたびに AWS を呼ばない。
//
// 取得に失敗しても**前回取得した定義で評価を続ける**(stale)。一度も取得できて
// いない間は PROVIDER_NOT_READY を返し、呼び出し側(core/flags)が宣言済みの
// 既定値へ倒す。フラグ基盤の停止でアプリを止めない、が規約の要請。
package appconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/appconfigdata"
	"github.com/open-feature/go-sdk/openfeature"
)

// Client は appconfigdata のうちこのプロバイダが使う操作。テストは偽物を差す。
type Client interface {
	StartConfigurationSession(ctx context.Context, in *appconfigdata.StartConfigurationSessionInput, opts ...func(*appconfigdata.Options)) (*appconfigdata.StartConfigurationSessionOutput, error)
	GetLatestConfiguration(ctx context.Context, in *appconfigdata.GetLatestConfigurationInput, opts ...func(*appconfigdata.Options)) (*appconfigdata.GetLatestConfigurationOutput, error)
}

// Config はプロバイダの設定。
type Config struct {
	// Application / Environment / Profile は AppConfig 側の識別子(名前でも ID でも可)。
	Application string
	Environment string
	Profile     string
	// PollInterval は GetLatestConfiguration の間隔。AppConfig の下限が 15 秒。
	// 未指定は 20 秒。
	PollInterval time.Duration
	// Client は appconfigdata のクライアント。必須(合成ルートが実物を、テストが偽物を渡す)。
	Client Client
}

// flagValue は AppConfig の feature flags 形式の1フラグ分。
// データプレーンは {"<flagKey>": {"enabled": bool, "<属性名>": 値, ...}} を返す。
type flagValue struct {
	Enabled    bool
	Attributes map[string]any // enabled 以外の属性
}

// Provider は openfeature.FeatureProvider の実装。
type Provider struct {
	cfg Config

	mu    sync.RWMutex
	flags map[string]flagValue
	ready bool // 一度でも定義を取得できたか

	token string // 次回 GetLatestConfiguration に渡すトークン
	stop  chan struct{}
	done  chan struct{}
}

// New はプロバイダを作る。openfeature.SetNamedProviderWithContextAndWait に渡すと
// Init(初回取得)の完了まで待ってから使われる。
func New(cfg Config) (*Provider, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("appconfig: Client が必要")
	}
	if cfg.Application == "" || cfg.Environment == "" || cfg.Profile == "" {
		return nil, fmt.Errorf("appconfig: Application / Environment / Profile が必要")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 20 * time.Second
	}
	if cfg.PollInterval < 15*time.Second {
		return nil, fmt.Errorf("appconfig: PollInterval %s は AppConfig の下限 15s を下回る", cfg.PollInterval)
	}
	return &Provider{cfg: cfg, stop: make(chan struct{}), done: make(chan struct{})}, nil
}

// Metadata は openfeature.FeatureProvider の実装。
func (p *Provider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{Name: "aws-appconfig"}
}

// Hooks は openfeature.FeatureProvider の実装。
func (p *Provider) Hooks() []openfeature.Hook { return nil }

// Init は openfeature.StateHandler の実装。セッションを開始して初回の定義を取得し、
// 以後のポーリングを開始する。初回取得に失敗したらエラーを返す(SetNamedProvider…AndWait が
// 失敗として観測できる。起動時に基盤へ届かないことは、静かに既定値で走り続けるより
// 起動ログで分かるほうがよい)。
func (p *Provider) Init(_ openfeature.EvaluationContext) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.startSession(ctx); err != nil {
		return err
	}
	if err := p.fetch(ctx); err != nil {
		return err
	}
	go p.poll()
	return nil
}

// Shutdown は openfeature.StateHandler の実装。
func (p *Provider) Shutdown() {
	close(p.stop)
	<-p.done
}

func (p *Provider) startSession(ctx context.Context) error {
	out, err := p.cfg.Client.StartConfigurationSession(ctx, &appconfigdata.StartConfigurationSessionInput{
		ApplicationIdentifier:                aws.String(p.cfg.Application),
		EnvironmentIdentifier:                aws.String(p.cfg.Environment),
		ConfigurationProfileIdentifier:       aws.String(p.cfg.Profile),
		RequiredMinimumPollIntervalInSeconds: aws.Int32(15),
	})
	if err != nil {
		return fmt.Errorf("appconfig: セッション開始: %w", err)
	}
	p.token = aws.ToString(out.InitialConfigurationToken)
	return nil
}

// fetch は最新の定義を取得して保持する。**変更が無いとき Configuration は空**で
// 返る(AppConfig の仕様)ので、空は「前回のまま」であり上書きしない。
func (p *Provider) fetch(ctx context.Context) error {
	out, err := p.cfg.Client.GetLatestConfiguration(ctx, &appconfigdata.GetLatestConfigurationInput{
		ConfigurationToken: aws.String(p.token),
	})
	if err != nil {
		return err
	}
	p.token = aws.ToString(out.NextPollConfigurationToken)
	if len(out.Configuration) == 0 {
		return nil
	}
	flags, err := parse(out.Configuration)
	if err != nil {
		return fmt.Errorf("appconfig: 定義の解釈: %w", err)
	}
	p.mu.Lock()
	p.flags = flags
	p.ready = true
	p.mu.Unlock()
	return nil
}

// poll は定期的に fetch する。失敗しても前回の定義で評価を続け(stale)、次の周期で
// 再試行する。トークンは失敗の種類によらず張り直す(セッションは 24h または
// ポーリング間隔の大幅超過で失効する。失効の種別を見分けるより、失敗したら
// セッションから作り直すほうが単純で、コストも1リクエスト分しか変わらない)。
func (p *Provider) poll() {
	defer close(p.done)
	t := time.NewTicker(p.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := p.fetch(ctx)
			if err != nil {
				slog.Warn("appconfig: 定義の取得に失敗した。前回の定義で評価を続ける",
					"err", err, "application", p.cfg.Application, "profile", p.cfg.Profile)
				if serr := p.startSession(ctx); serr != nil {
					slog.Warn("appconfig: セッションの張り直しに失敗した", "err", serr)
				}
			}
			cancel()
		}
	}
}

// parse は AppConfig feature flags 形式の JSON を解釈する。
func parse(raw []byte) (map[string]flagValue, error) {
	var doc map[string]map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]flagValue, len(doc))
	for key, body := range doc {
		fv := flagValue{Attributes: make(map[string]any)}
		for name, v := range body {
			if name == "enabled" {
				b, ok := v.(bool)
				if !ok {
					return nil, fmt.Errorf("フラグ %s の enabled が bool でない: %T", key, v)
				}
				fv.Enabled = b
				continue
			}
			fv.Attributes[name] = v
		}
		out[key] = fv
	}
	return out, nil
}

// lookup はフラグ名を解決する。AppConfig のフラグキーは
// ^[a-z][a-zA-Z0-9_-]{0,63}$ で "." を含められない(7.5 実測)ため、
// "." は属性の区切りとしてだけ現れ、曖昧にならない。素の "flag" はフラグ本体、
// "flag.attr" は属性。戻りは (フラグ, 属性名, ok)。属性名が空ならフラグ本体。
func (p *Provider) lookup(name string) (flagValue, string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if fv, ok := p.flags[name]; ok {
		return fv, "", true
	}
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			if fv, ok := p.flags[name[:i]]; ok {
				return fv, name[i+1:], true
			}
			return flagValue{}, "", false
		}
	}
	return flagValue{}, "", false
}

func (p *Provider) isReady() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ready
}

// BooleanEvaluation は openfeature.FeatureProvider の実装。
// 素のフラグ名は enabled を返す。disabled のフラグは false / DISABLED。
func (p *Provider) BooleanEvaluation(_ context.Context, flag string, def bool, _ openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	if !p.isReady() {
		return openfeature.BoolResolutionDetail{Value: def, ProviderResolutionDetail: notReady()}
	}
	fv, attr, ok := p.lookup(flag)
	if !ok {
		return openfeature.BoolResolutionDetail{Value: def, ProviderResolutionDetail: notFound(flag)}
	}
	if attr == "" {
		reason := openfeature.StaticReason
		if !fv.Enabled {
			reason = openfeature.DisabledReason
		}
		return openfeature.BoolResolutionDetail{Value: fv.Enabled, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: reason}}
	}
	v, detail := attribute[bool](fv, flag, attr, def)
	return openfeature.BoolResolutionDetail{Value: v, ProviderResolutionDetail: detail}
}

// StringEvaluation は openfeature.FeatureProvider の実装。属性("flag.attr")のみ解決する。
func (p *Provider) StringEvaluation(_ context.Context, flag string, def string, _ openfeature.FlattenedContext) openfeature.StringResolutionDetail {
	v, detail := attributeEvaluation[string](p, flag, def)
	return openfeature.StringResolutionDetail{Value: v, ProviderResolutionDetail: detail}
}

// FloatEvaluation は openfeature.FeatureProvider の実装。
func (p *Provider) FloatEvaluation(_ context.Context, flag string, def float64, _ openfeature.FlattenedContext) openfeature.FloatResolutionDetail {
	v, detail := attributeEvaluation[float64](p, flag, def)
	return openfeature.FloatResolutionDetail{Value: v, ProviderResolutionDetail: detail}
}

// IntEvaluation は openfeature.FeatureProvider の実装。JSON の数値は float64 で
// 届くので、整数に収まるものだけ int64 として返す。
func (p *Provider) IntEvaluation(_ context.Context, flag string, def int64, _ openfeature.FlattenedContext) openfeature.IntResolutionDetail {
	f, detail := attributeEvaluation[float64](p, flag, float64(def))
	if detail.ResolutionError != (openfeature.ResolutionError{}) {
		return openfeature.IntResolutionDetail{Value: def, ProviderResolutionDetail: detail}
	}
	n := int64(f)
	if float64(n) != f {
		return openfeature.IntResolutionDetail{Value: def, ProviderResolutionDetail: mismatch(flag, "整数でない数値")}
	}
	return openfeature.IntResolutionDetail{Value: n, ProviderResolutionDetail: detail}
}

// ObjectEvaluation は openfeature.FeatureProvider の実装。
// 素のフラグ名は属性全体(map)を返す。"flag.attr" は当該属性を型を問わず返す。
func (p *Provider) ObjectEvaluation(_ context.Context, flag string, def any, _ openfeature.FlattenedContext) openfeature.InterfaceResolutionDetail {
	if !p.isReady() {
		return openfeature.InterfaceResolutionDetail{Value: def, ProviderResolutionDetail: notReady()}
	}
	fv, attr, ok := p.lookup(flag)
	if !ok {
		return openfeature.InterfaceResolutionDetail{Value: def, ProviderResolutionDetail: notFound(flag)}
	}
	if !fv.Enabled {
		return openfeature.InterfaceResolutionDetail{Value: def, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.DisabledReason}}
	}
	if attr == "" {
		return openfeature.InterfaceResolutionDetail{Value: fv.Attributes, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.StaticReason}}
	}
	v, ok := fv.Attributes[attr]
	if !ok {
		return openfeature.InterfaceResolutionDetail{Value: def, ProviderResolutionDetail: notFound(flag)}
	}
	return openfeature.InterfaceResolutionDetail{Value: v, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.StaticReason}}
}

// attributeEvaluation は "flag.attr" 形式の型付き解決の共通部。
// (メソッドは型パラメータを持てないためパッケージ関数)
func attributeEvaluation[T comparable](p *Provider, flag string, def T) (T, openfeature.ProviderResolutionDetail) {
	if !p.isReady() {
		return def, notReady()
	}
	fv, attr, ok := p.lookup(flag)
	if !ok {
		return def, notFound(flag)
	}
	if attr == "" {
		return def, mismatch(flag, "属性でなくフラグ本体を型付きで参照している(bool は BooleanEvaluation、全体は ObjectEvaluation)")
	}
	return attribute[T](fv, flag, attr, def)
}

// attribute は属性を型 T として取り出す。disabled のフラグの属性は既定値へ倒す
// (AppConfig 自身も disabled のフラグの属性を応答から落とす)。
func attribute[T comparable](fv flagValue, flag, attr string, def T) (T, openfeature.ProviderResolutionDetail) {
	if !fv.Enabled {
		return def, openfeature.ProviderResolutionDetail{Reason: openfeature.DisabledReason}
	}
	raw, ok := fv.Attributes[attr]
	if !ok {
		return def, notFound(flag)
	}
	v, ok := raw.(T)
	if !ok {
		return def, mismatch(flag, fmt.Sprintf("属性 %s は %T", attr, raw))
	}
	return v, openfeature.ProviderResolutionDetail{Reason: openfeature.StaticReason}
}

func notReady() openfeature.ProviderResolutionDetail {
	return openfeature.ProviderResolutionDetail{
		ResolutionError: openfeature.NewProviderNotReadyResolutionError("定義を一度も取得できていない"),
		Reason:          openfeature.ErrorReason,
	}
}

func notFound(flag string) openfeature.ProviderResolutionDetail {
	return openfeature.ProviderResolutionDetail{
		ResolutionError: openfeature.NewFlagNotFoundResolutionError(flag),
		Reason:          openfeature.ErrorReason,
	}
}

func mismatch(flag, msg string) openfeature.ProviderResolutionDetail {
	return openfeature.ProviderResolutionDetail{
		ResolutionError: openfeature.NewTypeMismatchResolutionError(flag + ": " + msg),
		Reason:          openfeature.ErrorReason,
	}
}
