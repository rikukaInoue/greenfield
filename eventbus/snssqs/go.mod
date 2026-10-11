module github.com/rikukaInoue/greenfield/eventbus/snssqs

go 1.26.1

require (
	github.com/aws/aws-sdk-go-v2 v1.47.1
	github.com/aws/aws-sdk-go-v2/service/sns v1.47.2
	github.com/aws/aws-sdk-go-v2/service/sqs v1.52.1
	github.com/rikukaInoue/greenfield/core v0.0.0
)

require (
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4 // indirect
	github.com/aws/smithy-go v1.28.2 // indirect
)

replace github.com/rikukaInoue/greenfield/core => ../../core
