# Quickstart: 依存パッケージの更新 Pull Request の自動作成

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-09-09

本書は、実装が spec の受け入れ条件を満たすことを確認する手順をまとめる。
実装コードは含まない。設定の詳細は [contracts/](./contracts/) を参照。

---

## 前提

| 項目 | 内容 |
|---|---|
| 権限 | リポジトリの設定 (Dependabot secrets、ブランチ保護) を変更できること |
| Dependabot secret | `DPF_GO_READ_TOKEN` (`github.com/iij/dpf-go` の読み取り権限のみ) |
| Actions secret | `MODULE_TOKEN`、`DPF_TOKEN` (既存) |

**`DPF_GO_READ_TOKEN` を Actions secret として登録しないこと。** また
`MODULE_TOKEN` と `DPF_TOKEN` を Dependabot secret として登録しないこと。
区分の理由は [data-model.md](./data-model.md) の「資格情報の区分」にある。

---

## 1. 設定の不変条件

DPF にもネットワークにも依存しない。`make all` に含まれる。

```bash
go test ./test/config/... -v
```

| 確認 | 期待される結果 |
|---|---|
| `cooldown.default-days` が 5 | 成功 |
| 3 種別が揃っている | 成功 |
| 種別ごとに `groups` がある | 成功 |
| `open-pull-requests-limit` が 5 以下 | 成功 |
| 自動マージを行うワークフローがない | 成功 |
| 秘密情報を要する検査が条件付きで飛ばされていない | 成功 |

**この検査は実装より先に書く (憲章 原則 III)。** 設定を追加する前に実行し、
落ちることを確かめる。落ちない検査は何も守っていない。

---

## 2. 設定の構文

```bash
actionlint                                    # ワークフロー
python3 -c 'import yaml,sys; yaml.safe_load(open(".github/dependabot.yml"))'
```

Dependabot の設定に対する公式のローカル検証器はない。構文の誤りは GitHub 側で
検出され、リポジトリの Insights → Dependency graph → Dependabot に表示される。

---

## 3. 更新の検出 (US1)

**待機期間の検証には時間の経過が要る。** 版が公開されてから 5 日という条件は、
その場で確かめられない。次の 2 段で見る。

### 3a. 設定が読み込まれたことの確認

設定を main へ入れた後、リポジトリの Insights → Dependency graph → Dependabot を開く。

| 確認 | 期待される結果 |
|---|---|
| 3 種別が一覧に現れる | `gomod` / `github-actions` / `docker` |
| 最後に確認した時刻 | 設定の投入後に更新されている |
| エラー表示 | **ない**。非公開モジュールの解決に失敗していれば `gomod` にエラーが出る |

`gomod` にエラーが出る場合、`DPF_GO_READ_TOKEN` が未登録か権限不足である。
**この状態では Go の依存が 1 件も提案されない。**

### 3b. 提案の内容の確認

手動で更新を走らせる場合は、Insights → Dependency graph → Dependabot から
"Check for updates" を実行する。

| 確認 | 期待される結果 |
|---|---|
| 種別ごとの Pull Request の数 | 各 1 件 (まとめられている) |
| Pull Request の本文 | 更新前後の版と、上流の比較への導線がある |
| 公開から 5 日未満の版 | **提案に含まれない** |
| メジャー版への更新 | 提案に**含まれる** (除外しない) |
| ラベル | `dependencies` |

---

## 4. 更新 Pull Request の検査 (US2)

### 4a. Dependabot 起点の実行

| 確認 | 期待される結果 |
|---|---|
| シークレットの混入検査 | **成功する** |
| 整形、SPDX ヘッダ、`actionlint`、設定の不変条件 | **成功する** |
| ビルド、テスト、`golangci-lint`、`govulncheck` | **失敗する** |
| イメージのビルド、ASLR、ライセンス | **失敗する** |
| 実際の DPF に対する検証 | **失敗する** |
| 失敗した検査の表示 | `skipped` ではなく `failure` |
| 失敗の理由 | 「Dependabot 起点のため秘密情報を参照できない」と、**次に何をすべきか**が読める |

**`skipped` になっていないことが要点である。** GitHub は分岐で飛ばした検査を
ブランチ保護の上で成功として数える。`skipped` があれば、検査を経ずにマージ
できる状態になっている (FR-014、SC-004)。

### 4b. 保守担当者が引き取った後の実行

上流の差分を確認したうえで、Pull Request のブランチへコミットを 1 つ積む。

```bash
gh pr checkout <PR 番号>
git commit --allow-empty -m "chore: run the full gate with credentials"
git push
```

| 確認 | 期待される結果 |
|---|---|
| 起点 | 保守担当者 (`dependabot[bot]` ではない) |
| すべての必須検査 | **実行される** |
| 検査の結果 | Pull Request に紐づく (ブランチ保護の必須検査を満たせる) |
| 実際の DPF に対する検証 | 検証用ゾーンに対して実行される |

### 4c. マージ

| 確認 | 期待される結果 |
|---|---|
| 必須検査がすべて成功、レビュー未承認 | **マージできない** |
| レビュー承認済み、必須検査に失敗あり | **マージできない** |
| 両方満たす | マージできる |
| 自動でマージされること | **ない** (FR-009) |

---

## 5. 並列実行の抑止 (FR-016)

更新 Pull Request が 2 つ以上ある状態で、両方の実 DPF 検証を動かす。

| 確認 | 期待される結果 |
|---|---|
| 同時に走る検証の数 | **1 つ**。後続は待つ |
| 待たされた側 | 打ち切られず、順番が来たら実行される |
| 検証用ゾーンの残留レコード | **ない** |

既存の `concurrency` グループ `e2e-verification-zone` がこれを担う。
更新 Pull Request のために別の経路を作っていないため、設定は共有される。

---

## 6. 提案の欠落と滞留 (US3)

`scheduled.yml` の検査。手動で走らせる場合は workflow_dispatch を使う。

### 6a. 提案の欠落 (FR-017、SC-009)

| 手順 | 期待される結果 |
|---|---|
| 更新可能な依存があり、対応する提案がある | 報告なし |
| `DPF_GO_READ_TOKEN` を無効にして再実行 | **報告される** (Go の更新が提案されていない) |
| 公開から 5 日未満の版のみ更新可能 | 報告なし (提案されないのが正しい) |

2 行目が要点である。**「更新がない」と「仕組みが動いていない」を区別できる
ことがこの検査の目的**である。

### 6b. 滞留 (FR-018、SC-008)

| 手順 | 期待される結果 |
|---|---|
| 30 日以内の更新 Pull Request のみ | 報告なし |
| 30 日を超えた更新 Pull Request がある | **報告される** |
| 報告先 | 既存の通知経路 (Issue) |
| 自動で閉じられること | **ない** (FR-011) |

---

## 7. 公開後の後始末

`github.com/iij/dpf-go` が公開されたら、次を削除する。

| 対象 | 場所 |
|---|---|
| `registries` の節 | `.github/dependabot.yml` |
| `gomod` の `registries` 参照 | 同上 |
| `DPF_GO_READ_TOKEN` | Dependabot secrets |

削除後、3a を再実行して `gomod` にエラーが出ないことを確認する。
不要になった資格情報を残さない。
