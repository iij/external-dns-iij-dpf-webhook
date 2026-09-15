# Implementation Plan: ゾーン反映の実行者を DPF の記録に残す

**Branch**: `feature/create-webook` | **Date**: 2026-09-15 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/004-zone-apply-attribution/spec.md`

## Summary

ゾーンの反映を行うたびに、実行者が本サービスであることを DPF の履歴へ残す。
運用者が DPF の履歴を開いたとき、本サービスによる反映と人手による反映を見分け
られるようにする。

**変更は 1 箇所に収まる。** 本サービスは既に一括更新 (`atomic_changes`) で
レコードの更新とゾーンの反映を原子的に行っており、その要求は説明を添えられる。
固定文字列 `external-dns-iij-dpf-webhook` を添えるだけで足り、API 呼び出しは
増えない (research R1)。

設計上の判断は 3 つ。**(1)** 記録する内容を固定文字列に限る。上限は
**80 オクテット**とスキーマに宣言されており、28 オクテットの固定値は投げる前に
収まることが確かめられる (research R4/R5)。**(2)** `Backend` インタフェースを
変えない。説明は DPF の要求が持つ項目であり、境界の内側に留める
(原則 II、research R6)。**(3)** 記録が履歴に現れることを、人の目視ではなく
履歴 API の読み戻しで機械的に確かめる。説明で絞り込む問い合わせができる
(research R2)。

## Technical Context

**Language/Version**: Go 1.27 (001 から変更なし)

**Primary Dependencies**: `github.com/iij/dpf-go` v0.1.0。本機能で新しい依存は
増えない。既に使っている `PatchZoneAtomicChanges` の任意項目 `description`
(共通スキーマ `Description`、`maxLength: 80`) と、未使用だった
`ZoneHistoriesAPI` を用いる。典拠はモジュール同梱の `openapi.json`

**Storage**: なし。記録の保持は DPF が行う

**Testing**: `go test`。単体 (要求の組み立て) と実環境 (履歴への反映) の 2 層。
契約テストは不要 — webhook API の外形は変わらない

**Target Platform**: 001 から変更なし

**Project Type**: 単一の HTTP サービス (001 から変更なし)

**Performance Goals**: なし。API 呼び出しが増えないため、適用の所要時間に
影響しない

**Constraints**:

- 記録する内容は固定文字列。呼び出しごとに変えない
- `provider.Backend` の署名を変えない
- 追加の API 呼び出しを行わない

**Scale/Scope**: 変更は `internal/dpf` に閉じる。上位層は本機能の存在を知らない

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

適用する constitution: **v2.2.0**

| 原則 / 制約 | 本 plan での満たし方 | 判定 |
|---|---|---|
| I. ExternalDNS Webhook 契約への準拠 | webhook API の外形を変えない。独自エンドポイント・独自フィールドを追加しない。記録は DPF 側にのみ現れる | PASS |
| II. プロバイダ境界の分離 | 説明は DPF の要求が持つ項目であり、`internal/dpf` の内側に留める。`Backend` の署名を変えない。履歴の読み取りも生成型を外へ出さない (research R6) | PASS |
| III. テストファースト | 要求の組み立ては単体テストで先に固定する。履歴への反映は実環境の検証で確かめる。いずれも実装前に書く | PASS |
| IV. DNS 変更の安全性 | 記録はレコードデータではなく、DNS の状態を変えない。冪等性 (FR-010)、範囲外の不変、投入前ガード、反映完了までの待ち合わせのいずれにも影響しない | PASS |
| V. 可観測性と運用性 | 記録に認証情報を含めない (FR-004)。**本機能自体が可観測性の強化である** — ログに加えて DPF 側の履歴からも出どころを追えるようにする | PASS |
| VI. Default-Deny | 設定項目を増やさない。到達性も権限も増やさない。記録しない選択肢を設けないが、これは既定拒否の対象ではない (research R6) | PASS |
| Go コード品質 | 既存のゲートをそのまま適用する | PASS |
| DPF API クライアント | `dpf-go` の既存の型を使う。自前 HTTP クライアントを実装しない | PASS |
| ドメイン名の取り扱い | 本機能は名前を扱わない | 該当なし |
| ライセンス | 追加ファイルにも SPDX ヘッダを付ける | PASS |
| 実環境での検証 (v2.1.0) | **記録が履歴に現れることは実環境でしか確かめられない。** e2e に追加し、`concurrency` による並列抑止の対象に含める | PASS |
| 外部 API のエラー応答 (v2.2.0) | 既存の `wrapAPIError` の経路をそのまま通る。本機能で記録の扱いを変えない | PASS |

**違反なし。** Complexity Tracking は不要。

### 設計上の注意点 (違反ではないが監視が必要)

- **説明を可変にするなら 80 オクテットの上限を保証する**: 上限はスキーマに
  宣言されている (research R5)。固定文字列 28 オクテットなら定数として収まるが、
  変更件数やレコード名を足すと入力に依存して伸びる。**足すなら、80 を超えない
  ことを型または検査で保証すること。** 上限を確かめずに可変長の内容を入れると、
  001 の `TXT` と同じ経路をたどる。あちらはスキーマに宣言がなく実環境で初めて
  現れたが、こちらは宣言があるのに無視した、という違いだけになる
- **履歴の読み取りを通常の動作経路で使わない**: 検証のためだけに用意する。
  本サービスが自分の記録を読み返して動作を変える設計にすると、履歴が動作の
  前提になり、DPF 側の保持期間や取得失敗が DNS の更新を止める経路になる
- **レコードのコメントに触れない**: 001 のマージは管理対象外のレコードを逐語的に
  コピーしており、コメントもその一部である。本機能がここへ書き込むと、逐語コピーの
  保証が崩れる

## Project Structure

### Documentation (this feature)

```text
specs/004-zone-apply-attribution/
├── plan.md              # This file
├── research.md          # Phase 0 output (R1〜R6)
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── zone-apply-attribution.md   # 001 の dpf-client 契約への追加
├── checklists/
│   └── requirements.md  # spec 品質チェックリスト
└── tasks.md             # /speckit-tasks の出力 (本コマンドでは作成しない)
```

### Source Code (repository root)

本機能が触るのは `internal/dpf` だけである。

```text
internal/
└── dpf/
    ├── apply.go         # 一括更新の要求に説明を添える (変更)
    └── history.go       # 履歴の読み取り。検証にのみ用いる (新規)

test/
└── e2e/
    └── attribution_test.go   # 記録が履歴に現れることの検証 (新規)
```

**変更しないもの**: `internal/provider`、`internal/webhook`、`internal/config`、
`internal/server`、`internal/telemetry`、`cmd/`。上位層は本機能の存在を知らない。

**Structure Decision**: 001 の構造をそのまま使う。新しいパッケージを作らない。

履歴の読み取りを `internal/dpf/history.go` として分けるのは、`apply.go` が
適用の経路であり、そこに通常の動作で使わない操作を混ぜないためである。
ファイルが分かれていれば、通常の経路から呼ばれていないことが読んで分かる。

## Complexity Tracking

> Constitution Check に違反がないため、記入不要。

---

## Phase 1 成果物

- [research.md](./research.md) — Phase 0 の調査 (R1〜R6)
- [data-model.md](./data-model.md) — 反映の記録と、それが現れる場所
- [contracts/zone-apply-attribution.md](./contracts/zone-apply-attribution.md) — 001 の dpf-client 契約への追加
- [quickstart.md](./quickstart.md) — 動作確認の手順

## Post-Design Constitution Re-check

Phase 1 の設計後に再評価した。**新たな違反なし。**

- `Backend` の署名を変えない決定 (contracts) により、原則 II の境界が保たれた。
  上位層のテストは本機能の影響を受けない
- 記録をレコードデータから分離したこと (data-model 1) により、原則 IV の
  冪等性と範囲限定が構造的に無傷である。投入前ガードの検査対象にも入らない
- 履歴の読み取りを「検証にのみ用いる」と契約で明示したこと (contracts) により、
  DPF 側の保持期間や取得失敗が DNS の更新を止める経路が作られない
- 内容を固定文字列に限った決定 (research R4/R5) により、長さの上限が不明な項目に
  可変長の値を入れる危険が設計の段階で排除された

### 未確認事項

**なし。** 当初は「28 オクテットが受け入れられるか実環境まで分からない」としていたが、
OpenAPI 定義に `maxLength: 80` の宣言があり、投げる前に確かめられる (research R5)。

実環境での確認は引き続き行うが (quickstart 2、constitution v2.1.0)、それは
**未知の制約を探すためではなく、記録が履歴に現れることを確かめるため**である。
説明が要求に載ることと、DPF が履歴として保持することは別の事実である。
