<!--
Sync Impact Report
- Version change: 1.6.0 → 1.7.0
- Bump rationale: 原則 V にメトリクスとトレースの提供を追加し、ログ・メトリクス・トレースの
  提供形式 (標準出力 / Prometheus / OTLP・OTLP over HTTP) を規範として確定した。
  原則の実質的な拡張のため MINOR。
- Modified principles:
  - V. 可観測性と運用性: ログ / メトリクス / トレース / 共通 の 4 部構成に改稿。
    OTLP 送出の既定無効化、ラベル・属性への機微値混入の禁止、送出失敗時の処理継続を追加。
- Modified sections:
  - 技術・配布制約 > ExternalDNS webhook provider API: `/metrics` を SHOULD から MUST へ。
  - 技術・配布制約 > セキュリティ: コンテナイメージとチャートの既定拒否:
    OTLP 送出先への egress を opt-in 化。exposed ポートの ingress 制限と、
    healthz/metrics 同居に伴う制約を明記。
- Added sections: なし
- Removed sections: なし
- Deferred TODOs: なし

Sync Impact Report (v1.6.0)
- Version change: 1.5.0 → 1.6.0
- Bump rationale: バイナリの ASLR 有効化を MUST として追加し、あわせて scratch 選択の
  実装手段を plan へ委譲した。規範の追加のため MINOR。
- Modified principles: なし
- Modified sections:
  - 技術・配布制約 > セキュリティ: コンテナイメージとチャートの既定拒否:
    ASLR 要件を追加。scratch の実装手段 (CGO_ENABLED, USER 記法, tzdata, emptyDir,
    デバッグ手順) を削除し plan へ委譲。CA 証明書の要件は「TLS 検証が成立すること」という
    結果の表現に改めて残置。
  - 開発ワークフローと品質ゲート: CI ゲートに ASLR 検証を追加。
  - Governance: 規範と実装手段の分離規則を追加。
- Removed sections: なし
- Deferred TODOs: なし

Sync Impact Report (v1.5.0)
- Version change: 1.4.0 → 1.5.0
- Bump rationale: ベースイメージを `scratch` に確定し、その成立に必要な付随要件
  (静的リンク、CA 証明書の同梱、数値 UID、tzdata、一時領域、デバッグ手順) を追加した。
  選択肢の絞り込みと要件の追加であり、原則の削除・再定義ではないため MINOR。
- Modified principles: なし
- Modified sections:
  - 技術・配布制約 > セキュリティ: コンテナイメージとチャートの既定拒否:
    ベースイメージを「distroless または scratch 相当」から `scratch` に確定。
- Added sections: なし
- Removed sections: なし
- Deferred TODOs: なし

Sync Impact Report (v1.4.0)
- Version change: 1.3.0 → 1.4.0
- Bump rationale: ドメイン名の取り扱いに `github.com/miekg/dns` の使用を義務付け、
  文字列操作による判定を禁止した。あわせて内部表現を正規化名 (`dns.CanonicalName` の出力)
  に固定した。原則 IV に判定方法の要件を追記したため MINOR。
- Modified principles:
  - IV. DNS 変更の安全性: 範囲判定をラベル境界で行う要件を追記。
- Modified sections:
  - 技術・配布制約: 「ドメイン名の取り扱い」サブセクションを新設
    (内部表現 / 操作の 2 部構成)。
  - 開発ワークフローと品質ゲート: ドメイン名を扱う PR のレビュー観点を追加。
- Added sections: なし
- Removed sections: なし
- Deferred TODOs: なし

Sync Impact Report (v1.3.0)
- Version change: 1.2.0 → 1.3.0
- Bump rationale: Go のツールチェーン要件 (gofmt / golangci-lint / govulncheck) を
  規範として追加し、CI ゲートを具体化した。指針の実質的な拡張のため MINOR。
- Modified principles: なし
- Modified sections:
  - 技術・配布制約: 「Go コード品質」サブセクションを新設。
  - 開発ワークフローと品質ゲート: 静的解析ゲートを具体名で確定。定期実行を追加。
- Added sections: なし
- Removed sections: なし
- Deferred TODOs: なし

Sync Impact Report (v1.2.0)
- Version change: 1.1.0 → 1.2.0
- Bump rationale: 原則 VI「Default-Deny」を新設し、バイナリと Helm チャートの双方に
  既定拒否を規範として要求した。原則の追加にあたるため MINOR。
- Added principles:
  - VI. Default-Deny (NON-NEGOTIABLE)
- Modified sections:
  - 技術・配布制約 > セキュリティ: 既定拒否の具体的要件 (実行時・チャート) に全面改稿。
  - 開発ワークフローと品質ゲート: CI ゲートに既定値のセキュリティ検証を追加。
- Removed sections: なし
- Deferred TODOs: なし

Sync Impact Report (v1.1.0)
- Version change: 1.0.0 → 1.1.0
- Bump rationale: 初版で保留していた 3 件の TODO を確定値に置換し、対応 API バージョンと
  メディアタイプ、エンドポイント一覧、既定ポート、Go 最小バージョン、追随方針を新たに
  規範として追加した。指針の実質的な拡張にあたるため MINOR。
- Modified principles: なし (原則本文の変更なし)
- Modified sections:
  - 技術・配布制約: TODO 3 件を確定値に置換。「上流追随方針」を新設。
- Added sections: なし
- Removed sections: なし
- Deferred TODOs: なし (初版の 3 件はすべて解消)

Sync Impact Report (v1.0.0 初回批准時)
- Version change: (未記入テンプレート) → 1.0.0
- Bump rationale: 初回批准。全プレースホルダを具体化し、5 原則と 2 つの追加セクション、
  Governance を確定させたため MAJOR を 1 とする初版を発行する。
- Modified principles:
  - [PRINCIPLE_1_NAME] → I. ExternalDNS Webhook 契約への準拠 (NON-NEGOTIABLE)
  - [PRINCIPLE_2_NAME] → II. プロバイダ境界の分離
  - [PRINCIPLE_3_NAME] → III. テストファースト (NON-NEGOTIABLE)
  - [PRINCIPLE_4_NAME] → IV. DNS 変更の安全性
  - [PRINCIPLE_5_NAME] → V. 可観測性と運用性
- Added sections:
  - 技術・配布制約 ([SECTION_2_NAME] を具体化)
  - 開発ワークフローと品質ゲート ([SECTION_3_NAME] を具体化)
- Removed sections: なし
-->

# external-dns-iij-dpf-webhook Constitution

本プロジェクトは、Kubernetes の ExternalDNS に対して IIJ DNS プラットフォームサービス (DPF) を
DNS プロバイダとして提供する webhook provider である。単一の Go 製 HTTP サービスとして実装し、
external-dns Pod のサイドカーとして動作させることを前提とする。

## Core Principles

### I. ExternalDNS Webhook 契約への準拠 (NON-NEGOTIABLE)

本サービスの外部インタフェースは ExternalDNS webhook provider API の仕様そのものであり、
独自拡張によってこれを逸脱してはならない (MUST NOT)。

- 上流仕様が定めるエンドポイント、HTTP メソッド、ステータスコード、リクエスト/レスポンスの
  JSON スキーマ、およびネゴシエーション用メディアタイプに完全に従うこと (MUST)。
- 仕様に対する解釈が必要な箇所は、実装コードではなく契約テストに落とし込み、テストを
  仕様の唯一の実行可能な表現とすること (MUST)。
- 上流仕様の変更に追随する場合は、まず契約テストを更新し、その差分をレビュー対象とすること (MUST)。
- ExternalDNS 本体が期待しない独自エンドポイントやフィールドを追加しないこと (MUST NOT)。
  DPF 固有の挙動が必要な場合は、設定またはアノテーション経由で表現し、契約は変更しない。

**根拠**: webhook provider は ExternalDNS 本体から見れば差し替え可能な部品である。契約から
外れた瞬間に本体のアップグレードで壊れ、利用者は原因を切り分けられない。契約遵守は本
プロジェクトの存在意義そのものであり、交渉の余地はない。

### II. プロバイダ境界の分離

DPF API へのアクセスは専用のクライアント層に閉じ込め、その外へ漏らさないこと (MUST)。

- HTTP クライアント、認証トークンの取り扱い、DPF 固有のリクエスト/レスポンス型、リトライ、
  レート制御は、すべて DPF クライアント層の内側に置くこと (MUST)。
- webhook ハンドラおよびドメインロジックは、DPF のトランスポート詳細 (URL、ヘッダ、
  ステータスコード、SDK 型) を直接参照しないこと (MUST NOT)。両者はインタフェースで結合する。
- DPF のエラーは、クライアント層で本プロジェクト固有のエラー型に変換してから上位へ返すこと (MUST)。
- クライアント層はインタフェース経由で差し替え可能とし、上位層のテストが実際の DPF API に
  到達しないこと (MUST)。

**根拠**: 契約 (原則 I) と外部 API という 2 つの変わりうる境界の間に本サービスは位置する。
両者を直接結線すると、どちらの変更もコード全体に波及する。境界を分離して初めて、DPF API を
モックしたテストが書け、TDD (原則 III) が現実的なコストで成立する。

### III. テストファースト (NON-NEGOTIABLE)

TDD を必須とする。テストを先に書き、失敗を確認してから実装すること (MUST)。

- 実装より前にテストを書くこと (MUST)。テストが期待どおり失敗することを確認してから
  実装に着手すること (MUST)。
- Red-Green-Refactor のサイクルをレビューで確認可能にすること (MUST)。コミットまたは
  PR の記述から、テストが実装に先行した事実を追跡できること (MUST)。
- バグ修正は、まず当該バグを再現して失敗するテストを追加することから始めること (MUST)。
  再現テストのない修正はマージしないこと (MUST NOT)。
- テストのない実装コード、および実装に合わせて後から書き起こしたテストは受け入れない (MUST NOT)。

**根拠**: 本サービスは DNS レコードという不可逆かつ広範囲に影響する状態を書き換える。手元での
再現が難しい本番障害を、リリース後に発見する余裕はない。テストファーストは、書かれる仕様の
量ではなく、仕様が実装より先に存在することを保証するための規律である。

### IV. DNS 変更の安全性

DNS レコードの変更は、常に「意図した範囲に限定され、繰り返し実行しても安全」でなければ
ならない (MUST)。

- すべての適用操作は冪等であること (MUST)。同一の変更セットを再適用しても、最終状態が
  変わらず、追加の破壊的副作用を生まないこと。
- domain filter およびゾーン絞り込みの対象外にあるレコードは、いかなる場合も変更・削除しない
  こと (MUST NOT)。範囲判定はレコードを書き込む直前に行うこと (MUST)。
- 範囲判定はドメイン名のラベル境界で行うこと (MUST)。文字列としての部分一致や接尾辞一致で
  判定しないこと (MUST NOT)。判定方法は「ドメイン名の取り扱い」の規定に従う。
- 変更適用中にエラーが発生した場合は fail closed とし、部分的に成功した状態を「成功」として
  ExternalDNS に返さないこと (MUST NOT)。エラーは握り潰さず上位へ伝播させること (MUST)。
- DPF API が変更の反映に明示的な適用 (commit) 操作を要求する場合、その完了までを一つの
  変更操作として扱い、未適用の変更を残したまま成功を返さないこと (MUST NOT)。
- 破壊的操作を伴う変更経路には、その挙動を検証するテストを必ず用意すること (MUST)。

**根拠**: 誤った DNS 変更は、対象サービスの全面停止に直結し、TTL の分だけ復旧が遅れる。
ExternalDNS は本サービスを無人で繰り返し呼び出すため、冪等性と範囲限定は運用上の心構えでは
なくコードで保証すべき性質である。

### V. 可観測性と運用性

障害発生時に、Pod のログと標準的な Kubernetes の操作だけで原因を切り分けられること (MUST)。
ログ・メトリクス・トレースの 3 種類のテレメトリを提供すること (MUST)。

*ログ*

- ログは構造化ログとし、標準出力へ出力すること (MUST)。標準出力への出力は常に有効であり、
  他の出力先の設定によって停止しないこと (MUST NOT)。
- 標準出力に加えて、OpenTelemetry 形式でのログ出力を提供すること (MUST)。
  送出は OTLP (gRPC) および OTLP/HTTP に対応すること (MUST)。
- ログレベルは設定で変更可能とすること (MUST)。
- DNS レコードを変更する操作は、対象ゾーン・レコード名・操作種別・結果をログに残すこと (MUST)。

*メトリクス*

- メトリクスは Prometheus 形式と OpenTelemetry 形式の 2 種類で提供すること (MUST)。
  - Prometheus 形式は `/metrics` エンドポイントで公開すること (MUST)。
  - OpenTelemetry 形式は OTLP (gRPC) および OTLP/HTTP での送出に対応すること (MUST)。
- 両形式が同一の計測値を表すこと (MUST)。形式ごとに異なる意味の値を持たせないこと (MUST NOT)。
- DNS レコードの変更操作について、成否と件数を計測できること (MUST)。

*トレース*

- トレースは OpenTelemetry 形式で提供すること (MUST)。送出は OTLP (gRPC) および
  OTLP/HTTP に対応すること (MUST)。
- ExternalDNS からのリクエスト受信から DPF API 呼び出しまでを 1 つのトレースとして
  追跡可能にすること (MUST)。

*共通*

- API トークン、認証情報、およびそれらを含むリクエストヘッダを、ログ・エラーメッセージ・
  メトリクス・トレースのいずれにも出力しないこと (MUST NOT)。
- ゾーン名・レコード名・レコード値を、メトリクスのラベルおよびトレースの属性に
  既定で含めないこと (MUST NOT)。含める場合は明示的な opt-in とすること (MUST)。
  これらは基数が非有界であり、かつ認証なしに公開されうるため。ログへの出力は
  この制限の対象外とする。
- OTLP による送出は既定で無効とすること (MUST)。送出先が設定された場合にのみ有効化すること
  (MUST)。テレメトリの外部送出は明示的な許可を要する (原則 VI)。
- OTLP 送出先への接続では TLS 証明書の検証を既定で有効とすること (MUST)。
- テレメトリの送出失敗によって、DNS レコードの処理を停止させないこと (MUST NOT)。
  送出失敗はログに記録し、本来の処理は継続すること (MUST)。
- ヘルスチェック用エンドポイントを提供し、Kubernetes の liveness/readiness probe から
  利用可能にすること (MUST)。
- 設定は環境変数またはコマンドライン引数で与え、認証情報は Secret から注入すること (MUST)。
  設定値をイメージに焼き込まないこと (MUST NOT)。

**根拠**: 本サービスはサイドカーとして無人で動作し、利用者が最初に見るのはログだけである。
何が起きたかがログから読み取れなければ、利用者は ExternalDNS 側と DPF 側のどちらに問題が
あるのかすら判断できない。ログだけでは「遅い」「たまに失敗する」といった継続的な劣化を
検知できないためメトリクスを、ExternalDNS から DPF API までのどの区間で時間や失敗が
生じたかを特定するためトレースを、それぞれ必要とする。標準出力を常時有効とするのは、
テレメトリ基盤が未整備または障害中の環境でも、最低限の調査手段を残すためである。

### VI. Default-Deny (NON-NEGOTIABLE)

バイナリと Helm チャートの双方において、明示的に許可されていないものはすべて拒否すること
(MUST)。許可は常に opt-in であり、拒否が既定である。

- 設定を与えなかった場合の既定動作は「何もしない・何も許さない」であること (MUST)。
  未設定を「すべて許可」と解釈しないこと (MUST NOT)。
- 権限・到達性・機能は、必要と判明したものだけを個別に開けること (MUST)。
  「とりあえず広く開けて後で絞る」順序を採らないこと (MUST NOT)。
- 既定値を緩める設定項目は、利用者が明示的に指定した場合にのみ有効になること (MUST)。
  緩和時はその旨を警告としてログに出力すること (MUST)。
- 設定の解釈に失敗した場合、または想定外の値を受け取った場合は、起動を中止すること (MUST)。
  既定値へフォールバックして起動を続行しないこと (MUST NOT)。

**根拠**: 本サービスは DNS という到達性の根幹を書き換える権限と、DPF API の認証情報を併せ持つ。
既定が許可であれば、利用者の設定漏れがそのまま全ゾーンへの書き込み権限や不要な露出になる。
設定漏れの結果は「動かない」であるべきで、「気付かないうちに広く開いている」であってはならない。
これは原則 IV (DNS 変更の安全性) を、DNS 以外の層まで一貫して適用したものである。

## 技術・配布制約

**実装**

- 実装言語は Go とする (MUST)。go.mod の `go` ディレクティブは `1.27` とし、Go 1.27 系以上で
  ビルドできること (MUST)。Go は直近 2 つのメジャーバージョンのみがサポート対象であるため、
  新しい Go がリリースされた際は追随を検討すること (SHOULD)。
- 成果物は単一の HTTP サービスであり、CLI ツールや再利用ライブラリとしての公開は
  本プロジェクトのスコープ外とする (MUST NOT)。DPF クライアント層を独立パッケージとして
  公開する必要が生じた場合は、本 constitution の改訂を伴う。

**Go コード品質**

以下のツールをコード品質の基準とし、その判断を人の裁量で覆さないこと (MUST)。

- 整形は `gofmt` に従うこと (MUST)。`gofmt -l ./...` の出力が空でない状態をマージしないこと
  (MUST NOT)。整形について議論せず、ツールの出力をそのまま受け入れること (MUST)。
  他の整形ツールを併用する場合も、`gofmt` 互換の出力であること (MUST)。
- 静的解析は `golangci-lint` を用いること (MUST)。設定ファイルをリポジトリ内に置き、
  ローカルと CI が同一の設定で動作すること (MUST)。有効な linter とそのバージョンは
  設定ファイルで固定すること (MUST)。
- `golangci-lint` の指摘を抑制する場合は `nolint` コメントに理由を併記すること (MUST)。
  理由のない抑制、および設定ファイルからの linter の無効化によって個別の指摘を回避すること
  (MUST NOT)。抑制は対象行に限定し、ファイル単位・パッケージ単位で広く無効化しないこと
  (MUST NOT)。
- 脆弱性検査は `govulncheck` を用いること (MUST)。`govulncheck ./...` が到達可能な既知脆弱性を
  報告する状態でリリースしないこと (MUST NOT)。
- `govulncheck` の報告に対応できない場合は、影響評価と暫定対処を記録したうえで対応期限を
  定めること (MUST)。記録のないまま放置しないこと (MUST NOT)。

**根拠**: 整形と静的解析をツールに委ねることで、レビューの議論を設計と正しさに集中させる。
`govulncheck` は依存関係の既知脆弱性のうち、実際にコードから到達しうるものだけを報告するため、
原則 VI (Default-Deny) の供給網に対する適用として、検出済みの脆弱性を既定で「不可」とする。

**ドメイン名の取り扱い**

ドメイン名は文字列ではなく DNS の名前として扱うこと (MUST)。取り扱いには
`github.com/miekg/dns` を用いること (MUST)。

*内部表現*

- 内部で保持・受け渡しするドメイン名は、常に正規化名とすること (MUST)。正規化名とは
  小文字かつ末尾ドットで終わる FQDN であり、`dns.CanonicalName(string) string` の出力を指す。
- 正規化は境界で一度だけ行うこと (MUST)。外部から名前を受け取る箇所 (webhook リクエストの
  パース、設定・domain filter の読み込み、DPF API のレスポンス) で `dns.CanonicalName` を適用し、
  それ以降の内部処理では正規化済みであることを前提としてよい (MAY)。
- 正規化済みか否かが呼び出し側に依存する関数を作らないこと (MUST NOT)。内部関数が
  非正規化名を受け取りうる設計にしないこと (MUST NOT)。正規化状態は型または境界で
  保証すること (MUST)。
- 内部処理の途中で再び正規化し直すこと、および正規化を解いた表現へ戻すことをしないこと
  (MUST NOT)。末尾ドットの有無や大文字小文字が処理経路によって変わる状態を作らないため。
- DPF API へ渡す名前は正規化名とすること (MUST)。DPF は正規化された状態で名前を扱うため、
  内部表現をそのまま渡してよい (MAY)。
- ExternalDNS へ返す名前の表現は webhook 契約に従うこと (MUST)。内部の正規化名と契約上の
  表現が異なる場合、その変換は応答を組み立てる境界に限定し、契約テストで表現を固定すること
  (MUST)。

*操作*

- 正規化、比較、包含判定、ラベル分割には `github.com/miekg/dns` が提供する関数を用いること
  (MUST)。`strings` パッケージの関数 (`HasSuffix`, `TrimSuffix`, `Contains`, `Split`,
  `EqualFold`, `ToLower` など) でこれらを実装しないこと (MUST NOT)。
- 末尾ドットの付与・除去、および大文字小文字の変換を自前で実装しないこと (MUST NOT)。
  FQDN 化のみが必要な場合も `dns.CanonicalName` を用いること (MUST)。
- 名前の比較は、正規化名どうしの比較として行うこと (MUST)。比較のたびに
  大文字小文字を無視する比較関数を呼ぶ実装にしないこと (MUST NOT)。
- あるゾーンが別の名前を包含するかの判定には `dns.IsSubDomain` を用いること (MUST)。
  接尾辞一致で代用しないこと (MUST NOT)。
- ラベルの分割・数え上げには `dns.SplitDomainName` および `dns.CountLabel` を用いること (MUST)。
  区切り文字での分割で代用しないこと (MUST NOT)。エスケープされたドットを含むラベルを
  誤って分割するため。
- 外部から受け取った名前は、使用前に `dns.IsDomainName` で妥当性を検証すること (MUST)。
  検証に失敗した名前を管理対象として扱わないこと (MUST NOT)。
- ワイルドカード名は明示的に判定し、通常の名前と同じ経路で暗黙に処理しないこと (MUST)。
- 表示・ログ出力の整形目的で文字列操作を行うことは許容する。ただしその結果を、
  判定・比較・API へ渡す値として再利用しないこと (MUST NOT)。

**根拠**: ドメイン名の文字列比較は、原則 IV が禁じる「範囲外への書き込み」を直接引き起こす。
`strings.HasSuffix(name, "example.com")` は `evil-example.com` に一致し、`strings.EqualFold` は
DNS の大文字小文字規則と一致せず、ドットでの分割はエスケープされたラベルを壊す。
いずれも単体では動いているように見え、特定の名前でのみ範囲判定をすり抜ける。
判定を仕様準拠のライブラリに委ねることは、原則 VI (Default-Deny) を名前解決層で成立させる前提である。
内部表現を正規化名に固定するのは、比較の正しさを個々の呼び出し箇所の注意深さに依存させないためである。
表現が経路によって揺れる状態では、同一の名前が別物として扱われ、レコードの重複作成や
削除漏れという冪等性 (原則 IV) の破れとして現れる。

**ExternalDNS webhook provider API**

対応対象は ExternalDNS v0.22.0 が定義する webhook provider API であり、その OpenAPI 仕様
(`api/webhook.yaml`) を契約の正とする (MUST)。

- メディアタイプは `application/external.dns.webhook+json;version=1` とすること (MUST)。
  リクエストの `Accept` ヘッダを読み、レスポンスの `Content-Type` に同じ値を設定すること (MUST)。
- provider エンドポイントとして以下を実装すること (MUST):
  - `GET /` — DomainFilter のネゴシエーション。成功時 `200`
  - `GET /records` — レコード一覧の取得。成功時 `200`
  - `POST /records` — 変更の適用。成功時 `204 No Content`
  - `POST /adjustendpoints` — プロバイダ固有の調整。成功時 `200`
- exposed エンドポイントとして以下を提供すること (MUST):
  - `GET /healthz` — liveness/readiness probe 用
  - `GET /metrics` — Prometheus 形式のメトリクスの公開 (MUST)。上流仕様では optional だが、
    原則 V により本プロジェクトでは必須とする。
- エラーは仕様に従って区別すること (MUST)。一時的エラーは `5xx`、恒久的エラーは `4xx` を返し、
  一時的エラーを `4xx` として返さないこと (MUST NOT)。ExternalDNS 側のリトライ判断を誤らせるため。
- 待ち受けポートの既定値は、provider エンドポイントを `8888`、exposed エンドポイントを `8080`
  とすること (MUST)。いずれも設定で変更可能とすること (MUST)。

**上流追随方針**

- 動作保証する ExternalDNS 本体のバージョンは v0.22.0 以降とし、README に明記すること (MUST)。
- 上流がメディアタイプの `version` パラメータを変更する、またはエンドポイントの契約を
  変更した場合、自動的に追随しないこと (MUST NOT)。影響範囲を評価したうえで本 constitution を
  改訂し、その改訂に基づいて対応方針を決めること (MUST)。
- 上記に該当しない上流のマイナー更新への追随は、契約テストが通ることを条件に通常の
  Pull Request として扱ってよい。

**配布**

- 一次成果物はコンテナイメージとする (MUST)。タグ付きリリースごとに OCI イメージを公開すること。
- Kubernetes へのデプロイ用 Helm チャートおよびマニフェストを本リポジトリ内で維持すること (MUST)。
  コードとデプロイ成果物のバージョン整合を、リリース時に確認すること (MUST)。
- リリースは SemVer に従いタグ付けすること (MUST)。ExternalDNS webhook API の互換性を壊す
  変更は MAJOR とすること (MUST)。
- `latest` タグのみに依存した利用手順をドキュメントに記載しないこと (MUST NOT)。

**セキュリティ: バイナリの既定拒否**

原則 VI をバイナリの実行時挙動に適用した具体要件。

- domain filter またはゾーンの許可リストが未設定の場合、いかなるレコードも管理対象と
  しないこと (MUST)。未設定を「全ゾーンを管理」と解釈しないこと (MUST NOT)。
  管理対象が空である旨をログに出力し、サービス自体は正常に応答すること (MUST)。
- 取り扱うレコード種別は許可リスト方式とすること (MUST)。未知または未対応の種別を
  暗黙に通過させないこと (MUST NOT)。
- provider エンドポイント (既定 `8888`) は、既定でループバックアドレスにのみバインドすること
  (MUST)。ExternalDNS とは同一 Pod 内のサイドカーとして通信するため、Pod 外への公開は
  既定では不要である。Pod 外へ公開する場合は明示的な設定を要求し、その危険性を
  README に記載すること (MUST)。
- exposed エンドポイント (既定 `8080`) が返す情報は、healthz と metrics に限定すること (MUST)。
  ゾーン名・レコード値・設定内容など、DNS 構成が推測できる情報を認証なしで返さないこと
  (MUST NOT)。
- DPF API への接続では TLS 証明書の検証を常に有効とすること (MUST)。検証を無効化する
  設定を提供する場合、既定は有効とし、無効化時は警告をログに出力すること (MUST)。
- 認証情報が未設定のまま起動しないこと (MUST NOT)。起動時に必須設定の充足を検証し、
  不足していれば異常終了すること (MUST)。

**セキュリティ: コンテナイメージとチャートの既定拒否**

原則 VI を配布成果物に適用した具体要件。チャートの既定値 (`values.yaml`) は、
利用者が何も上書きしなくても以下を満たすこと (MUST)。

- ベースイメージは `scratch` とすること (MUST)。シェル、パッケージマネージャ、
  および実行に不要なファイルをイメージに含めないこと (MUST NOT)。
- DPF API への TLS 証明書検証が成立する状態でイメージを配布すること (MUST)。
  `scratch` には証明書ストアが存在しないため、検証に必要な CA 証明書をイメージ自身が
  備えること (MUST)。実現手段は plan で定める。
- バイナリは ASLR が有効な状態でビルドすること (MUST)。位置依存の実行ファイル、または
  ASLR が無効な実行ファイルを配布しないこと (MUST NOT)。ASLR が有効であることを
  CI で検証すること (MUST)。実現手段は plan で定める。
- コンテナは非 root の固定数値 UID で実行すること (MUST)。`runAsNonRoot: true`,
  `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`,
  `capabilities.drop: ["ALL"]`, `seccompProfile.type: RuntimeDefault` を既定とすること (MUST)。
- `privileged`, `hostNetwork`, `hostPID`, `hostIPC`, hostPath ボリュームを既定で有効に
  しないこと (MUST NOT)。Pod Security Standards の `restricted` プロファイルを満たすこと (MUST)。
- 本サービスは Kubernetes API を利用しない。`automountServiceAccountToken: false` を既定とし、
  Role/ClusterRole を既定で作成しないこと (MUST)。
- NetworkPolicy を既定で有効とし、ingress・egress ともに全拒否を起点に、必要な通信のみを
  明示的に許可すること (MUST)。既定で許可する egress は DPF API エンドポイントと
  名前解決に限ること (MUST)。
- OTLP 送出先への egress は、送出先が設定された場合にのみ許可すること (MUST)。
  OTLP を使わない利用者の NetworkPolicy に、テレメトリ用の穴を既定で開けないこと (MUST NOT)。
- exposed ポート (既定 `8080`) への ingress は、probe とスクレイプに必要な送信元に
  限定すること (MUST)。全ての送信元に開放しないこと (MUST NOT)。
- `/healthz` と `/metrics` は同一ポートで提供されるため、NetworkPolicy で両者を区別できない。
  probe を通す設定は同時に `/metrics` を同じ送信元へ露出させる。この前提のもとで、
  メトリクスに機微な値を含めない要件 (原則 V) を満たすこと (MUST)。
- provider ポートを公開する Service を既定で作成しないこと (MUST NOT)。
- CPU・メモリの requests と limits を既定で設定すること (MUST)。
- 認証情報は Secret 参照としてのみ受け取ること (MUST)。認証情報をリポジトリ、イメージ、
  および Helm チャートの既定値に含めないこと (MUST NOT)。平文の値を `values.yaml` に
  書かせる設定項目を用意しないこと (MUST NOT)。
- 上記のいずれかを緩和する values は、既定を安全側に置いたうえで opt-in とすること (MUST)。

## 開発ワークフローと品質ゲート

- 変更は Pull Request 経由で行い、レビュー承認なしに main へマージしないこと (MUST NOT)。
- レビュアは、当該 PR が本 constitution の各原則に適合しているかを確認すること (MUST)。
  特に原則 III (テストが実装に先行したか)、原則 IV (範囲限定・冪等性)、
  原則 VI (既定値が拒否側か) を明示的に確認する。
- 設定項目または values を追加する PR は、その既定値が拒否側であることをレビューで
  確認すること (MUST)。既定値を許可側に変更する PR は、理由を記載すること (MUST)。
- ドメイン名を扱うコードを追加・変更する PR は、`strings` パッケージによる判定が
  含まれていないことをレビューで確認すること (MUST)。可能であれば、この検出を
  `golangci-lint` の設定として自動化すること (SHOULD)。
- CI は以下をすべて通過することをマージの条件とすること (MUST):
  - 整形の検証 (`gofmt -l ./...` の出力が空であること)
  - ビルド (`go build ./...`)
  - 静的解析 (`golangci-lint run`)
  - 脆弱性検査 (`govulncheck ./...`)
  - 全テスト (`go test ./...`)
  - ExternalDNS webhook API の契約テスト
  - 既定設定での起動時挙動の検証 (domain filter 未設定時に管理対象が空であること、
    必須設定の不足時に異常終了すること、provider ポートがループバックにのみ
    バインドされること)
  - 配布バイナリの ASLR が有効であることの検証
  - コンテナイメージのビルド、および脆弱性スキャン
  - Helm チャートの lint、および既定 values のレンダリング結果が
    Pod Security Standards の `restricted` を満たすことの検証
- `govulncheck` とコンテナイメージの脆弱性スキャンは、PR 時に加えて定期的に実行すること
  (MUST)。コード変更がなくても新たな脆弱性は公開されるため、PR 時の実行だけでは
  検出が漏れる。
- CI で用いる Go、`golangci-lint`、`govulncheck` のバージョンを固定すること (MUST)。
  ツールの暗黙のバージョン更新によって、同一コードの判定結果が変わる状態にしないこと
  (MUST NOT)。
- 原則から逸脱する実装を行う場合は、その理由と代替案を検討した結果を PR に記載すること (MUST)。
  記載のない逸脱はマージしないこと (MUST NOT)。
- 複雑さは正当化を要する。より単純な代替が存在する場合、単純な方を選ぶこと (MUST)。

## Governance

- 本 constitution は、本リポジトリにおける他のあらゆる慣習・慣行に優先する (MUST)。
  個別の判断が本文書と矛盾する場合、本文書が優先する。
- 改訂は Pull Request で提案し、内容の合意を得たうえでマージすること (MUST)。改訂 PR には
  変更理由と、既存コード・既存 spec への影響 (移行が必要な場合はその手順) を記載すること (MUST)。
- バージョニングは SemVer に従う (MUST):
  - MAJOR: 原則の削除、または後方互換性のない再定義を行った場合。
  - MINOR: 原則やセクションを追加した場合、または指針を実質的に拡張した場合。
  - PATCH: 文言の明確化、誤字修正など、意味を変えない改訂。
- 改訂時は `**Version**` 行と `**Last Amended**` を必ず更新すること (MUST)。
  `**Ratified**` は初回批准日であり、変更しないこと (MUST NOT)。
- TODO として保留されている項目は、それを確定できる最初の機能開発の中で解消し、
  その際に本文書を改訂すること (MUST)。
- 本文書には「守るべき結果」のみを記載すること (MUST)。ある選択の帰結として導かれる
  実装手段 (ビルドフラグ、Dockerfile の具体的な記述、ライブラリの呼び出し手順) は
  plan に置き、本文書に書かないこと (MUST NOT)。手段を本文書に書くと、その変更のたびに
  改訂手続きが必要となり、規範と実装詳細の区別が失われる。
- 実行時の開発ガイダンス (ディレクトリ構成、コーディング規約、頻用コマンド) は
  リポジトリルートの `CLAUDE.md` に置き、本文書とは分離すること (MUST)。
  本文書は「何を守るか」を、`CLAUDE.md` は「どう作業するか」を扱う。

**Version**: 1.7.0 | **Ratified**: 2026-09-04 | **Last Amended**: 2026-09-04
