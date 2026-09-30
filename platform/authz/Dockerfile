# platform/authz のデプロイ用イメージ。ビルド条件は services/photo/Dockerfile と同じ:
# リポジトリルートをコンテキストに、GOWORK=off でこのモジュールだけを閉じて固める。
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY core/go.mod core/go.sum ./core/
COPY platform/authz/go.mod platform/authz/go.sum ./platform/authz/
COPY telemetry/go.mod telemetry/go.sum ./telemetry/
RUN cd platform/authz && GOWORK=off go mod download
COPY core ./core
COPY telemetry ./telemetry
COPY platform/authz ./platform/authz
RUN cd platform/authz && GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/authz ./cmd/authz

FROM alpine:3.22
RUN apk add --no-cache curl ca-certificates
# root で動かす理由がない(#213)。専用の非特権ユーザーで実行する
RUN adduser -D -H -u 65532 app
USER app
COPY --from=build /out/authz /usr/local/bin/authz
ENTRYPOINT ["/usr/local/bin/authz"]
