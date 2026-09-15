# Specification Quality Checklist: ExternalDNS webhook provider 本体

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-04
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- **解決済み**: FR-026 (対応レコード種別の範囲) は DPF マニュアル
  (<https://manual.iij.jp/dpf/help/19629152.html>) と ExternalDNS v0.22.0 の
  `KnownRecordTypes` を突き合わせ、交差する 9 種別 (A, AAAA, CNAME, TXT, SRV, NS, PTR,
  MX, NAPTR) に確定した。あわせて DPF 由来の種別ごとの制約を FR-029〜FR-033 として追加。
- 検証時に修正した点:
  - 初版では ExternalDNS webhook API のエンドポイントパスとメディアタイプを spec に
    記載していたため、実装手段として plan へ委譲し、機能単位の記述に改めた
    (constitution v1.6.0 の「本文書には守るべき結果のみを記載する」規則に対応)。
  - Success Criteria から応答時間のミリ秒指定を除き、利用者から見た成果 (反映までの時間、
    振動の不在) に置き換えた。
- **plan 段階での spec 改訂 (2026-09-04)**: `/speckit-plan` の設計中に判明した内容を
  spec へ反映した。いずれも既存要件と矛盾せず、チェックリストの判定は変わらない。
  - 「利用前提条件」セクションを新設 (PC-001〜PC-003)。ゾーンを他の機械的手段で
    変更する場合、同じロックを取得するか reconcile 動作を持つことを求める。
    本 provider の設計では防げない範囲を、利用者への前提として明示したもの。
  - FR-032 を精密化。制限は character-string 1 個あたり 255 **オクテット**であり、
    複数 character-string の**合計長は制限しない** (DKIM 鍵などが該当)。
    元の「1 文字列が 255 文字」は対象と単位の両方が曖昧だった。
  - FR-032a を追加。`TXT` の分割状態を保つ要件 (読み取り時に連結せず、
    書き込み時に分割位置を変えない)。
- **spec 改訂 (2026-09-15): 親子・孫ゾーン併存時の適用先と `NS` の除外**:
  利用者の指摘により、適用時のゾーン選択規則が spec に書かれていなかったことが判明した
  ため追加した。規則自体は plan 段階の data-model.md 3 (最長一致) で既に決まっており、
  spec 側だけが欠けていた。続く指摘でゾーンカットの両側問題が判明し、`NS` を対応
  レコード種別から外す判断に至った。チェックリストの判定は変わらない。
  - User Story 5 (P2) を追加。`example.jp` / `sub.example.jp` / `a.sub.example.jp` の
    3 階層が併存する環境で、最も深く一致するゾーンだけが更新されること。
    受け入れシナリオ 10 件。
  - FR-040〜FR-045「書き込み先ゾーンの決定」。最長一致による適用先の決定 (FR-040)、
    該当ゾーンなしの恒久的失敗 (FR-041)、複数ゾーンにまたがる変更セットの扱い (FR-042)、
    他ゾーンへの波及の禁止 (FR-043)、読み取り側も同じ規則でゾーンに帰属させ、
    読み取り元が最長一致ゾーンでないレコードを返さない (FR-044)、適用先ゾーンの
    記録 (FR-045)。
  - **FR-026 を 9 種別から 8 種別へ。`NS` を除外した。** FR-029 を「ゾーン apex の `NS`
    を変更しない」から「`NS` 全体を管理対象としない」へ改め、対応レコード種別の節へ
    移した (DPF の制約ではなく適用範囲の判断になったため)。除外の理由は 3 つ:
    (1) ゾーンカットでは委任の `NS` (親側) と apex の `NS` (子側) が名前も種別も同じまま
    両側に存在し、ExternalDNS が渡す名前と種別だけでは区別できない、
    (2) apex の `NS` は `overwrite_zone_apex_ns` が常に false のため投入しても
    取り込まれない、
    (3) ExternalDNS が Ingress や Service から算出するレコードに `NS` は現れない。
  - SC-010〜SC-013 を追加。3 ゾーン併存下で意図しないゾーンの変更が 0 件、親子併存下でも
    差分の振動が起きない、`NS` の変更・削除が 0 件、子ゾーンの権威レコードが親側の値を
    理由に変更されない。
  - **実装・下流成果物との差分 (plan / tasks で解消が必要)**:
    - `NS` の除外が未反映: `internal/provider/types.go` の `TypeNS` と
      `supportedRecordTypes`、`internal/dpf/rrtype.go` の対応表、
      `internal/provider/validate.go` の `validateApexNSModification`
      (`NS` が種別として消えれば不要になる)。テストは
      `internal/provider/validate_test.go`、`internal/dpf/rrtype_test.go`、
      `test/integration/apply_test.go` が `TypeNS` を参照する。
      DPF 層は `currentRecords` が生のレコードを種別で絞らず返すため、apex `NS` の
      逐語コピーは許可リストの変更に影響されない。
    - FR-044 が未実装: `internal/provider/provider.go` の `Records` は関連ゾーンを順に
      舐めて範囲内の名前をすべて返すため、親ゾーン側に残るレコードと子ゾーン側の
      レコードが重複して返る。`Record` 型 (`internal/provider/types.go`) がゾーンを
      保持せず、除外判定に必要な「読み取り元ゾーン」が上位層へ届いていない。
    - 下流の設計文書が 9 種別のまま: `data-model.md` (種別一覧、FR-029 の参照)、
      `plan.md` (FR-029 の記述)、`README.md` (対応種別の表と `NS` の説明)、
      `docs/reference.md` (`NS` の行)。
    - FR-040〜FR-043・FR-045 は既存実装が満たしている。
- Items marked incomplete require spec updates before `/speckit-clarify` or `/speckit-plan`
