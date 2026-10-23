# Changelog

本ファイルの形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に、
バージョン番号は [Semantic Versioning](https://semver.org/lang/ja/) に従います。

記録する対象は**利用者に見える挙動**です
(constitution「利用者に見える変更の記録」)。内部の構造変更、テストの追加、
挙動を変えない依存の更新は記載しません。何を利用者に見える変更として扱うかは
constitution が定めています。

## [Unreleased]

### Security

- ビルドに使う Go を 1.27.3 へ更新し、標準ライブラリの脆弱性を修正した
  - `crypto/x509`: GO-2026-6809 (侵害された中間証明書を信頼しない)。
    DPF API への TLS 接続での証明書検証の経路にある

## [0.1.0] - 2026-10-16

最初のリリースです。

### Added

- ExternalDNS (v0.22.0 以降) の webhook provider として、
  IIJ DNSプラットフォームサービス (DPF) のレコードを操作する。
  provider リスナー (既定 `127.0.0.1:8888`) が公開するのは `GET /`、`GET /records`、`POST /records`、`POST /adjustendpoints` の
  4 経路。**独自のエンドポイントは持たない。** メディアタイプは
  `application/external.dns.webhook+json;version=1` で、ネゴシエートできない
  `Accept` は `406` で拒否する
- 対応するレコード種別は `A` / `AAAA` / `CNAME` / `TXT` / `SRV` / `PTR` / `MX` /
  `NAPTR`。`NS` は扱わない。一覧外の種別は返さず、DPF 上に存在しても変更・削除しない
- 親子・孫のゾーンが DPF 上に併存する場合、レコードを最も深く一致するゾーンへ書き込む
- 管理対象ドメインを `--domain-filter` で指定する。**未指定の場合、管理対象は
  空になる。** 設定の不足が「すべてを操作する」に倒れないようにするため
- アクセストークンを、マウントしたファイル (`--dpf-token-file`、必須) から読む。
  要求のたびに読み直し、ファイルが差し替われば再起動なしに使う。起動時にも 1 度
  読み、読めない・空なら起動に失敗する。**環境変数と引数からは受け取らない。**
  外部のシークレット管理サービスへは直接接続しない。そこに置いた値は、
  External Secrets Operator や Secrets Store CSI Driver で Pod 内のファイルにして
  渡す (README 参照)
- 変更の適用を、ゾーンロックの下で行う。反映済みレコードの読み直し、マージ、
  一括更新、反映の待ち合わせを 1 つの操作として扱い、**反映が完了するまで成功を
  返さない。** 管理対象外のレコードは読み取った表現のまま書き戻して保全する
- ゾーンロックの状態をゾーンのラベル `lock.dpf-go` に置く (dpf-go v0.6.0 以降の
  既定の排他)。**運用者がそのゾーンに付けられるラベルは 9 個までになる。**
  同じゾーンを機械的に変更する他の手段も、同じ排他を使う必要がある (README の
  利用前提条件 PC-001)
- 投入する内容が反映済みの内容と同じ場合は、一括更新とゾーン反映を省く。
  ゾーンのシリアルと反映の履歴を無用に進めず、DPF 上の未反映の編集を破棄しない
- 投入前の検査で、削除要求のないレコードが失われる場合は適用を中止する。
  マージの誤りが「レコードが消える」ではなく「適用されない」として現れる
- ゾーン反映の履歴に、実行者として `external-dns-iij-dpf-webhook` を記録する。
  運用者が DPF の履歴を開いたとき、本サービスによる反映と人手による反映を
  見分けられる
- 本サービスが書いたレコード (所有権の記録の `TXT` を含む) に
  `managed-by: external-dns-iij-dpf-webhook` のラベルを付ける。DPF 上でこのラベルに
  よる絞り込みができる。ラベルは更新のたびに上書きする。以前からあるレコードに
  遡って付けることはしない
- 255 オクテットを超える `TXT` の値を、DPF が保存できる形へ自動で分割する。
  `POST /adjustendpoints` は、DPF に保存される形へ整えた結果を返す
- 名前の表現の違い (末尾ドットの有無、大文字小文字) を同一のレコードとして扱う。
  応答で返す名前は ExternalDNS が突き合わせに使う表記 (末尾ドットなし) に揃える
- exposed リスナー (既定 `:8080`) で `GET /healthz` と `GET /metrics` を公開する。
  **この 2 つ以外を持たない**
- 計測値として `dns_record_changes_total`、`dns_apply_failures_total`、
  `dpf_api_calls_total`、`dpf_api_call_duration_seconds` を Prometheus 形式で
  公開する。**ゾーン名・レコード名・レコード値はラベルに含めない。** `/metrics` は
  無認証で公開されるため
- OpenTelemetry のトレースと計測値を OTLP で送出できる (`--otlp-endpoint`)。
  **未指定なら送出しない。** ログの OTLP 送出は未実装
- エラーを一時的と恒久的に分類して返す。一時的なら `5xx` を返して ExternalDNS に
  再試行させ、恒久的なら `4xx` を返して再試行させない。**判断がつかないものは
  一時的に倒す**
- 配布はコンテナイメージで行う。ベースイメージは `scratch`。依存のライセンス本文を
  `/licenses/` に同梱し、SBOM と provenance を紐づける
- 上流の external-dns チャート (**1.22.0 以降**) へサイドカーとして与える推奨 values を
  README に示す。ExternalDNS 本体のバージョンはチャートの appVersion に従い、
  `image.tag` では固定しない。同期方法は `policy: upsert-only` (削除しない) とし、
  削除まで同期させる場合は `sync` にする
- `/metrics` のスクレイプ元だけに ingress を絞る NetworkPolicy の例を README に示す。
  egress は同居する ExternalDNS 本体が Kubernetes API に接続するため、例を示さない

### Security

- ビルドに使う Go を 1.27.2 へ更新し、標準ライブラリの脆弱性を修正した。
  いずれも本サービスの HTTP サーバーから到達する経路にある
  - `net/http` / HTTP/2: GO-2026-6617、GO-2026-6613、GO-2026-6612、GO-2026-6611、
    GO-2026-6610、GO-2026-6605、GO-2026-6603
  - `net/textproto`: GO-2026-6608
  - `crypto/tls`: GO-2026-6607
  - `html/template`: GO-2026-6600、GO-2026-6599
- `golang.org/x/net` を v0.61.0 へ更新し、HTTP/2 の SETTINGS フレームによる
  サービス拒否 (CVE-2026-78669) を修正した。配布イメージの走査で検出されたもの

[Unreleased]: https://github.com/iij/external-dns-iij-dpf-webhook/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/iij/external-dns-iij-dpf-webhook/releases/tag/v0.1.0
