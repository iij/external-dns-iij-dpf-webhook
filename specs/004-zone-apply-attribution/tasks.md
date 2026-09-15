# Tasks: ゾーン反映の実行者を DPF の記録に残す

**Input**: Design documents from `/specs/004-zone-apply-attribution/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: **必須。** constitution v2.2.0 の原則 III (テストファースト) が
NON-NEGOTIABLE であるため、テストタスクは省略できない。各実装タスクの前にテストを
置き、**期待どおり失敗することを確認してから**実装に着手する。

**Organization**: 本機能のユーザーストーリーは 1 本だけである (spec.md)。価値が
「履歴から出どころを見分けられる」ことに尽き、独立して価値を持つ別のスライスが
存在しない。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 並行実行可能 (別ファイル、未完了タスクへの依存なし)
- **[Story]**: 対応するユーザーストーリー (US1)
- 各タスクに具体的なファイルパスを含む

---

## Phase 1: Setup

**不要。** 新しい依存、新しいディレクトリ、新しい設定項目のいずれも増えない
(plan.md Technical Context)。`dpf-go` の既存の型と、未使用だった
`ZoneHistoriesAPI` を使うだけである。

形だけのタスクを置かない。置くと、実体のない作業が完了印だけ付いて並ぶ。

---

## Phase 2: Foundational

**不要。** 本機能は 001 の適用経路に 1 項目を加えるものであり、ストーリーを
ブロックする前提作業がない。

---

## Phase 3: User Story 1 - 履歴から変更の出どころを見分ける (Priority: P1) 🎯 MVP

**Goal**: ゾーンの反映を行うたびに、実行者が本サービスであることが DPF の履歴に
残る。運用者が履歴を見て、本サービスによる反映と人手による反映を見分けられる。

**Independent Test**: 検証用ゾーンへ変更を適用し、ゾーン反映履歴を読む。当該の
反映の説明に `external-dns-iij-dpf-webhook` が含まれることを確認する
(quickstart.md 2)。

### Tests for User Story 1 ⚠️ 先に書いて失敗を確認する

- [X] T001 [P] [US1] `internal/dpf/apply_test.go` を新規に作り、一括更新の要求の組み立てを検証する。説明に `external-dns-iij-dpf-webhook` が入ること (FR-002)、変更セットの内容とゾーンを変えても説明が変わらないこと (data-model 1 の不変条件)、説明が 80 オクテット以内であること (FR-006)、`overwrite_soa` と `overwrite_zone_apex_ns` が引き続き `false` であること (001 の回帰防止)
- [X] T002 [P] [US1] `internal/dpf/merge_test.go` に、識別名がレコードの `Description` に書かれないことのテストを追加する (FR-007)。既存のコメントが逐語的に保たれることは `TestMerge_*` が既に固定しているため、**新たに書き込まれないこと**の側を押さえる
- [X] T003 [P] [US1] `test/e2e/attribution_test.go` を新規に作り、適用後にゾーン反映履歴を読んで説明に識別名が含まれることを検証する (FR-001、SC-002)。`DPF_E2E_TOKEN_FILE` と `DPF_E2E_ZONE` が未設定ならスキップする既存の流儀に揃える。連続 2 回の適用で 2 件とも記録が付くこと、適用前に存在した履歴が書き換わっていないこと (FR-008) も含める

### Implementation for User Story 1

- [X] T004 [US1] `internal/dpf/apply.go` に識別名の定数を置き、一括更新の要求の組み立てを `atomicChangesBody` として切り出して `Description` を設定する。切り出すのは、API を呼ばずに組み立てだけを検証できるようにするため (T001 が依存)
- [X] T005 [US1] `internal/dpf/history.go` を新規に作り、ゾーン反映履歴の読み取りを実装する。`*Client` のメソッドとし、**`provider.Backend` には加えない** (contracts)。`observe` でラップして操作名を `list_zone_histories` とする。生成型を境界の外へ出さず、反映時刻と説明の対だけを返す
- [X] T006 [US1] `docs/reference.md` の `dpf-operations` 表に `list_zone_histories` の行を追加する。`test/docs/reference_test.go:395` が `observe` の引数と表を `assertSameSet` で突き合わせるため、**T005 を入れた時点でこれを済ませるまで `go test ./test/docs/` は失敗する。** 行には検証にのみ用いる操作である旨を添える

**Checkpoint**: `go test ./...` が通る。実環境で `TestApplyAttribution` が通る

---

## Phase 4: Polish & Cross-Cutting Concerns

- [X] T007 [P] `README.md` に、DPF の履歴から本 provider による反映を見分けられることを追記する。利用前提条件 (PC-001〜PC-004) の節の近くに置く。**記録があっても競合そのものは防げない**ことを併記する (spec の背景節)
- [X] T008 [P] `specs/001-webhook-provider/contracts/dpf-client.md` の「変更の適用」に、説明を添えることを 004 への相互参照として 1 行加える。001 の契約を読んだ人が、実装との差分に気付けないままになるのを防ぐ
- [X] T009 `golangci-lint run` と `go vet ./...` を実行し、新規ファイルに SPDX ヘッダがあることを `make license-check` で確認する (constitution v1.9.0)

**Checkpoint**: `make all` 通過

---

## Phase 5: 実環境で判明した修正 (履歴の並び順)

初回の実環境検証で `TestApplyAttribution` が落ちた。**履歴の並び順の既定は昇順で
あり、件数を絞ると最古の履歴だけが返っていた。** 実装側の思い込みであり、DPF の
挙動の意外性ではない (research R2 に追記)。

- [X] T010 `internal/dpf/history.go` で `Order(SEARCHORDER_DESC)` を明示する。既定 (`ASC`) に頼らない。あわせて「新しい順に返る」という誤った注釈を、既定が昇順である事実と打ち切り窓の注意に書き換える
- [X] T011 `test/e2e/attribution_test.go` の表明を打ち切り窓に耐える形へ直す。**件数の増減で判定しない** (窓は常に同じ件数で埋まる)。既存の履歴の不変は ID で突き合わせ、窓に残っているものだけを検査する
- [X] T012 [P] `specs/004-zone-apply-attribution/research.md` の R2 に並び順の既定と打ち切り窓の事実を追記し、`contracts/` に降順を明示する要件を MUST として加える

**Checkpoint**: `make all` 通過。次回の実環境検証で `TestApplyAttribution` が通ること

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1) / Foundational (Phase 2)**: 不要
- **US1 (Phase 3)**: 依存なし。即座に開始できる
- **Polish (Phase 4)**: US1 の完了に依存

### Within User Story 1

- **テストを先に書き、失敗を確認してから実装する (原則 III、NON-NEGOTIABLE)**
- T001 は T004 に依存する (組み立ての切り出しがないと呼べない)。**先にテストを
  書いて、切り出しが必要であることをテストの側から示す**
- T005 → T006 は直列。`test/docs` が等価を要求するため、T005 だけ入れた状態は
  検査が落ちる
- T003 (実環境) は T004・T005 の双方に依存する

### Parallel Opportunities

- テスト 3 本 (T001〜T003) はすべて別ファイルで並行できる
- Polish の T007・T008 は別ファイルで並行できる
- 実装 (T004〜T006) は直列。T004 と T005 は別ファイルだが、T006 が T005 に
  依存するため、まとめて順に進めるのが早い

---

## Implementation Strategy

本機能はユーザーストーリーが 1 本であり、MVP と完成が一致する。段階的な
デリバリの余地はない。

1. テスト 3 本を書き、**3 本とも落ちることを確認する**
2. T004 → T005 → T006 の順に実装する
3. `go test ./...` で単体と文書検査が通ることを確認する
4. 実環境で `TestApplyAttribution` を走らせる
5. Polish

### 実装時に特に注意する点

- **T004 (組み立ての切り出し)**: `atomicChanges` は現在、要求の組み立てと API 呼び出しを
  ひとつの関数で行っている。組み立てだけを取り出さないと、説明が載ることを API なしに
  検証できない。**切り出しはテストのためだけの変更ではなく、検証可能性そのものである**
- **T005 (履歴の読み取り)**: 通常の動作経路から呼ばない。本サービスが自分の記録を
  読み返して動作を変える設計にすると、DPF 側の保持期間や取得失敗が DNS の更新を
  止める経路になる (plan.md 設計上の注意点)。ファイルを分けるのは、通常の経路から
  呼ばれていないことが読んで分かるようにするため
- **T006 (文書の追随)**: `assertSameSet` は等価を要求する。操作を増やしたら表も
  増やす。001 の `record-types` で同じ結合を踏んでいる
- **説明を可変にしない**: 上限 80 オクテットはスキーマに宣言されている
  (research R5)。変更件数などを足したくなったら、80 を超えないことを型または
  検査で保証すること

### 機械化しない確認

quickstart.md 3 (人手の変更と区別できること) は DPF コンソールの操作を伴うため
機械化しない。**機械化しない理由を `test/e2e/attribution_test.go` の冒頭に記す**
(001 の `quickstart_test.go` と同じ方式)。理由を書かずに省くと、抜けと区別が
つかなくなる。
