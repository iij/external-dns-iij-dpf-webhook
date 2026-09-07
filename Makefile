# constitution v1.8.0 が CI ゲートとして要求する項目を、ローカルでも同じ内容で実行する。
#
# github.com/iij/dpf-go は公開されるまで非公開のため、モジュール取得に認証が要る。
# 詳細は docs/development.md を参照。
export GOPRIVATE ?= github.com/iij/dpf-go

IMAGE ?= external-dns-iij-dpf-webhook
CONTAINER_TOOL ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)

.PHONY: all
all: fmt-check build lint vuln test

## fmt-check: 整形されていないファイルがあれば失敗する (gofmt -l の出力が空であること)
.PHONY: fmt-check
fmt-check:
	@out="$$(gofmt -l . 2>&1)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt が必要なファイル:"; echo "$$out"; \
		echo "'make fmt' を実行してください"; \
		exit 1; \
	fi

## fmt: gofmt を適用する
.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: build
build:
	go build ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: vuln
vuln:
	govulncheck ./...

.PHONY: test
test:
	go test ./...

## image: scratch イメージをビルドする
.PHONY: image
image:
	$(CONTAINER_TOOL) build -t $(IMAGE) -f build/Containerfile .

## verify-aslr: 配布バイナリが ASLR 有効 (PIE) かつ静的であることを検証する
## constitution v1.6.0: ASLR が有効であることを CI で検証すること (MUST)
## research R1: CGO_ENABLED=0 -buildmode=pie は PT_INTERP を持ち scratch で起動しない
.PHONY: verify-aslr
verify-aslr: image
	@bin=$$(mktemp); \
	cid=$$($(CONTAINER_TOOL) create $(IMAGE)); \
	$(CONTAINER_TOOL) cp "$$cid:/webhook" "$$bin" >/dev/null; \
	$(CONTAINER_TOOL) rm "$$cid" >/dev/null; \
	type=$$(readelf -h "$$bin" | awk '/^ *Type:/ {print $$2}'); \
	if [ "$$type" != "DYN" ]; then \
		echo "NG: ELF Type が $$type です (PIE ではないため ASLR が無効)"; rm -f "$$bin"; exit 1; \
	fi; \
	if readelf -l "$$bin" | grep -q INTERP; then \
		echo "NG: PT_INTERP を持つため scratch では起動しません"; rm -f "$$bin"; exit 1; \
	fi; \
	echo "OK: static PIE (ELF Type=DYN, PT_INTERP なし)"; \
	rm -f "$$bin"
