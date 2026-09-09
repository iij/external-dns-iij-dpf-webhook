# 開発手順

## 前提

- Go 1.27 以降
- `podman` または `docker` (イメージのビルドと ASLR 検証に必要)
- `golangci-lint`、`govulncheck`

## 非公開モジュールへの依存

`github.com/iij/dpf-go` は本プログラムの完成後に公開される予定であり、**それまでは
非公開**である。モジュールの取得に認証が必要になる。

```bash
export GOPRIVATE=github.com/iij/dpf-go
```

`GOPRIVATE` はプロキシと checksum データベースの両方を迂回させる。これを設定しないと、
公開プロキシ経由の取得を試みて失敗する。

取得には git の認証が要る。いずれかを設定する。

```bash
# gh CLI の認証情報を git に流用する
gh auth setup-git

# あるいは SSH 経由に書き換える
git config --global url."git@github.com:iij/".insteadOf "https://github.com/iij/"
```

`dpf-go` が公開されたら `GOPRIVATE` の設定は不要になる。`Makefile` の既定値と本節を
その時点で削除すること。

> **公開順序**: 本リポジトリを `dpf-go` より先に公開すると、外部からビルドできない
> 状態になる。`dpf-go` の公開を先に行うこと。

## 品質ゲート

constitution が CI ゲートとして要求する項目を、ローカルでも同じ内容で実行できる。

```bash
make all          # fmt-check → build → lint → vuln → test
```

個別に実行する場合:

```bash
make fmt-check    # gofmt -l の出力が空であること
make build
make lint         # golangci-lint run
make vuln         # govulncheck ./...
make test
```

整形は議論せずツールの出力を受け入れる。差分が出たら `make fmt` を適用する。

## イメージのビルドと ASLR 検証

```bash
make image
make verify-aslr
```

`verify-aslr` は、配布バイナリが **static PIE** であることを検証する。

- ELF Type が `DYN` であること (PIE であり ASLR が有効)
- `PT_INTERP` を持たないこと (静的であり `scratch` で起動できる)

この 2 つを同時に満たすビルド方法は限られる。詳細は
[research.md R1](../specs/001-webhook-provider/research.md) を参照。ビルドフラグを
変更する場合は、必ず `make verify-aslr` を通すこと。

## リリース

リリースを公開すると `.github/workflows/release.yml` が走り、次を行う。

1. 配布イメージをビルドし、`ghcr.io` へ公開する
2. **公開したイメージをダイジェスト指定で走査**して SBOM を生成する
3. SBOM の内容を検証する
4. SBOM と provenance を**イメージのアテステーションとして紐づける** (referrers)
5. SBOM ファイル自体にも署名する
6. SBOM をリリースページへ添付する

走査を「push 済みのイメージのダイジェスト」に対して行うのは、アテステーションの
対象と走査の対象を同一に固定するためである。手元のイメージを走査すると、
公開したものとは別の成果物の SBOM に署名しうる。

SBOM の対象はソースツリーではなく**配布されるコンテナイメージ**である。実際に
リンクされた依存を反映するのはイメージを走査した結果だからである
(constitution v1.10.0 が配布形態をコンテナイメージのみと定めている)。

手元で同じ内容を確かめられる。

```bash
make sbom     # イメージをビルドし、SBOM を生成して内容を検証する
```

`syft` が要る。CI と同じ版に固定すること。手元と CI で走査結果が食い違わないようにする。

```bash
go install github.com/anchore/syft/cmd/syft@v1.51.1
```

### 添付前の検証

`.github/scripts/verify_sbom.py` が、添付する前に SBOM の内容を確かめる。

- SPDX 形式であること
- パッケージが 50 件以上あること (走査対象の取り違えを検出する)
- 自身のモジュール、`github.com/miekg/dns`、`github.com/iij/dpf-go` を含むこと

空や壊れた SBOM をそのまま添付すると、供給網の情報が「あるのに使えない」状態で
配布される。そのため検証に失敗した場合は添付せず、ワークフローを失敗させる。

### アテステーション

**Artifact Attestations** で署名する。鍵の保管が要らず、GitHub の OIDC ID で
署名されるため、誰がどのワークフローで作ったものかを検証できる。

| 対象 | 述語 | 置き場所 |
|---|---|---|
| イメージ | SBOM | レジストリの referrer + GitHub |
| イメージ | provenance | レジストリの referrer + GitHub |
| `sbom.spdx.json` (リリース資産) | provenance | GitHub |

イメージのアテステーションは `push-to-registry: true` によりレジストリの
**referrer** として紐づく。referrers はレジストリ上の関連付けであり、手元の
イメージには付けられない。そのため push と同時に行う (constitution v1.10.0)。

リリース資産への署名を別に作るのは、対象が異なるためである。リリースページから
落とした `sbom.spdx.json` が本ワークフローの産物であることは、イメージの
アテステーションからは確かめられない。

### 検証

```bash
# イメージのアテステーション (SBOM と provenance)
gh attestation verify oci://ghcr.io/iij/external-dns-iij-dpf-webhook:v0.1.0 \
  --repo iij/external-dns-iij-dpf-webhook

# リリースへ添付された SBOM ファイル
gh attestation verify sbom.spdx.json --repo iij/external-dns-iij-dpf-webhook

# referrer として何が紐づいているか
cosign tree ghcr.io/iij/external-dns-iij-dpf-webhook:v0.1.0
```

### 必要なシークレットと権限

| 種別 | 名前 | 用途 | 公開後 |
|---|---|---|---|
| シークレット | `MODULE_TOKEN` | `github.com/iij/dpf-go` の取得。`GITHUB_TOKEN` は当該リポジトリにしか及ばないため使えない | 不要になる |
| シークレット | `DPF_TOKEN` | 実環境での検証 (`e2e`) で検証用ゾーンを操作する | 引き続き必要 |
| 変数 | `DPF_TEST_ZONE_NAME` | 検証用ゾーン名 | 引き続き必要 |

未設定でもワークフローは失敗せず警告を出す。`dpf-go` の公開後を見据えているため。
ただし公開前は、イメージのビルドが `go mod download` の段で失敗する。

権限は `GITHUB_TOKEN` で足りる。追加のシークレットは要らない。

| 権限 | 用途 |
|---|---|
| `contents: write` | リリースページへの添付 |
| `packages: write` | `ghcr.io` への push |
| `id-token: write` | Sigstore への署名 (OIDC) |
| `attestations: write` | アテステーションの作成 |

## テスト

```bash
go test ./...
```

### 実環境での検証 (e2e)

constitution v2.1.0 は、`main` へマージする前に実際の DPF に対してレコードの
追加・変更・削除を **CI で** 検証することを MUST とする。手元での確認では代えられない。

`.github/workflows/e2e.yml` が Pull Request のたびに実行する。

**このワークフローは並列に走らない。** 検証用ゾーンは共有される状態であり、
2 つの実行が同時にゾーンを読み・マージし・書き戻すと互いの変更を取り消し合う。
またゾーンの内容に対する表明が実行のタイミングに依存し、失敗が再現しなくなる。
`concurrency` で 1 度に 1 実行へ制限している。

手元で走らせる場合 (**破壊的操作を行う。検証用ゾーンでのみ**):

```bash
export DPF_E2E_TOKEN_FILE=/path/to/token
export DPF_E2E_ZONE=sub.example.jp.
go test ./test/e2e/... -v -count=1
```

環境変数が揃っていなければスキップする。誤って本番ゾーンへ向かうより、
検証が行われないことが明示される方が安全である。

テストは実際の DPF API に到達しない。`internal/dpf` は `dpf-go` のインタフェースを、
`internal/provider` は `internal/provider/ports.go` のインタフェースを差し替えて検証する。

実 API を用いた確認手順は
[quickstart.md](../specs/001-webhook-provider/quickstart.md) にまとめてある。
**破壊的操作を含むため、検証用ゾーンでのみ実行すること。**

## 設計文書

実装の判断根拠は `specs/001-webhook-provider/` にある。

| 文書 | 内容 |
|---|---|
| [spec.md](../specs/001-webhook-provider/spec.md) | 要件、成功基準、利用前提条件 |
| [plan.md](../specs/001-webhook-provider/plan.md) | 技術的な文脈と構造 |
| [research.md](../specs/001-webhook-provider/research.md) | 技術判断とその理由、却下した代替案 |
| [data-model.md](../specs/001-webhook-provider/data-model.md) | 実体と変換規則、状態遷移 |
| [contracts/](../specs/001-webhook-provider/contracts/) | 外部契約と内部境界の契約 |

守るべき原則は [`.specify/memory/constitution.md`](../.specify/memory/constitution.md)
にある。実装手段ではなく「守るべき結果」だけが書かれている。
