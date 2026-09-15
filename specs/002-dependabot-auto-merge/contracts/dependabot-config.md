# Contract: 更新の検出と提案の設定

**Feature**: [../spec.md](../spec.md) | **Date**: 2026-09-09

`.github/dependabot.yml` が満たすべき契約。形式は GitHub が定める
([options reference](https://docs.github.com/en/code-security/dependabot/working-with-dependabot/dependabot-options-reference))。
本書はそのうち本機能が要求する内容を定める。

**この契約は `test/config/dependabot_test.go` が機械的に検査する。** 文書の記述
だけでは、後から緩める変更を止められない。

---

## 満たすべき内容

### 待機期間 (FR-002、FR-003)

- すべての `updates` 要素に `cooldown.default-days: 5` を置くこと (MUST)。
- 省略しないこと (MUST NOT)。省略すると既定の 3 日になり、要件と食い違う。
- `cooldown` をセキュリティ更新へ広げる設定を書かないこと (MUST NOT)。
  Dependabot はバージョン更新にのみ待機期間を適用する。この既定を変えない。

### 対象 (FR-004)

次の 3 つの `updates` 要素が存在すること (MUST)。

| `package-ecosystem` | `directory` | 対象 |
|---|---|---|
| `gomod` | `/` | `go.mod` / `go.sum` |
| `github-actions` | `/` | `.github/workflows/*.yml` |
| `docker` | `/build` | `build/Containerfile` |

### まとめ (FR-005)

- 各 `updates` 要素に `groups` を 1 つ定義し、`patterns: ["*"]` で種別内の
  更新を 1 つの Pull Request にまとめること (MUST)。
- 種別をまたいでまとめないこと (MUST NOT)。

### 提案数の上限 (FR-008)

- `open-pull-requests-limit` を明示し、既定 (5) 以下にすること (MUST)。
- 上限を設けない設定を書かないこと (MUST NOT)。実際の DPF に対する検証が
  直列であるため、上限がないと待ち行列が伸び続ける。

### 更新の大きさによる除外 (FR-007)

- 互換性のない変更を含む版 (メジャー版) を `ignore` で除外しないこと (MUST NOT)。
  提案は行い、取り込むかどうかは人が判断する。

### 非公開モジュールの解決 (FR-017、research R3)

**解消済み (2026-09-14)。** `github.com/iij/dpf-go` とその副モジュールは公開され、
公開プロキシと checksum データベースから解決できる。

- `registries` を置かないこと (MUST NOT)。Go モジュールの解決に認証は要らない。
- モジュール取得のための Dependabot secret を登録しないこと (MUST NOT)。
  不要になった資格情報を残さない。

---

## 書いてはならない内容

| 書いてはならないもの | 理由 |
|---|---|
| 自動マージを行う設定・ワークフロー | 憲章がレビュー承認なしのマージを禁じる (FR-009) |
| `DPF_TOKEN` を Dependabot secret として参照する記述 | 更新後の依存コードと秘密情報が同居する (FR-013) |
| モジュール取得のための `registries` と資格情報 | `dpf-go` は公開済みであり、認証を要さない (FR-017) |
| メジャー版を `ignore` する記述 | 提案自体は行う (FR-007) |
| `cooldown` の省略 | 既定 3 日に緩む (FR-002) |
| `open-pull-requests-limit` の省略または 5 超 | 検証の待ち行列が有界でなくなる (FR-008) |

---

## 参考: 想定する形

契約を満たす一例。実装時に細部が変わってよいが、上記の MUST / MUST NOT は
変えられない。

```yaml
# SPDX-License-Identifier: Apache-2.0

version: 2

updates:
  - package-ecosystem: gomod
    directory: /
    schedule:
      interval: weekly
      day: monday
      time: "04:00"
      timezone: Asia/Tokyo
    # 版の公開から 5 日は提案しない。汚染された版や誤って公開された版が
    # 取り下げられる窓を避ける。**省略すると既定 3 日に緩む。**
    # セキュリティ更新には適用されない (Dependabot の仕様)。
    cooldown:
      default-days: 5
    # 実際の DPF に対する検証は検証用ゾーンを共有するため直列に走る。
    # 提案の数がそのまま検証の待ち行列になるため、まとめて有界に保つ。
    groups:
      go-modules:
        patterns: ["*"]
    open-pull-requests-limit: 3
    labels: ["dependencies"]
    commit-message:
      prefix: chore
      include: scope

  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
      day: monday
      time: "04:00"
      timezone: Asia/Tokyo
    cooldown:
      default-days: 5
    groups:
      actions:
        patterns: ["*"]
    open-pull-requests-limit: 3
    labels: ["dependencies"]
    commit-message:
      prefix: chore

  # build/Containerfile のビルド段 (golang:1.27-alpine) が対象。
  # FROM scratch には版がないため対象にならない。
  - package-ecosystem: docker
    directory: /build
    schedule:
      interval: weekly
      day: monday
      time: "04:00"
      timezone: Asia/Tokyo
    cooldown:
      default-days: 5
    groups:
      base-images:
        patterns: ["*"]
    open-pull-requests-limit: 3
    labels: ["dependencies"]
    commit-message:
      prefix: chore
```
