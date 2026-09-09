# external-dns-iij-dpf-webhook

[ExternalDNS](https://github.com/kubernetes-sigs/external-dns) の webhook provider として、
**IIJ DNSプラットフォームサービス (DPF)** を DNS プロバイダにするサービスです。

> [!IMPORTANT]
> 本プログラムは IIJ DNSプラットフォームサービスのサポート対象外です。
> バグ報告や機能追加の要望は、サポートセンターではなく GitHub の Issue へお願いします。

ExternalDNS Pod のサイドカーとして動作し、Ingress や Service から算出された DNS レコードを
DPF に反映します。

---

## 対応範囲

| 項目 | 内容 |
|---|---|
| ExternalDNS | **v0.22.0 以降** |
| webhook provider API | メディアタイプ `application/external.dns.webhook+json;version=1` |
| 配布形態 | **コンテナイメージのみ** (バイナリ単体の配布はありません) |

### 対応レコード種別

ExternalDNS が表現できる種別と DPF が提供する種別の**交差である 9 種別**を扱います。

```
A  AAAA  CNAME  TXT  SRV  NS  PTR  MX  NAPTR
```

対象外になるものが両側にあります。

| 種別 | 理由 |
|---|---|
| `SOA` `CAA` `DS` `HTTPS` `SVCB` `TLSA` `ANAME` | DPF は対応するが ExternalDNS 側に表現がない。**DPF 上に存在しても変更・削除しません** |
| `DNAME` | ExternalDNS は表現できるが DPF に対応する種別がない |

---

## ⚠ 利用前提条件

本 provider は変更の適用時に、**ゾーンの内容を読み取り、変更を反映した内容を書き戻します**。
この読み取りと書き戻しの間に他者が同じゾーンを変更すると、その変更は書き戻しによって
取り消されます。区間はゾーンロックで保護しますが、**ロックを取らない変更者には効きません**。

導入前に、以下を満たせるか確認してください。

### PC-001: 他の機械的な変更者は、ロックに参加するか収束動作を持つこと

同一ゾーンを本 provider 以外の**機械的な手段**で変更する場合、その手段は次のいずれかを
満たす必要があります。

- **(a)** 本 provider と同じゾーンロックを取得してから変更する
- **(b)** あるべき状態へ継続的に収束させる (取り消されても次の周回で再適用される)

本 provider 自身は (b) を満たします。ExternalDNS が周期的に再計算するためです。

### PC-002: 上記を満たさない変更は失われうる

いずれも満たさない機械的な変更は、競合したとき黙って取り消されることがあります。
**本 provider はこれを検出も防止もできません。**

### PC-003: 人手による変更も同様

DPF コンソールでの操作も、適用の区間に重なれば取り消されうります。頻度は低いものの、
原理は同じです。

### PC-004: 未反映の編集を残さないこと

適用はゾーンの内容を置き換えるため、その時点で存在した**保留変更 (未反映の編集) は
破棄されます**。権威 DNS へ公開されることはありませんが、編集作業そのものが失われます。
DPF コンソールでの作業を途中で中断したまま放置しないでください。

破棄の記録は DPF サービス側のログに残ります。本 provider は検出も記録もしません。

> **前提を満たせない場合**: 本 provider に管理させるゾーンを、他の機械的な変更者と
> 共有しないでください。**ゾーンを分けるのが最も確実です。**

---

## デプロイ

上流の [external-dns Helm チャート](https://kubernetes-sigs.github.io/external-dns/)
を用います。同チャートは webhook provider をサイドカーとして扱う仕組みを備えており、
**本リポジトリは独自のチャートを提供しません**。

### 1. アクセストークンの Secret を作る

```bash
kubectl create secret generic dpf-token \
  --namespace external-dns \
  --from-literal=token='<DPF のアクセストークン>'
```

### 2. values を用意する

```yaml
# values.yaml
provider:
  name: webhook
  webhook:
    image:
      repository: ghcr.io/iij/external-dns-iij-dpf-webhook
      tag: v0.1.0          # latest に依存しないこと

    args:
      - --dpf-token-file=/secrets/token
      - --domain-filter=example.jp    # 指定しないと 1 件も管理されません

    extraVolumeMounts:
      - name: dpf-token
        mountPath: /secrets
        readOnly: true

    # 既定拒否。緩めるのは必要になったときだけにしてください。
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      runAsNonRoot: true
      runAsUser: 65532
      capabilities:
        drop: ["ALL"]

    resources:
      requests:
        cpu: 10m
        memory: 32Mi
      limits:
        memory: 128Mi

extraVolumes:
  - name: dpf-token
    secret:
      secretName: dpf-token

# 本 provider は Kubernetes API を使いません。
automountServiceAccountToken: false
serviceAccount:
  automountServiceAccountToken: false
```

### 3. インストールする

```bash
helm repo add external-dns https://kubernetes-sigs.github.io/external-dns/
helm upgrade --install external-dns external-dns/external-dns \
  --namespace external-dns --create-namespace \
  --values values.yaml
```

### ポートについて

チャートと本 provider の既定値は噛み合うようにできています。**変更しないでください。**

| 経路 | ポート |
|---|---|
| ExternalDNS 本体 → webhook の provider API | `localhost:8888`（ExternalDNS の既定） |
| kubelet の probe、Prometheus のスクレイプ | `8080`（チャートが `containerPort` に直書き） |

`8080` はチャートのテンプレートに直接書かれており **values で変更できません**。
本 provider の `--exposed-addr` を既定から変えると、probe と serviceMonitor が
届かなくなります。

### ⚠ NetworkPolicy はチャートに含まれません

チャートには NetworkPolicy のテンプレートがなく、**別途マニフェストを適用する必要が
あります**。以下は egress を全拒否から始める例です。DPF API のエンドポイントと
名前解決だけを許可します。

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: external-dns-egress
  namespace: external-dns
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: external-dns
  policyTypes: [Egress, Ingress]

  egress:
    # 名前解決
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
    # DPF API と Kubernetes API。宛先は環境に合わせて絞ってください。
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
      ports:
        - protocol: TCP
          port: 443

  ingress:
    # probe とスクレイプに必要な送信元のみ。全開放しないでください。
    - from:
        - namespaceSelector: {}
      ports:
        - protocol: TCP
          port: 8080
```

> [!NOTE]
> `/healthz` と `/metrics` は同一ポートで提供されるため、**NetworkPolicy で
> 区別できません**。probe を通す設定は、同時に `/metrics` を同じ送信元へ
> 露出させます。メトリクスにゾーン名やレコード値を載せていないのはこのためです。

OTLP を使う場合は、送出先への egress を上記に追加してください。使わない構成では
穴を開けないでください。

---

## アクセストークンの与え方

トークンの供給元は **2 つに限られます**。

### ファイル (Secret のマウント)

```
--dpf-token-file /secrets/token
```

要求のたびにファイルを読み直すため、**Secret の内容が更新されれば再起動なしに反映されます**。

### シークレット管理サービス

```
--dpf-token-secret-manager aws \
--dpf-token-secret-id prod/dpf/token
```

対応するのは `vault` / `aws` / `azure` / `gcp` です。各サービスへの接続資格情報は、
それぞれの標準的な仕組み (環境変数、ワークロード ID、インスタンスメタデータなど) から
解決されます。

`azure` のみ Key Vault の URL が環境から導けないため、明示指定が必要です。

```
--dpf-token-secret-manager azure \
--dpf-token-secret-endpoint https://myvault.vault.azure.net/ \
--dpf-token-secret-id dpf-token
```

### 環境変数と引数からは受け取りません

`DPF_API_TOKEN` 環境変数や、トークンを直接渡すコマンドライン引数は**用意していません**。
環境変数は `kubectl describe pod` やプロセスの環境から読み取れ、ファイルやシークレット
管理サービスと同じ保護水準を満たさないためです。

---

## 設定

```
--domain-filter example.jp    管理対象ドメイン (複数指定可)
--provider-addr 127.0.0.1:8888  webhook の待ち受け (既定: ループバックのみ)
--exposed-addr :8080          healthz と metrics の待ち受け
--otlp-endpoint <host:port>   OTLP の送出先 (未指定なら送出しない)
--log-level info              debug|info|warn|error
```

全項目は `--help` で確認できます。

> **`--domain-filter` を指定しないと、レコードは 1 件も管理されません。**
> 未指定を「全ドメインを管理」とは解釈しません。設定漏れの結果が
> 「気付かないうちに全ゾーンを操作できる」状態になるのを避けるためです。

### 待ち受けアドレスの既定値

**webhook の待ち受けはループバックのみです。** ExternalDNS とは同一 Pod 内のサイドカーと
して通信するため、Pod 外への公開は不要です。

`healthz` と `metrics` は kubelet と Prometheus から到達する必要があるため、Pod 外へ
公開されます。この 2 つは同一ポートで提供されるため、**NetworkPolicy で区別できません**。
probe を通す設定は同時に `/metrics` を同じ送信元へ露出させます。

---

## 可観測性

| 種別 | 提供形式 |
|---|---|
| ログ | 標準出力への構造化ログ (**常時有効**) |
| メトリクス | Prometheus 形式 (`/metrics`) と OTLP |
| トレース | OTLP |

OTLP は `--otlp-endpoint` を指定した場合にのみ有効になります。TLS 検証は既定で有効です。

**送出先が到達不能でも DNS の更新は止まりません。** 標準出力へのログも、OTLP の設定に
関わらず出続けます。

メトリクスのラベルには**ゾーン名・レコード名・レコード値を含めません**。基数が非有界で
あることに加え、`/metrics` は probe と同一ポートで公開されるためです。

---

## コンテナイメージ

### ライセンス

イメージの `/licenses/` にライセンス本文を同梱しています。

```
/licenses/
├── LICENSE            本プロジェクト (Apache-2.0)
├── NOTICE
└── third-party/       依存モジュール (モジュールパスの階層のまま)
```

`org.opencontainers.image.licenses` アノテーションにも `Apache-2.0` を設定しています。

依存モジュールのライセンスは、次の範囲に限っています。

| 許容 | 条件 |
|---|---|
| Apache-2.0 / MIT / BSD-2-Clause / BSD-3-Clause / ISC | 表示のみ |
| MPL-2.0 | ソース提供義務を満たす形で同梱 |

**GPL / AGPL / LGPL / SSPL の依存は含みません。** 本サービスは静的リンクしたバイナリを
配布するため、これらが入ると配布物全体に同じ条件が及びます。

### SBOM

リリースを公開すると、**配布イメージの SBOM が SPDX JSON (`sbom.spdx.json`) で
リリースページに添付されます**。

```bash
gh release download v0.1.0 --pattern sbom.spdx.json
```

SBOM の対象はソースツリーではなく**配布されるコンテナイメージ**です。実際にリンクされた
依存を反映するのはイメージを走査した結果だからです。

内容を確認する例:

```bash
# パッケージ数
jq '.packages | length' sbom.spdx.json

# ライセンスの内訳
jq -r '.packages[].licenseConcluded' sbom.spdx.json | sort | uniq -c | sort -rn

# 特定の依存を探す
jq -r '.packages[] | select(.name | test("miekg")) | "\(.name) \(.versionInfo)"' sbom.spdx.json
```

手元で同じ内容を生成することもできます。

```bash
make sbom
```

### 署名の検証

SBOM と provenance は **Artifact Attestations** で署名されています。鍵の配布は
不要で、GitHub の OIDC ID により「どのリポジトリのどのワークフローが作ったか」を
検証できます。

```bash
# イメージに紐づくアテステーション (SBOM と provenance)
gh attestation verify oci://ghcr.io/iij/external-dns-iij-dpf-webhook:v0.1.0 \
  --repo iij/external-dns-iij-dpf-webhook

# リリースへ添付された SBOM ファイル
gh attestation verify sbom.spdx.json --repo iij/external-dns-iij-dpf-webhook

# referrer として何が紐づいているか
cosign tree ghcr.io/iij/external-dns-iij-dpf-webhook:v0.1.0
```

イメージのアテステーションはレジストリの **referrer** として紐づいているため、
イメージを取得した先でも検証できます。

### デバッグ

イメージのベースは `scratch` で、**シェルもパッケージマネージャも含みません**。
`kubectl exec` でシェルに入ることはできません。

調査には ephemeral container を使います。

```bash
kubectl debug -it <pod> \
  --image=busybox:latest \
  --target=<webhook-container> \
  --share-processes
```

`--target` でプロセス名前空間を共有すると、`/proc/<pid>/root/` 経由で webhook 側の
ファイルシステムを参照できます。

```sh
# ephemeral container 内から
ls /proc/1/root/licenses/
```

ログとメトリクスで足りる調査は、そちらを先に使ってください。

---

## 検証状況

`main` へのマージ前に、実際の DPF に対する検証を CI で行っています
(追加・変更・削除・再適用時の冪等性、および管理対象外レコードが変化しないこと)。
検証は破壊的操作を行うため、専用の検証用ゾーンに対して実行しています。

---

## 開発

[docs/development.md](docs/development.md) を参照してください。

設計の判断根拠は `specs/001-webhook-provider/` にあります。守るべき原則は
[`.specify/memory/constitution.md`](.specify/memory/constitution.md) にまとまっています。

---

## ライセンス

[Apache License 2.0](LICENSE)

Copyright 2026 Internet Initiative Japan Inc.
