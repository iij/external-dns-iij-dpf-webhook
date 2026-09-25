# Specification Quality Checklist: リファレンス文書

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-09
**Feature**: [spec.md](../spec.md)

## Content Quality

- [X] No implementation details (languages, frameworks, APIs)
- [X] Focused on user value and business needs
- [X] Written for non-technical stakeholders
- [X] All mandatory sections completed

## Requirement Completeness

- [X] No [NEEDS CLARIFICATION] markers remain
- [X] Requirements are testable and unambiguous
- [X] Success criteria are measurable
- [X] Success criteria are technology-agnostic (no implementation details)
- [X] All acceptance scenarios are defined
- [X] Edge cases are identified
- [X] Scope is clearly bounded
- [X] Dependencies and assumptions identified

## Feature Readiness

- [X] All functional requirements have clear acceptance criteria
- [X] User scenarios cover primary flows
- [X] Feature meets measurable outcomes defined in Success Criteria
- [X] No implementation details leak into specification

## Notes

- Items marked incomplete require spec updates before `/speckit-clarify` or `/speckit-plan`

### 検証の記録

**全項目達成。[NEEDS CLARIFICATION] なし。**

判断に迷う点はあったが、いずれも既定を置いて Assumptions に記録した。
問い返すより、根拠を書いて進める方が速く、後から覆せる。

| 論点 | 置いた既定 | 根拠 |
|---|---|---|
| 掲載先 | `docs/` 配下の単一文書、README からリンク | README が既に長い。既存の `docs/development.md` と同じ配置規則 |
| 記述言語 | 日本語 | 既存の利用者向け文書と揃える |
| 機械検査の範囲 | 列挙できるものに限る (名前・ラベル・種別・経路) | 散文は実装から導けない。何を検査しないかを明示する方が誠実 |
| 未実装の規範要求の扱い | 事実を書き、実装は別機能 | 利用者への要求は「文書化」であり、機能追加ではない |

### 対象を広げた経緯

本仕様は当初、`/metrics` が出す系列の一覧に限定していた
(`003-metrics-reference-docs`)。次の 2 つの指示で対象を広げた。

1. 「シークレット管理サービスも、KMS を使った時に、どの key になるかが
   書いてないです。認証方法も不明なので、これもドキュメント化してください」
2. 「003 は metrics に限定せずリファレンス文書にしましょう」

これに伴い、優先度を組み替えた。**設定とトークンの供給元が P1** である。
これが分からないと起動できず、計測値を読む段階に到達しない。当初 P1 だった
計測値の一覧は P2 になった。

旧仕様の FR-010 (文書と実装の食い違いを機械的に検出する) と SC-005 は、
対象を計測値から設定項目まで広げたうえで FR-031〜FR-033 と SC-008 に
引き継いだ。

### 計画時に持ち越す論点

仕様の欠陥ではないが、`/speckit-plan` で扱う必要がある。

1. **機械的な検査をどう実装するか** (FR-031〜FR-033)。文書を構造化して読み取る
   のか、実装側から一覧を生成して突き合わせるのかで、書き手の負担が変わる。
2. **散文の正しさをどう保つか** (FR-032 の裏側)。認証の解決順や必要な権限は
   機械検査できない。人の注意に委ねる範囲をどう狭めるか。
3. **既存の `docs/reference.md` との差分**。US1・US2 の記述は既に存在する
   (コミット `98783eb`)。不足しているのは US3 の検査である。

### 範囲外として明示した事項

- **ログの OpenTelemetry 形式での送出が未実装**。規範 (原則 V) が MUST と
  しているが実装されていない。本機能では既知の制限として記載するに留める。
  **別の機能として実装が必要である。**
- ゾーンロックの取得・解放が計測されていない。同じく記載に留める。
