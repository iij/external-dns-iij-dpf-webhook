# Tasks: ExternalDNS webhook provider 本体

**Input**: Design documents from `/specs/001-webhook-provider/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: **必須。** constitution v1.8.0 の原則 III (テストファースト) が NON-NEGOTIABLE で
あるため、テストタスクは省略できない。各実装タスクの前にテストタスクを置き、
**テストが期待どおり失敗することを確認してから**実装に着手する。

**Organization**: タスクはユーザーストーリー単位に分け、各ストーリーが独立して
実装・テスト・デリバリできるようにしてある。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 並行実行可能 (別ファイル、未完了タスクへの依存なし)
- **[Story]**: 対応するユーザーストーリー (US1〜US4)
- 各タスクに具体的なファイルパスを含む

## Path Conventions

plan.md の構造に従う。`cmd/webhook/`、`internal/`、`test/`、`build/` をリポジトリ
ルート直下に置く。

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: プロジェクトの初期化と、constitution が要求する品質ゲートの土台作り

- [X] T001 `go.mod` をリポジトリルートに作成する。モジュールパスは `github.com/iij/external-dns-iij-dpf-webhook`、`go` ディレクティブは `1.27`
- [X] T002 [P] `internal/`、`cmd/webhook/`、`test/contract/`、`test/integration/`、`build/` のディレクトリ構造を plan.md の Source Code 節に従って作成する
- [X] T003 [P] `.golangci.yml` を作成し、有効にする linter とそのバージョンを固定する。ローカルと CI が同一設定で動くこと (constitution: Go コード品質)
- [X] T004 [P] `Makefile` に `fmt-check` / `build` / `lint` / `vuln` / `test` ターゲットを定義する。`fmt-check` は `gofmt -l ./...` の出力が空でないとき失敗すること
- [X] T005 [P] `build/Containerfile` を作成する。alpine ビルダー段で `CGO_ENABLED=1 -buildmode=pie -tags 'netgo osusergo' -ldflags '-s -w -linkmode external -extldflags "-static-pie"'`、最終段は `scratch` に CA 証明書とバイナリのみを置き `USER 65532:65532` とする (research R1/R2)
- [ ] T006 **(保留: CI は後回しとする方針)** [P] `.github/workflows/ci.yml` に品質ゲートを定義する。`gofmt -l` / `go build` / `golangci-lint run` / `govulncheck ./...` / `go test ./...` / イメージビルド / 脆弱性スキャンを実行し、Go・golangci-lint・govulncheck のバージョンを固定する
- [ ] T007 **(保留: CI は後回しとする方針)** [P] `.github/workflows/ci.yml` に配布バイナリの ASLR 検証ステップを追加する。ELF Type が `DYN` であり `PT_INTERP` を持たないことを確認して失敗させる (constitution v1.6.0)
- [ ] T008 **(保留: CI は後回しとする方針)** [P] `.github/workflows/scheduled.yml` を作成し、`govulncheck` とイメージ脆弱性スキャンを定期実行する (constitution: コード変更がなくても新規脆弱性は公開されるため)
- [X] T009 [P] `docs/development.md` に、`dpf-go` が公開されるまでの `GOPRIVATE=github.com/iij/dpf-go` 設定と、CI での認証付きモジュール取得手順を記載する (research R10)

**Checkpoint**: 空のプロジェクトで全 CI ゲートが通る状態

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: すべてのユーザーストーリーが依存する基盤。ここが揃うまでストーリーに着手できない

**⚠️ CRITICAL**: このフェーズが完了するまで、いかなるユーザーストーリーの実装も開始しない

### 正規化名 (すべての層が依存)

- [X] T010 [P] `internal/dnsname/name_test.go` に、正規化名の型のテストを書く。生成時に `dns.CanonicalName` が適用されること、`dns.IsDomainName` を満たさない名前が実体化できないこと、大文字混じり・末尾ドット有無の入力が同一の値になること
- [X] T011 `internal/dnsname/name.go` に正規化名の型を実装する。生の文字列から直接構築できない設計とし、生成経路を境界に限定する (constitution v1.4.0)
- [X] T012 [P] `internal/dnsname/scope_test.go` に包含判定のテストを書く。`example.jp` を範囲としたとき `evil-example.jp` が含まれないこと、`a.b.example.jp` が含まれること
- [X] T013 `internal/dnsname/scope.go` に包含判定と最長一致の選択を実装する。`dns.IsSubDomain` / `dns.SplitDomainName` / `dns.CountLabel` のみを用い、`strings` による判定を書かない

### 設定 (default-deny)

- [X] T014 [P] `internal/config/config_test.go` に設定読み込みのテストを書く。必須設定の欠落で起動失敗すること、解釈不能な値で既定値にフォールバックせず失敗すること、domain filter 未設定が「範囲なし」になること (FR-002/FR-017/FR-018)
- [X] T015 `internal/config/config.go` に設定の読み込みと検証を実装する。トークン取得経路はファイルとシークレット管理サービスの 2 つに限り、環境変数・引数からのトークン受け取りを提供しない (constitution v1.8.0)

### テレメトリ基盤 (ログのみ。計測値とトレースは US4)

- [X] T016 [P] `internal/telemetry/log_test.go` に、構造化ログが標準出力へ出ること、OTLP 送出先の設定有無に関わらず標準出力が止まらないことのテストを書く (原則 V)
- [X] T017 `internal/telemetry/log.go` に構造化ログの初期化を実装する。ログレベルを設定で変更可能にする

### DPF クライアント層の土台

- [X] T018 [P] `internal/provider/ports.go` に、provider が dpf 層に期待するインタフェースを宣言する。contracts/dpf-client.md の「提供する操作」に対応させ、`dpf-go` の生成型を一切含めない (原則 II)
- [X] T019 [P] `internal/dpf/errors_test.go` に、エラー分類のテストを書く。応答不能・レート制限・ロック取得不能が一時的、形式違反・権限不足・トークン取得失敗が恒久的に分類されること (contracts/dpf-client.md)
- [X] T020 `internal/dpf/errors.go` に、`dpf-go` 由来のエラーを本プロジェクトのエラー型へ変換する処理を実装する。`*utils.TokenError` を恒久的に分類する
- [X] T021 [P] `internal/dpf/client_test.go` に、トークン供給のテストを書く。ファイル経路とシークレット管理サービス経路が設定でき、環境変数経路が提供されないこと、トークン取得失敗時のエラーに値やファイル内容が含まれないこと (FR-036/FR-039)
- [X] T022 `internal/dpf/client.go` に、`utils.WithTokenFile` / `utils.WithTokenProvider` を用いたクライアント構築を実装する。`utils.WithToken` と環境変数既定は使わない (research R5)
- [X] T023 [P] `internal/dpf/rrtype_test.go` に、種別対応付けのテストを書く。9 種別が対応すること、`DNAME` が対応しないこと、DPF 固有の 7 種別が管理対象外として識別されること (FR-026/FR-027/FR-028)
- [X] T024 `internal/dpf/rrtype.go` に、許可リスト方式の種別対応付けを実装する

### HTTP 基盤

- [X] T025 [P] `test/contract/negotiation_test.go` に、メディアタイプ `application/external.dns.webhook+json;version=1` のネゴシエーションと、`4xx`/`5xx` の使い分けの契約テストを書く (contracts/webhook-api.md)
- [X] T026 `internal/webhook/negotiation.go` にメディアタイプの解釈と応答ヘッダ設定を実装する
- [X] T027 `internal/webhook/errors.go` に、エラー分類から HTTP 状態コードへの写像を実装する。一時的な障害を `4xx` にしない
- [X] T028 [P] `internal/server/server_test.go` に、provider リスナーがループバックのみに待ち受けること、exposed リスナーが `/healthz` を提供することのテストを書く (原則 VI、FR-019)
- [X] T029 `internal/server/server.go` に 2 つのリスナー (provider 既定 `8888` / exposed 既定 `8080`) の起動・停止と `/healthz` を実装する
- [X] T030 `cmd/webhook/main.go` に起動処理を実装する。設定検証 → 各層の組み立て → リスナー起動 → 終了処理。必須設定が欠ければ異常終了する

**Checkpoint**: 基盤が揃い、ユーザーストーリーの実装を開始できる

---

## Phase 3: User Story 1 - 管理対象の宣言とレコードの把握 (Priority: P1) 🎯 MVP

**Goal**: 管理対象ドメインを ExternalDNS に通知し、DPF の既存レコードを一覧として返す。
DNS を一切変更しない。

**Independent Test**: 本 provider を起動し、管理対象ドメインの問い合わせとレコード一覧の
取得を行う。管理対象のレコードが漏れなく、管理対象外を含まずに返ること。書き込みは発生しない。

### Tests for User Story 1 ⚠️ 先に書いて失敗を確認する

- [X] T031 [P] [US1] `test/contract/negotiate_test.go` に `GET /` の契約テストを書く。管理対象ドメインが返ること、未設定時に空の範囲が返り「全ドメイン」にならないこと (FR-001/FR-002)
- [X] T032 [P] [US1] `test/contract/records_get_test.go` に `GET /records` の契約テストを書く。成功時 `200`、名前が正規化名 (小文字・末尾ドット) で返ること (research R7)
- [X] T033 [P] [US1] `test/integration/list_records_test.go` に統合テストを書く。管理対象外ゾーンのレコードが含まれないこと、大文字混じりで登録された名前が同一名として扱われること、管理対象外種別が除外されること (FR-004/FR-006/FR-027)
- [X] T034 [P] [US1] `test/integration/list_failure_test.go` に、DPF が応答しないとき一時的な失敗として `5xx` が返ることのテストを書く (FR-016)

### Implementation for User Story 1

- [X] T035 [P] [US1] `internal/dpf/zone.go` にゾーン解決を実装する。ゾーン名を正規化して返し、最長一致で書き込み先を選べる形にする (data-model.md 3)
- [X] T036 [US1] `internal/dpf/records.go` に反映済みレコードの全件取得を実装する。全ページを取得し、許可リスト外の種別を除外し、名前を正規化して返す (contracts/dpf-client.md)
- [X] T037 [P] [US1] `internal/provider/scope.go` に管理対象範囲の判定を実装する。集合が空なら常に偽とし、「空集合＝全許可」に読み替えない (FR-002/FR-003)
- [X] T038 [US1] `internal/provider/list.go` にレコード一覧の取得を実装する (T035〜T037 に依存)
- [X] T039 [US1] `internal/webhook/negotiate.go` に `GET /` のハンドラを実装する
- [X] T040 [US1] `internal/webhook/records.go` に `GET /records` のハンドラを実装する
- [X] T041 [US1] `internal/provider/list.go` に、管理対象が空である旨の起動時ログを追加する (FR-002 の可視化)

**Checkpoint**: US1 が単独で動作し、本番ゾーンに対しても安全に検証できる

---

## Phase 4: User Story 2 - レコードの作成・更新・削除 (Priority: P2)

**Goal**: ExternalDNS の変更セットを DPF に反映する。DNS を実際に変更する唯一の経路。

**Independent Test**: 検証用ゾーンで作成・更新・削除を実行し、DPF 上の状態が期待どおりに
変わること。同じ変更を 2 回適用しても結果が変わらないこと。

### Tests for User Story 2 ⚠️ 先に書いて失敗を確認する

- [X] T042 [P] [US2] `test/contract/records_post_test.go` に `POST /records` の契約テストを書く。成功時が `204 No Content` であること、空の変更セットが成功すること、末尾ドット有無の異なる名前が同一レコードとして扱われること (contracts/webhook-api.md)
- [X] T043 [P] [US2] `internal/provider/validate_test.go` に種別ごとの検証のテストを書く。apex NS の削除要求、`CNAME` の複数値・他種別との共存、`A`/`AAAA` の名前に含まれる `_`、`MX`/`SRV` の数値範囲外がいずれも恒久的な失敗になること (FR-029〜FR-031/FR-033)
- [X] T044 [P] [US2] `internal/provider/validate_test.go` に `TXT` の検証テストを書く。character-string 1 個が 256 オクテットなら失敗、複数 character-string の合計が 255 を超えるのは成功、往復で分割位置が変わらないこと (FR-032/FR-032a)
- [X] T045 [P] [US2] `internal/dpf/merge_test.go` にマージ規則のテストを書く。管理対象は変更後、変更セット外の管理対象は現在値、管理対象外は逐語コピー、SOA と apex NS は投入対象外になること (data-model.md 5)
- [X] T046 [P] [US2] `internal/dpf/merge_test.go` に投入前ガードのテストを書く。変更セット外のレコードが失われる内容になったとき、適用が中止され一時的な失敗になること (data-model.md 5)
- [X] T047 [P] [US2] `test/integration/apply_idempotent_test.go` に冪等性のテストを書く。同一の変更セットを 10 回適用しても最終状態が変わらないこと (FR-010/SC-002)
- [X] T048 [P] [US2] `test/integration/apply_scope_test.go` に、管理対象外のレコードが一切変更されないことのテストを書く。管理対象外の種別と範囲外の名前の双方について、TTL・値・コメント・ラベルが不変であること (FR-009/FR-027/SC-004)
- [X] T049 [P] [US2] `test/integration/apply_failure_test.go` に、適用が途中で失敗したとき成功を返さないこと、反映完了前に成功を返さないことのテストを書く (FR-011/FR-012)
- [X] T050 [P] [US2] `test/integration/apply_lock_test.go` に、ロックが適用ハンドラの内側に閉じることのテストを書く。レコード取得のみを行ってもロックが取得・保持されないこと (research R4)

### Implementation for User Story 2

- [X] T051 [P] [US2] `internal/provider/changeset.go` に変更セットの型を実装する。作成・更新・削除を保持する (data-model.md 5)
- [X] T052 [P] [US2] `internal/provider/validate.go` に種別ごとの検証規則を実装する。DPF へ送る前に判定し、違反を恒久的な失敗とする (T043・T044 に対応)
- [X] T053 [US2] `internal/dpf/merge.go` にマージ処理を実装する。管理対象外レコードはドメインモデルを通さず逐語コピーする (data-model.md 5、T045 に対応)
- [X] T054 [US2] `internal/dpf/merge.go` に投入前ガードを実装する。失われるレコードが変更セットの削除対象と一致しなければ中止する (T046 に対応)
- [X] T055 [P] [US2] `internal/dpf/lock.go` にゾーンロックを実装する。`utils.NewMutex` / `LockWait` / `Unlock` を用い、有効期限を設定し、成功・失敗のいずれの経路でも解放する (research R4)
- [X] T056 [US2] `internal/dpf/apply.go` に変更の適用を実装する。ロック取得 → 反映済みレコードの全件取得 → マージ → ガード → 一括更新とゾーン反映 → 完了待ち → ロック解放。編集中を含む一覧を土台にしない (contracts/dpf-client.md)
- [X] T057 [US2] `internal/dpf/apply.go` の完了待ちに `JobsAPI.SyncWait` を用いる。反映完了前に成功を返さない (FR-011)
- [X] T058 [US2] `internal/provider/apply.go` に適用のドメインロジックを実装する。範囲外レコードを除外し (失敗にしない)、空の変更セットを成功として扱う (T051〜T056 に依存)
- [X] T059 [US2] `internal/webhook/records.go` に `POST /records` のハンドラを実装する。成功時 `204` を返す
- [X] T060 [US2] `internal/provider/apply.go` に、変更操作の対象ゾーン・レコード名・操作種別・結果のログ出力を追加する (FR-020)

**Checkpoint**: US1 と US2 が独立して動作する

---

## Phase 5: User Story 3 - DPF の制約に合わせたレコードの調整 (Priority: P3)

**Goal**: ExternalDNS が算出したレコードを DPF が受け付ける形に補正し、差分の振動を防ぐ。

**Independent Test**: DPF の制約に抵触する内容を調整に掛け、受け付けられる形で返ること。
調整結果を再度調整に掛けても変化しないこと。

### Tests for User Story 3 ⚠️ 先に書いて失敗を確認する

- [X] T061 [P] [US3] `test/contract/adjustendpoints_test.go` に `POST /adjustendpoints` の契約テストを書く。成功時 `200`、調整不要の入力がそのまま返ること
- [X] T062 [P] [US3] `internal/provider/adjust_test.go` に冪等性のテストを書く。調整済みの内容を再度調整しても変化しないこと (FR-015)
- [X] T063 [P] [US3] `internal/provider/adjust_test.go` に TTL 補正のテストを書く。DPF が許容しない TTL が許容範囲に補正されること (FR-014)

### Implementation for User Story 3

- [X] T064 [US3] `internal/provider/adjust.go` に調整処理を実装する。冪等であること
- [X] T065 [US3] `internal/webhook/adjust.go` に `POST /adjustendpoints` のハンドラを実装する

**Checkpoint**: US1〜US3 が独立して動作し、差分の振動が起きない

---

## Phase 6: User Story 4 - 運用状態の把握と障害の切り分け (Priority: P3)

**Goal**: メトリクスとトレースを提供し、障害の所在を切り分けられるようにする。

**Independent Test**: 稼働状態の確認、変更操作の成否と件数の計測、要求 1 件が DPF 呼び出しまで
追跡できることを確認する。

### Tests for User Story 4 ⚠️ 先に書いて失敗を確認する

- [X] T066 [P] [US4] `internal/telemetry/metrics_test.go` に、計測器 1 組に Prometheus と OTLP の 2 リーダーが接続され、両形式が同一の計測値を表すことのテストを書く (原則 V、research R8)
- [X] T067 [P] [US4] `internal/telemetry/metrics_test.go` に、ゾーン名・レコード名・レコード値が既定でラベルに含まれないことのテストを書く (原則 V)
- [X] T068 [P] [US4] `internal/telemetry/otlp_test.go` に、OTLP 送出先が未設定なら送出しないこと、TLS 検証が既定で有効であることのテストを書く (原則 VI、constitution v1.7.0)
- [X] T069 [P] [US4] `test/integration/telemetry_failure_test.go` に、テレメトリ送出先が到達不能でも DNS 処理が継続することのテストを書く (FR-024)
- [X] T070 [P] [US4] `test/integration/secret_leak_test.go` に、ログ・計測値・トレース・エラーメッセージのいずれにもトークンが現れないことのテストを書く (FR-023/SC-006)

### Implementation for User Story 4

- [X] T071 [P] [US4] `internal/telemetry/metrics.go` に計測器を 1 組定義する。レコード変更操作の成否と件数、DPF 呼び出しの成否と所要時間 (FR-021)
- [X] T072 [US4] `internal/telemetry/metrics.go` に Prometheus リーダーと OTLP リーダーを接続する。同一の計測器から両形式を出す
- [X] T073 [US4] `internal/server/server.go` の exposed リスナーに `/metrics` を追加する。返す情報を healthz と metrics に限る (contracts/webhook-api.md)
- [X] T074 [P] [US4] `internal/telemetry/trace.go` にトレースの初期化を実装する。OTLP (gRPC) と OTLP/HTTP に対応する
- [X] T075 [US4] `internal/dpf/client.go` に `dpf-go` の OpenTelemetry 連携を組み込み、要求受信から DPF 呼び出しまでを 1 つのトレースに接続する (FR-022)
- [X] T076 [US4] `internal/telemetry/log.go` に OpenTelemetry 形式のログ送出を追加する。標準出力への出力は止めない (原則 V)

**Checkpoint**: 全ユーザーストーリーが独立して動作する

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: 複数ストーリーにまたがる仕上げ

- [X] T077 [P] `README.md` に利用前提条件 PC-001〜PC-004 を記載する。特に、ロックを取らない他の機械的変更者の変更が失われうること、管理対象ゾーンに未反映の編集を残さないこと (spec.md 利用前提条件、plan.md)
- [X] T078 [P] `README.md` に対応レコード種別 (9 種別)、対応する ExternalDNS のバージョン (v0.22.0 以降)、webhook API のメディアタイプを明記する (constitution v1.1.0)
- [X] T079 [P] `README.md` に、シェルを持たないイメージのデバッグ手順を ephemeral container を用いる方法として記載する (research R2)
- [X] T080 [P] `README.md` に、トークンの供給方法 (ファイルマウント / シークレット管理サービス) と、環境変数を使わない理由を記載する
- [ ] T081 検証用ゾーンで [quickstart.md](./quickstart.md) の全手順を実行し、結果を記録する
- [ ] T082 1,000 件規模のレコードを持つ検証用ゾーンで、レコード一覧の取得と適用が成立することを確認する (SC-008)
- [ ] T083 ExternalDNS と本 provider をサイドカー構成で動かし、Ingress の作成から 5 分以内にレコードが反映されること、差分の振動が起きないことを確認する (SC-001/SC-007)
- [X] T085 `LICENSE` (Apache-2.0) と `NOTICE` をリポジトリルートに置き、全 Go ファイルに SPDX ヘッダを付与する。`make license-check` で検証する (constitution v1.9.0)
- [ ] T086 [P] 依存モジュールに許容するライセンスの範囲を確定し、constitution の TODO(DEPENDENCY_LICENSE_POLICY) を解消する。252 モジュールに依存しており、方針なしでは非互換なものの混入に気付けない
- [X] T087 [P] 配布物へのライセンス表記の同梱方式を確定し、constitution の TODO(DISTRIBUTION_NOTICE) を解消する (v1.10.0)。`/licenses/` に本体と依存の本文を同梱し、OCI アノテーションを付与。`make verify-licenses` で検証する
- [ ] T088 [P] リリース時に SBOM と provenance を referrers として紐づける手順を確立する。`make image-push` (docker buildx --attest) を用いる。レジストリが必要なため、実際の付与確認はリリース環境で行う (constitution v1.10.0)
- [ ] T089 [P] `README.md` に SBOM / provenance の参照方法 (`cosign tree` など) を記載する。`/licenses/` の構成は記載済み。SBOM のリリース手順は別途対応中のため、それが固まってから書く
- [X] T084 `golangci-lint` の指摘と `govulncheck` の報告を解消する。抑制する場合は `nolint` に理由を併記する (constitution: Go コード品質)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 依存なし。即座に開始できる
- **Foundational (Phase 2)**: Setup の完了に依存。**全ユーザーストーリーをブロックする**
- **User Stories (Phase 3〜6)**: Foundational の完了に依存
- **Polish (Phase 7)**: 対象とするストーリーの完了に依存

### User Story Dependencies

- **US1 (P1)**: Foundational 完了後に開始できる。他ストーリーに依存しない
- **US2 (P2)**: Foundational 完了後に開始できる。US1 とはコードを共有するが、
  独立してテストできる。**DNS を破壊しうる唯一のストーリー**であり、US1 の確立後に
  着手することを推奨する
- **US3 (P3)**: Foundational 完了後に開始できる。単独でも契約テストは通るが、
  価値が出るのは US2 と組み合わせたとき (差分の振動の防止)
- **US4 (P3)**: Foundational 完了後に開始できる。観測対象があるのは US1・US2 の後

### Within Each User Story

- **テストを先に書き、失敗を確認してから実装する (原則 III、NON-NEGOTIABLE)**
- 型・モデル → ドメインロジック → ハンドラ の順
- `internal/dpf` の実装は `internal/provider/ports.go` の宣言に従う (原則 II)

### Parallel Opportunities

- Phase 1 の T002〜T009 はすべて並行実行できる
- Phase 2 では、正規化名 (T010〜T013)、設定 (T014〜T015)、テレメトリ (T016〜T017)、
  DPF 土台 (T018〜T024)、HTTP 基盤 (T025〜T029) の 5 群が並行できる。
  T030 は全群の完了に依存する
- 各ストーリーのテストタスク ([P] 付き) はすべて並行して書ける
- Foundational 完了後、US1〜US4 を別々の担当者が並行して進められる

---

## Parallel Example: User Story 2

```bash
# US2 のテストをまとめて書く (すべて別ファイル、相互依存なし):
Task: "契約テスト POST /records in test/contract/records_post_test.go"
Task: "種別ごとの検証テスト in internal/provider/validate_test.go"
Task: "マージ規則と投入前ガードのテスト in internal/provider/merge_test.go"
Task: "冪等性の統合テスト in test/integration/apply_idempotent_test.go"
Task: "管理対象外レコード不変の統合テスト in test/integration/apply_scope_test.go"
Task: "失敗時の挙動の統合テスト in test/integration/apply_failure_test.go"
Task: "ロック範囲の統合テスト in test/integration/apply_lock_test.go"

# 失敗を確認してから、独立した実装を並行で進める:
Task: "変更セットの型 in internal/provider/changeset.go"
Task: "種別ごとの検証 in internal/provider/validate.go"
Task: "ゾーンロック in internal/dpf/lock.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 のみ)

1. Phase 1: Setup を完了する
2. Phase 2: Foundational を完了する (**全ストーリーをブロックする**)
3. Phase 3: US1 を完了する
4. **停止して検証**: 管理対象の宣言とレコード一覧の取得を単独で確認する。
   この段階では DNS を変更しないため、本番ゾーンに対しても安全に試せる
5. 問題なければ次へ

### Incremental Delivery

1. Setup + Foundational → 基盤が揃う
2. US1 → 単独で検証 → **MVP**。読み取り専用で安全
3. US2 → **検証用ゾーンで**単独検証 → DNS 更新が動く
4. US3 → 単独で検証 → 差分の振動が止まる
5. US4 → 単独で検証 → 運用可能になる

US2 に着手する前に US1 を確立させる理由は、破壊的操作を持つ経路を、読み取りが
正しいと分かっている土台の上に載せるためである。範囲判定 (US1) が誤ったまま US2 を
実装すると、範囲外のレコードを壊す。

### 実装時に特に注意する点

- **T053 (マージ)**: 管理対象外レコードを逐語コピーする。ドメインモデルを通すと、
  変換の誤りがゾーン全体に及ぶ (research R3)
- **T054 (投入前ガード)**: これと T053 が、失敗時の影響範囲を抑える唯一の防壁である
- **T055・T056 (ロック)**: ロックは適用ハンドラの内側に閉じる。レコード取得側に
  広げると、適用が来ないままロックが残留してゾーンが操作不能になる (research R4)
- **T056 (土台の一覧)**: 反映済みレコードの一覧を使う。編集中を含む一覧を使うと、
  他者の未レビューの編集を公開してしまう
