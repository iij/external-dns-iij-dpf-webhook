# Tasks: レコードに管理者の印を付ける

**Input**: Design documents from `/specs/005-managed-by-label/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: **必須。** constitution v2.2.0 の原則 III (テストファースト) が
NON-NEGOTIABLE であるため、テストタスクは省略できない。各実装タスクの前にテストを
置き、**期待どおり失敗することを確認してから**実装に着手する。

**Organization**: 本機能のユーザーストーリーは 1 本だけである (spec.md)。価値が
「管理下のレコードを見分けられる」ことに尽き、独立して価値を持つ別のスライスが
存在しない。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 並行実行可能 (別ファイル、未完了タスクへの依存なし)
- **[Story]**: 対応するユーザーストーリー (US1)
- 各タスクに具体的なファイルパスを含む

---

## Phase 1: Setup

**不要。** 新しい依存、新しいディレクトリ、新しい設定項目のいずれも増えない
(plan.md Technical Context)。`dpf-go` の既存の型 (`OverwriteRecordsInner.Labels`) と、
未使用だった `ApiGetRecordCurrentsRequest.KeywordsLabel` を使うだけである。

形だけのタスクを置かない。置くと、実体のない作業が完了印だけ付いて並ぶ。

---

## Phase 2: Foundational

**不要。** 本機能は 001 の適用経路に 1 段を挟むものであり、ストーリーをブロックする
前提作業がない。

---

## Phase 3: User Story 1 - 管理下のレコードを見分ける (Priority: P1) 🎯 MVP

**Goal**: 本 provider が書くレコードのラベルが、常に
`managed-by: external-dns-iij-dpf-webhook` のみになる。運用者が印で絞り込めば、
管理下のレコードだけを一覧できる。

**Independent Test**: 検証用ゾーンへレコードを作成し、DPF 上でラベルが印のみである
ことを確認する。人手で作ったレコードには印が付かず、印で絞ると前者だけが返ることを
確認する (quickstart.md 1〜3)。

### Tests for User Story 1 ⚠️ 先に書いて失敗を確認する

- [X] T001 [P] [US1] `internal/dpf/label_test.go` を新規に作り、印の設定を検証する。作成・更新のレコードのラベルが**印のみ**になること (FR-001)、`TXT` も同様であること (FR-002)、変更セットに含まれないレコードのラベルが変わらないこと (FR-003)、独自のラベルを持つレコードでは**それが失われること** (PC-001)、`managed-by` に別の値があれば置き換わること、2 回適用しても変わらないこと (FR-005)
- [X] T002 [P] [US1] `internal/dpf/label_test.go` に、**反映済みレコードのラベルのマップが書き換わらないこと**のテストを追加する。`toOverwrite` は元のマップへの参照を投入集合へ入れており、上書き方式が「新しいマップを代入する」ことでこれを避けている (research R3)。加算方式へ戻されたときに落ちる表明である
- [X] T003 [P] [US1] `test/e2e/label_test.go` を新規に作り、DPF 上のラベルが印のみであることを検証する (SC-001)。`DPF_E2E_TOKEN_FILE` と `DPF_E2E_ZONE` が未設定ならスキップする既存の流儀に揃える。所有権 `TXT` にも印が付くこと (SC-002)、印で絞り込んだ一覧に管理外のレコードが含まれないこと (SC-003)、触らないレコードのラベルが変わらないこと (SC-004) を含める

### Implementation for User Story 1

- [X] T004 [US1] `internal/dpf/label.go` を新規に作り、投入集合のうち変更セットに含まれるレコードのラベルを印のみに設定する。**`merge.go` は変更しない。** マージ結果に対する独立した段とすることで、投入集合の組み立ての誤りと印の誤りを区別できる (research R3)。印の値は `apply.go` の `applyAttribution` を使う。**定数を 2 つにしない** (research R6)
- [X] T005 [US1] `internal/dpf/apply.go` の `Apply` で、`merge` の後・`guard` の前に印の設定を挟む。**ガードより前に置く理由**: 投入前ガードは投入する集合を検査する。印を付けた後の集合を検査しなければ、検査した集合と送る集合が違う
- [X] T006 [US1] `internal/dpf/records.go` に、ラベルで絞った反映済みレコードの取得を実装する。`KeywordsLabel` に `managed-by=external-dns-iij-dpf-webhook` を渡す。`observe` でラップして操作名を `list_records_by_label` とする。**通常の動作経路では使わない** (contracts)
- [X] T007 [US1] `docs/reference.md` の `dpf-operations` 表に `list_records_by_label` の行を追加する。`test/docs/reference_test.go` が `observe` の引数と表を `assertSameSet` で突き合わせるため、**T006 を入れた時点でこれを済ませるまで `go test ./test/docs/` は失敗する**。004 の `list_zone_histories` で踏んだのと同じ結合である

**Checkpoint**: `go test ./...` が通る。実環境で `TestManagedByLabel` が通る

---

## Phase 4: Polish & Cross-Cutting Concerns

- [X] T008 [P] `README.md` に、本 provider が書くレコードには印が付くこと、印で絞り込めることを追記する。**利用前提条件の節に PC-001 を置く** — 印の付いたレコードを他の手段から変更することは可能だが動作は保証しない、ラベルは次の更新で印のみに戻る。**保証しない範囲を数え上げない** (spec の PC-001)
- [X] T009 [P] `specs/001-webhook-provider/contracts/dpf-client.md` の「変更の適用」に、ラベルを設定することを 005 への相互参照として 1 行加える。001 の契約を読んだ人が、実装との差分に気付けないままになるのを防ぐ (004 の T008 と同じ理由)
- [X] T010 `golangci-lint run` と `go vet ./...` を実行し、新規ファイルに SPDX ヘッダがあることを `make license-check` で確認する (constitution v1.9.0)

**Checkpoint**: `make all` 通過

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1) / Foundational (Phase 2)**: 不要
- **US1 (Phase 3)**: 依存なし。即座に開始できる
- **Polish (Phase 4)**: US1 の完了に依存

### Within User Story 1

- **テストを先に書き、失敗を確認してから実装する (原則 III、NON-NEGOTIABLE)**
- T001・T002 は T004 に依存する (印の設定を行う関数がないと呼べない)。
  **先にテストを書いて、必要な形をテストの側から示す**
- T005 は T004 に依存する
- T006 → T007 は直列。`test/docs` が等価を要求するため、T006 だけ入れた状態は
  検査が落ちる
- T003 (実環境) は T004・T005・T006 のすべてに依存する

### Parallel Opportunities

- テスト 3 本 (T001〜T003) はすべて並行して書ける (T001・T002 は同一ファイルだが
  内容が独立している)
- 実装は T004 → T005、T006 → T007 の 2 系列。**系列どうしは並行できる**
- Polish の T008・T009 は別ファイルで並行できる

---

## Implementation Strategy

本機能はユーザーストーリーが 1 本であり、MVP と完成が一致する。段階的な
デリバリの余地はない。

1. テスト 3 本を書き、**落ちることを確認する**
2. T004 → T005 (印の設定と、適用の流れへの挟み込み)
3. T006 → T007 (絞り込みの読み取りと、文書の追随)
4. `go test ./...` で単体と文書検査が通ることを確認する
5. 実環境で `TestManagedByLabel` を走らせる
6. Polish

### 実装時に特に注意する点

- **T004 (`merge.go` を変更しない)**: 印の設定を `merge` に織り込むと、投入集合の
  組み立ての誤りと印の誤りが区別しにくくなる。独立した段に保つ
- **T004 (定数を 2 つにしない)**: 印の値は 004 の `applyAttribution` と同じである。
  別に定義すると、片方だけ直した変更で食い違う (research R6)
- **T004 (マップを代入する)**: 既存のマップを書き換えず、新しいマップを代入する。
  `toOverwrite` は反映済みレコードのマップへの参照を投入集合へ入れている
  (research R3)
- **T005 (ガードより前に置く)**: 投入前ガードは投入する集合を検査する。印を付けた後の
  集合を検査しなければ、検査した集合と送る集合が違う
- **T006 (通常の動作で呼ばない)**: 本サービスが自分の印を読み返して動作を変える
  設計にすると、DPF 側の取得失敗が DNS の更新を止める経路になる (004 の
  `ZoneHistories` と同じ扱い)
- **上限の扱いを足さない**: ラベルは常に 1 件であり、DPF の上限 (10) に触れる経路が
  存在しない。**分岐を足したくなったら、上書き方式を崩していないか確かめる**
  (research R2)

### 機械化しない確認

quickstart.md 3 のうち、**DPF コンソールから人手でレコードを作る手順は機械化しない。**
コンソールの操作を伴うためである。**機械化しない理由を
`test/e2e/label_test.go` の冒頭に記す** (001・004 と同じ方式)。理由を書かずに省くと、
抜けと区別がつかなくなる。

一方、**触らないレコードのラベルが変わらないこと** (FR-003、SC-004) は機械化する。
人手のレコードを巻き込まないことが、印の価値の前提である。
