# constitution v1.8.0 が CI ゲートとして要求する項目を、ローカルでも同じ内容で実行する。
#
# github.com/iij/dpf-go は公開されるまで非公開のため、モジュール取得に認証が要る。
# 詳細は docs/development.md を参照。
export GOPRIVATE ?= github.com/iij/dpf-go

IMAGE ?= external-dns-iij-dpf-webhook

# リリースへ添付する SBOM。.github/workflows/release.yml と同じ内容を
# ローカルでも再現できるようにしておく。
SBOM_FILE ?= sbom.spdx.json
# ツールのバージョンはここで一元管理する。
#
# constitution v1.3.0: CI で用いる Go、golangci-lint、govulncheck のバージョンを
# 固定すること (MUST)。ツールの暗黙のバージョン更新によって、同一コードの
# 判定結果が変わる状態にしないこと (MUST NOT)。
#
# .github/workflows/ci.yml は同じ値を参照する。片方だけ更新しないこと。
SYFT_VERSION ?= v1.51.1
GOLANGCI_LINT_VERSION ?= v2.13.2
GOVULNCHECK_VERSION ?= v1.7.0
GO_LICENSES_VERSION ?= latest
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
all: fmt-check license-check license-deps build lint vuln test

## tools: 固定したバージョンの開発ツールを導入する
##
## CI と同じ版を入れる。手元と CI で判定結果が食い違わないようにするため。
.PHONY: tools
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install github.com/google/go-licenses/v2@$(GO_LICENSES_VERSION)
	go install github.com/anchore/syft/cmd/syft@$(SYFT_VERSION)

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

## license-deps: 依存モジュールのライセンスが許容範囲に収まるかを検証する
## constitution v1.11.0
##
## 本サービスは静的リンクしたバイナリを配布する。GPL / AGPL / LGPL の依存が
## 入ると、配布物全体にそれらの条件が及び、Apache-2.0 として配布できなくなる。
## 混入は依存の依存として静かに起きるため、機械的に検査する。
.PHONY: license-deps
license-deps:
	@command -v go-licenses >/dev/null 2>&1 || { \
		echo "go-licenses が見つかりません。'make tools' を実行してください"; \
		exit 1; \
	}
	@go-licenses check ./cmd/webhook \
		--disallowed_types=forbidden,restricted 2>/dev/null \
		&& echo "OK: 禁止ライセンスの依存なし" \
		|| { echo "NG: 許容範囲外のライセンスを持つ依存があります"; exit 1; }

## license-report: 依存モジュールのライセンス内訳を表示する
.PHONY: license-report
license-report:
	@go-licenses report ./cmd/webhook 2>/dev/null \
		| awk -F, '{print $$3}' | sort | uniq -c | sort -rn

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
		echo "syft が見つかりません。'make tools' を実行してください"; \
		exit 1; \
	}
	syft scan $(CONTAINER_SCHEME):localhost/$(IMAGE):latest \
		-o spdx-json=$(SBOM_FILE) -q
	python3 .github/scripts/verify_sbom.py $(SBOM_FILE)

## verify-defaults: 既定設定が拒否側であることをイメージ上で検証する
## constitution v1.2.0 (原則 VI) / 開発ワークフローと品質ゲート
##
## 単体テストでも同じ性質を検証しているが、ここでは配布される成果物そのものが
## その性質を持つことを確かめる。ビルドや設定の受け渡しで崩れうるため。
.PHONY: verify-defaults
verify-defaults: image
	@tmp=$$(mktemp -d); chmod 755 "$$tmp"; \
	echo dummy-token > "$$tmp/token"; chmod 644 "$$tmp/token"; \
	trap "rm -rf $$tmp" EXIT; \
	\
	echo "--- 必須設定なしでは起動しない (FR-017) ---"; \
	if $(CONTAINER_TOOL) run --rm $(IMAGE) >/dev/null 2>&1; then \
		echo "NG: トークン供給元なしで起動に成功した"; exit 1; \
	fi; \
	echo "OK: 異常終了した"; \
	\
	echo "--- 解釈できない設定では起動しない (FR-018) ---"; \
	if $(CONTAINER_TOOL) run --rm -v "$$tmp:/secrets:Z" $(IMAGE) \
		--dpf-token-file /secrets/token --domain-filter 'not..a..name' >/dev/null 2>&1; then \
		echo "NG: 妥当でない domain-filter で起動に成功した"; exit 1; \
	fi; \
	echo "OK: 異常終了した"; \
	\
	echo "--- トークンを引数で渡す経路がない (constitution v1.8.0) ---"; \
	if $(CONTAINER_TOOL) run --rm $(IMAGE) --dpf-token secret >/dev/null 2>&1; then \
		echo "NG: --dpf-token が受け付けられた"; exit 1; \
	fi; \
	echo "OK: 拒否された"; \
	\
	echo "--- domain-filter 未設定なら管理対象が空 (FR-002) ---"; \
	cid=$$($(CONTAINER_TOOL) run -d --rm -v "$$tmp:/secrets:Z" -p 18899:8888 $(IMAGE) \
		--dpf-token-file /secrets/token --provider-addr 0.0.0.0:8888); \
	sleep 3; \
	body=$$(curl -s -H 'Accept: application/external.dns.webhook+json;version=1' \
		http://127.0.0.1:18899/ || echo ""); \
	$(CONTAINER_TOOL) stop "$$cid" >/dev/null 2>&1 || true; \
	case "$$body" in \
		*'"filters":[]'*) echo "OK: 空の範囲が返った" ;; \
		*) echo "NG: 応答が空の範囲ではない: $$body"; exit 1 ;; \
	esac; \
	\
	echo "--- provider ポートは既定でループバックのみ (原則 VI) ---"; \
	cid=$$($(CONTAINER_TOOL) run -d --rm -v "$$tmp:/secrets:Z" -p 18898:8888 $(IMAGE) \
		--dpf-token-file /secrets/token); \
	sleep 3; \
	reachable=0; \
	curl -s -m 2 -o /dev/null http://127.0.0.1:18898/ && reachable=1; \
	$(CONTAINER_TOOL) stop "$$cid" >/dev/null 2>&1 || true; \
	if [ "$$reachable" -eq 1 ]; then \
		echo "NG: 既定で Pod 外から provider ポートへ到達できた"; exit 1; \
	fi; \
	echo "OK: 到達しなかった"

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
