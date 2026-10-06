# Implementation Plan: アクセストークンの供給元をファイルに限る

**Branch**: `feature/007-file-only-token` | **Date**: 2026-10-06 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/007-file-only-token/spec.md`

## Summary

DPF のアクセストークンを、外部のシークレット管理サービス (Vault / AWS / Azure / GCP)
から直接取得する経路を廃止する。供給元は `--dpf-token-file` の 1 つに限る。
本サービスは Kubernetes 上でしか動かず、外部サービスの値をファイルとして Pod へ
届ける仕組みは Kubernetes 側にあるためである。

**機能を足すのではなく、削る変更である。** ファイル経路の振る舞いは変えない。
設計上の判断は 3 つ。**(1)** 廃止するフラグは定義ごと消し、未定義フラグとして
拒否させる。既存の `--dpf-token` と同じ仕組みで、専用の分岐を作らない (research R1)。
**(2)** 設定の型からシークレット管理サービスのフィールドを消し、ファイルを必須に
する (research R2)。**(3)** SDK を依存から外したうえで、`depguard` で再び import
されることを CI で止める。constitution v3.0.0 の MUST NOT を、レビューではなく
機械で守らせる (research R5)。

## Technical Context

**Language/Version**: Go 1.27 (変更なし)

**Primary Dependencies**: `github.com/iij/dpf-go` v0.6.0 の `utils.TokenFromFile`
(既に使用中)。**直接依存を 10 件削除する**: `github.com/hashicorp/vault/api`、
`github.com/aws/aws-sdk-go-v2/{config,service/secretsmanager}`、
`github.com/Azure/azure-sdk-for-go/sdk/{azidentity,security/keyvault/azsecrets}`、
`cloud.google.com/go/secretmanager`、`github.com/iij/dpf-go/misc/{vault,aws,azure,gcp}`。
新しい依存は増えない

**Storage**: なし

**Testing**: `go test`。単体 (設定の解釈、トークンプロバイダ) と文書・実装の一致検査
(`test/docs`)。e2e は入力が変わらないため、既存のローテーション検証をそのまま使う。
契約テストは不要 — webhook API の外形は変わらない

**Target Platform**: Kubernetes 上のサイドカー (変更なし)

**Project Type**: 単一の HTTP サービス (変更なし)

**Performance Goals**: なし。ファイル経路は従来からキャッシュせず、変わらない

**Constraints**:

- ファイル経路の振る舞い (要求ごとの読み直し、空白の除去、恒久的な失敗、内容を
  漏らさないこと) を変えない
- `provider.Backend` の署名を変えない。変更は設定層と `internal/dpf` のトークン部分に閉じる
- 移行期間を設けない (未リリースのため。spec Assumptions)

**Scale/Scope**: Go コード 3 ファイル (`config.go`、`token.go`、`client.go`) と
そのテスト、`test/docs`、`.golangci.yml`、`go.mod` / `go.sum`、README、
`docs/reference.md`、001 の spec への注記。
基準値: リンクされるモジュール 97、うち HashiCorp 系 10 (research「現状の把握」)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

適用する constitution: **v3.0.0** (2026-10-06 改訂、未コミット)

| 原則・規定 | 判定 | 根拠 |
|---|---|---|
| I. Webhook 契約への準拠 | ✅ | webhook API の外形に触れない |
| II. プロバイダ境界の分離 | ✅ | 変更は設定層と `internal/dpf` のトークン組み立てに閉じる。`Backend` は不変 |
| III. テストファースト | ✅ (実装時に遵守) | 「廃止フラグで起動に失敗する」「ファイル未指定で失敗する」「`--help` に現れない」テストを先に書き、失敗を確認してから削除する。tasks で順序を固定する |
| IV. DNS 変更の安全性 | ✅ | DNS の適用経路に触れない |
| V. 可観測性 | ✅ | トークン取得失敗のログとメッセージの定型化は従来どおり |
| VI. Default-Deny | ✅ | ファイル未指定は起動失敗。廃止フラグは黙って無視せず起動失敗 (R1) |
| 技術・配布制約 > バイナリの既定拒否 (v3.0.0) | ✅ | **本機能がこの改訂への適合そのもの**。現行コードは違反しており、本機能で解消する |
| 技術・配布制約 > イメージとチャートの既定拒否 (v3.0.0) | ✅ | 推奨 values に元々シークレット管理サービスの egress はない。README の案内で egress を足さない |
| ライセンス | ✅ | 依存が減るのみ。実測値の更新を求める MUST があり、tasks に含める |
| DPF API クライアント | ✅ | `dpf-go` の利用は変わらない |
| 品質ゲート | ✅ | `depguard` を足す。ゲートを緩めない |
| Governance「手段を constitution に書かない」 | ✅ | `depguard` の設定などの手段は plan と research に置く |

**ゲート判定: 通過**。違反なし。Complexity Tracking は空。

### Phase 1 後の再確認

設計成果物 (data-model、contracts、quickstart) で新たに生じた論点はない。

- 契約 (contracts/configuration.md) は廃止フラグを「起動失敗」とし、原則 VI に沿う
- `depguard` の追加は linter を 1 つ足すだけで、ゲートを緩める方向の変更はない
- 依存の削減により、constitution の「ライセンス」節の実測値 (2026-09-09 時点) が
  古くなる。**実装の最後に実測し直し、constitution に反映すること** (v3.0.0 の
  Follow-up、MUST)。v3.0.0 が未マージのうちは同じ版の本文として直し、
  マージ済みなら PATCH (v3.0.1) とする

**再判定: 通過**。

## Project Structure

### Documentation (this feature)

```text
specs/007-file-only-token/
├── spec.md
├── plan.md              # 本ファイル
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── configuration.md # Phase 1 — コマンドライン引数の契約
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks で作成)
```

### Source Code (repository root)

```text
internal/
├── config/
│   ├── config.go          # 3 フラグ・3 フィールド・許可リストを削除、TokenFile を必須に
│   └── config_test.go     # 廃止フラグ拒否のテストを追加、シークレット管理サービスのテストを削除
└── dpf/
    ├── token.go           # ファイル経路のみに縮小、SDK の import を削除
    ├── token_test.go      # シークレット管理サービスのテスト 2 件を削除、空ファイルのテストを追加
    └── client.go          # TTL 分岐と secretManagerTokenTTL を削除

test/docs/
├── reference_test.go      # TestReference_SecretManagers と "secret-managers" を削除
├── doc.go                 # 該当コメントを削除
└── extract.go             # 該当コメントを削除

.golangci.yml              # depguard を有効化し SDK の import を禁止
go.mod / go.sum            # go mod tidy で直接依存 10 件を削除

README.md                  # 「アクセストークンの与え方」を改訂、外部サービス利用時の案内と subPath の注意を追加
docs/reference.md          # 設定表・検証規則・供給元の節・一致検査の表・既知の制限を改訂

specs/001-webhook-provider/spec.md   # FR-035 / FR-036 に廃止の注記
.specify/memory/constitution.md      # 実装後、依存ライセンスの実測値を更新 (PATCH)
```

**Structure Decision**: 既存の単一サービス構成のまま。新しいパッケージ・ディレクトリは
作らない。

## 実装の順序 (tasks への申し送り)

1. **先にテストを書く** (原則 III)
   - `config`: 廃止 3 フラグそれぞれで `Load` が失敗し、エラーにフラグ名が含まれる
   - `config`: `--dpf-token-file` と廃止フラグの併用で失敗する
   - `config`: `--dpf-token-file` なしで `ErrMissingRequired`、メッセージに
     `--dpf-token-file` を含む
   - `config`: `Usage()` に `secret` が現れない
   - `dpf`: 空・空白のみのファイルで、トークン取得 (またはクライアント経由の
     呼び出し) が失敗する
   - この時点でテストが**失敗する**ことを確認する
2. 設定層 (`config.go`) を縮める
3. `token.go` と `client.go` を縮める。シークレット管理サービスのテストを削除
4. `go mod tidy`、`depguard` の設定
5. `test/docs` と `docs/reference.md` を同時に改める (片方だけでは `make all` が通らない)
6. README を改める
7. 001 の spec に注記
8. `quickstart.md` の手順 1〜4・6 を実行。依存の実測値を constitution に反映

## Complexity Tracking

なし。
