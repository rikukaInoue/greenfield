# モジュールは go.work の use から拾う。CIはワークスペースを切って（GOWORK=off）
# 各モジュールを単体でビルドし、go.mod の自己完結性＝モジュール境界を毎回検証する
# （ワークスペースモードでは use 内の全モジュールが require なしでimportできてしまうため）。
MODULES := $(shell awk '/^\t\.\//{print $$1}' go.work)

.PHONY: build-ws build test vet fmt tidy run-allinone

build-ws: ## ワークスペースモードで全モジュールをビルド（日常用）
	go build $(MODULES:%=%/...)

build: ## 各モジュールを GOWORK=off で単体ビルド（CI相当）
	@for m in $(MODULES); do echo "== $$m"; (cd $$m && GOWORK=off go build ./...) || exit 1; done

test:
	@for m in $(MODULES); do echo "== $$m"; (cd $$m && GOWORK=off go test ./...) || exit 1; done

vet:
	@for m in $(MODULES); do echo "== $$m"; (cd $$m && GOWORK=off go vet ./...) || exit 1; done

fmt:
	@test -z "$$(gofmt -l $(MODULES))" || (gofmt -l $(MODULES); exit 1)

tidy:
	@for m in $(MODULES); do (cd $$m && GOWORK=off go mod tidy) || exit 1; done

run-allinone: ## 全サービスを1プロセスで起動（Tier 1）
	go run ./dev/allinone
