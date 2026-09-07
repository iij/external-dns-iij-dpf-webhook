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

## テスト

```bash
go test ./...
```

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
