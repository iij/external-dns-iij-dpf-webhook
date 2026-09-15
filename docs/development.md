# 開発手順

## 前提

- Go 1.27 以降
- `podman` または `docker` (イメージのビルドと ASLR 検証に必要)
- `golangci-lint`、`govulncheck`、`actionlint`

固定した版は `make tools` で一括導入できる。CI と同じ版が入る。

## 品質ゲート

constitution が CI ゲートとして要求する項目を、ローカルでも同じ内容で実行できる。

```bash
make all          # fmt-check → build → lint → workflow-lint → vuln → test
```

個別に実行する場合:

```bash
make fmt-check    # gofmt -l の出力が空であること
make build
make lint         # golangci-lint run
make workflow-lint # actionlint (ワークフローの静的検査)
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

| 種別 | 名前 | 用途 |
|---|---|---|
| シークレット | `DPF_TOKEN` | 実環境での検証 (`e2e`) で検証用ゾーンを操作する |
| 変数 | `DPF_TEST_ZONE_NAME` | 検証用ゾーン名 |

未設定でも `e2e` は失敗せず警告を出す。ただしその場合、実環境での検証は
**行われていない**。「テストが緑」と「検証が行われた」は別のことである。

依存モジュールはすべて公開プロキシから取得できるため、ビルドに資格情報は要らない。

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

### 検証の内訳

| ファイル | 対象 | 実行するジョブ |
|---|---|---|
| `e2e_test.go` | 追加・変更・削除・再適用、管理対象外の保全 | `e2e` |
| `quickstart_test.go` | webhook 契約の 4 経路、名前の表現、TXT の往復、形式違反の拒否、範囲外の除外、ロックの範囲、probe と計測値 | `e2e` |
| `token_test.go` | トークンのローテーションと失効 (SC-009) | `e2e` |
| `scale_test.go` | 1,000 件規模での取得と適用 (SC-008) | `scale` |
| `sidecar_test.go` | 反映の待ち合わせと後始末 (SC-001) | `e2e-sidecar` |

規模とサイドカーの検証は既定では走らない。追加の環境変数が要る。

```bash
# 1,000 件規模 (検証用ゾーンを大きく書き換える)
DPF_E2E_SCALE=1 go test ./test/e2e/... -run TestScale -v -count=1 -timeout=40m
```

| 環境変数 | 既定 | 意味 |
|---|---|---|
| `DPF_E2E_SCALE` | (未設定) | `1` のとき規模の検証を実行する |
| `DPF_E2E_SCALE_RECORDS` | `1000` | 作成する件数 |
| `DPF_E2E_READ_BUDGET` | `5s` | `GET /records` に許す時間。ExternalDNS の `--webhook-provider-read-timeout` の既定 |
| `DPF_E2E_WAIT_NAME` | (未設定) | 反映を待ち合わせる名前。サイドカー検証から渡される |
| `DPF_E2E_WAIT_STATE` | `present` | `present` または `absent` |
| `DPF_E2E_WAIT_BUDGET` | `5m` | 待つ時間。SC-001 の上限 |
| `DPF_E2E_DELETE_PREFIX` | (未設定) | 残留レコードの削除に使う接頭辞。後始末から渡される |

サイドカー構成の検証は kind クラスタと上流チャートを要するため、手元では
`.github/workflows/e2e-sidecar.yml` の手順を追う形になる。ワークフローは
README に載せた推奨 values をそのまま使う。**README を直したらワークフロー側も
直すこと。** 食い違うと、README の値が動かないまま気付けない。

テストは実際の DPF API に到達しない。`internal/dpf` は `dpf-go` のインタフェースを、
`internal/provider` は `internal/provider/ports.go` のインタフェースを差し替えて検証する。

実 API を用いた確認手順は
[quickstart.md](../specs/001-webhook-provider/quickstart.md) にまとめてある。
**破壊的操作を含むため、検証用ゾーンでのみ実行すること。**

## 依存の更新

依存パッケージの更新は Dependabot が検出し、更新 Pull Request を自動で作る。
設定は [`.github/dependabot.yml`](../.github/dependabot.yml)。**マージは自動化
しない。** 人のレビューと承認を経る。

### 更新 Pull Request を取り込む手順

Dependabot 起点の実行は Actions secrets を参照できない。**これは設定の不足では
なく、意図した状態である。** 更新後の依存コードと検証用ゾーンのトークンを同じ
実行に同居させないためである。汚染された版のコードがトークンを読み取れる状態に
しては、待機期間を置く意味が薄れる。

そのため、実際の DPF に対する検証 (`e2e` / `e2e-sidecar`) は Dependabot 起点の
実行では**失敗する**。次の手順で引き取る。

1. **更新内容 (上流の差分) を確認する。** Pull Request 本文に更新前後の版と
   比較への導線がある
2. **その Pull Request のブランチへコミットを 1 つ積む**
3. 以降の実行は保守担当者が起点となり、**必須検査すべてが実行される**。
   結果はその Pull Request に紐づく
4. レビュー承認を経てマージする

**この経路のために新しいワークフローを作らない。** 通常の `pull_request` の
実行がそのまま該当する。依存更新のために検査を減らした経路を設けないことが
要件である。

### 失敗を飛ばさない

秘密情報が無いことを理由に検査を条件付きで飛ばさないこと。**飛ばした検査は
「skipped」となり、ブランチ保護では成功として数えられる。** 検査を経ずに
マージできる状態が生まれる。

「実行できなかった」と「通った」を同じ色にしない。この不変条件は
`test/config/workflows_test.go` が機械的に検査する。自動マージを足す変更も
同じ検査に落ちる。

### Dependabot の対象外

依存の記述ではないものは Dependabot が扱わない。**これらの更新は人が行う。**

| 対象 | 場所 |
|---|---|
| `GOLANGCI_LINT_VERSION` | `Makefile`、`.github/workflows/ci.yml` の `env` |
| `GOVULNCHECK_VERSION` | 同上 |
| `BETTERLEAKS_VERSION` | 同上 |
| `ACTIONLINT_VERSION` | 同上 |
| `SYFT_VERSION` | `Makefile` |
| `GO_LICENSES_VERSION` | `Makefile` |

`Makefile` と `ci.yml` の双方に同じ値が書かれている。**片方だけ更新しない
こと。** 手元と CI で判定結果が食い違う。

Dependabot が扱うのは次の 3 種別である。

| 種別 | 対象 |
|---|---|
| `gomod` | `go.mod` / `go.sum` |
| `github-actions` | `.github/workflows/*.yml` のアクションの版 |
| `docker` | `build/Containerfile` のビルド段の基底イメージ |

`FROM scratch` には版が無いため更新の対象にならない。

### リポジトリ設定 (ファイルに現れない)

次はリポジトリの設定であり、作業ツリーに現れないため機械的に検査できない。

| 設定 | 場所 |
|---|---|
| Dependabot alerts / security updates を有効にする | Settings → Advanced Security |
| ブランチ保護 (必須検査 + レビュー承認) | Settings → Branches |

**セキュリティ更新が無効だと、脆弱性の修正も待機期間 5 日に阻まれた状態が
続く。** `.github/dependabot.yml` をどう書いても有効にはできない。

## リファレンス

設定項目、アクセストークンの供給元 (鍵の位置・認証・必要な権限)、メトリクス、
ログ、トレース、エラーの分類、既知の制限は [reference.md](./reference.md) に
まとめてある。

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
