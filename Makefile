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

# dpf-go が非公開である間、イメージのビルドにモジュール取得用の資格情報が要る。
# gh CLI があればそのトークンを使う。公開後はこの一式ごと不要になる。
# := で即時評価する。?= だと参照のたびに mktemp が走り、別の名前になる。
GH_TOKEN_FILE := $(shell mktemp -u)

## image: scratch イメージをビルドする
.PHONY: image
image:
	@if command -v gh >/dev/null 2>&1; then \
		gh auth token > $(GH_TOKEN_FILE) 2>/dev/null || : > $(GH_TOKEN_FILE); \
	else \
		: > $(GH_TOKEN_FILE); \
	fi; \
	trap 'rm -f $(GH_TOKEN_FILE)' EXIT; \
	$(CONTAINER_TOOL) build -t $(IMAGE) -f build/Containerfile \
		--secret id=gh_token,src=$(GH_TOKEN_FILE) .

## verify-aslr: 配布バイナリが ASLR 有効 (PIE) かつ静的であることを検証する
## readelf の出力はロケールで翻訳されるため LC_ALL=C で固定する
## constitution v1.6.0: ASLR が有効であることを CI で検証すること (MUST)
## research R1: CGO_ENABLED=0 -buildmode=pie は PT_INTERP を持ち scratch で起動しない
.PHONY: verify-aslr
verify-aslr: image
	@bin=$$(mktemp); \
	cid=$$($(CONTAINER_TOOL) create $(IMAGE)); \
	$(CONTAINER_TOOL) cp "$$cid:/webhook" "$$bin" >/dev/null; \
	$(CONTAINER_TOOL) rm "$$cid" >/dev/null; \
	type=$$(LC_ALL=C readelf -h "$$bin" | awk '/^ *Type:/ {print $$2}'); \
	if [ "$$type" != "DYN" ]; then \
		echo "NG: ELF Type が $$type です (PIE ではないため ASLR が無効)"; rm -f "$$bin"; exit 1; \
	fi; \
	if LC_ALL=C readelf -l "$$bin" | grep -q INTERP; then \
		echo "NG: PT_INTERP を持つため scratch では起動しません"; rm -f "$$bin"; exit 1; \
	fi; \
	echo "OK: static PIE (ELF Type=DYN, PT_INTERP なし)"; \
	rm -f "$$bin"
