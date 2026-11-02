module github.com/rikukaInoue/greenfield/services/photo

go 1.26.1

require (
	github.com/XSAM/otelsql v0.44.0
	github.com/aws/aws-sdk-go-v2 v1.47.5
	github.com/aws/aws-sdk-go-v2/config v1.33.12
	github.com/aws/aws-sdk-go-v2/credentials v1.20.12
	github.com/aws/aws-sdk-go-v2/service/appconfigdata v1.32.6
	github.com/aws/aws-sdk-go-v2/service/s3 v1.114.6
	github.com/aws/aws-sdk-go-v2/service/sns v1.47.7
	github.com/aws/smithy-go v1.28.7
	github.com/danielgtaylor/huma/v2 v2.40.0
	github.com/go-sql-driver/mysql v1.10.1
	github.com/golang-migrate/migrate/v4 v4.20.1
	github.com/open-feature/go-sdk v1.20.0
	github.com/open-feature/go-sdk-contrib/providers/flagd v0.8.0
	github.com/rikukaInoue/greenfield/core v0.0.0
	github.com/rikukaInoue/greenfield/eventbus/snssqs v0.0.0
	github.com/rikukaInoue/greenfield/flagprovider/appconfig v0.0.0-00010101000000-000000000000
	github.com/rikukaInoue/greenfield/telemetry v0.0.0-00010101000000-000000000000
)

require (
	connectrpc.com/connect/v2 v2.0.0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.6 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.31.0 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.72.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.71.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.1 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260928230214-8a89bd6388cc // indirect
)

require (
	buf.build/gen/go/open-feature/flagd/connectrpc/go v1.21.0-20260824202443-4824facda674.1 // indirect
	buf.build/gen/go/open-feature/flagd/grpc/go v1.6.2-20260824202443-4824facda674.1 // indirect
	buf.build/gen/go/open-feature/flagd/protocolbuffers/go v1.36.12-20260824202443-4824facda674.2 // indirect
	connectrpc.com/connect v1.21.0 // indirect
	connectrpc.com/otelconnect v0.12.0 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.25 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.5 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.12.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sqs v1.52.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.39.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.44.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.6 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/diegoholiveira/jsonlogic/v3 v3.9.1 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-chi/chi/v5 v5.5.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-memdb v1.3.5 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/open-feature/flagd-schemas v0.2.13 // indirect
	github.com/open-feature/flagd/core v0.19.0 // indirect
	github.com/prometheus/client_golang v1.24.1 // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.72.0 // indirect
	github.com/prometheus/procfs v0.22.0 // indirect
	github.com/rikukaInoue/greenfield/services/gear-client v0.0.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/twmb/murmur3 v1.1.8 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0
	go.uber.org/mock v0.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/exp v0.0.0-20260908205506-85c1c2202aba // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// モジュール間の依存はタグではなくreplaceで結ぶ（リポジトリ外から消費されるまでタグは打たない）。
// 他サービスへの依存は <name>-client のみ許可。実装モジュールをここに書いてはならない。
replace github.com/rikukaInoue/greenfield/core => ../../core

replace github.com/rikukaInoue/greenfield/flagprovider/appconfig => ../../flagprovider/appconfig

replace github.com/rikukaInoue/greenfield/telemetry => ../../telemetry

replace github.com/rikukaInoue/greenfield/services/gear-client => ../gear-client

replace github.com/rikukaInoue/greenfield/eventbus/snssqs => ../../eventbus/snssqs
