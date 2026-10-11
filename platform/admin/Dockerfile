# platform/admin のデプロイ用イメージ。ビルド条件は services/photo/Dockerfile と同じ:
# リポジトリルートをコンテキストに、GOWORK=off でこのモジュールだけを閉じて固める。
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY core/go.mod core/go.sum ./core/
COPY services/photo-client/go.mod services/photo-client/go.sum ./services/photo-client/
COPY platform/admin/go.mod platform/admin/go.sum ./platform/admin/
RUN cd platform/admin && GOWORK=off go mod download
COPY core ./core
COPY services/photo-client ./services/photo-client
COPY platform/admin ./platform/admin
RUN cd platform/admin && GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/admin ./cmd/admin

FROM alpine:3.22
RUN apk add --no-cache curl ca-certificates
# root で動かす理由がない(#213)。専用の非特権ユーザーで実行する
RUN adduser -D -H -u 65532 app
USER app
COPY --from=build /out/admin /usr/local/bin/admin
ENTRYPOINT ["/usr/local/bin/admin"]
