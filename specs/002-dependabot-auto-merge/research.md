# Research: 依存パッケージの更新 Pull Request の自動作成

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-09-09

本書は Phase 0 の調査結果を記録する。仕様から持ち越した論点と、設定として
書けるかどうかを実際の文書で確認した結果をまとめる。

---

## R1. 待機期間を設定として表現できるか

**Decision**: Dependabot の `cooldown.default-days` に `5` を与える。更新の
大きさによる区別 (`semver-*-days`) は与えない。

**Rationale**: `cooldown` は「版が公開されてから一定日数が経つまで、その版への
更新を提案しない」設定である。仕様の FR-002 がそのまま表現できる。

対応状況を [Dependabot options reference](https://docs.github.com/en/code-security/dependabot/working-with-dependabot/dependabot-options-reference)
で確認した。

| 種別 | `default-days` | `semver-*-days` |
|---|---|---|
| `gomod` | 可 | 可 |
| `github-actions` | 可 | **不可** |
| `docker` | 可 | **不可** |

`gomod` だけが版の大きさによる区別に対応する。3 種別で書き方を揃えられないため、
一律 `default-days: 5` とする。指示も「cooldown は 5 日間」であり区別を求めて
いない (spec Assumptions)。

**重要**: `cooldown` は**バージョン更新にのみ適用される。セキュリティ更新には
適用されない。** これは FR-003 (脆弱性の修正に待機期間を適用しない) を、
こちらで何も書かずに満たす。逆に、待機期間を理由に修正の適用が遅れる懸念は
生じない。

**Alternatives considered**:

- **`semver-major-days` を長くする (メジャーのみ 30 日)**: 却下。`github-actions` と
  `docker` が対応せず、種別ごとに意味の違う設定になる。どの版がいつ提案されるかを
  保守担当者が予測しにくくなる。
- **待機期間を設けず、提案後に人が判断する**: 却下。指示が明示的に 5 日を求めて
  いる。また汚染された版は公開直後に取り下げられることが多く、待機はその窓を
  避ける最も単純な手段である。

---

## R2. 更新 Pull Request の CI に秘密情報をどう与えるか

**Decision**: **与えない。** 更新 Pull Request による実行では、秘密情報を要する
検査が失敗する。保守担当者が更新内容を確認した後、**その Pull Request のブランチへ
コミットを 1 つ積む**。以降の実行は保守担当者が起点となるため、通常どおり
すべての秘密情報が使える。

**Rationale**: これは本機能の動機と直結する。

Dependabot が起点の実行では、[GitHub の仕様により](https://docs.github.com/en/code-security/dependabot/troubleshooting-dependabot/troubleshooting-dependabot-on-github-actions)
読み取り専用の `GITHUB_TOKEN` と Dependabot secrets のみが使える。

> When a Dependabot event triggers a workflow, the only secrets available to the
> workflow are Dependabot secrets. GitHub Actions secrets are **not available**.

秘密情報を使えるようにする手段は存在するが、**`go test ./...` は更新後の依存の
コードを実行する。** 汚染された版が検証用ゾーンのトークンを読み取れる状態に
すると、待機期間を置く意味が薄れる。動機に反する設計は採らない (FR-013)。

秘密情報がなければ検査は失敗する。これは FR-014 (未実行を成功として扱わない) を
自然に満たす。**検査を条件付きで飛ばさない。** GitHub の分岐で飛ばした検査は
「skipped」となり、ブランチ保護では成功として数えられる。飛ばす設計は
FR-014 に反する。

保守担当者がコミットを積む経路を選んだのは、新しい仕組みを一切足さずに済むから
である。起点が人になれば `pull_request` の実行は通常の実行と同じになり、検査の
結果は Pull Request に紐づく。ブランチ保護の必須検査もそれで満たされる (FR-010)。

**Alternatives considered**:

- **DPF トークンと MODULE_TOKEN を Dependabot secrets に複製する**: 却下。
  更新後の依存コードと同じ実行にトークンが同居する。本機能の動機に反する
  (FR-013)。
- **Actions secrets へのアクセスを許可する設定を入れる**: 却下。上と同じ露出に
  加え、他のすべての秘密情報も露出範囲に入る。範囲が広い分だけ悪い。
- **`pull_request_target` をラベルで起動する**: 却下。秘密情報を持つ文脈で
  Pull Request の内容を実行するという、最も危険な形になる。人がラベルを貼るまで
  動かないという緩和はあるが、Dependabot の差分は版番号の変更だけであり、
  差分を見ても汚染は分からない。緩和として実質的に働かない。同じ人手を要する
  なら、仕組みを足さないコミット追加の方が単純である (憲章「複雑さは正当化を
  要する」)。
- **秘密情報を要する検査を Dependabot の実行では飛ばす**: 却下。skipped が成功と
  して数えられ、検査を経ずにマージできる状態になる (FR-014、SC-004)。

**残る危険**: 保守担当者がコミットを積んだ後の実行では、更新後の依存コードと
トークンが同居する。これは避けられない (テストは依存のコードを実行する)。
影響範囲を限定することで受け入れる。

- 検証用ゾーンのトークンは**検証用ゾーンにのみ**権限を持つ。本番ゾーンには
  及ばない (憲章「実環境での検証」)。
- 5 日の待機期間により、公開直後に取り下げられる版は提案されない。
- 保守担当者は上流の差分 (Dependabot が Pull Request に載せる比較) を確認した
  うえでコミットを積む。

---

## R3. 非公開モジュールの更新を検出できるか

**Decision (改訂 2026-09-14)**: `registries` も専用トークンも置かない。
`github.com/iij/dpf-go` とその副モジュールが公開され、公開プロキシと checksum
データベースから解決できるようになったため、認証は要らなくなった。

以下は公開前の判断の記録である。

**Rationale**: `github.com/iij/dpf-go` は非公開である。Dependabot が更新を検出する
には、この解決に認証が必要になる。[private registries の文書](https://docs.github.com/en/code-security/dependabot/working-with-dependabot/configuring-access-to-private-registries-for-dependabot)
が `git` 型を示している。

```yaml
registries:
  iij-private:
    type: git
    url: https://github.com
    username: x-access-token
    password: ${{secrets.DPF_GO_READ_TOKEN}}
```

ここで `secrets` は **Dependabot secrets** を指す。

**名前を分けるのが要点である。** Dependabot secrets は Dependabot 起点の
ワークフロー実行からも参照できる。しかしワークフローが参照しない秘密情報は、
実行環境に現れない。ワークフロー側は `secrets.MODULE_TOKEN` (Actions secret) を
参照し、Dependabot 起点の実行ではそれが空になる。`DPF_GO_READ_TOKEN` は
`dependabot.yml` だけが使うため、依存のコードから届かない。

同じ名前にすると、Dependabot 起点の実行でトークンが環境に載り、R2 の判断が
崩れる。

**このモジュールが公開されたら、`registries` の設定と専用トークンは不要になる。**
→ 2026-09-14 に公開された。上記の改訂のとおり、いずれも設けない。

**Alternatives considered**:

- **`MODULE_TOKEN` を Dependabot secrets にも同名で登録する**: 却下。上記のとおり
  Dependabot 起点の実行でトークンが環境に載る。
- **組織設定で Dependabot に非公開リポジトリへのアクセスを許可する**: 保留。
  組織の設定であり、本リポジトリの変更として表現できない。`registries` の方が
  設定がリポジトリに残り、公開後に消し忘れても差分として見える。
- **公開まで `gomod` を対象から外す**: 却下。Go の依存が本体であり、それを
  外すと機能の意味がほとんど残らない。

---

## R4. 更新が提案されていないことに気付けるか

**Decision**: 既存の `scheduled.yml` に検査を 1 つ足す。`go list -m -u all` で
更新可能な依存を求め、対応する Dependabot の Pull Request が存在しないものが
あれば報告する。

**Rationale**: Dependabot の更新処理が失敗している状態は、「更新がない」状態と
外から区別できない。Dependabot の設定が壊れた場合がまさにこれで、**Go の依存が
1 件も提案されなくなるが、リポジトリは静かなまま**になる。仕様が FR-017 と SC-009
でこれを禁じている。

Dependabot の実行状態を問い合わせる公開 API はない。そこで、**Dependabot に
依存しない側から見る。** 自分で更新可能な依存を数え、提案の数と突き合わせる。
差があれば、提案の仕組みが動いていない。

定期実行の文脈では起点が Dependabot ではないため、Actions secrets が使える。
ただし `dpf-go` の公開 (2026-09-14) により、モジュールの取得に資格情報は要らない。

**Alternatives considered**:

- **Dependabot の実行結果を API で確認する**: 却下。公開 API が存在しない。
  Insights の画面には「最後に確認した時刻」と失敗が出るが、人が見に行かなければ
  気付けない。
- **何もせず、文書で「Insights を見ること」と案内する**: 却下。仕様が
  「保守担当者に認識されていない状態で存在する件数が 0 件」を求めている
  (SC-009)。人が定期的に画面を見る前提は、その保証にならない。

---

## R5. 滞留した提案に気付けるか

**Decision**: R4 と同じ定期実行に、開いたままの Dependabot Pull Request の
経過日数を調べる検査を足す。**30 日**を超えるものがあれば報告する。

**Rationale**: マージが人の操作になったため、Pull Request が開いたまま忘れられる
状態は必ず生まれる (FR-018、SC-008)。脆弱性の修正が放置されると、自動化して
いない場合より危険である。「自動化してあるから大丈夫」と思われるためである。

報告先は既存の `notify` ジョブと同じ形にする。すでに `govulncheck` とイメージの
走査結果を Issue として起こす仕組みがあり、同じ経路に載せる。新しい通知手段を
足さない。

**Alternatives considered**:

- **Dependabot の `open-pull-requests-limit` に任せる**: 却下。上限に達すると
  新しい提案が作られなくなるだけで、滞留していることは伝わらない。むしろ
  「提案が来ない」という R4 の問題に化ける。
- **一定期間で自動的に閉じる**: 却下。FR-011 が閉じることを禁じている。
  閉じても更新の必要は消えず、次の実行で同じ提案が作られるだけである。

---

## R6. 提案の数を抑える

**Decision**: 種別ごとに `groups` を 1 つ置き、`patterns: ["*"]` で全依存を
1 つの Pull Request にまとめる。`open-pull-requests-limit` は種別ごとに `3`。

**Rationale**: 実際の DPF に対する検証は検証用ゾーンを共有するため並列に
実行できない (憲章「実環境での検証」、FR-016)。提案 1 件ごとに検証が要るため、
提案の数がそのまま検証の待ち行列になる。

現在の依存は Go モジュールが 100 件を超える。まとめなければ、更新のある週に
検証が数十回直列に積まれる。1 回の `e2e` が数分、`e2e-sidecar` を含めると
さらに長い。まとめることで週あたり最大 3 件 (種別の数) に収まる。

上限を種別ごとに `3` としたのは、まとめても Dependabot が分けて提案する場合
(まとめの対象外になる依存がある場合など) に備えた余裕である。既定の `5` より
狭くするのは、待ち行列を有界に保つため (原則 VI)。

**まとめることの代償**: 1 つの Pull Request に複数の更新が入るため、CI が
失敗したときどの更新が原因かが直ちに分からない。これは受け入れる。原因の
切り分けは、Pull Request 内の依存を個別に外して試せば足りる。検証の待ち行列が
伸び続ける方が運用への影響が大きい。

**Alternatives considered**:

- **まとめない (依存 1 件ごとに Pull Request)**: 却下。上記のとおり検証が
  積み上がる。
- **種別をまたいで 1 つにまとめる**: 却下。Go の依存とコンテナ基底イメージが
  同じ Pull Request に入ると、失敗したときの切り分けが難しくなる。また
  基底イメージの更新は `docker` 種別だけで完結し、Go の更新とは無関係である。
- **更新の大きさで分ける (パッチ・マイナー / メジャー)**: 保留。有用だが、
  `github-actions` と `docker` は `update-types` による分割に対応する一方
  `cooldown` の `semver-*` に対応しないなど、種別ごとの差が設定を複雑にする。
  まず一律でまとめ、運用して不便が出てから分ける。

---

## 対象となる依存の所在

| 種別 | 対象 | ディレクトリ |
|---|---|---|
| `gomod` | `go.mod` / `go.sum` | `/` |
| `github-actions` | `.github/workflows/*.yml` のアクションの版 | `/` |
| `docker` | `build/Containerfile` の `FROM docker.io/library/golang:1.27-alpine` | `/build` |

`docker` 種別が `Containerfile` を見つけるかを確認した。dependabot-core の
[docker/file_fetcher.rb](https://github.com/dependabot/dependabot-core/blob/main/docker/lib/dependabot/docker/file_fetcher.rb)
が `DOCKER_REGEXP = /dockerfile|containerfile/i` で照合しており、`Containerfile`
も対象になる。

`FROM scratch` には版がないため更新の対象にならない。基底イメージのうち
更新されるのはビルド段の `golang:1.27-alpine` のみである。

**Makefile に固定したツールの版は対象外である。** `GOLANGCI_LINT_VERSION` などは
Dependabot が扱う依存の記述ではない。これらの更新は引き続き人が行う。この事実を
文書に残す。
