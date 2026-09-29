module github.com/rikukaInoue/greenfield/services/gear

go 1.26.0

require (
	github.com/aws/aws-sdk-go-v2/config v1.33.6
	github.com/aws/aws-sdk-go-v2/service/sqs v1.52.1
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/go-sql-driver/mysql v1.10.1
	github.com/golang-migrate/migrate/v4 v4.20.1
	github.com/rikukaInoue/greenfield/core v0.0.0
	github.com/rikukaInoue/greenfield/services/photo-client v0.0.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/aws/aws-sdk-go-v2 v1.47.1 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.20.6 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.10.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sns v1.47.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.38.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.1 // indirect
	github.com/aws/smithy-go v1.28.1 // indirect
	github.com/go-chi/chi/v5 v5.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
)

// モジュール間の依存はタグではなくreplaceで結ぶ（リポジトリ外から消費されるまでタグは打たない）。
// 他サービスへの依存は <name>-client のみ許可。実装モジュールをここに書いてはならない。
replace github.com/rikukaInoue/greenfield/core => ../../core

replace github.com/rikukaInoue/greenfield/services/photo-client => ../photo-client
