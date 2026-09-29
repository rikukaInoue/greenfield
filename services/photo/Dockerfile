# photo サービスのデプロイ用イメージ。
#
# モジュール境界を跨がないよう、ビルドは **リポジトリルート**をコンテキストに取り、
# GOWORK=off でこのモジュールだけを閉じて固める（CI の go ジョブと同じ検証条件）。
# ワークスペースを効かせると require の無い import が通ってしまい、
# 「モノレポだから動く」イメージができる（internal-01 の境界が崩れる）。
FROM golang:1.26-alpine AS build
WORKDIR /src
# 依存の解決だけ先に。go.sum が変わらない限りこの層は再利用される
COPY core/go.mod core/go.sum ./core/
COPY services/photo/go.mod services/photo/go.sum ./services/photo/
COPY telemetry/go.mod telemetry/go.sum ./telemetry/
COPY services/photo-client/go.mod ./services/photo-client/
RUN cd services/photo && GOWORK=off go mod download
COPY core ./core
COPY telemetry ./telemetry
COPY services/photo ./services/photo
COPY services/photo-client ./services/photo-client
RUN cd services/photo && GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/photo ./cmd/photo

FROM alpine:3.22
# healthcheck が curl を使う（compose の healthcheck からも同じものを呼ぶ）
RUN apk add --no-cache curl ca-certificates
COPY --from=build /out/photo /usr/local/bin/photo
# 引数なしで3リスナーを起動する。migrate は同じイメージに引数を渡して別ジョブが実行する
ENTRYPOINT ["/usr/local/bin/photo"]

