// Package flagsource はフラグ定義の取得元を組み立て、OpenFeature へ登録する。
//
// 評価はアプリのプロセス内で行う（定義を同期してローカル評価する）。
// AWS AppConfig や GrowthBook と同じ形であり、本番へ移るときに形が変わらない（docs/adr/0013）。
//
// プロバイダの実装は grpc / connect を持ち込むため core には置かない（blobstore と同じ扱い）。
// core/flags は評価とミドルウェアだけを持ち、OpenFeature SDK にしか依存しない。
package flagsource

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/appconfigdata"
	flagd "github.com/open-feature/go-sdk-contrib/providers/flagd/pkg"
	"github.com/open-feature/go-sdk/openfeature"

	"github.com/rikukaInoue/greenfield/flagprovider/appconfig"
)

// flagd のログはアプリの slog に寄せられない（flagd v0.7.0、#142 で実測）。
//
// `flagd.WithLogger(logr.Logger)` は**rpc リゾルバにしか渡らない**。我々が使う
// in-process リゾルバ（ADR 0013: 定義を同期してプロセス内で評価する）は
// `NewInProcessService` が無条件に自前の zap ロガーを作り、`process.Configuration` に
// 渡す口が無い。
//
//	provider.go:69     rpc      -> providerConfiguration.log を渡している
//	provider.go:72-92  inProcess-> Configuration に log フィールドが無い
//	in_process/service.go:145   -> log := logger.NewLogger(NewRaw(), false)
//	in_process/zap.go:41        -> sink は os.Stderr 固定、レベルは Info 固定
//
// 結果として flagd は `{"level":"info","ts":...}`（zap の既定フィールド名）を **stderr** へ、
// アプリは `{"time":"...Z","level":"INFO",...}`（service.name 付き）を **stdout** へ出す。
// **1形式に揃えるという方針の唯一の例外**であり、上流が in-process 側にロガーを通すまで直せない。
//
// `WithLogger` を渡す配線は**あえて入れていない**。我々の経路では no-op で、
// 「書いてあるが効いていない」コードを増やすだけになるため。

// Kind は定義の取得元。
type Kind string

const (
	// Sync は flagd の flag sync service から定義を同期する。ローカル開発の既定。
	Sync Kind = "sync"
	// File はファイルを直接読む。flagd を立てずに済むので CI とテストで使う。
	File Kind = "file"
	// AppConfig は AWS AppConfig から定義を同期する(docs/adr/0011)。AWS 上の既定。
	AppConfig Kind = "appconfig"
)

// Config は取得元の設定。
type Config struct {
	Kind Kind
	// Host / Port は Kind が Sync のときの flagd の sync service。
	Host string
	Port int
	// Path は Kind が File のときの定義ファイル。
	Path string
	// Application / Environment / Profile は Kind が AppConfig のときの識別子。
	Application string
	Environment string
	Profile     string
	// PollInterval は Kind が AppConfig のときの同期間隔(AppConfig の下限 15s)。
	PollInterval time.Duration
}

// ConfigFromEnv は環境変数から設定を読む。既定は flagd の sync service。
func ConfigFromEnv() Config {
	return Config{
		Kind:         Kind(envOr("FLAGS_SOURCE", string(Sync))),
		Host:         envOr("FLAGD_HOST", "localhost"),
		Port:         envIntOr("FLAGD_PORT", 8015),
		Path:         os.Getenv("FLAGS_FILE"),
		Application:  os.Getenv("APPCONFIG_APPLICATION"),
		Environment:  os.Getenv("APPCONFIG_ENVIRONMENT"),
		Profile:      os.Getenv("APPCONFIG_PROFILE"),
		PollInterval: time.Duration(envIntOr("FLAGS_POLL_SECONDS", 20)) * time.Second,
	}
}

// Register は domain 付きでプロバイダを登録する。
// ドメイン付きにするのは、1プロセスに複数サービスが載る場合（dev/allinone）に
// 互いのプロバイダを上書きしないため。
func Register(ctx context.Context, domain string, cfg Config) error {
	provider, err := newProvider(cfg)
	if err != nil {
		return err
	}
	return openfeature.SetNamedProviderWithContextAndWait(ctx, domain, provider)
}

func newProvider(cfg Config) (openfeature.FeatureProvider, error) {
	switch cfg.Kind {
	case AppConfig:
		awsCfg, err := awsconfig.LoadDefaultConfig(context.Background())
		if err != nil {
			return nil, fmt.Errorf("flagsource: AWS 設定: %w", err)
		}
		return appconfig.New(appconfig.Config{
			Application:  cfg.Application,
			Environment:  cfg.Environment,
			Profile:      cfg.Profile,
			PollInterval: cfg.PollInterval,
			Client:       appconfigdata.NewFromConfig(awsCfg),
		})
	case File:
		if cfg.Path == "" {
			return nil, fmt.Errorf("flagsource: FLAGS_SOURCE=file には FLAGS_FILE が必要")
		}
		return flagd.NewProvider(flagd.WithFileResolver(), flagd.WithOfflineFilePath(cfg.Path))
	case Sync, "":
		return flagd.NewProvider(
			flagd.WithInProcessResolver(),
			flagd.WithHost(cfg.Host),
			flagd.WithPort(uint16(cfg.Port)),
		)
	default:
		return nil, fmt.Errorf("flagsource: 未知の取得元 %q（sync か file）", cfg.Kind)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
