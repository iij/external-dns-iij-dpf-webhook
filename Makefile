# constitution v1.8.0 が CI ゲートとして要求する項目を、ローカルでも同じ内容で実行する。
#
# github.com/iij/dpf-go は公開されるまで非公開のため、モジュール取得に認証が要る。
# 詳細は docs/development.md を参照。
export GOPRIVATE ?= github.com/iij/dpf-go

IMAGE ?= external-dns-iij-dpf-webhook

# リリースへ添付する SBOM。.github/workflows/release.yml と同じ内容を
# ローカルでも再現できるようにしておく。
SBOM_FILE ?= sbom.spdx.json
# CI と同じ版に固定する。走査結果が手元と CI で食い違わないようにするため。
SYFT_VERSION ?= v1.51.1
# REGISTRY_IMAGE はレジストリへ push する際の完全な参照。
# SBOM と provenance は referrers としてレジストリに紐づくため、push が前提になる。
REGISTRY_IMAGE ?= $(IMAGE)
CONTAINER_TOOL ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
# syft に渡す走査元の指定 (podman / docker)。CONTAINER_TOOL は絶対パスに
# なるため、スキーマ名としては使えない。
CONTAINER_SCHEME ?= $(notdir $(CONTAINER_TOOL))

# OCI アノテーションに埋める来歴情報。
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
CREATED ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

BUILD_ARGS = --build-arg VERSION=$(VERSION) \
             --build-arg REVISION=$(REVISION) \
             --build-arg CREATED=$(CREATED)

.PHONY: all
all: fmt-check license-check build lint vuln test

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

## license-check: 全 Go ファイルに SPDX ヘッダがあることを検証する
## constitution v1.9.0: すべての Go ファイルの先頭に SPDX を記載すること (MUST)
.PHONY: license-check
license-check:
	@missing=""; \
	for f in $$(git ls-files '*.go'; git ls-files --others --exclude-standard '*.go'); do \
		head -1 "$$f" | grep -q 'SPDX-License-Identifier: Apache-2.0' || missing="$$missing $$f"; \
	done; \
	if [ -n "$$missing" ]; then \
		echo "SPDX ヘッダがないファイル:"; \
		for f in $$missing; do echo "  $$f"; done; \
		echo "各ファイルの先頭に '// SPDX-License-Identifier: Apache-2.0' を追加してください"; \
		exit 1; \
	fi

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
		$(BUILD_ARGS) \
		--secret id=gh_token,src=$(GH_TOKEN_FILE) .

## image-push: SBOM と provenance を referrers として付けてレジストリへ push する
##
## constitution v1.10.0: 配布するイメージに SBOM と provenance を referrers として
## 紐づけること (MUST)。
##
## referrers はレジストリ上の関連付けであり、ローカルのイメージには付けられない。
## そのため push と同時に行う。docker buildx が要る (podman build には
## --attest 相当がない)。
##
##   make image-push REGISTRY_IMAGE=ghcr.io/iij/external-dns-iij-dpf-webhook:v0.1.0
.PHONY: image-push
image-push:
	@if [ "$(REGISTRY_IMAGE)" = "$(IMAGE)" ]; then \
		echo "REGISTRY_IMAGE にレジストリを含む完全な参照を指定してください"; \
		echo "  例: make image-push REGISTRY_IMAGE=ghcr.io/iij/$(IMAGE):v0.1.0"; \
		exit 1; \
	fi
	@if command -v gh >/dev/null 2>&1; then \
		gh auth token > $(GH_TOKEN_FILE) 2>/dev/null || : > $(GH_TOKEN_FILE); \
	else \
		: > $(GH_TOKEN_FILE); \
	fi; \
	trap 'rm -f $(GH_TOKEN_FILE)' EXIT; \
	docker buildx build -t $(REGISTRY_IMAGE) -f build/Containerfile \
		$(BUILD_ARGS) \
		--secret id=gh_token,src=$(GH_TOKEN_FILE) \
		--attest=type=sbom \
		--attest=type=provenance,mode=max \
		--push .

## image-sign: push 済みイメージに cosign で署名する
##
## 鍵なし署名 (keyless) を既定とする。CI の OIDC ID で署名するため、
## 鍵の保管と失効の管理が要らない。
##
##   make image-sign REGISTRY_IMAGE=ghcr.io/iij/external-dns-iij-dpf-webhook:v0.1.0
.PHONY: image-sign
image-sign:
	@command -v cosign >/dev/null 2>&1 || { \
		echo "cosign が見つかりません: https://docs.sigstore.dev/cosign/installation/"; \
		exit 1; \
	}
	cosign sign --yes $(REGISTRY_IMAGE)

## image-verify: 署名と attestation を検証する
.PHONY: image-verify
image-verify:
	@command -v cosign >/dev/null 2>&1 || { echo "cosign が見つかりません"; exit 1; }
	cosign verify $(REGISTRY_IMAGE) \
		--certificate-identity-regexp='.*' \
		--certificate-oidc-issuer-regexp='.*'
	cosign tree $(REGISTRY_IMAGE)

## sbom: 配布イメージの SBOM を SPDX JSON で生成し、内容を検証する
##
## 対象はソースツリーではなく配布されるイメージである。実際にリンクされた
## 依存を反映するのはイメージを走査した結果だからである。
##
## リリース時の自動生成と添付は .github/workflows/release.yml が行う。
## 本ターゲットはその内容を手元で確かめるためのもの。
.PHONY: sbom
sbom: image
	@command -v syft >/dev/null 2>&1 || { \
		echo "syft が見つかりません:"; \
		echo "  go install github.com/anchore/syft/cmd/syft@$(SYFT_VERSION)"; \
		exit 1; \
	}
	syft scan $(CONTAINER_SCHEME):localhost/$(IMAGE):latest \
		-o spdx-json=$(SBOM_FILE) -q
	python3 .github/scripts/verify_sbom.py $(SBOM_FILE)

## verify-licenses: イメージにライセンス本文と OCI アノテーションがあることを検証する
## constitution v1.10.0
.PHONY: verify-licenses
verify-licenses: image
	@cid=$$($(CONTAINER_TOOL) create $(IMAGE)); \
	trap "$(CONTAINER_TOOL) rm $$cid >/dev/null" EXIT; \
	tmp=$$(mktemp -d); \
	$(CONTAINER_TOOL) cp "$$cid:/licenses" "$$tmp/licenses" >/dev/null 2>&1 || { \
		echo "NG: /licenses がイメージに存在しません"; exit 1; \
	}; \
	for f in LICENSE NOTICE; do \
		[ -f "$$tmp/licenses/$$f" ] || { echo "NG: /licenses/$$f がありません"; exit 1; }; \
	done; \
	n=$$(find "$$tmp/licenses/third-party" -type f 2>/dev/null | wc -l); \
	[ "$$n" -gt 0 ] || { echo "NG: /licenses/third-party が空です"; exit 1; }; \
	echo "OK: /licenses/ (LICENSE, NOTICE, third-party $$n 件)"; \
	rm -rf "$$tmp"
	@lic=$$($(CONTAINER_TOOL) inspect --format '{{ index .Config.Labels "org.opencontainers.image.licenses" }}' $(IMAGE)); \
	if [ "$$lic" != "Apache-2.0" ]; then \
		echo "NG: org.opencontainers.image.licenses = '$$lic' (want Apache-2.0)"; exit 1; \
	fi; \
	echo "OK: org.opencontainers.image.licenses=$$lic"

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
