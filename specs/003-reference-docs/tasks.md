---

description: "Task list for feature implementation"
---

# Tasks: リファレンス文書

**Input**: Design documents from `/specs/003-reference-docs/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: 本機能の成果物**そのもの**が検査である。原則 III (テストファースト) は、
「検査を書き、落ちることを確かめてから、通るようにする」という形で適用する。

**Organization**: タスクはユーザーストーリーごとに分けてある。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 並行して進められる (別のファイル、未完了のタスクに依存しない)
- **[Story]**: 対応するユーザーストーリー (US1, US2, US3)

## 現在の状態

**US1 と US2 に相当する記述は `docs/reference.md` として既に存在する**
(コミット `98783eb`)。これらのフェーズは、書き起こしではなく**受け入れ条件に
照らした確認と、欠けの補填**が中心になる。

**US3 (文書と実装の一致検査) は未実装である。** 本機能の作業量の大半がここにある。

---

## Phase 1: Setup

**Purpose**: 検査を置く場所を用意する

- [X] T001 `test/docs/doc.go` を作り、パッケージの目的 (`docs/reference.md` と実装の一致を検査する) と、検査の対象・非対象の要約を doc コメントに書く。SPDX ヘッダを付ける
- [X] T002 `go test ./test/docs/...` と `make all` の双方で `test/docs` が実行されることを確認する。環境変数による読み飛ばしを設けない (FR-033、常に走る)

---

## Phase 2: Foundational (US3 の前提)

**Purpose**: 6 つの検査が共有する読み取りの仕組みを作る

**⚠️ このフェーズは US3 のみを阻む。** US1 と US2 は `docs/reference.md` の
確認作業であり、ここを待たずに着手できる (Dependencies 参照)。

- [X] T003 `test/docs/extract_test.go` に、文書の表の読み取りに対する表明を書く。印の直後の表から行を取り出せること、見出し行と区切り行を含まないこと、セルから `` ` `` と `**` を剥がすことを検査する。**まだ実装がないため落ちることを確認する**
- [X] T004 `test/docs/extract.go` に文書の表を読み取る関数を実装し、T003 を通す。印は `<!-- reference:<名前> -->` の形で単独行に置かれ、その直後 (空行を挟んでよい) の表を対象とする (data-model.md 2、3)
- [X] T005 `test/docs/extract_test.go` に、印の異常に対する表明を追加する。印が無い / 印の直後に表が無い / 表の行が 0 件 / 同じ名前の印が 2 つ / 未知の名前の印、のいずれもエラーとして返ること。**落ちることを確認してから実装する** (原則 VI、research R3)
- [X] T006 `test/docs/extract.go` に T005 の異常検出を実装し、通す
- [X] T007 `test/docs/extract.go` に、Go の構文木から呼び出しの引数である文字列リテラルを集める関数を実装する。対象パッケージのディレクトリと関数名 (`observe` / `HandleFunc` / `Handle`) を受け取り、第 n 引数の文字列リテラルを返す。**コメントや無関係な文字列を拾わないこと** (contracts/reference-checks.md)
- [X] T008 `test/docs/extract_test.go` に T007 の表明を書く。呼び出しの引数だけを拾い、コメント中や他の文字列を拾わないことを検査する

---

## Phase 3: User Story 1 - 設定とトークンの置き場所を正しく知る (Priority: P1)

**Goal**: 実装コードを読まずに、4 つのシークレット管理サービスそれぞれにトークンを
格納して起動できる状態にする。

**Independent Test**: 実装コードを読んだことのない人が、`docs/reference.md` だけを
見て 4 つの供給元それぞれで起動できるか。

- [X] T009 [US1] `docs/reference.md` の「アクセストークンの供給元」を読み、供給元ごとに **「識別子に渡すもの」「トークンが読まれる位置」「認証の手段」「必要な権限」の 4 項目**が揃っているか確認する。欠けがあれば補う (FR-007〜FR-011、SC-002)
- [X] T010 [US1] `docs/reference.md` に、値の中の特定のフィールドが読まれる供給元 (Vault の `token`) について、**フィールド名と、それが変更できるかどうか**が書かれていることを確認する (FR-009)
- [X] T011 [US1] `docs/reference.md` に、`--dpf-token-secret-endpoint` の供給元ごとの意味の違い (Azure = Key Vault の URL・必須、GCP = プロジェクト ID、AWS = 未使用、Vault = 接続先 URL) が明示されていることを確認する (FR-012)
- [X] T012 [US1] `docs/reference.md` に、供給元ごとのトークン格納手順と、供給元をまたいだ比較表があることを確認する (FR-013、FR-014)
- [X] T013 [US1] `docs/reference.md` に、設定項目の一覧、**意図的に用意していない項目とその理由**、および設定の組み合わせ規則 (排他・必須・条件付き必須) があることを確認する (FR-004〜FR-006)
- [X] T014 [US1] `docs/reference.md` に、トークンの取得頻度と、外部で差し替えたときの反映のされ方が書かれていることを確認する (FR-015)
- [X] T015 [US1] `README.md` から `docs/reference.md` へ**リンク 1 回**で到達でき、シークレット管理サービスの節から該当箇所へ直接たどれることを確認する (FR-002、SC-003)

---

## Phase 4: User Story 2 - 観測した値の意味を知る (Priority: P2)

**Goal**: 実装コードを読まずに、計測値の意味とエラーの分類を理解できる状態にする。

**Independent Test**: 実装コードを読んだことのない人が、文書だけを見て
「DNS レコードの変更に失敗した回数」を取り出す監視の式を組み立てられるか。

- [X] T016 [US2] `docs/reference.md` の計測値の表に、系列ごとに **名前・種別・単位・意味・付与されるラベル・値が変化する契機**の 6 項目が揃っているか確認し、欠けがあれば補う (FR-016、SC-006)
- [X] T017 [US2] `docs/reference.md` の計測値の名前が、**実際の出力に現れる名前**であることを確認する。内部の計測器名 (`dns_record_changes`) ではなく出力名 (`dns_record_changes_total`) であること。分布が `_bucket` / `_sum` / `_count` に展開される旨も記載する (FR-017、FR-019)
- [X] T018 [US2] `docs/reference.md` に、ラベルごとの取りうる値の候補、両形式が同一の計測値を表す旨、ゾーン名・レコード名・レコード値・認証情報が含まれない旨**と理由**、未記録の系列が出力に現れない旨があることを確認する (FR-018、FR-020〜FR-022)
- [X] T019 [US2] `docs/reference.md` に、ログの内容と外部 API のエラーの記録のされ方、追跡情報の送出条件、エラーの分類と外部への見え方、レコード種別ごとの制約、名前の正規化があることを確認する (FR-023〜FR-028)
- [X] T020 [US2] `docs/reference.md` の `--otlp-endpoint` の説明が「ログも送出される」と読めないことを確認する。効くのは計測値と追跡情報のみである (FR-029、quickstart §6)

---

## Phase 5: User Story 3 - 文書が実装とずれていないと信じられる (Priority: P3)

**Goal**: 実装を変えて文書を更新しないまま品質ゲートを実行すると失敗する状態にする。

**Independent Test**: 実装に設定項目または計測値を 1 つ追加し、文書を更新しないまま
`make all` を実行して失敗すること。

**⚠️ 各検査タスクは同じファイル (`test/docs/reference_test.go`) を編集するため
並行して進められない。** 順に行う。

- [X] T021 [US3] `test/docs/reference_test.go` に、**期待する 6 つの印がすべて存在すること**を突き合わせより先に確かめる表明を書く。期待する印の一覧は**検査側のコードに持つ**。文書側に持たせない (contracts/reference-checks.md)。印を置く前なので落ちることを確認する
- [X] T022 [US3] `docs/reference.md` の設定項目の表の直前に `<!-- reference:flags -->` を置き、`test/docs/reference_test.go` に `config.Usage()` の出力から設定項目の名前と既定値を取り出して突き合わせる検査を実装する (data-model.md 1)
- [X] T023 [US3] `docs/reference.md` の供給元の比較表の直前に `<!-- reference:secret-managers -->` を置き、`test/docs/reference_test.go` に `config.Load` が各供給元名を受理することを確かめる検査を実装する。実在しない名前が拒否されることも併せて確認する
- [X] T024 [US3] `docs/reference.md` の計測値の表の直前に `<!-- reference:metrics -->` を置き、`test/docs/reference_test.go` に **Prometheus 形式の出力から系列名を取り出して**突き合わせる検査を実装する。各計測器に 1 件ずつ記録してから収集する (research R4、FR-022 を同時に満たす)
- [X] T025 [US3] `docs/reference.md` の `operation` ラベルの値の表の直前に `<!-- reference:dpf-operations -->` を置き、`test/docs/reference_test.go` に `internal/dpf` の構文木から `observe` の第 2 引数を集めて突き合わせる検査を実装する (T007 の関数を使う)
- [X] T026 [US3] `docs/reference.md` の対応レコード種別の表の直前に `<!-- reference:record-types -->` を置き、`test/docs/reference_test.go` に `provider.SupportedRecordTypes()` と突き合わせる検査を実装する
- [X] T027 [US3] `docs/reference.md` の経路の表の直前に `<!-- reference:endpoints -->` を置き、`test/docs/reference_test.go` に `internal/webhook` と `internal/server` の構文木から `mux.Handle` / `mux.HandleFunc` の第 1 引数を集めて突き合わせる検査を実装する。**これにより原則 I「独自のエンドポイントを追加しない」が機械的に守られる**
- [X] T028 [US3] `test/docs/reference_test.go` の失敗メッセージが、**どちら側にだけ存在する値か**を示すことを確認する。「一致しません」だけでは文書と実装のどちらを直すか判断できない (contracts/reference-checks.md、quickstart §3d)
- [X] T029 [US3] [quickstart.md](./quickstart.md) §3 に従い、食い違いの検出を確かめる。文書から 1 行消す / 実在しない行を足す / 実装に設定項目と計測値を足して文書を更新しない、のいずれでも失敗すること。確認後に元へ戻す (SC-004、SC-005、SC-008)
- [X] T030 [US3] [quickstart.md](./quickstart.md) §4 に従い、検査を無効化できないことを確かめる。印を消す / 表ごと消す / 表を見出し行だけにする / 同じ印を 2 つ置く / 印の名前を綴り間違える、のいずれでも失敗すること。確認後に元へ戻す (原則 VI)
- [X] T031 [US3] `docs/reference.md` に**検査の境界**を明記する。印の付いた表は機械的に守られていること、印のない記述は人が保つこと、および検査しない対象の一覧 (contracts/reference-checks.md「検査しないもの」) を書く (FR-032)
- [X] T032 [US3] `docs/reference.md` に、**トークンが読まれる位置が検査対象外である**ことを明記する。依存ライブラリの既定値であるため源がないこと、依存の版を固定していることが緩和であること、版が上がる際は Pull Request で確かめられることを併記する (research R6、contracts/reference-checks.md)

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T033 `docs/reference.md` の「既知の制限」に、**ログの OpenTelemetry 形式での送出が未実装**であること (規範 原則 V が MUST としている)、ゾーンロックの取得・解放が計測されていないことが 1 か所にまとまって記載されていることを確認する (FR-030、SC-009)
- [X] T034 `docs/reference.md` 全体を通読し、規範が要求する姿を実装済みであるかのように書いた箇所が 0 件であることを確認する (FR-029、SC-009)
- [X] T035 `make all` を実行し、`test/docs` を含めてすべて通ることを確認する。`gofmt`、`golangci-lint`、SPDX ヘッダの検査を含む
- [X] T036 [quickstart.md](./quickstart.md) の全手順を実行し、結果を記録する。§7 (US1 の受け入れ) は人が読んで判断する項目である

---

## 実装で判明した設計の修正

タスクの通りに進める中で、設計の 2 点に誤りが見つかった。**事実の方を採り、
設計文書 (research.md、data-model.md、plan.md、contracts/) を実装に合わせた。**

### 1. 計測値を出力だけから採ると、追加を検出できない

当初の設計 (research R4) は「計測値は実際の出力から採る」としていた。T029 で
実装側に計測器を 1 つ足して確かめたところ、**検出できなかった**。記録を書いて
いない計測器は出力に現れないためである。SC-008 を満たせていなかった。

宣言された計測器を構文木から取り、種別ごとの接尾辞を付けて系列名を導く方式に
改めた。導出の規則 (Counter は `_total`、Histogram は `_bucket` `_sum` `_count`)
は Prometheus 形式の慣習であってこちらのコードにないため、**毎回の実行で実際の
出力と突き合わせて確かめる**。未知の種別の計測器が現れたら失敗させる。

**この修正は、検査が落ちることを確かめなければ見つからなかった。** T029 と
T030 を省いていれば、「通っているように見えるだけの検査」が残っていた。

### 2. 経路の印は 2 つ要る

経路は文書上 provider と exposed の 2 つの表に分かれている。印は表 1 つを
指すため、`endpoints-provider` と `endpoints-exposed` の 2 つになった。
検査する組は 6 つではなく **7 つ**である。

### 併せて見つかった文書の誤り

計測器の名前は既に `_total` を含んでいた (`dns_record_changes_total`)。
文書の「由来」列が `dns_record_changes` (Counter) と書いていたのは誤りで、
検査が指摘した。

---

## Dependencies

```
Phase 1 (Setup)
   └─> Phase 2 (Foundational) ──> Phase 5 (US3)
                                        └─> Phase 6 (Polish)

Phase 3 (US1) ──┐   ← Setup / Foundational を待たない
Phase 4 (US2) ──┴─> Phase 6 (Polish)
```

- **US1 と US2 は Setup / Foundational に依存しない。** `docs/reference.md` の
  確認作業であり、Phase 1・2 と**並行して進められる**
- **US3 は Phase 2 に依存する。** 6 つの検査が共有する読み取りの仕組みが要る
- Phase 5 の内部は**順に行う**。T021〜T028 が同じファイル
  (`test/docs/reference_test.go`) を編集する
- T029・T030 は T021〜T028 の完了後に行う。すべての検査が揃った状態で
  「落ちること」を確かめる必要がある
- T031・T032 は文書側の作業であり、T021〜T028 と並行できる

### 並行して進められる組み合わせ

| 組 | タスク |
|---|---|
| A | T001〜T002 (Setup) |
| B | T009〜T015 (US1) |
| C | T016〜T020 (US2) |
| D | T031〜T032 (検査の境界の明記) |

A・B・C・D は互いに独立している。B と C は `docs/reference.md` の別の節を
扱うため、同時に編集する場合のみ注意する。

---

## Implementation Strategy

### MVP

**US1 (Phase 3) が MVP である。** これが満たされないと運用者は起動できず、
以降のどの記述も意味を持たない。記述は既に存在するため、MVP に必要なのは
**受け入れ条件に照らした確認と欠けの補填**である。

### 段階的な進め方

1. **US1 を確認して閉じる** (T009〜T015)。運用者が起動できる状態を保証する
2. **US2 を確認して閉じる** (T016〜T020)。観測した値を読める状態にする
3. **Setup と Foundational** (T001〜T008)。検査の土台を作る
4. **US3 を実装する** (T021〜T032)。文書が古くならない状態にする
5. **Polish** (T033〜T036)

1 と 2 は既存の文書に対する確認であり、短時間で終わる見込みである。
作業量の大半は 3 と 4 にある。

### 原則 III の適用のしかた

本機能では**成果物そのものが検査**であるため、「テストが実装に先行したか」は
次の形で確かめる。

- T003・T005・T008: 読み取りの仕組みに対する表明を先に書き、落ちることを確認する
- T021: 印を置く前に、印の存在を要求する表明を書き、落ちることを確認する
- T029・T030: **検査が確かに落ちることを確かめる。** 落ちない検査は何も
  守っていない。この 2 つを省くと、本機能は「通っているように見えるだけの
  検査」になる

### 範囲外 (本機能では行わない)

- **ログの OpenTelemetry 形式での送出の実装。** 規範が MUST としているが未実装で
  ある。本機能では既知の制限として記載するに留める (T033)。実装は別の機能として
  扱う
- ゾーンロックの計測の追加
- 製品コードの変更全般。検査は公開関数・実行時の出力・構文木の読み取りだけで
  成立する。**検査のために製品の公開面を広げない**
