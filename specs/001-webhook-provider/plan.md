# Implementation Plan: ExternalDNS webhook provider 本体

**Branch**: `001-webhook-provider` | **Date**: 2026-09-04 (改訂 2026-09-15) | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-webhook-provider/spec.md`

## Summary

ExternalDNS の webhook provider として動作する単一の Go HTTP サービスを実装する。
ExternalDNS からの 4 つの要求 (管理対象ドメインのネゴシエーション、レコード一覧の取得、
プロバイダ固有の調整、変更の適用) を受け、IIJ DPF の DNS API を通じてレコードを読み書きする。

技術的な要点は 5 つ。**(1)** 変更の適用は「ゾーンロック取得 → 反映済みレコードの全件取得 →
マージ → 一括更新とゾーン反映を原子的に実行 → 完了待ち」を 1 操作として扱う。
**(2)** 一括更新は渡さなかったレコードを消すため、管理対象外のレコードは読み取った値を
逐語的にコピーして投入し、投入前ガードで想定外の削除を検出する。**(3)** ドメイン名は
正規化名を保持する専用型で扱い、文字列比較による範囲判定を型の段階で排除する。
**(4)** 名前がどのゾーンに属するかを決める規則を 1 箇所だけに置き、読み取りと書き込みの
双方がそれを使う。親子・孫ゾーンが併存する環境で、両者の帰属がずれることを構造で防ぐ。
**(5)** ASLR 有効かつ `scratch` で動くバイナリは、cgo を有効にした外部リンカ経由の
static-pie ビルドでのみ得られる (実測で確認済み)。

**改訂 (2026-09-15)**: spec に User Story 5 (親子・孫ゾーンの併存) と FR-040〜FR-045 が
追加され、`NS` が対応レコード種別から外れた。要点 (4) はこの改訂で加わったものである
(research R12)。

## Technical Context

**Language/Version**: Go 1.27 (go.mod の `go` ディレクティブは `1.27`)

**Primary Dependencies**:

- `github.com/iij/dpf-go` — DPF API クライアント (認証・ページング・レート制御・
  リトライ・非同期ジョブ待ち・ゾーンロック)。v1.0.0 未満のため固定バージョンで参照
- `github.com/miekg/dns` — ドメイン名の正規化・比較・包含判定・ラベル分割
- OpenTelemetry Go SDK — ログ・メトリクス・トレース
- `github.com/iij/dpf-go/misc/{vault,aws,azure,gcp}` — シークレット管理サービス連携 (任意)

**Storage**: なし。状態は DPF が保持する。本サービスは永続状態を持たない

**Testing**: `go test`。契約テスト / 統合テスト (DPF API はモック) / 単体テストの 3 層

**Target Platform**: Linux コンテナ (`scratch`)。Kubernetes 上で external-dns Pod の
サイドカーとして動作

**Project Type**: 単一の HTTP サービス

**Performance Goals**: 管理対象ゾーンに 1,000 件のレコードがある状態で、レコード一覧の
取得が ExternalDNS の待ち受け時間内に完了する (SC-008)

**Constraints**:

- provider エンドポイント (既定 `8888`) はループバックのみにバインド
- exposed エンドポイント (既定 `8080`) で `/healthz` と `/metrics` を提供
- バイナリは ASLR 有効、`scratch` 上で動作、非 root 数値 UID
- アクセストークンはファイルまたはシークレット管理サービスからのみ取得

**Scale/Scope**: 単一の DPF 契約に属する複数ゾーン。ExternalDNS 1 インスタンスに対して
サイドカー 1 つ

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

適用する constitution: **v2.2.0**

| 原則 / 制約 | 本 plan での満たし方 | 判定 |
|---|---|---|
| I. ExternalDNS Webhook 契約への準拠 | `internal/webhook` を契約の実装に限定。`api/webhook.yaml` に対する契約テストを仕様の実行可能な表現とする。独自エンドポイント・独自フィールドを追加しない | PASS |
| II. プロバイダ境界の分離 | `internal/dpf` に `dpf-go` を閉じ込め、生成型とライブラリ固有エラーを外へ出さない。上位層はインタフェース結合。上位層のテストは DPF API に到達しない | PASS |
| III. テストファースト | 全層でテストを先に書く。契約テスト → 単体 → 統合の順に赤を確認してから実装。tasks で順序を強制する | PASS |
| IV. DNS 変更の安全性 | 範囲判定を書き込み直前に `dns.IsSubDomain` で実施。管理対象外レコードは逐語コピーで保全し、投入前ガードで想定外の削除を検出。更新と反映が原子的なため部分成功が生じない。反映完了まで成功を返さない | PASS |
| V. 可観測性と運用性 | 計測器 1 組に Prometheus / OTLP の 2 リーダーを接続。標準出力ログは常時有効。OTLP は既定無効 | PASS |
| VI. Default-Deny | domain filter 未設定は「管理対象なし」。種別は許可リスト。provider ポートはループバック既定。OTLP・シークレット管理サービスの egress は opt-in | PASS |
| Go コード品質 | `gofmt` / `golangci-lint` / `govulncheck` を CI ゲートに設定 | PASS |
| DPF API クライアント | `dpf-go` を使用。自前 HTTP クライアントを実装しない | PASS |
| ドメイン名の取り扱い | 正規化名を保持する専用型を導入。`strings` による判定を行わない | PASS |
| ベースイメージ `scratch` / ASLR | cgo + 外部リンカ + `-static-pie` + `netgo,osusergo`。実測で両立を確認 (research R1/R2) | PASS |
| 実装手段を plan に置く | ビルドフラグ・証明書の配置・ライブラリの呼び出しは本 plan と research に記載し、constitution へ戻さない | PASS |
| ライセンス (v1.9.0〜v1.11.0) | 本体 Apache-2.0。全 Go ファイルに SPDX ヘッダ。依存ライセンスは許容リスト内であることを CI で検査 | PASS |
| 配布 (v1.10.0) | コンテナイメージのみ。`/licenses/` へライセンス本文を同梱し、OCI アノテーションと SBOM / provenance を付与 | PASS |
| シークレットの検査 (v1.12.0) | Betterleaks による検査をマージの条件とする | PASS |
| README の要件 (v1.13.0) | サポート対象外である旨とライセンスを README に明記 | PASS |
| サービスの呼称 (v1.14.0) | 「IIJ DNSプラットフォームサービス (DPF)」に統一 | PASS |
| 配布経路 (v2.0.0) | 上流 external-dns Helm チャートを経路とし、独自チャートを持たない。推奨 values は README に示す (research R11) | PASS |
| 実環境での検証 (v2.1.0) | main へのマージ前に、実際の DPF に対するレコードの追加・変更・削除を CI で検証する。同検証は並列実行しない | PASS |
| 外部 API のエラー応答 (v2.2.0) | DPF のエラー応答を切り詰めずに全文ログへ記録する。`request_id` を失わないため。HTTP 応答本文へは載せない | PASS |

**違反なし。** Complexity Tracking は不要。

### 設計上の注意点 (違反ではないが監視が必要)

- **cgo の有効化**: ASLR 要件から `CGO_ENABLED=1` となる。`netgo` / `osusergo` タグを
  外すと名前解決が壊れるため、ビルド設定の変更時は R1 の表を再確認する
- **失敗時の影響範囲**: 一括更新方式のため、マージの誤りはゾーン全体に及びうる。
  管理対象外レコードの逐語コピーと投入前ガードがこれを抑える唯一の防壁であり、
  この 2 つを弱める変更はレビューで特に厳しく見る (research R3)
- **ロックの範囲**: ゾーンロックは適用ハンドラの内側に閉じる。webhook のレコード取得
  要求と適用要求は別々の HTTP 要求であり、両者をまたいでロックを保持すると、適用が
  来ないままロックが残留してゾーンが操作不能になる。適用時にマージの土台を読み直す
  ことでこれを避ける (research R4)
- **ゾーンの帰属を 2 箇所に書かない**: 名前からゾーンを選ぶ規則は `internal/provider` の
  ゾーン索引 1 つに限る。読み取り側と書き込み側がそれぞれ独自に判定を書くと、片方だけ
  直した変更で両者がずれる。ずれた状態では、親ゾーン側に残ったレコードを見た削除要求が
  子ゾーンの権威レコードに当たる (research R12)。この単一化を弱める変更は、投入前ガードと
  同じ扱いでレビューする
- **`NS` 除外の副作用**: ゾーンが分かって初めて判断できる制約が 1 つもなくなる。
  `ValidateForZone` と `validateApexNSModification` は役目を終えるため取り除く。
  将来ゾーン依存の制約が現れたときに、この継ぎ目を作り直す
- **送信量**: 適用のたびにゾーン全件を送信する。1,000 件程度は 1 回で投入できることを
  確認済み。それを大きく超える規模のゾーンは想定外である
- **ロックを取らない変更者**: 適用の読み取りと書き戻しの区間に入った他者の変更は
  取り消される。これは設計では防げず、spec の利用前提条件 PC-001〜PC-003 で対処する。
  README にも同内容を記載すること
- **保留変更の破棄**: 一括更新は置き換えであるため、ゾーンに残っていた未反映の編集は
  破棄される。適用は中止せず、**有無の検出も行わない** (FR-020a)。中止する設計は
  下書きの放置で適用が恒久停止する。検出はマージの土台に使う一覧では見えず追加の
  API 呼び出しを要し、得られる記録は DPF サービス側のログと重複する。
  README に PC-004 として記載すること

## Project Structure

### Documentation (this feature)

```text
specs/001-webhook-provider/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── webhook-api.md   # ExternalDNS webhook provider API の契約
│   └── dpf-client.md    # 内部の DPF クライアント層インタフェース
├── checklists/
│   └── requirements.md  # spec 品質チェックリスト
└── tasks.md             # /speckit-tasks の出力 (本コマンドでは作成しない)
```

### Source Code (repository root)

```text
cmd/
└── webhook/
    └── main.go              # 起動・設定検証・各層の組み立て・終了処理

internal/
├── config/                  # 設定の読み込みと検証。既定は拒否側 (原則 VI)
├── dnsname/                 # 正規化名の型と操作。miekg/dns を閉じ込める
├── webhook/                 # ExternalDNS webhook 契約の HTTP ハンドラ
├── provider/                # ドメインロジック: 範囲判定・差分の適用・調整
│   ├── ports.go             # DPF クライアント層のインタフェース定義 (原則 II)
│   └── zoneindex.go         # 名前の帰属を決める唯一の規則 (research R12)
├── dpf/                     # DPF クライアント層。dpf-go をここに閉じ込める
│   ├── client.go            # 認証・接続の組み立て
│   ├── records.go           # レコードの取得と編集
│   ├── zone.go              # ゾーンの解決・反映・ロック
│   └── rrtype.go            # 種別の対応付け (許可リスト)
├── telemetry/               # ログ・メトリクス・トレースの初期化と計測器
└── server/                  # 2 つのリスナー (provider / exposed) の起動と停止

test/
├── contract/                # webhook API 契約テスト
└── integration/             # provider + DPF クライアント層 (DPF はモック)

build/
└── Containerfile            # ビルダー (alpine) + scratch の 2 段構成
```

**Structure Decision**: 単一サービスの標準的な Go レイアウトを採る。`internal/` 配下を
役割ごとに分けた理由は原則 II にある。DPF に依存してよいのは `internal/dpf` だけであり、
`internal/provider` はそこを `internal/provider/ports.go` のインタフェース越しにしか
参照しない。この境界があるため、provider のテストは DPF API に到達せずに書ける。

`internal/dnsname` を独立させたのは、正規化名を型で保証するためである
(constitution v1.4.0「正規化状態は型または境界で保証すること」)。名前を扱う他の層は
生の `string` を受け取らない。

`cmd/` と `internal/` を分けることで、本成果物が単一の HTTP サービスであり
再利用ライブラリではないという constitution の制約が構造として表れる。

## Complexity Tracking

> Constitution Check に違反がないため、記入不要。

---

## Phase 1 成果物

- [research.md](./research.md) — Phase 0 の調査。R12 で `NS` の除外とゾーン帰属を決定
- [data-model.md](./data-model.md) — 実体と変換規則、状態遷移。3 にゾーン索引
- [contracts/webhook-api.md](./contracts/webhook-api.md) — 外部契約
- [contracts/dpf-client.md](./contracts/dpf-client.md) — 内部境界の契約
- [quickstart.md](./quickstart.md) — 動作確認の手順

## Post-Design Constitution Re-check

Phase 1 の設計後に再評価した。**新たな違反なし。**

- `internal/provider/ports.go` によって原則 II の境界が型として表現された
- `internal/dnsname` の型により、原則 IV「範囲判定をラベル境界で行う」が
  呼び出し側の注意ではなく型検査で担保される
- 変更適用の状態遷移 (data-model.md) が、原則 IV「反映完了まで成功を返さない」と
  「部分成功を成功としない」を明示的な状態として表現している。更新と反映が原子的で
  あるため、後者は巻き戻し処理の正しさではなく構造によって保証される
- `NS` を許可リストから外したことで、FR-029 (`NS` を管理対象としない) が個別の検証
  ロジックではなく、種別の許可リスト 1 箇所で担保される。読み取り側の除外も同じ
  許可リストが担う。`overwrite_soa` / `overwrite_zone_apex_ns` を常に false で送る
  設定は据え置く。マージの土台は種別で絞らない生のレコードであり、SOA と apex NS は
  従来どおり逐語コピーで投入集合に含まれる (research R3、R12)
- ゾーン索引を `internal/provider` に 1 つだけ置いたことで、原則 IV の「範囲判定は
  書き込む直前に行う」が読み取り側の帰属判定と同じ規則になった。両者のずれは、
  呼び出し側の注意ではなく定義が 1 つしかないことで防がれる (data-model.md 3)
- テレメトリの計測器を 1 組に保つ構成により、原則 V「両形式が同一の計測値を表す」が
  構成上自動的に満たされる
