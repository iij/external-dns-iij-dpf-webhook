# Implementation Plan: レコードに管理者の印を付ける

**Branch**: `feature/create-webook` | **Date**: 2026-09-16 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/005-managed-by-label/spec.md`

## Summary

本 provider が書くレコードに、ラベル `managed-by: external-dns-iij-dpf-webhook` を
付ける。運用者が DPF 上でレコードを見たとき、本 provider が管理しているものを
見分けられるようにする。ExternalDNS が所有権の記録として書く `TXT` も対象である。

**変更は 1 箇所に収まる。** 001 のマージは「反映済みを土台にする → 削除を落とす →
作成と更新を反映する」の 3 段で構成され、**3 段目の対象がちょうど印を付ける対象と
一致する** (research R3)。段 3 を通ったレコードにだけ印が付き、触らないレコードは
段 1 のまま逐語コピーされる。

**ラベルは毎回上書きする。** 本 provider が書くレコードのラベルを、印のみからなる
状態に設定する。既存のラベルは保持しない。これで制約が 2 つ構造的に消える —
**ラベル 10 件の上限に触れる経路が存在しなくなり** (常に 1 件になる)、**反映済み
レコードのラベルのマップを書き換える危険もなくなる** (マップごと差し替えるため)。
**設定しない分岐は壊れない** (research R2)。

代償は、運用者が本 provider のレコードに付けたラベルが次の更新で失われることである。
spec の利用前提条件 PC-001 として明示した。

設計上の判断は 2 つ。**(1)** 印の付与を `merge` の内部に織り込まず、マージ結果に
対する独立した段とする。単体で検証でき、どちらの誤りで落ちたのかが読める。
**(2)** 印の値は 004 の反映の記録と同じ定数を使う。同じものを指す印が 2 つの表記を
持たないようにする。

## Technical Context

**Language/Version**: Go 1.27 (001 から変更なし)

**Primary Dependencies**: `github.com/iij/dpf-go` v0.1.0。本機能で新しい依存は
増えない。既に使っている `OverwriteRecordsInner.Labels` と、未使用だった
`ApiGetRecordCurrentsRequest.KeywordsLabel` を用いる。典拠はモジュール同梱の
`openapi.json` と DPF マニュアル「ラベルの共通ルール」

**Storage**: なし。ラベルの保持は DPF が行う

**Testing**: `go test`。単体 (投入集合の組み立て) と実環境 (DPF 上への保存と
絞り込み) の 2 層。契約テストは不要 — webhook API の外形は変わらない

**Target Platform**: 001 から変更なし

**Project Type**: 単一の HTTP サービス (001 から変更なし)

**Performance Goals**: なし。API 呼び出しが増えないため、適用の所要時間に
影響しない

**Constraints**:

- 値は 1〜63 文字。名前と値の文字種は英数字と `.` `-` `_` (research R1)
- ラベルは毎回上書きする。**上限 (10 件) に触れる経路を作らない**
- `provider.Backend` の署名を変えない
- 印を ExternalDNS へ返さない

**Scale/Scope**: 変更は `internal/dpf` に閉じる。上位層は本機能の存在を知らない

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

適用する constitution: **v2.2.0**

| 原則 / 制約 | 本 plan での満たし方 | 判定 |
|---|---|---|
| I. ExternalDNS Webhook 契約への準拠 | webhook API の外形を変えない。応答にラベルを足さない。印は DPF 側にのみ現れる | PASS |
| II. プロバイダ境界の分離 | ラベルは DPF の概念であり `internal/dpf` の内側に留める。`Backend` の署名を変えない。絞り込みの読み取りも生成型を外へ出さない | PASS |
| III. テストファースト | 投入集合の組み立てを単体テストで先に固定する。DPF 上への保存と絞り込みは実環境の検証で確かめる。いずれも実装前に書く | PASS |
| IV. DNS 変更の安全性 | 印は DNS のデータではなく、冪等性・範囲限定・投入前ガードのいずれにも影響しない。毎回同じ状態へ収束するため、繰り返し適用しても結果が変わらない | PASS |
| V. 可観測性と運用性 | **本機能自体が可観測性の強化である** — レコードそのものが出どころを示す | PASS |
| VI. Default-Deny | 設定項目を増やさない。到達性も権限も増やさない。印を付けない選択肢を設けないが、これは既定拒否の対象ではない (外部への到達性も権限も増やさない) | PASS |
| Go コード品質 | 既存のゲートをそのまま適用する | PASS |
| DPF API クライアント | `dpf-go` の既存の型を使う。自前 HTTP クライアントを実装しない | PASS |
| ドメイン名の取り扱い | 本機能は名前を扱わない | 該当なし |
| ライセンス | 追加ファイルにも SPDX ヘッダを付ける | PASS |
| 実環境での検証 (v2.1.0) | 印が DPF 上に保存され、絞り込みが効くことは実環境でしか確かめられない。e2e に追加し、`concurrency` による並列抑止の対象に含める | PASS |
| 外部 API のエラー応答 (v2.2.0) | 既存の `wrapAPIError` の経路をそのまま通る | PASS |

**違反なし。** Complexity Tracking は不要。

### 設計上の注意点 (違反ではないが監視が必要)

- **加算方式へ戻さない**: 「運用者のラベルを保持したい」と考えたときは、上限 (10 件) の
  扱いと、反映済みレコードのラベルのマップを写す手当てが同時に必要になる。**上書き方式は
  その 2 つを設計から消している。** 戻すなら両方を復活させること (research R2)
- **`current` のラベルを後から参照しない**: `toOverwrite` は反映済みレコードのマップへの
  参照を投入集合へ入れている。上書き方式では元のマップに触れないため現状は無害だが、
  この共有自体は残っている。ラベルを読み返す処理を足すときは注意が要る (research R3)
- **印を上位層へ出さない**: ドメインの型がラベルを持たないため現状は自然に満たされる。
  **転送形にラベルを足したくなっても足さない。** 004 では逆方向 (上流のフィールドを
  落とす) で ExternalDNS の状態を壊した。境界を越える情報は、方向ごとに要件がある
- **遡って印を付けない**: 全件を書き換えれば既存のレコードにも付くが、ゾーン全体を
  書き換える操作は得るものに対して危険が大きい (research R7)

## Project Structure

### Documentation (this feature)

```text
specs/005-managed-by-label/
├── plan.md              # This file
├── research.md          # Phase 0 output (R1〜R7)
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── managed-by-label.md   # 001 の dpf-client 契約への追加
├── checklists/
│   └── requirements.md  # spec 品質チェックリスト
└── tasks.md             # /speckit-tasks の出力 (本コマンドでは作成しない)
```

### Source Code (repository root)

本機能が触るのは `internal/dpf` だけである。

```text
internal/
└── dpf/
    ├── label.go         # 印の付与。マージ結果に対する独立した段 (新規)
    ├── apply.go         # 印の付与を適用の流れに挟む (変更)
    └── records.go       # 印で絞った一覧の取得。検証にのみ用いる (変更)

test/
└── e2e/
    └── label_test.go    # 印が DPF 上に保存され、絞り込めることの検証 (新規)
```

**変更しないもの**: `internal/provider`、`internal/webhook`、`internal/config`、
`internal/server`、`internal/telemetry`、`internal/dnsname`、`cmd/`。
上位層は本機能の存在を知らない。

**`merge.go` を変更しない**: 印の付与を `internal/dpf/label.go` として分けるのは、
`merge` が投入集合の組み立てという一つの責務を持ち、テストもそれを見ているためである。
印の設定を混ぜると、投入集合の組み立ての誤りと印の誤りが区別しにくくなる
(research R3)。

**Structure Decision**: 001 の構造をそのまま使う。新しいパッケージを作らない。

## Complexity Tracking

> Constitution Check に違反がないため、記入不要。

---

## Phase 1 成果物

- [research.md](./research.md) — Phase 0 の調査 (R1〜R7)
- [data-model.md](./data-model.md) — 印と、その制約・対象・絞り込み
- [contracts/managed-by-label.md](./contracts/managed-by-label.md) — 001 の dpf-client 契約への追加
- [quickstart.md](./quickstart.md) — 動作確認の手順

## Post-Design Constitution Re-check

Phase 1 の設計後に再評価した。**新たな違反なし。**

- 印の付与をマージ結果に対する独立した段としたこと (contracts) により、
  001 の「管理対象外は逐語コピー」が無傷で保たれる。段 1 を通ったレコードには
  触れないことが、構造として読める
- 印を DNS のデータから分離したこと (data-model 1) により、原則 IV の冪等性と
  範囲限定が構造的に無傷である。投入前ガードの検査対象にも入らない
- ラベルを毎回上書きする決定 (research R2) により、**上限の扱いとマップの共有という
  2 つの問題が設計から消えた。** 初版は加算方式を前提に、上限に達した場合の分岐を
  重要な要件として扱っていた。分岐を置かずに済ませるほうが強い
- `Backend` の署名を変えない決定 (contracts) により、原則 II の境界が保たれた。
  上位層のテストは本機能の影響を受けない
- 絞り込みの読み取りを「検証にのみ用いる」と契約で明示したこと (contracts) により、
  本サービスが自分の印を読み返して動作を変える経路が作られない

### 未確認事項

**なし。** ラベルの制約 (個数・長さ・文字種) は `openapi.json` と DPF マニュアルの
双方で確かめた (research R1)。004 で「生成された Go の型が検証を持たないこと」を
「スキーマに宣言がないこと」と混同した反省を適用し、先に典拠を読んだ。

実環境での確認は行うが (quickstart 2、constitution v2.1.0)、それは**未知の制約を
探すためではなく、印が DPF 上に保存され絞り込みが効くことを確かめるため**である。

### 本機能の外に残る問題

**検証の後始末が動いていない理由は分かっていない。** `残留レコードを消す` と
`TestSidecarForceDelete` が ✓ を返しているにもかかわらず、前回の実行のレコードが
検証用ゾーンに残っていた。

本機能は「消す対象を安全に特定できる状態」を作るが、**消す処理そのものが動いて
いなければ残り続ける。** 掃除の仕組みを作る前に、後始末が動いていない理由を
調べる必要がある (research R7)。
