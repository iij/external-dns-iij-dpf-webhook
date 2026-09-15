# Implementation Plan: 依存パッケージの更新 Pull Request の自動作成

**Branch**: `002-dependabot-auto-merge` | **Date**: 2026-09-09 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-dependabot-auto-merge/spec.md`

## Summary

依存パッケージの更新を Dependabot に検出させ、更新 Pull Request を自動で作らせる。
版が公開されてから **5 日**が経つまで提案しない (`cooldown.default-days: 5`)。
汚染された版や誤って公開された版が取り下げられる窓を避けるためである。
セキュリティ更新はこの待機の対象外であり、Dependabot 側の仕様としてそうなっている。

**マージは自動化しない。** 人のレビューと承認を経る。憲章がレビュー承認なしの
マージを禁じているためであり、また当初の指示から自動マージが除外された。

設計上の要点は 1 つに集約される。**更新後の依存のコードと、CI が使う秘密情報を
同じ実行に同居させない。** `go test ./...` は依存のコードを実行する。汚染された
版がトークンを読み取れる状態にすると、待機期間を置く意味が薄れる。

したがって、更新 Pull Request による実行では秘密情報を与えず、秘密情報を要する
検査は失敗させる。保守担当者が上流の差分を確認したうえで、その Pull Request の
ブランチへコミットを 1 つ積む。以降の実行は保守担当者が起点となり、通常どおり
すべての検査が実行され、その結果が Pull Request に紐づく。新しい仕組みを足さずに、
人の確認を経路の中に置ける。

`github.com/iij/dpf-go` は公開されており、モジュールの解決に資格情報は要らない。
Dependabot 側に持たせる秘密情報は無い。

## Technical Context

**Language/Version**: 設定ファイルとシェル。Go の変更はない (Go 1.27、既存の
コードには手を入れない)

**Primary Dependencies**: GitHub Dependabot (`.github/dependabot.yml`)、
GitHub Actions (既存の `ci.yml` / `e2e.yml` / `e2e-sidecar.yml` / `scheduled.yml`)、
`gh` CLI (定期検査での Pull Request の照会)、`go list -m -u all` (更新可能な依存の列挙)

**Storage**: N/A

**Testing**: `actionlint` (ワークフローの静的検査)、`.github/dependabot.yml` の
不変条件を検査する仕組み (待機期間が 5 日であること、自動マージが有効でないこと、
3 種別が揃っていること)。既存の `make all` はそのまま

**Target Platform**: GitHub (Actions ランナーは `ubuntu-latest`)

**Project Type**: 既存サービスのリポジトリに対する CI / 保守自動化の追加。
配布物には影響しない

**Performance Goals**: 週あたりに作られる更新 Pull Request を **3 件以内**に
収める (種別ごとに 1 件)。実際の DPF に対する検証が直列であるため、提案の数が
そのまま検証の待ち行列になる

**Constraints**:
- 実際の DPF に対する検証を並列に実行しない (憲章「実環境での検証」、FR-016)
- 更新後の依存コードと秘密情報を同じ実行に同居させない (FR-013)
- 検査を条件付きで飛ばさない。飛ばした検査は成功として数えられる (FR-014)

**Scale/Scope**: Go モジュールの直接・間接依存が 100 件超、ワークフロー 5 本、
基底イメージ 1 件。変更するファイルは新規 1 本と既存 3 本の見込み

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### Phase 0 時点の評価

| 原則 | 評価 | 根拠 |
|---|---|---|
| I. ExternalDNS 契約準拠 | 該当なし | エンドポイントもメディアタイプも変えない。契約テストは既存のまま必須検査に含まれる |
| II. プロバイダ境界の分離 | 該当なし | Go のコードに手を入れない |
| III. テストファースト | **要対応** | 設定ファイルの追加でも、要件を機械的に検査できる形にする。下記参照 |
| IV. DNS 変更の安全性 | **適合** | FR-016 が検証の直列実行を保つ。既存の `concurrency` をそのまま使う |
| V. 可観測性と運用性 | **適合** | FR-014 (未実行を成功にしない)、FR-017・FR-018 (検出の停止と滞留を伝える) が、この原則の「気付けること」に対応する |
| VI. Default-Deny | **適合** | 自動マージを有効にしない (FR-009)。秘密情報を既定で与えない (FR-013)。提案数に上限を置く (FR-008)。いずれも許可側を既定にしない |

**原則 III への対応**: 「設定ファイルだからテストは書けない」で済ませない。
本機能の要件のうち、次は機械的に検査できる。実装より先にこの検査を書き、
落ちることを確かめてから設定を追加する。

- `cooldown.default-days` が `5` であること (FR-002)
- 3 種別 (`gomod` / `github-actions` / `docker`) が揃っていること (FR-004)
- 種別ごとに `groups` が定義され、提案がまとめられること (FR-005)
- `open-pull-requests-limit` が既定 (5) より狭いこと (FR-008)
- **自動マージを行うワークフローが存在しないこと** (FR-009)
- 秘密情報を要する検査が、Dependabot 起点の実行で条件付きに飛ばされていないこと
  (FR-014)

最後の 2 つが要点である。将来「便利だから」と自動マージを足す変更や、
「失敗が邪魔だから」と検査を飛ばす変更は、この検査に落ちる。文書の記述だけでは
そうした変更を止められない。

### 技術・配布制約との関係

- **バージョン固定 (constitution v1.3.0)**: Dependabot はアクションの版を
  `@vX.Y.Z` の形のまま書き換える。固定という性質は保たれる。ただし
  **Makefile と `ci.yml` の `env` に書いたツールの版 (`GOLANGCI_LINT_VERSION` など)
  は Dependabot の対象外**である。これらは依存の記述ではないため、更新は
  引き続き人が行う。この非対称を文書に残す (research R6 末尾)。
- **SPDX ヘッダ**: 新規の `.github/dependabot.yml` にも既存のワークフローと同じく
  ヘッダを置く。`make license-check` の対象は Go ファイルだが、リポジトリの
  慣習に合わせる。
- **配布物への影響なし**: イメージにも成果物にも入らない。

### Phase 1 設計後の再評価

**新たな違反なし。**

- 追加する仕組みは、設定ファイル 1 本と既存ワークフローへの追記に収まった。
  新しいワークフローを増やしていない (憲章「複雑さは正当化を要する」)
- 秘密情報を要する検査を飛ばす分岐を一切入れない設計になっている。
  「失敗する」ことが FR-014 の要件そのものであり、分岐を入れないことが
  最も単純な実装になっている
- 原則 III の検査 (`test/config/dependabot_test.go`) が、自動マージの追加と
  検査の飛ばしの双方を機械的に禁じる形になった。原則が文書ではなくテストとして
  表現されている
- 原則 VI について、`cooldown` が「新しい版を既定で採らない」という形の
  既定拒否になっている。設定を消すと既定 3 日に緩む点だけ、テストで固定した

## Project Structure

### Documentation (this feature)

```text
specs/002-dependabot-auto-merge/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── dependabot-config.md
│   └── ci-gates.md
├── checklists/
│   └── requirements.md  # /speckit-specify output
└── tasks.md             # /speckit-tasks output (未作成)
```

### Source Code (repository root)

```text
.github/
├── dependabot.yml            # 新規。更新の検出と提案の設定
└── workflows/
    ├── ci.yml                # 秘密情報が無い場合の案内を足す
    ├── e2e.yml               # 変更なし (concurrency をそのまま使う)
    ├── e2e-sidecar.yml       # 変更なし
    └── scheduled.yml         # 未提案の更新と滞留の検査を足す

test/
└── config/
    └── dependabot_test.go    # 新規。設定の不変条件を検査する

docs/
└── development.md            # 更新 PR を取り込む手順を足す
```

**Structure Decision**: 既存の構造にそのまま載せる。

- 設定は `.github/dependabot.yml` に置く。GitHub が読む場所が決まっている
- 不変条件の検査は `test/config/` に置く。既存の `test/contract`、
  `test/integration`、`test/e2e` と同じ階層であり、`go test ./...` に乗る。
  独立したスクリプトにすると `make all` から漏れる
- 通知は既存の `scheduled.yml` に足す。すでに `govulncheck` とイメージ走査の
  結果を Issue として起こす経路があり、通知手段を増やさない
- **新しいワークフローを作らない。** 更新 Pull Request の検査は既存の
  `ci.yml` / `e2e.yml` / `e2e-sidecar.yml` がそのまま担う。依存更新のために
  別の検査経路を設けないことが FR-010 の要件である

## Complexity Tracking

> Constitution Check に違反がないため、記載事項なし。
