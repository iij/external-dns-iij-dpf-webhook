# Research: アクセストークンの供給元をファイルに限る

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-10-06

Technical Context に NEEDS CLARIFICATION は残っていない。以下は、既存コードを
読んで確定した設計判断である。

## 現状の把握

| 箇所 | シークレット管理サービスに関わる内容 |
|---|---|
| `internal/config/config.go` | フラグ 3 つ (`--dpf-token-secret-manager` / `-id` / `-endpoint`)、`DPF.SecretManager` / `SecretID` / `SecretEndpoint`、`UsesSecretManager()`、許可リスト `supportedSecretManagers`、`validateTokenSource` の分岐、`Usage()` の案内文 |
| `internal/dpf/token.go` | `newSecretManagerProvider` と 4 つの `new*Provider`。5 つの SDK と `dpf-go/misc/{vault,aws,azure,gcp}` を import |
| `internal/dpf/client.go` | `UsesSecretManager()` のときだけ `utils.WithTokenTTL(secretManagerTokenTTL)` (30 秒) を付ける |
| テスト | `config_test.go` に 4 件、`token_test.go` に 2 件、`test/docs/reference_test.go` の `TestReference_SecretManagers`、`TestUsage_ListsKeySettings` の期待値 1 つ |
| 文書 | README「アクセストークンの与え方」、`docs/reference.md` の設定表・検証の規則・供給元の節 (Vault / AWS / Azure / GCP / 比較)・検査対象の表・既知の制限 4 行 |
| `go.mod` | 直接依存 9 件 (SDK 5 + `dpf-go/misc/*` 4) |

e2e (`.github/workflows/e2e*.yml`、`test/`) はトークンをファイルでのみ与えており、
シークレット管理サービスを使っていない。

**計測の基準値** (2026-10-06、`go list -deps ./cmd/...` のモジュール数):
バイナリにリンクされるモジュールは **97**。うち HashiCorp 系 (MPL-2.0) が **10**。

## R1: 廃止するフラグをどう拒否するか

**Decision**: フラグの定義そのものを削除する。指定された場合は、標準の `flag`
パッケージが未定義フラグとして拒否する。`config.Load` は既にその誤りを
`ErrInvalid` で包んで返しており、`main` は起動を中止する。メッセージは
`flag provided but not defined: -dpf-token-secret-manager` の形で、**受け付け
なかったフラグ名を含む** (FR-004)。

**Rationale**:
- `--dpf-token` を拒否している既存の仕組みと同じであり、新しい経路を作らない
- フラグの定義が 1 箇所 (`newFlagSet`) にあるため、`--help` と受理される項目が
  ずれない
- 併用 (`--dpf-token-file` と同時指定) も同じ理由で拒否される (US2 シナリオ 2)。
  検証の分岐を足す必要がない

**Alternatives considered**:
- *廃止済みと分かる専用メッセージ* (「廃止しました。--dpf-token-file を使って
  ください」): 移行者に親切だが、廃止フラグの名前を実装に残し続けることになる。
  まだリリースがなく移行する利用者がいない (spec Assumptions) ため、得るものが
  ない。案内は `docs/reference.md` の「意図的に存在しないもの」の表で行う
- *警告を出して無視する*: 原則 VI (想定外の値で起動を続行しない) に反する

## R2: 設定の型をどう縮めるか

**Decision**: `config.DPF` から `SecretManager` / `SecretID` / `SecretEndpoint` と
`UsesSecretManager()` を削除する。`supportedSecretManagers` も削除する。
`validateTokenSource` は「`TokenFile` が空なら `ErrMissingRequired`」だけになる。

**Rationale**: 使われないフィールドを残すと、ゼロ値の組み合わせを考える必要が
残る。型から消せば、シークレット管理サービスを指す設定がコンパイル時に存在
しなくなる。

**Alternatives considered**: フィールドを残して検証で拒否する — 型が嘘をつく。却下。

関数名 `validateTokenSource` は、検証内容が 1 行になっても残す。起動時の必須項目の
検証であることを名前が示しており、呼び出し側の読みやすさは変わらないため。

## R3: トークンプロバイダの組み立て

**Decision**: `newTokenProvider` は残し、`cfg.TokenFile` が空なら誤り、そうでなければ
`utils.TokenFromFile(cfg.TokenFile)` を返すだけにする。`ctx` 引数は不要になるため
削除する。`newSecretManagerProvider` と 4 つの `new*Provider` は削除する。

**Rationale**:
- `config.Load` が先に弾く前提でも、境界の内側で既定を拒否側に置く (原則 VI)。
  既存のコメントと方針どおり
- 関数を残すのは、ファイル経路の性質 (ローテーション追随、内容を漏らさない) を
  検証する既存テスト 3 件の入口になっているため
- `unparam` が未使用の `ctx` を指摘するため、引数から外す

## R4: トークンのキャッシュ

**Decision**: `client.go` の `UsesSecretManager()` 分岐と `secretManagerTokenTTL`
定数を削除する。ファイル経路は従来どおりキャッシュしない (FR-006)。

**Rationale**: TTL はシークレット管理サービスへの問い合わせ回数を抑えるためだけに
あった。ファイルの読み取りは安価であり、キャッシュはローテーションの反映を遅らせる
だけである (既存コメントの判断を維持)。

**付記**: `docs/reference.md` は「キャッシュしない」と書きつつ、実装はシークレット
管理サービスに 30 秒の TTL を付けていた。本機能でシークレット管理サービスの経路が
消えるため、この食い違いも解消する。

## R5: 依存の除去と、再び入り込まないことの保証

**Decision**:
1. import を消したうえで `go mod tidy` を行い、直接依存 9 件を `go.mod` から外す
2. `golangci-lint` の **`depguard`** を有効にし、次の import を禁止する
   - `github.com/hashicorp/vault`
   - `github.com/aws/aws-sdk-go-v2`
   - `github.com/Azure/azure-sdk-for-go`
   - `cloud.google.com/go/secretmanager`
   - `github.com/iij/dpf-go/misc`

   禁止理由として constitution v3.0.0 を明記する

**Rationale**:
- FR-002 / FR-007 は「接続しない」「接続する機能を含まない」を求める。接続する
  コードが**リンクされていない**ことが、最も強い保証である
- FR-005 (SDK が読む環境変数を使わない) も同じ理由で満たされる。`VAULT_TOKEN` や
  `AWS_*` を読むのは各 SDK であり、SDK がなければ誰も読まない
- 後から誰かが便利だと思って SDK を足すことを、レビューに頼らず CI
  (`golangci-lint run`、品質ゲート) で止められる。constitution の MUST NOT を
  機械的に守らせる

**Alternatives considered**:
- *レビューだけに頼る*: 依存の追加は `go.mod` の差分に埋もれやすい。却下
- *`go list -deps` の結果を検査するテストを書く*: テストから `go` コマンドを
  呼ぶことになり、既存の linter で足りるものを自作することになる。却下

**検証**: 実装後に `go list -deps ./cmd/...` のモジュール数を測り、基準値 (97 / 10)
と比べる。HashiCorp・AWS・Azure・Google Cloud のモジュールが 0 であること。
`google.golang.org/grpc` などは OTLP が使うため残ってよい。

## R6: 文書の扱い

**Decision**:
- `docs/reference.md`
  - 設定表から 3 行を削除。「意図的に存在しないもの」に 3 つのフラグを追加し、
    理由を「v3.0.0 でファイルに限った。外部サービスの値は Kubernetes 側で
    ファイルとして届ける」とする
  - 「検証の規則」を「`--dpf-token-file` が必須」に改める
  - 「アクセストークンの供給元」から、用語の節、対応サービスの表
    (`<!-- reference:secret-managers -->`)、Vault / AWS / Azure / GCP の各節、
    比較の節を削除する。共通の性質の表はファイルの性質の表に統合する
  - 「この文書と実装の一致」から、シークレット管理サービスの行と、
    トークンが読まれる位置に関する注意を削除する
  - 「既知の制限」から、キャッシュ期間・Vault・JSON 解釈・バージョン固定の 4 行を削除する
- README
  - 「アクセストークンの与え方」を、ファイルのみの説明にする
  - **外部のシークレット管理サービスを使う場合**の節を新設する。Kubernetes 側の
    仕組み (External Secrets Operator で Secret へ同期する、Secrets Store CSI Driver で
    ボリュームとしてマウントする) で Pod 内のファイルにし、そのパスを
    `--dpf-token-file` に渡すことを示す。これらの導入手順は各プロジェクトの文書に委ねる
  - **`subPath` でマウントしないこと**の注意を書く (ローテーションが届かない)
  - Secret の更新がファイルに届くまで遅延があることを書く
- `test/docs`: `secret-managers` の印と `TestReference_SecretManagers` を削除する。
  `doc.go` / `extract.go` のコメントから該当の記述を除く

**Rationale**: spec FR-008 / FR-009 / SC-006。文書と実装の一致検査は、文書の印を
消せば検査も消す必要がある (spec Dependencies)。

**Alternatives considered**: CSI Driver / ESO の具体的な manifest を README に載せる —
上流の仕様に追随する負担を本リポジトリが負うことになる。考え方と注意点に留める。

## R7: 001 の spec の扱い

**Decision**: `specs/001-webhook-provider/spec.md` の FR-035 と FR-036 に、
「007 / constitution v3.0.0 で廃止」と注記する。本文は書き換えない。

**Rationale**: spec は判断の記録である。過去の要求を消すと、なぜその経路が
あったのか、いつ消えたのかを追えなくなる。

## R8: リリース記録

**Decision**: 本機能では `CHANGELOG.md` に触れない。

**Rationale**: `CHANGELOG.md` は `main` に存在せず、constitution もその記載を
求めていない (CHANGELOG を求める改訂案は 2026-10-06 に破棄された)。
未リリースのため、本機能の変更は初回リリースの内容に含まれる。
