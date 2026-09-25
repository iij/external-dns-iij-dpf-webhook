# Specification Quality Checklist: レコードに管理者の印を付ける

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-16
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

- **設計の変更 (2026-09-16)**: 利用者の指示により、**ラベルを毎回上書きする**方式に
  改めた。本 provider が書くレコードのラベルは、常に印 1 つだけになる。

  これで制約が 2 つ**構造的に消えた**。

  | 消えたもの | 理由 |
  |---|---|
  | ラベル 10 件の上限 | 常に 1 件になる。触れる経路が存在しない |
  | ラベルのマップの共有 | マップごと差し替えるため、元のマップに触れない |

  初版は加算方式を前提とし、上限に達した場合の扱い (旧 FR-006) を重要な要件として
  扱っていた。**上書き方式ではその要件自体が不要になる。設定しない分岐は壊れない。**

  旧 US2 (既存のラベルを壊さない)、旧 FR-004、旧 FR-006 を削除した。
- **前提の訂正 (2026-09-16)**: 初版は「運用者のラベルが 10 件ある既存レコードに
  印を足す」場合を中心に据えていた。上流の `plan.calculateChanges` は更新と削除を
  所有者で絞り込み、ExternalDNS は自分が所有していないレコードを更新しない。
  **変更セットに載るレコードのラベルは 0 件から始まる。** 上流の実装を確かめずに
  「既存のレコードにラベルを足す」機能として考えていた。

  この訂正の後に上書き方式へ移ったため、前提の誤りは設計に残っていない。
- **代償を利用前提条件として明示した (PC-001)**。運用者が本 provider のレコードに
  付けたラベルは、次の更新で失われる。

  **保証しない範囲を数え上げることはしない。** 手で所有権の `TXT` を消すことも
  できるように、外から触れる経路は列挙しきれない。PC-001 は「保証しない」ことと、
  001 の利用前提条件を言い直したものであることだけを述べる。
- 利用者の指示にあった「txt での metadata のレコードも含む」を FR-002 と SC-002 に
  独立した要件として置いた。これらは ExternalDNS が生成するが、**DPF へ書くのは
  本 provider** であり、印が付かない理由がない。
- **掃除そのものを範囲外とした。** 本機能は絞り込める状態を作るところまでを担う。
  何をいつ消すかは運用の判断であり、spec を分けた。動機は掃除だが、印付けと
  掃除は独立して価値と危険を持つ。
- **遡って印を付けないことを明記した。** 全件を書き換える操作は、得るものに対して
  危険が大きい。既存のレコードは次に更新されたときに印が付く。
- 印の名前 `managed-by` は前置きを持たない一般的な名前であり、他の仕組みが同じ名前を
  別の意味で使う余地がある。その場合その値は失われるが、これは PC-001 の範囲である。
  DPF のラベルは前置き付きの名前も許すが、文字種の規則が明確でないため、利用者が
  指定した形をそのまま採った。
- Items marked incomplete require spec updates before `/speckit-clarify` or `/speckit-plan`
