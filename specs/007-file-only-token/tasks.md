# Tasks: アクセストークンの供給元をファイルに限る

**Input**: Design documents from `/specs/007-file-only-token/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/configuration.md](./contracts/configuration.md),
[quickstart.md](./quickstart.md)

**Tests**: **必須。** constitution v3.0.0 の原則 III (テストファースト) が
NON-NEGOTIABLE であるため。テストを先に書き、**期待どおり失敗することを確認してから**
実装に着手する。

**Organization**: US1 (ファイルのみ) と US2 (廃止フラグの拒否) は、**同じ削除で
同時に満たされる。** フラグの定義を消せば、ファイルが唯一の経路になり、かつ
廃止フラグは未定義として拒否される (research R1)。そのため両ストーリーの
失敗するテストを Phase 2 にまとめて先に書き、Phase 3 の削除でまとめて通す。
Phase 4 (US2) は、削除で得た拒否を利用者の目に届く形 (バイナリの挙動と文書) で
確かめる。US3 は文書のみで、コードに依存しない。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 並行実行可能 (別ファイル、未完了タスクへの依存なし)
- **[Story]**: 対応するユーザーストーリー (US1 / US2 / US3)
- 各タスクに具体的なファイルパスを含む

---

## Phase 1: Setup

- [X] T001 作業ブランチ `feature/007-file-only-token` を `main` から作成し、未コミットの `.specify/memory/constitution.md` (v3.0.0) と `specs/007-file-only-token/` 一式を、それぞれ別のコミットとして載せる (constitution: `docs: amend constitution to v3.0.0 (limit DPF token source to mounted files)`、spec: `docs(spec): add 007 file-only token source`)

新しい依存・ディレクトリは増えない。他の Setup は不要。

---

## Phase 2: Foundational — 先行テスト (US1・US2 共通)

**Purpose**: Phase 3 の削除が満たすべき振る舞いを、削除の前にテストで固定する。

**⚠️ CRITICAL**: このフェーズのテストのうち T002〜T005 が**失敗する**ことを
`go test ./internal/config/...` で確認するまで、Phase 3 に進まない。

- [ ] T002 [P] `internal/config/config_test.go` に `TestLoad_RejectsRemovedSecretManagerFlags` を追加する。`--dpf-token-secret-manager=aws`、`--dpf-token-secret-id=x`、`--dpf-token-secret-endpoint=https://example.invalid/` の 3 つそれぞれについて、`baseArgs(t)` (有効な `--dpf-token-file` を含む) に足して `config.Load` を呼び、(a) エラーになること、(b) `errors.Is(err, config.ErrInvalid)`、(c) エラー文字列にそのフラグ名 (`dpf-token-secret-manager` 等、先頭のダッシュを除く) が含まれることを確かめる。サブテストに分ける。コメントに「spec 007 FR-004 / constitution v3.0.0」と書く。**現状は `-id` と `-endpoint` のサブテストが (c) で失敗する** (現行は併用を `ErrInvalid` で拒否するが、メッセージは常に「`--dpf-token-file` と `--dpf-token-secret-manager` の併用は不可」であり、`dpf-token-secret-id` / `dpf-token-secret-endpoint` を含まない)。`-manager` のサブテストは現状でも通る
- [ ] T003 [P] `internal/config/config_test.go` に `TestLoad_RejectsRemovedFlagsWithoutTokenFile` を追加する。`--dpf-token-secret-manager=aws --dpf-token-secret-id=x` だけ (ファイルなし) で `config.Load` がエラーになり、エラー文字列に `dpf-token-secret-manager` が含まれ、かつ `errors.Is(err, config.ErrInvalid)` であることを確かめる。**現状は成功するため失敗する**
- [ ] T004 [P] `internal/config/config_test.go` の `TestLoad_RequiresTokenSource` を改める。`config.Load(nil)` のエラーが `ErrMissingRequired` であることに加え、文字列に `--dpf-token-file` を含み、**`secret` を含まない**ことを確かめる。**現状のメッセージは `--dpf-token-secret-manager` を含むため失敗する**
- [ ] T005 [P] `internal/config/config_test.go` に `TestUsage_HasNoSecretManagerOption` を追加し、`config.Usage()` に `secret` (大文字小文字を区別しない) が現れないことを確かめる。あわせて `TestUsage_ListsKeySettings` の期待値から `"-dpf-token-secret-manager"` を削除する。**現状は失敗する**
- [ ] T006 [P] `internal/dpf/token_test.go` に `TestNewClient_BlankTokenFileIsPermanent` を追加する。空白と改行だけ (`"  \n"`) のトークンファイルを作り、`httptest.NewServer` で受けた要求数を数えるサーバを立て、`NewClient(t.Context(), config.DPF{TokenFile: path, Endpoint: srv.URL}, nil, nil)` で作ったクライアントの `ListZones` を呼ぶ。(a) エラーが `errors.Is(err, provider.ErrPermanent)` であること (`ListZones` が分類済みでなければ `Classify` を通す)、(b) サーバが受けた要求数が 0 であること、(c) エラー文字列にファイルパスが含まれないこと、を確かめる。**これは現行の振る舞いを固定する特性テストであり、現状でも成功してよい** (data-model「トークンファイル」、dpf-go v0.6.0 が空のトークンを `ErrTokenRequired` として扱う)。成功・失敗のどちらになったかを記録する
- [ ] T006a T002〜T006 の変更だけをコミットする。メッセージは `test(config,dpf): pin file-only token source before removal` とし、本文に「T002〜T005 は失敗する。T006 は特性テスト」と、`go test ./internal/config/... ./internal/dpf/...` の失敗の要約を記す。実装の変更を含めない (原則 III: テストの先行を履歴から追えるようにする)

**Checkpoint**: T002〜T005 が失敗し、T006 が成功 (または失敗理由が判明) し、T006a のコミットが存在すること。

---

## Phase 3: User Story 1 - トークンをファイルだけで与える (Priority: P1) 🎯 MVP

**Goal**: トークンの供給元を `--dpf-token-file` だけにし、シークレット管理サービスへ
接続するコードと依存を取り除く。

**Independent Test**: quickstart 手順 1・2・4。`make all` が通り、`--dpf-token-file`
だけで設定が成立し、シークレット管理サービスの SDK がリンクされていない。

### Implementation for User Story 1

- [ ] T007 [US1] `internal/config/config.go` を縮める。(1) `newFlagSet` から `dpf-token-secret-manager` / `dpf-token-secret-id` / `dpf-token-secret-endpoint` の定義と `values` の 3 フィールドを削除。(2) `DPF` 構造体から `SecretManager` / `SecretID` / `SecretEndpoint` と `UsesSecretManager()` を削除。(3) `supportedSecretManagers` と、不要になる `slices` の import を削除。(4) `validateTokenSource` を「`TokenFile` が空なら `fmt.Errorf("%w: --dpf-token-file を指定してください", ErrMissingRequired)`、そうでなければ nil」にし、doc コメントを「トークンの供給元 (ファイル) が指定されていることを確かめる」に改める。(5) `Usage()` 末尾の案内を「アクセストークンは --dpf-token-file で与えます。\n環境変数と引数からトークンを受け取る経路は用意していません。\n」にする。(6) `newFlagSet` の「意図的に定義しないフラグ」のコメントに `--dpf-token-secret-manager / -id / -endpoint: 供給元をファイルに限った (constitution v3.0.0)` を追記する
- [ ] T008 [US1] `internal/config/config_test.go` から、T007 で削除したフィールドやフラグを前提とするテストを削除する: `TestLoad_AcceptsSecretManager`、`TestLoad_RejectsBothTokenSources` (T002 が置き換える)、`TestLoad_RejectsUnknownSecretManager`、`TestLoad_SecretManagerRequiresSecretID`。`go test ./internal/config/...` で T002〜T005 を含む全テストが成功することを確認する
- [ ] T009 [US1] `internal/dpf/token.go` を縮める。`newTokenProvider` の署名を `func newTokenProvider(cfg config.DPF) (utils.TokenProvider, error)` にし (ctx を削除)、`cfg.TokenFile == ""` なら既存の「供給元が設定されていません」エラー、そうでなければ `utils.TokenFromFile(cfg.TokenFile)` を返す。`newSecretManagerProvider`、`newVaultProvider`、`newAWSProvider`、`newAzureProvider`、`newGCPProvider` と、それらのための import (SDK 5 つ、`dpf-go/misc/*` 4 つ、`context`) を削除する。doc コメントの「2 つに限る (constitution v1.8.0)」を「マウントされたファイルに限る (constitution v3.0.0)」に改める
- [ ] T010 [US1] `internal/dpf/client.go` を改める。`newTokenProvider(ctx, cfg)` の呼び出しを `newTokenProvider(cfg)` にし、`if cfg.UsesSecretManager() { ... WithTokenTTL ... }` の分岐とその直前のコメント、`secretManagerTokenTTL` 定数とその doc コメントを削除する。`time` は `observe` が使うため残る。ファイル経路をキャッシュしない理由 (FR-037) のコメントは、`opts` 組み立ての近くに 1 文で残す
- [ ] T011 [US1] `internal/dpf/token_test.go` を改める。`TestNewTokenProvider_RejectsUnknownSecretManager` と `TestNewTokenProvider_KnownSecretManagersAreRoutable` を削除し、残るテストの `newTokenProvider(t.Context(), ...)` を `newTokenProvider(...)` に改める。`go test ./internal/dpf/...` が成功することを確認する
- [ ] T012 [US1] `go mod tidy` を実行し、`go.mod` の直接依存から `cloud.google.com/go/secretmanager`、`github.com/Azure/azure-sdk-for-go/sdk/azidentity`、`github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets`、`github.com/aws/aws-sdk-go-v2/config`、`github.com/aws/aws-sdk-go-v2/service/secretsmanager`、`github.com/hashicorp/vault/api`、`github.com/iij/dpf-go/misc/{aws,azure,gcp,vault}` が消えたことを確認する (`go.mod`、`go.sum`)
- [ ] T013 [US1] `.golangci.yml` の `linters.enable` に `depguard` を追加し、`linters.settings.depguard.rules` に `main` ルール (対象 `$all`) を置いて、次の import を `deny` する: `github.com/hashicorp/vault`、`github.com/aws/aws-sdk-go-v2`、`github.com/Azure/azure-sdk-for-go`、`cloud.google.com/go/secretmanager`、`github.com/iij/dpf-go/misc`。各 `desc` は「constitution v3.0.0: トークンはマウントされたファイルからのみ取得する。シークレット管理サービスへ接続しない」とする。他の linter と同じく行末に目的のコメントを付ける。`golangci-lint run` が成功し、試しに `internal/dpf/token.go` へ `_ "github.com/hashicorp/vault/api"` を足すと失敗することを確かめてから戻す
- [ ] T014 [P] [US1] `docs/reference.md` の機械検査される表を改める。「設定」の表 (`<!-- reference:flags -->`) から `--dpf-token-secret-manager` / `--dpf-token-secret-id` / `--dpf-token-secret-endpoint` の 3 行を削除。「対応するシークレット管理サービス」の節 (`<!-- reference:secret-managers -->` の表を含む) を削除。「この文書と実装の一致 > 検査されている記述」の表から「対応するシークレット管理サービス」の行を削除
- [ ] T015 [P] [US1] `test/docs/reference_test.go` から `"secret-managers"` (印の一覧、31 行目付近) と `TestReference_SecretManagers` を削除する。`test/docs/doc.go` の印の説明から `secret-managers` の行を、`--dpf-token-secret-endpoint` と `kms:Decrypt` に触れた例示を削除または一般化する。`test/docs/extract.go` の `supportedSecretManagers` を例示したコメントを、残る許可リスト (例: レコード種別) の例に置き換える。`sliceVarStrings` は `test/docs/extract_test.go` が単体で検査しており、将来の許可リストの検査にも使えるため残す
- [ ] T016 [US1] `make all` を実行し、すべて成功することを確認する (quickstart 手順 1)。続いて quickstart 手順 4 を実行し、モジュール数が基準値 97 より少なく、`hashicorp|aws-sdk|azure-sdk|cloud.google.com/go/secretmanager|dpf-go/misc` が 0 件であることを記録する

**Checkpoint**: US1 完了。トークンはファイルからのみ取得され、SDK はリンクされず、
再び import されれば CI が止める。

---

## Phase 4: User Story 2 - 廃止した設定を誤って残したまま起動しない (Priority: P2)

**Goal**: 廃止フラグの拒否が、利用者の目に見える形 (バイナリの終了コードと出力、
文書) で成立していることを確かめる。

**Independent Test**: quickstart 手順 3。

### Implementation for User Story 2

- [ ] T017 [P] [US2] `docs/reference.md` の「意図的に存在しないもの」の表に、`--dpf-token-secret-manager`、`--dpf-token-secret-id`、`--dpf-token-secret-endpoint` を 1 行ずつ (または 1 行にまとめて) 追加する。理由は「供給元をマウントされたファイルに限った (constitution v3.0.0)。外部のシークレット管理サービスの値は Kubernetes 側でファイルとして届ける ([README](../README.md))」。表の直後の「いずれも指定すると未定義のフラグとして起動に失敗する。黙って無視しない」が新しい行にも当てはまることを確認する。「検証の規則」を「`--dpf-token-file` は必須。指定しなければ起動失敗」と「`--domain-filter` …」の 2 項目に改める
- [ ] T018 [US2] quickstart 手順 3 を実行する。ビルドしたバイナリに廃止フラグを 1 つずつ (`--dpf-token-file` と併用で) 渡し、終了コードが非 0 で、出力にそのフラグ名が現れることを確認する。`--help` の出力に `secret` が 0 件であることを確認する。結果を PR の説明に記す

**Checkpoint**: US2 完了。廃止フラグは黙って無視されず、その事実が文書にある。

---

## Phase 5: User Story 3 - 外部のシークレット管理サービスの値を使い続ける (Priority: P3)

**Goal**: シークレット管理サービスにトークンを置いている運用者が、Kubernetes 側で
ファイルにして本サービスへ渡す構成へ、README から到達できる。

**Independent Test**: quickstart 手順 6。README とリファレンスに、直接接続を前提とした
記述が残らず、移行先の案内と `subPath` の注意がある。

### Implementation for User Story 3

- [ ] T019 [P] [US3] `README.md` の「アクセストークンの与え方」を改める。冒頭を「トークンの供給元は **マウントされたファイルだけ**です」とし、「ファイル (Secret のマウント)」の節を残す。「シークレット管理サービス」の節 (`--dpf-token-secret-manager` の例、vault/aws/azure/gcp の説明、注意 2 点、azure の例) を削除する。「環境変数と引数からは受け取りません」の節の「ファイルやシークレット管理サービスと同じ保護水準」を「ファイルと同じ保護水準」に改める
- [ ] T020 [US3] `README.md` の「アクセストークンの与え方」に「外部のシークレット管理サービスを使う場合」の節を新設する (T019 と同じファイルのため T019 の後)。内容: (1) 本サービスは外部サービスへ直接接続しない。(2) Kubernetes 側の仕組みで Pod 内のファイルにし、そのパスを `--dpf-token-file` に渡す。例として External Secrets Operator (外部の値を Secret に同期し、それをマウントする) と Secrets Store CSI Driver (ボリュームとして直接マウントする) を名前とリンクで挙げ、導入手順は各プロジェクトの文書に委ねる。(3) ⚠ **`subPath` でマウントしない**こと。`subPath` のマウントには Secret の更新が反映されず、ローテーションしても古いトークンが使われ続ける。(4) Secret の更新がファイルに届くまでには Kubernetes 側の遅延があり、その間は古いトークンが使われる。(5) CSI Driver でローテーションを反映させるには、Driver 側でローテーションを有効にする必要がある旨。(6) これらの仕組みが外部サービスへの egress を要する場合、それはその仕組みの側の設定であり、本サービスの NetworkPolicy に穴を開ける必要はない
- [ ] T021 [P] [US3] `docs/reference.md` の「アクセストークンの供給元」を改める。冒頭を「供給元は**マウントされたファイル**に限る」とし、「用語について」の節、「HashiCorp Vault」「AWS Secrets Manager」「Azure Key Vault」「Google Secret Manager」の各節 (認証・権限の小節を含む)、「供給元の比較」の節を削除する。「すべての供給元に共通する性質」の表は「ファイル (Secret のマウント)」の節に統合し、「キャッシュしないことの代償」の段落を削除する。外部サービスを使う場合は README の該当節を参照させる 1 文を置く
- [ ] T022 [US3] `docs/reference.md` の残りを改める (T021 と同じファイルのため T021 の後)。「この文書と実装の一致 > 検査されていない記述」の表から、トークンが読まれる位置・認証の解決順・必要な権限・シークレットへの格納手順の 4 行と、その直後の `[!IMPORTANT]` の注意 (Vault の `token` フィールド等) を削除する。「既知の制限」の表から、トークンのキャッシュ期間・Vault のマウント等・Vault の認証・JSON 解釈・バージョン固定の 5 行を削除する。目次や文中のアンカー (`#アクセストークンの供給元`、`#aws-secrets-manager` など) への参照が切れていないことを確認する
- [ ] T023 [US3] quickstart 手順 6 を実行し、README と `docs/reference.md` に `secret-manager|vault|secrets manager|key vault|secret manager` が、廃止フラグの行と README の新設節以外に現れないことを確認する。`make all` (test/docs を含む) が成功することを確認する。あわせて README の推奨 values (「2. values を用意する」) と NetworkPolicy の例 (「NetworkPolicy はチャートに含まれません」) に、シークレット管理サービスへの egress、およびそれを示唆する記述がないことを確認する (FR-010)

**Checkpoint**: US3 完了。移行先の案内があり、直接接続を前提とした記述が残らない。

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T024 [P] `specs/001-webhook-provider/spec.md` の FR-035 と FR-036 の末尾に「**(007 / constitution v3.0.0 で廃止。供給元はマウントされたファイルのみ)**」と注記する。本文は書き換えない (research R7)
- [ ] T025 `go-licenses` で依存ライセンスを実測し (`make license-deps` が使う方法と同じ)、ライセンス別の件数を得る。`.specify/memory/constitution.md` の「依存ライセンスの実測」の段落を新しい日付と件数に更新し、MPL-2.0 の件数と由来の記述を実測に合わせる (0 件なら「MPL-2.0 の依存はない」とし、許容リストの MPL-2.0 の行は残す)。v3.0.0 の Sync Impact Report を更新する。Modified sections の「ライセンス」を「実測値を更新」に改め、Follow-up の「依存ライセンスの実測を 007 の実装後に更新する」を削除する。バージョンは v3.0.0 のまま据え置く (未マージのため)。**ただし T025 の時点で v3.0.0 が既に `main` にマージされていた場合は、v3.0.1 (PATCH) として改訂し、Sync Impact Report を先頭に足して `**Version**` と `**Last Amended**` を更新する**
- [ ] T026 コンテナイメージをビルドし (`make` の該当ターゲット、または CI の `ci.yml` と同じ手順)、`/licenses/third-party/` に HashiCorp・AWS・Azure・Google Cloud のディレクトリが無いことを確認する
- [ ] T027 grep で残骸を確認する: `grep -rn -i -E 'secret.?manager|SecretID|SecretEndpoint|vault|azsecrets|secretsmanager' --include='*.go' .` が 0 件 (`.golangci.yml` の deny リストと `specs/` は除く)。残っていれば削除する
- [ ] T028 PR を作成する。本文に、constitution v3.0.0 に基づく変更であること、廃止したフラグ、移行先 (README の新設節)、依存モジュール数の変化 (T016 の記録)、T018 の確認結果、未リリースのため移行期間を設けないことを記す。constitution の改訂を含むため、Governance に従い既存コード・spec への影響 (001 の FR-035/036 の廃止) を記す。PR の CI で `e2e` (トークンのローテーションを含む)、`scale`、`e2e-sidecar` が成功することを確認する (SC-005、constitution「実環境での検証」)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: なし
- **Foundational (Phase 2)**: Phase 1 の後。**Phase 3 をブロックする** (テストファースト)
- **US1 (Phase 3)**: Phase 2 の後。T007→T008、T009→T010→T011 は順に。T012 は T009 の後。T013 は T012 の後。T014・T015 は T007 の後なら並行可で、**両方そろうまで `make all` は通らない**。T016 は最後
- **US2 (Phase 4)**: T007 (フラグ削除) の後。T017 は T014 と同じファイルのため T014 の後
- **US3 (Phase 5)**: コードには依存しない。ただし `docs/reference.md` を触るため T014・T017 の後。README の T019→T020 は順に
- **Polish (Phase 6)**: T025・T026 は T012 の後。T027・T028 は全フェーズの後

### User Story Dependencies

- **US1**: Phase 2 のテストに依存
- **US2**: US1 の T007 に依存 (同じ削除で成立するため)。独立したコード変更はない
- **US3**: コード上の依存なし。文書の衝突を避けるため `docs/reference.md` の順序だけ守る

### Parallel Opportunities

- Phase 2: T002〜T005 は同じファイルだが、それぞれ独立したテスト関数の追加であり、1 人が続けて書く想定。T006 は別ファイルで並行可
- Phase 3: T014 (`docs/reference.md`) と T015 (`test/docs/`) は並行可
- Phase 5: T019 (README) と T021 (`docs/reference.md`) は並行可
- Phase 6: T024 は他と並行可

---

## Parallel Example: User Story 1

```bash
# T007 の後、文書と検査を同時に改める:
Task: "T014 docs/reference.md の設定表とシークレット管理サービスの表を削除"
Task: "T015 test/docs から secret-managers の検査を削除"
```

## Parallel Example: User Story 3

```bash
Task: "T019 README.md の「アクセストークンの与え方」を改める"
Task: "T021 docs/reference.md の「アクセストークンの供給元」を改める"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 (ブランチ作成、constitution と spec のコミット)
2. Phase 2 (先行テスト、失敗を確認)
3. Phase 3 (削除、依存除去、depguard)
4. **止めて確認**: `make all` と quickstart 手順 1・2・4

US1 の時点で、constitution v3.0.0 への違反は**コード上は**解消する。US2 は同じ削除で
すでに成立しており、Phase 4 は確認と文書化である。

### Incremental Delivery

US1〜US3 は 1 つの PR にまとめる。US1 だけで出すと、README がシークレット管理
サービスの使い方を案内したまま、そのフラグが起動に失敗する状態になる
(SC-006 違反)。

---

## Notes

- 削除が中心の変更であり、テストの多くは「消える」。消すテストが守っていた性質
  (未知のサービス名の拒否、併用の拒否) が、新しいテスト (T002・T003) で引き続き
  守られていることを確かめてから消す
- コミットは少なくとも次の単位で分ける。順序を入れ替えない (原則 III)。
  1. T001: constitution、spec 一式 (2 コミット)
  2. T006a: 失敗するテストのみ
  3. Phase 3 以降: 実装。テストの削除・改変 (T008、T011) は、それが前提とする
     実装の削除と同じコミットに入れる (分けるとビルドが通らない中間状態ができるため)
