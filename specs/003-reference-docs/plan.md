# Implementation Plan: リファレンス文書

**Branch**: `003-reference-docs` | **Date**: 2026-09-10 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/003-reference-docs/spec.md`

## Summary

外部から観測・設定できる面を 1 つのリファレンス文書にまとめ、その記述が実装から
ずれないようにする。

**US1 (設定とトークンの供給元) と US2 (計測値・ログ・追跡・エラー) に相当する記述は、
`docs/reference.md` として既に存在する** (コミット `98783eb`)。残っているのは
**US3 — 文書と実装の一致を機械的に検出すること**である。

方式は、**文書を人が書き、実装から取り出した事実と突き合わせる**。文書を生成
しない。文書の価値の大半は散文にあり (Vault が読むフィールド名、
`--dpf-token-secret-endpoint` の供給元ごとの意味、AWS で `kms:Decrypt` が要る条件)、
これらは実装から機械的に導けないためである。

検査する表の直前に HTML コメントの印を置く。印は解析器に対象を伝えるだけでなく、
**読み手に「ここは機械的に守られている」と示す**。印のない記述は人が保つ。
この非対称を文書自身に書くことが FR-032 の要求である。

**製品コードを一切変更しない。** 追加するのは文書中の印と、検査を行うテスト
パッケージだけである。

## Technical Context

**Language/Version**: Go 1.27 (既存)

**Primary Dependencies**: 標準ライブラリのみ (`go/parser`、`go/ast`、`regexp`、
`net/http/httptest`)。新たな依存を追加しない

**Storage**: N/A

**Testing**: `go test ./test/docs/...`。既存の `make all` と CI がそのまま実行する

**Target Platform**: N/A (文書と検査のみ。配布物に影響しない)

**Project Type**: 既存サービスに対する文書と、その正しさを保つ検査の追加

**Performance Goals**: 検査は外部接続を伴わず、既存のテスト実行時間に対して
無視できる範囲に収める

**Constraints**:
- **製品コードを変更しない** (spec の範囲外指定)
- 検査は既存の品質ゲートに載せる。独立した手順にしない (FR-033)
- 検査対象の印が欠けていたら**失敗する**こと。黙って何も検査しない状態を作らない
- 検査できない範囲を文書に明記すること (FR-032)

**Scale/Scope**: 検査する組は 7 つ (設定項目、シークレット管理サービス、計測値の
系列名、`operation` ラベル、レコード種別、provider の経路、exposed の経路)。
経路は文書上 2 つの表に分かれるため印も 2 つになる。変更するファイルは
既存 1 本と新規 3 本

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### Phase 0 時点の評価

| 原則 | 評価 | 根拠 |
|---|---|---|
| I. ExternalDNS 契約準拠 | **適合 (強化される)** | 公開する経路を文書と突き合わせることで、「独自のエンドポイントを追加しない」が**機械的に守られる**。これまで文書上の約束でしかなかった |
| II. プロバイダ境界の分離 | 該当なし | 製品コードを変更しない |
| III. テストファースト | **適合** | 本機能の成果物そのものが検査である。**検査を書き、落ちることを確かめてから**印を置く |
| IV. DNS 変更の安全性 | 該当なし | DNS に触れない |
| V. 可観測性と運用性 | **適合** | 計測値の意味とエラーの分類を読める形にすることは、この原則の「運用者が気付ける」に直接対応する |
| VI. Default-Deny | **適合** | 検査対象の印が欠けていたら失敗させる。「対象がないので成功」としない (research R3) |

**原則 VI の適用が本機能の要点である。** 検査が「見つからない → 何もしない →
成功」と振る舞うと、印を消すだけで検査を無効化でき、文書の表ごと消しても
気付けない。期待する印の一覧を検査側に持ち、すべて揃っていることを先に
確かめる。検査を減らすには検査側のコードを変える必要があり、差分としてレビューに
現れる。

**原則 III について**: 文書 (`docs/reference.md`) は既に存在するため、
「テストが実装に先行したか」の対象は**検査の仕組み**である。検査を書き、
印を置く前に落ちることを確かめる。加えて、印を 1 つ消す・表から 1 行消すという
操作で確かに落ちることを確かめる。落ちない検査は何も守っていない。

### 技術・配布制約との関係

- **配布物への影響なし**: 文書もテストもイメージに入らない
- **バージョン固定**: 新たな依存を追加しないため影響しない。ただし
  research R6 のとおり、**トークンが読まれる位置は依存ライブラリの既定値**で
  あり検査できない。版の固定がその緩和になっている旨を文書に注記する
- **SPDX ヘッダ**: 新規の Go ファイルに付与する。`make license-check` の対象

### Phase 1 設計後の再評価

**新たな違反なし。**

- 製品コードの変更がゼロに収まった。検査は公開関数 (`config.Usage`、
  `config.Load`、`provider.SupportedRecordTypes`)、Prometheus 形式の出力、
  構文木の読み取りだけで成立する。**検査の都合で製品の公開面を広げていない**
- 構文木の読み取りを使うのは、他に源がない 2 組 (`operation` ラベル、経路) に
  限った。公開関数がある組はそちらを使っている
- 原則 VI が「印の欠落で失敗する」という形で設計に埋め込まれた。文書側の記述を
  変えるだけでは検査を緩められない
- 検査しない範囲を [contracts/reference-checks.md](./contracts/reference-checks.md)
  に列挙し、同じ内容を文書にも書く。**検査の境界が利用者にも見える**

## Project Structure

### Documentation (this feature)

```text
specs/003-reference-docs/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── reference-checks.md
├── checklists/
│   └── requirements.md  # /speckit-specify output
└── tasks.md             # /speckit-tasks output (未作成)
```

### Source Code (repository root)

```text
docs/
└── reference.md              # 既存。検査対象の表に印を足し、検査の境界を明記する

test/
└── docs/
    ├── doc.go                # 新規。検査の範囲と非範囲の要約
    ├── reference_test.go     # 新規。文書と実装の突き合わせ
    ├── extract.go            # 新規。文書の表の読み取りと、構文木からの literal 収集
    ├── extract_test.go       # 新規。読み取りの仕組みに対する表明
    └── testdata/sample/      # 新規。構文木の読み取りを確かめる題材

README.md                     # 既存。導線は追加済み (98783eb)
```

**Structure Decision**: 検査を `test/docs/` に 1 つのパッケージとして置く。

- `test/` 配下は既存の構成に合わせた (`test/contract`、`test/integration`、
  `test/e2e`、`test/config`)。`go test ./...` に載るため、`make all` と CI が
  そのまま実行する (FR-033)
- 外部接続を伴わないため、`test/e2e` のような環境変数による読み飛ばしは
  **設けない。常に走る**
- 文書を読む処理と構文木を読む処理を `extract.go` に分け、表明を
  `reference_test.go` に置く。表明の側を読めば「何を検査しているか」が
  一覧できる
- **製品コードのパッケージに白箱テストを分散させない。** 文書を読む処理が
  重複し、「文書との一致」という 1 つの関心事が散る

## Complexity Tracking

> Constitution Check に違反がないため、記載事項なし。

構文木の読み取りという、この規模の機能にしては重い手段を 2 組に用いている。
これは「検査のために製品の公開面を広げない」ことと引き換えに選んだものであり、
より単純な代替 (公開関数を足す) は spec の範囲外指定に反する。判断の記録は
[research.md](./research.md) の R4 にある。
