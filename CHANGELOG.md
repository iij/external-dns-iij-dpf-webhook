# Changelog

本プロジェクトの利用者に関わる変更を記録します。

書式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に、
バージョン番号は [Semantic Versioning](https://semver.org/lang/ja/) に従います。

## [0.1.0] - 2026-10-06

初回リリースです。

### Added

- ExternalDNS (v0.22.0 以降) の webhook provider として、
  IIJ DNSプラットフォームサービス (DPF) のレコードを管理する機能。
  メディアタイプは `application/external.dns.webhook+json;version=1` です。
- 対応レコード種別は `A` `AAAA` `CNAME` `TXT` `SRV` `PTR` `MX` `NAPTR` の 8 種別です。
  `NS` は扱いません。対象外の種別のレコードは、DPF 上にあっても変更・削除しません。
- 親子・孫のゾーンが DPF 上に併存する場合は、最も深く一致するゾーンへ書き込みます。
- 変更の適用中は、dpf-go v0.6.0 以降の既定の排他でゾーンをロックします。
  ロックの状態はゾーンのラベル `lock.dpf-go` に置くため、運用者がゾーンに
  付けられるラベルは 9 個までになります。
- 本 provider が書くレコード (所有権の記録の `TXT` を含む) に、ラベル
  `managed-by: external-dns-iij-dpf-webhook` を付けます。ラベルは更新のたびに
  上書きします。
- ゾーン反映の履歴の説明に、実行者として `external-dns-iij-dpf-webhook` を記録します。
- 反映済みの内容と変わらない変更では、ゾーン反映を行いません。
- アクセストークンは、マウントしたファイル、またはシークレット管理サービスから
  取得します。外部でローテーションされたトークンは再起動なしに使われます。
  環境変数とコマンドライン引数からは受け取りません。
- 構造化ログ (標準出力、常時有効)、Prometheus 形式のメトリクス (`/metrics`)、
  OTLP (gRPC / HTTP) によるメトリクスとトレースの送出。
  ログの OTLP 送出は未実装です。
- コンテナイメージでの配布。ベースイメージは `scratch` です。イメージに依存の
  ライセンス本文 (`/licenses/`) を同梱し、SBOM と provenance を紐づけます。

### 既定値

- `--domain-filter` を指定しない場合、レコードを 1 件も管理しません。
- webhook の待ち受け (`--provider-addr`) はループバックのみ (`127.0.0.1:8888`) です。
- healthz と metrics の待ち受け (`--exposed-addr`) は `:8080` です。
- OTLP の送出は、`--otlp-endpoint` を指定した場合にのみ有効になります。
  送出先の TLS 証明書は既定で検証します。

[0.1.0]: https://github.com/iij/external-dns-iij-dpf-webhook/releases/tag/v0.1.0
