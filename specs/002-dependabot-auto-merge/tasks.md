---

description: "Task list for feature implementation"
---

# Tasks: 依存パッケージの更新 Pull Request の自動作成

**Input**: Design documents from `/specs/002-dependabot-auto-merge/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: 本機能の成果物は設定ファイルとワークフローである。原則 III
(テストファースト) を「**設定ファイルだからテストは書けない**」で済ませない。
plan.md「原則 III への対応」が列挙する不変条件を `test/config/` の検査として
先に書き、**落ちることを確かめてから**設定を足す。

**Organization**: タスクはユーザーストーリーごとに分けてある。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 並行して進められる (別のファイル、未完了のタスクに依存しない)
- **[Story]**: 対応するユーザーストーリー (US1, US2, US3)

## 現在の状態

**本機能は未着手である。** `.github/dependabot.yml` は存在せず、`scheduled.yml`
には `govulncheck` / `image` / `notify` の 3 ジョブしかない。

### 前提の改訂 (2026-09-14)

`github.com/iij/dpf-go` が公開され、モジュールの取得に資格情報が要らなくなった。
これにより **data-model.md「必須検査の集合」の表が変わっている**。

- Dependabot 起点の実行でも、ビルド・静的解析・テスト・イメージのビルドは**通る**
- 失敗するのは `DPF_TOKEN` を要する `e2e` / `e2e-sidecar` だけ
- `registries` と `DPF_GO_READ_TOKEN` は**設けない** (research R3 改訂)

FR-013 (更新後の依存コードと検証用ゾーンのトークンを同居させない) と FR-014
(実行できない検査を成功として扱わない) の要求は変わらない。対象が e2e 系に
絞られただけである。**改訂後の data-model.md を前提にタスクを進めること。**

### 設計文書に対して見つかった欠け

- **`actionlint` がどこにも組み込まれていない。** contracts/ci-gates.md は
  「ワークフローの静的検査」を、秘密情報を要さず Dependabot 起点でも実行される
  検査として挙げている (FR-012)。現状は `ci.yml` にも `Makefile` にも無い。
  T025〜T027 で補う

---

## Phase 1: Setup

**Purpose**: 不変条件の検査を置く場所と、YAML を読む手段を用意する

- [X] T001 `test/config/doc.go` を作り、パッケージの目的 (`.github/dependabot.yml` と既存ワークフローの不変条件を検査する) と検査の対象・非対象を doc コメントに書く。SPDX ヘッダを付ける。`test/contract`、`test/docs` と同じ階層に置くのは `go test ./...` と `make all` に自動で乗せるため (plan.md「Structure Decision」)
- [X] T002 `gopkg.in/yaml.v3` を直接依存として `go.mod` に加える (`go get gopkg.in/yaml.v3`)。既にモジュールグラフ上には存在するが直接依存ではない。`go.sum` の追加分を確認し、`make all` が通ることを確かめる

---

## Phase 2: Foundational (US1 / US2 の検査が共有する土台)

**Purpose**: 設定ファイルとワークフローを読み取る仕組みを作る

**⚠️ このフェーズは US1 と US2 を阻む。** US3 (`scheduled.yml` への追記) は
この土台を必要としないため、待たずに着手できる (Dependencies 参照)。

- [X] T003 `test/config/load_test.go` に読み取りに対する表明を書く。`.github/dependabot.yml` を構造体へ読めること、**ファイルが無い場合はエラーとして返ること**、解析できない場合もエラーとなることを検査する。**まだ実装がないため落ちることを確認する**
- [X] T004 `test/config/load.go` に `.github/dependabot.yml` の読み取りを実装し、T003 を通す。`version` / `registries` / `updates` (`package-ecosystem`、`directory`、`schedule`、`cooldown`、`groups`、`open-pull-requests-limit`、`labels`、`ignore`) を保持する構造体を定義する (data-model.md 1、contracts/dependabot-config.md)
- [X] T005 `test/config/load.go` に `.github/workflows/*.yml` を列挙して読み取る関数を追加する。US2 の「自動マージが存在しないこと」「検査が条件付きで飛ばされていないこと」の検査が使う。ファイルが 1 本も見つからない場合はエラーとする (原則 VI: 対象が空でも通る検査にしない)
- [X] T006 `test/config/load_test.go` に T005 の表明を追加する。既存の 5 本を列挙できること、対象が空のときエラーになることを検査する

**Checkpoint**: 読み取りの土台ができた。US1 と US2 の検査を書ける

---

## Phase 3: User Story 1 - 依存の更新が自動で提案される (Priority: P1) 🎯 MVP

**Goal**: 依存に新しい版が出てから 5 日が経つと、その更新を適用する Pull Request が
自動で作られる。Go モジュール・GitHub Actions・コンテナ基底イメージの 3 種別が
対象で、種別ごとに 1 件へまとめられる。

**Independent Test**: 更新可能な版が 5 日以上前に公開されている状態で、Insights →
Dependency graph → Dependabot に 3 種別が現れ、エラーが出ず、種別ごとに 1 件の
Pull Request が作られることを確認する (quickstart.md 3a、3b)。

### 検査を先に書く (原則 III) ⚠️

> **これらは実装 (T015) より先に書き、落ちることを確認する。**

- [X] T007 [P] [US1] `test/config/dependabot_test.go` に、`cooldown.default-days` が `5` であることの表明を書く。**省略されている場合も失敗させる。** 省略すると Dependabot の既定 3 日に緩み、FR-002 と食い違う (data-model.md 2、research R1)
- [X] T008 [P] [US1] `test/config/dependabot_test.go` に、`gomod` / `github-actions` / `docker` の 3 種別が揃っていることの表明を書く。`directory` がそれぞれ `/`、`/`、`/build` であることも検査する (FR-004、research「対象となる依存の所在」)
- [X] T009 [P] [US1] `test/config/dependabot_test.go` に、種別ごとに `groups` が 1 つ定義され `patterns` が `["*"]` であることの表明を書く (FR-005、research R6)
- [X] T010 [P] [US1] `test/config/dependabot_test.go` に、`open-pull-requests-limit` が種別ごとに明示され、既定の `5` より小さいことの表明を書く。**省略されている場合は失敗させる** (FR-008、原則 VI)
- [X] T011 [P] [US1] `test/config/dependabot_test.go` に、メジャー版を `ignore` していないことの表明を書く。`update-types` に `version-update:semver-major` を含む `ignore` があれば失敗させる (FR-007)
- [X] T012 [P] [US1] `test/config/dependabot_test.go` に、`registries` が定義されていないことの表明を書く。`dpf-go` は公開済みであり、モジュール取得のための資格情報を残さない (contracts/dependabot-config.md「書いてはならない内容」、research R3 改訂)
- [X] T013 [P] [US1] `test/config/dependabot_test.go` に、`schedule` が週 1 回 (`interval: weekly`) であることと、`labels` に `dependencies` が含まれることの表明を書く (Assumptions「更新の検出間隔は週 1 回」、quickstart.md 3b)
- [X] T014 [US1] `go test ./test/config/...` を実行し、**T007〜T013 のすべてが落ちること**を確認する。設定ファイルがまだ無いため「読み取りエラー」で落ちるはずである。この時点でエラーの文面が原因を示しているかを確かめる

### 実装

- [X] T015 [US1] `.github/dependabot.yml` を新規作成し、T007〜T013 を通す。SPDX ヘッダを置く。`version: 2`、3 種別、`cooldown.default-days: 5`、`groups` (`patterns: ["*"]`)、`open-pull-requests-limit: 3`、`schedule` は毎週月曜 04:00 Asia/Tokyo、`labels: [dependencies]`。**`registries` は書かない** (contracts/dependabot-config.md「参考: 想定する形」)
- [X] T016 [US1] `.github/dependabot.yml` に、各設定値がどの要件に対応するかをコメントで書く。とくに `cooldown` について「**省略すると既定 3 日に緩む**」ことと、`open-pull-requests-limit` について「検証用ゾーンを共有するため待ち行列を有界に保つ」ことを残す
- [X] T017 [US1] `go test ./test/config/...` と `make all` が通ることを確認する

### 検査が確かに落ちることの確認 ⚠️

> **通る検査を書いただけでは何も守っていない。** 壊して落ちることを確かめる。

- [X] T018 [US1] `.github/dependabot.yml` を一時的に壊し、次のそれぞれで対応する検査が落ちることを確認する: `cooldown` の節を消す / `default-days` を `3` にする / `docker` の節を消す / `groups` を消す / `open-pull-requests-limit` を `10` にする / メジャー版の `ignore` を足す / `registries` を足す / ファイルごと消す。確認後は元に戻す

### 設定が読まれたことの確認 (GitHub 側)

- [ ] T019 [US1] `.github/dependabot.yml` を main へ入れた後、Insights → Dependency graph → Dependabot を開き、3 種別が一覧に現れること、最後に確認した時刻が投入後に更新されていること、**エラーが出ていないこと**を確認する (quickstart.md 3a)
- [ ] T020 [US1] "Check for updates" を手動実行し、種別ごとの Pull Request が各 1 件にまとまっていること、本文に更新前後の版と上流の比較への導線があること、ラベル `dependencies` が付くことを確認する (FR-006、quickstart.md 3b)
- [ ] T021 [US1] リポジトリ設定で **Dependabot alerts** と **Dependabot security updates** を有効にする。**`.github/dependabot.yml` では有効にできない。** FR-003 (脆弱性の修正に待機期間を適用しない) と SC-003 (公開から 1 日以内に提案される) はこの設定に依存しており、無効のままだと脆弱性の修正も `cooldown: 5` に阻まれた状態が続く (research R8、data-model.md「設定の所在」、quickstart.md 3c)

**Checkpoint**: US1 が独立して機能する。更新が自動で提案される

---

## Phase 4: User Story 2 - 更新の可否をレビュー時点で判断できる (Priority: P2)

**Goal**: 更新 Pull Request を開いた時点で、何が変わるのか・既存の品質ゲートを
通るのかが揃っている。実行できない検査は**成功として現れない**。

**Independent Test**: Dependabot 起点の更新 Pull Request に対して、秘密情報を
要さない検査が実行され、`e2e` 系が「skipped」ではなく「失敗」として現れること、
失敗の説明に次に何をすべきかが書かれていることを確認する (quickstart.md 4a)。

### 検査を先に書く (原則 III) ⚠️

- [X] T022 [P] [US2] `test/config/workflows_test.go` に、**自動マージを行う仕組みが存在しないことの表明**を書く。`.github/workflows/*.yml` と `.github/dependabot.yml` に `gh pr merge`、`--auto`、`enable-pull-request-automerge`、`pull_request_target` のいずれも現れないことを検査する (FR-009、plan.md「原則 III への対応」)
- [X] T023 [P] [US2] `test/config/workflows_test.go` に、**秘密情報を要する検査が条件付きで飛ばされていないことの表明**を書く。`github.actor` や `dependabot` を条件に含む `if:` が、秘密情報を使うジョブ・ステップに付いていないことを検査する。飛ばした検査は「skipped」となりブランチ保護では成功として数えられる (FR-014、SC-004)
- [X] T024 [US2] `go test ./test/config/...` を実行し、T022 と T023 が現状で**通る**ことを確認する。通ってよい (今は自動マージも条件分岐も無い)。そのうえで、試しに自動マージのステップと `if: github.actor != 'dependabot[bot]'` を一時的に足し、**確かに落ちること**を確認してから戻す

### 実装

- [X] T025 [P] [US2] `Makefile` に `actionlint` の実行ターゲット (`workflow-lint`) を足し、`all` の依存に加える。バージョンは `ACTIONLINT_VERSION` として `Makefile` 冒頭のツール版一元管理の並びに固定し、`tools` ターゲットの `go install` にも加える (constitution v1.3.0、research R7)
- [X] T026 [US2] `.github/workflows/ci.yml` に `actionlint` を実行するステップを足す。**秘密情報を要さないため、Dependabot 起点でも実行される** (FR-012、contracts/ci-gates.md「秘密情報を要しない検査」)。`Makefile` と同じ版を参照する
- [X] T027 [US2] `docs/development.md` のツール一覧に `actionlint` を追記する
- [X] T028 [US2] `.github/workflows/e2e.yml` の「検証用の資格情報を用意する」ステップの失敗メッセージに、**Dependabot 起点の場合の次の手順**を足す。contracts/ci-gates.md が示す文面 (上流の差分を確認したうえで、この Pull Request のブランチへコミットを 1 つ積む) を `GITHUB_STEP_SUMMARY` に出す。**`if:` で飛ばす形にしない** (FR-014、FR-019)
- [X] T029 [US2] `.github/workflows/e2e-sidecar.yml` の「検証用の資格情報を確認する」ステップにも T028 と同じ案内を足す
- [X] T030 [US2] `docs/development.md` に「依存更新 Pull Request を取り込む手順」の節を足す。上流の差分を確認する → その Pull Request のブランチへコミットを 1 つ積む → 以降の実行では全検査が走る → レビュー承認を経てマージする、という流れを書く。**新しいワークフローを作らないこと**と、その理由 (FR-010、FR-015) を併記する
- [X] T031 [US2] `docs/development.md` に、**Dependabot の対象外**である項目を明記する。`Makefile` と `ci.yml` の `env` に固定したツール版 (`GOLANGCI_LINT_VERSION`、`GOVULNCHECK_VERSION`、`SYFT_VERSION`、`ACTIONLINT_VERSION`) は依存の記述ではないため更新は人が行う (plan.md「技術・配布制約との関係」、research R6 末尾)

### ブランチ保護 (GitHub 側の手作業)

- [ ] T032 [US2] `main` のブランチ保護で、必須検査に `ci.yml` の全ジョブと `e2e` を設定する。**依存更新のために必須検査の集合を変えない** (FR-010、contracts/ci-gates.md「マージ」)
- [ ] T033 [US2] `main` のブランチ保護で、レビュー承認を 1 件以上必須にする。自動マージの設定を有効にしない (FR-009)
- [ ] T034 [US2] 実際の更新 Pull Request で、秘密情報を要さない検査が実行され、`e2e` 系が**失敗**として現れること (skipped ではないこと)、失敗の説明に次の手順が書かれていることを確認する (quickstart.md 4a)
- [ ] T035 [US2] その Pull Request のブランチへコミットを 1 つ積み、**必須検査すべてが実行されその結果が Pull Request に紐づくこと**を確認する (FR-015、quickstart.md 4b)

**Checkpoint**: US1 と US2 が独立して機能する。提案と判断材料が揃う

---

## Phase 5: User Story 3 - 提案が滞留していることに気付ける (Priority: P3)

**Goal**: 更新の検出が止まっていること、および取り込まれずに残っている提案が
あることが、人が画面を見に行かなくても伝わる。

**Independent Test**: `scheduled.yml` を `workflow_dispatch` で走らせ、更新可能な
依存があるのに提案が無い場合と、30 日を超えた Pull Request がある場合に、既存の
`notify` 経路で Issue が立つことを確認する (quickstart.md 6a、6b)。

**⚠️ このフェーズは Phase 2 を必要としない。** `scheduled.yml` への追記であり、
`test/config` の土台に依存しない。

### 実装

- [X] T036 [US3] `.github/workflows/scheduled.yml` の `permissions` に `pull-requests: read` を足す。提案の照合に開いている Pull Request の一覧が要る
- [X] T037 [US3] `.github/workflows/scheduled.yml` に「提案の欠落を検査する」ジョブを足す。`go list -m -u all` で更新可能な**直接依存**を求め、`gh pr list --author app/dependabot` が返す Pull Request が含む依存と照合する。期待側にあって実際側にないものがあれば失敗させる (FR-017、SC-009、research R4、contracts/ci-gates.md)
- [X] T038 [US3] `.github/workflows/scheduled.yml` の T037 のジョブに**待機期間の考慮**を入れる。公開から 5 日未満の版は提案されないのが正しいため、報告の対象から外す。版の公開日は `go list -m -u -json all` の `Time` から採る。**待機期間内の版を「提案されていない」と報告しないこと** (contracts/ci-gates.md「提案の欠落」)
- [X] T039 [US3] `.github/workflows/scheduled.yml` に「滞留を検査する」ジョブを足す。開いている Dependabot の Pull Request のうち作成から **30 日**を超えたものがあれば失敗させる。**自動で閉じない** (FR-011、FR-018、SC-008、research R5)
- [X] T040 [US3] `.github/workflows/scheduled.yml` の `notify` ジョブの `needs` に T037 と T039 のジョブを足し、Issue の本文を検出内容で出し分ける。脆弱性・提案の欠落・滞留のどれで起きたかが題名から分かるようにする。**新しい通知手段を足さない** (research R5)
- [X] T041 [US3] `.github/workflows/scheduled.yml` の `notify` の重複抑止 (未解決の同種 issue があればコメントに留める) が、増えた検出種別でも種別ごとに効くことを確認する。題名が同一だと別種の検出が 1 つの issue に埋もれる

### 検査が確かに落ちることの確認 ⚠️

- [ ] T042 [US3] `workflow_dispatch` で `.github/workflows/scheduled.yml` を走らせ、正常時に報告が出ないことを確認する (quickstart.md 6a の 1 行目、6b の 1 行目)
- [ ] T043 [US3] `.github/dependabot.yml` の `gomod` の節を一時的に壊した状態で T037 を走らせ、**提案の欠落が報告されること**を確認する。「更新がない」と「仕組みが動いていない」が区別できることがこの検査の目的である (quickstart.md 6a)
- [ ] T044 [US3] `.github/workflows/scheduled.yml` の滞留の判定について、30 日以内の Pull Request で報告が出ないこと、30 日を超えたもので報告が出ることを確認する。閾値を一時的に下げて確かめてよい (quickstart.md 6b)

**Checkpoint**: US1・US2・US3 がすべて独立して機能する

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T045 [P] `make all` と `actionlint` がすべて通ることを確認する
- [ ] T046 [P] `specs/002-dependabot-auto-merge/quickstart.md` を最初から通しで実行し、各節の期待される結果と一致することを確認する
- [ ] T047 憲章「開発ワークフローと品質ゲート」および「実環境での検証」が必須とする検査の一覧と、ブランチ保護の必須検査の設定を突き合わせる。**依存更新のために減らした集合になっていないこと**を確認する (FR-010、SC-004)
- [ ] T048 `.specify/feature.json` が別の feature を指している場合に備え、本機能の完了時点で `specs/002-dependabot-auto-merge/tasks.md` のチェックがすべて埋まっていることを確認する
- [ ] T049 Dependabot secrets に不要な資格情報が残っていないことを確認する。`DPF_GO_READ_TOKEN` は設けない。`DPF_TOKEN` を Dependabot secret として登録しない (FR-013、quickstart.md 7)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 依存なし。すぐ着手できる
- **Foundational (Phase 2)**: Setup の完了に依存。**US1 と US2 を阻む**
- **US1 (Phase 3)**: Foundational の完了後
- **US2 (Phase 4)**: Foundational の完了後。US1 とは独立して進められるが、T034・T035 は実際の更新 Pull Request を要するため US1 の T019 以降を待つ
- **US3 (Phase 5)**: **Setup / Foundational を待たない。** `scheduled.yml` への追記であり `test/config` を使わない。ただし T043 は US1 の成果物 (`.github/dependabot.yml`) を要する
- **Polish (Phase 6)**: すべてのストーリーの完了後

### User Story Dependencies

- **US1 (P1)**: 他のストーリーに依存しない。単独で価値がある (更新が提案される)
- **US2 (P2)**: US1 に依存しない (検査の振る舞いは更新 Pull Request の有無と独立に整えられる)。ただし**確認作業**は US1 の成果物を要する
- **US3 (P3)**: US1・US2 に依存しない。ただし T043 の確認だけ US1 を要する

### Within Each User Story

- 検査を先に書き、**落ちることを確認してから**実装する (原則 III)
- 設定の追加 → 検査が通ることの確認 → **壊して落ちることの確認**、の順を守る
- GitHub 側の確認 (Insights、ブランチ保護) は、成果物が main に入った後になる

### Parallel Opportunities

- T007〜T013 は同じファイルに書くが、内容は独立しており分担できる
- T022・T023 は T007〜T013 と別ファイルであり並行できる
- T025 (Makefile) と T028・T029 (ワークフロー) は別ファイルであり並行できる
- **US3 (Phase 5) は Phase 1・2 と並行して進められる**

---

## Parallel Example: User Story 1 の検査

```bash
# T007〜T013 は独立した表明であり、分担して同時に書ける
Task: "cooldown.default-days が 5 であることの表明"          # T007
Task: "3 種別が揃っていることの表明"                          # T008
Task: "groups が patterns ['*'] であることの表明"             # T009
Task: "open-pull-requests-limit が 5 未満であることの表明"     # T010
Task: "メジャー版を ignore していないことの表明"               # T011
Task: "registries が存在しないことの表明"                     # T012
Task: "schedule が weekly、labels に dependencies の表明"     # T013
```

---

## Implementation Strategy

### MVP First (User Story 1 のみ)

1. Phase 1 (Setup) を終える
2. Phase 2 (Foundational) を終える — **US1 と US2 を阻む**
3. Phase 3 (US1) を終える
4. **止めて確認する**: 3 種別の提案が週次で作られ、5 日未満の版が含まれないこと
5. この時点で「更新に気付く」「更新差分を用意する」手作業が消える。単独で価値がある

### Incremental Delivery

1. Setup + Foundational → 土台ができる
2. US1 を足す → 単独で確認 → **MVP**。更新が自動で提案される
3. US2 を足す → 単独で確認 → 判断材料が Pull Request 上で揃う
4. US3 を足す → 単独で確認 → 検出の停止と滞留に気付ける

US3 は Phase 1・2 を待たないため、US1 と並行して進めてもよい。

### 本機能に固有の注意

- **「設定ファイルだからテストは書けない」で済ませない。** 将来「便利だから」と
  自動マージを足す変更や、「失敗が邪魔だから」と検査を飛ばす変更は、T022 と
  T023 の検査に落ちる。文書の記述だけではそうした変更を止められない
  (plan.md「原則 III への対応」)
- **落ちない検査は何も守っていない。** T018・T024・T043・T044 を省かない
- **確認作業の多くが GitHub 側の操作**である。Insights の画面、ブランチ保護の
  設定、実際の更新 Pull Request での挙動は、手元では確かめられない

---

## Notes

- `[P]` は別ファイルで依存の無いタスクを指す
- `[Story]` はトレーサビリティのためのラベルである
- 各タスクまたは論理的なまとまりごとにコミットする
- チェックポイントで止まり、そのストーリーを単独で確認できる
