# リファレンス

本書は `external-dns-iij-dpf-webhook` の設定と外部から観測できる振る舞いを
網羅的に記す。使い方の導入は [README](../README.md)、開発の手順は
[development.md](./development.md) にある。

- [エンドポイント](#エンドポイント)
- [設定](#設定)
- [アクセストークンの供給元](#アクセストークンの供給元)
- [メトリクス](#メトリクス)
- [ログ](#ログ)
- [トレース](#トレース)
- [エラーの分類と状態コード](#エラーの分類と状態コード)
- [レコード種別と制約](#レコード種別と制約)
- [この文書と実装の一致](#この文書と実装の一致)
- [既知の制限](#既知の制限)

---

## エンドポイント

待ち受けは 2 つに分かれる。公開範囲が違うためである。

### provider リスナー (既定 `127.0.0.1:8888`)

ExternalDNS からの webhook 要求を受ける。既定ではループバックのみに待ち受ける。

<!-- reference:endpoints-provider -->

| メソッド | 経路 | 成功時 | 内容 |
|---|---|---|---|
| `GET` | `/` | `200` | 管理対象ドメイン (`{"filters": [...]}`) |
| `GET` | `/records` | `200` | 管理対象のレコード一覧 |
| `POST` | `/records` | **`204`** | 変更セットの適用。`200` ではない |
| `POST` | `/adjustendpoints` | `200` | DPF に保存される形へ整えた結果 |

メディアタイプは `application/external.dns.webhook+json;version=1`。
ネゴシエートできない `Accept` は `406` で拒否する。

上記以外の経路は `404` になる。独自のエンドポイントは持たない。

### exposed リスナー (既定 `:8080`)

kubelet の probe と Prometheus のスクレイプを受ける。Pod 外から到達できる。

<!-- reference:endpoints-exposed -->

| メソッド | 経路 | 成功時 | 内容 |
|---|---|---|---|
| `GET` | `/healthz` | `200` | `ok`。本文に状態の詳細は載せない |
| `GET` | `/metrics` | `200` | Prometheus 形式の計測値 |

**この 2 つ以外を持たない。** DNS 構成が推測できる情報を無認証で返さないため。

---

## 設定

すべてコマンドライン引数で与える。**環境変数からは設定を読まない。**

<!-- reference:flags -->

| フラグ | 既定 | 内容 |
|---|---|---|
| `--domain-filter` | (なし) | 管理対象ドメイン。複数指定可。**未指定なら管理対象なし** |
| `--dpf-endpoint` | (なし) | DPF API のエンドポイント。未指定なら `dpf-go` の既定値 |
| `--dpf-token-file` | (なし) | アクセストークンを収めたファイルのパス |
| `--dpf-token-secret-manager` | (なし) | `vault` \| `aws` \| `azure` \| `gcp` |
| `--dpf-token-secret-id` | (なし) | シークレット管理サービス上の識別子 |
| `--dpf-token-secret-endpoint` | (なし) | サービスごとに意味が違う。[下記参照](#アクセストークンの供給元) |
| `--provider-addr` | `127.0.0.1:8888` | provider リスナーの待ち受けアドレス |
| `--exposed-addr` | `:8080` | exposed リスナーの待ち受けアドレス |
| `--otlp-endpoint` | (なし) | OTLP の送出先。**未指定なら送出しない** |
| `--otlp-protocol` | `grpc` | `grpc` \| `http` |
| `--otlp-insecure` | `false` | OTLP 送出先への TLS 検証を無効にする |
| `--log-level` | `info` | `debug` \| `info` \| `warn` \| `error` |

### 意図的に存在しないもの

| なし | 理由 |
|---|---|
| `--dpf-token` | トークンを引数で渡す経路は作らない。プロセス一覧から読める |
| 環境変数 `DPF_API_TOKEN` | `kubectl describe pod` とプロセス環境から読める。`dpf-go` の既定経路をあえて使わない |

いずれも指定すると**未定義のフラグとして起動に失敗する**。黙って無視しない。

### 検証の規則

- `--dpf-token-file` と `--dpf-token-secret-manager` は**どちらか一方**。併用は起動失敗
- `--dpf-token-secret-manager` を指定したら `--dpf-token-secret-id` が必須
- いずれの供給元も指定しなければ起動失敗
- `--domain-filter` が未指定でも**起動する**。ただし管理対象は空になり、
  レコードは 1 件も変更されない (原則 VI)

---

## アクセストークンの供給元

供給元は**マウントされたファイル**と**外部シークレット管理サービス**の 2 系統に限る。

### 用語について

構成の文脈では「KMS」と呼ばれることがあるが、本サービスが対応するのは
**シークレット管理サービス** (secret manager) である。任意の値を保管して
取り出す用途のものであり、鍵の管理・暗号操作を行う KMS (Key Management Service)
そのものではない。

ただし AWS では両者が関係する。Secrets Manager のシークレットが
カスタマー管理の KMS キーで暗号化されている場合、取得側に `kms:Decrypt` が
必要になる ([下記](#aws-secrets-manager))。

### 対応するシークレット管理サービス

`--dpf-token-secret-manager` に渡せる値は次のとおり。

<!-- reference:secret-managers -->

| 値 | サービス |
|---|---|
| `vault` | HashiCorp Vault (KV シークレットエンジン) |
| `aws` | AWS Secrets Manager |
| `azure` | Azure Key Vault |
| `gcp` | Google Secret Manager |

### すべての供給元に共通する性質

| 事項 | 振る舞い |
|---|---|
| 取得のタイミング | **DPF API を呼ぶたびに取得する。キャッシュしない** |
| ローテーション | 外部で差し替えれば**再起動なしに**次の要求から反映される |
| 値の整形 | 前後の空白を取り除く。末尾の改行は気にしなくてよい |
| 取得失敗の分類 | **恒久的な失敗**。ExternalDNS は再試行しない |
| 失敗時のメッセージ | 「アクセストークンを取得できませんでした」の定型文に置き換える。**トークン値やファイル内容を含めない** |

**キャッシュしないことの代償**: シークレット管理サービスを使う場合、
DPF API の呼び出し 1 回ごとにサービスへの問い合わせが 1 回発生する。
ExternalDNS の同期間隔 (既定 1 分) ごとにレコード一覧の取得があり、
変更時はさらに増える。呼び出し課金とレート制限に影響する。

キャッシュ期間を指定する設定は**現在用意していない** ([既知の制限](#既知の制限))。

---

### ファイル (Secret のマウント)

```
--dpf-token-file=/secrets/token
```

| 事項 | 内容 |
|---|---|
| 指定するもの | トークンを収めたファイルのパス |
| 鍵の位置 | ファイルの**内容全体**がトークン。JSON として解釈しない |
| 認証 | 不要。ファイルを読めればよい |
| 権限 | プロセスがそのファイルを読めること |

Kubernetes では Secret をマウントする。README の推奨 values を参照。

```bash
kubectl create secret generic dpf-token \
  --namespace external-dns \
  --from-literal=token='<DPF のアクセストークン>'
```

`--from-literal=token=...` のキー名 (`token`) が、マウント先のファイル名になる。
上の例では `/secrets/token` に置かれる。

---

### HashiCorp Vault

```
--dpf-token-secret-manager=vault \
--dpf-token-secret-id=dpf/api
```

| 事項 | 内容 |
|---|---|
| `--dpf-token-secret-id` | **KV シークレットエンジンのマウント配下のパス** |
| マウントパス | **`secret` 固定** |
| KV バージョン | **2 固定** |
| 鍵の名前 | **`token` 固定** |
| バージョン | 最新 |
| `--dpf-token-secret-endpoint` | Vault の接続先 URL。省略時は `VAULT_ADDR` |

**マウントパス・鍵の名前・KV バージョンは変更できない。** `dpf-go` 側には
オプションがあるが、本サービスは既定のまま使う。

上の例で読まれるのは、KV v2 マウント `secret` のパス `dpf/api` にある
**`token` フィールド**である。格納は次のようになる。

```bash
vault kv put secret/dpf/api token='<DPF のアクセストークン>'
```

#### 必要な権限

読み取り権限は **KV v2 のデータパス** に対して与える。`secret/dpf/api` ではなく
`secret/data/dpf/api` である点に注意する。KV v2 は API 上のパスに `data/` が
挟まる。

```hcl
path "secret/data/dpf/api" {
  capabilities = ["read"]
}
```

#### 認証

Vault SDK の既定設定を用いる。**認証は環境変数で解決される。**

| 環境変数 | 用途 |
|---|---|
| `VAULT_TOKEN` | **Vault の認証トークン。これがないと認証できない** |
| `VAULT_ADDR` | 接続先。`--dpf-token-secret-endpoint` を指定した場合はそちらが優先 |
| `VAULT_NAMESPACE` | Vault Enterprise の名前空間 |
| `VAULT_CACERT` / `VAULT_CAPATH` / `VAULT_CACERT_BYTES` | サーバ証明書の検証 |
| `VAULT_CLIENT_CERT` / `VAULT_CLIENT_KEY` | クライアント証明書 |
| `VAULT_SKIP_VERIFY` | TLS 検証の無効化 |
| `VAULT_TLS_SERVER_NAME` | 証明書の名前検証の上書き |
| `VAULT_CLIENT_TIMEOUT` / `VAULT_MAX_RETRIES` | 接続の挙動 |
| `VAULT_AGENT_ADDR` / `VAULT_PROXY_ADDR` | Agent / プロキシ経由の接続 |

**本サービスは AppRole や Kubernetes 認証のログイン処理を行わない。**
`VAULT_TOKEN` に有効なトークンが与えられている前提である。Kubernetes 上では
Vault Agent Injector や Secrets Store CSI Driver で `VAULT_TOKEN` を用意する、
あるいはそれらでトークンをファイルとして配置し `--dpf-token-file` を使う。

> **注意**: `VAULT_TOKEN` は環境変数として渡すことになる。本サービスが DPF の
> トークンを環境変数から受け取らないのは、`kubectl describe pod` やプロセス環境から
> 読めるためである。**Vault の認証トークンにも同じ問題がある。** Vault を使うより
> `--dpf-token-file` に CSI Driver でトークンを配置する方が、露出は小さい。

---

### AWS Secrets Manager

```
--dpf-token-secret-manager=aws \
--dpf-token-secret-id=prod/dpf/token
```

| 事項 | 内容 |
|---|---|
| `--dpf-token-secret-id` | シークレットの**名前**、または**完全な ARN** |
| 鍵の位置 | シークレットの**値全体**がトークン。**JSON として解釈しない** |
| バージョン | 最新 (`AWSCURRENT`) |
| `--dpf-token-secret-endpoint` | **使わない**。リージョンは SDK の既定解決に従う |

**シークレットの値には生のトークンを入れる。** `{"token": "..."}` のような
JSON を入れると、その JSON 文字列全体がトークンとして DPF に送られ、認証に失敗する。

```bash
aws secretsmanager create-secret \
  --name prod/dpf/token \
  --secret-string '<DPF のアクセストークン>'
```

コンソールで作る場合は「その他のシークレットのタイプ」→「プレーンテキスト」を
選ぶ。「キー/値」で作ると JSON になる。

`SecretString` が空の場合は `SecretBinary` が使われる。

#### 認証

AWS SDK の既定の資格情報解決順に従う。

1. 環境変数 (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`)
2. 共有設定ファイル (`~/.aws/credentials`、`~/.aws/config`、`AWS_PROFILE`)
3. Web Identity トークン (`AWS_WEB_IDENTITY_TOKEN_FILE` / `AWS_ROLE_ARN`) —
   **EKS の IRSA / Pod Identity がこれ**
4. インスタンスメタデータ (EC2 インスタンスプロファイル)

リージョンは `AWS_REGION` または共有設定から解決される。**指定するフラグはない。**

#### 必要な権限

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "secretsmanager:GetSecretValue",
      "Resource": "arn:aws:secretsmanager:<region>:<account>:secret:prod/dpf/token-*"
    }
  ]
}
```

**シークレットがカスタマー管理の KMS キーで暗号化されている場合**、
そのキーに対する `kms:Decrypt` も必要になる。

```json
{
  "Effect": "Allow",
  "Action": "kms:Decrypt",
  "Resource": "arn:aws:kms:<region>:<account>:key/<key-id>"
}
```

AWS 管理キー (`aws/secretsmanager`) を使う既定の構成では、この追加は要らない。

---

### Azure Key Vault

```
--dpf-token-secret-manager=azure \
--dpf-token-secret-endpoint=https://myvault.vault.azure.net/ \
--dpf-token-secret-id=dpf-token
```

| 事項 | 内容 |
|---|---|
| `--dpf-token-secret-id` | シークレットの**名前** |
| 鍵の位置 | シークレットの**値全体**がトークン。JSON として解釈しない |
| バージョン | 最新 |
| `--dpf-token-secret-endpoint` | **Key Vault の URL。必須** |

**4 サービスのうち Azure だけ接続先が必須である。** Key Vault の URL は
環境から導けないため。省略すると起動に失敗する。

```bash
az keyvault secret set \
  --vault-name myvault \
  --name dpf-token \
  --value '<DPF のアクセストークン>'
```

#### 認証

`DefaultAzureCredential` の解決順に従う。

1. 環境変数 (`AZURE_TENANT_ID` / `AZURE_CLIENT_ID` / `AZURE_CLIENT_SECRET` など)
2. ワークロード ID (`AZURE_FEDERATED_TOKEN_FILE`) — **AKS のワークロード ID がこれ**
3. マネージド ID
4. Azure CLI / Azure Developer CLI / Azure PowerShell (開発時)

#### 必要な権限

RBAC で構成した Key Vault の場合、**Key Vault Secrets User** ロールを与える。

```bash
az role assignment create \
  --role "Key Vault Secrets User" \
  --assignee <principal-id> \
  --scope <key-vault-resource-id>
```

アクセスポリシーで構成した Key Vault の場合、シークレットに対する `get` を与える。

```bash
az keyvault set-policy --name myvault \
  --object-id <principal-id> --secret-permissions get
```

---

### Google Secret Manager

```
--dpf-token-secret-manager=gcp \
--dpf-token-secret-endpoint=my-project \
--dpf-token-secret-id=dpf-api-token
```

| 事項 | 内容 |
|---|---|
| `--dpf-token-secret-id` | シークレット ID、または完全修飾リソース名 |
| 鍵の位置 | ペイロードの**値全体**がトークン。JSON として解釈しない |
| バージョン | `latest` |
| `--dpf-token-secret-endpoint` | **プロジェクト ID。接続先 URL ではない** |

**`--dpf-token-secret-endpoint` の意味が他のサービスと違う。**
Azure では Key Vault の URL、Vault では接続先 URL だが、**GCP ではプロジェクト ID**
として解釈される。

`--dpf-token-secret-id` の書き方によって、プロジェクト ID が必要かどうかが変わる。

| `--dpf-token-secret-id` に渡す値 | プロジェクト ID | 取得されるもの |
|---|---|---|
| `dpf-api-token` | **必須** | `projects/<project>/secrets/dpf-api-token/versions/latest` |
| `projects/my-project/secrets/dpf-api-token` | 不要 | 同上の `versions/latest` |
| `projects/my-project/secrets/dpf-api-token/versions/3` | 不要 | そのバージョン |

3 行目のように完全修飾名で書けば、`--dpf-token-secret-endpoint` は要らず、
**バージョンも固定できる**。

```bash
printf '%s' '<DPF のアクセストークン>' | \
  gcloud secrets create dpf-api-token --data-file=-
```

`printf` を使うのは末尾に改行を入れないため。ただし取得側で前後の空白は
取り除かれるので、改行が入っていても動く。

#### 認証

アプリケーションの既定資格情報 (ADC) の解決順に従う。

1. `GOOGLE_APPLICATION_CREDENTIALS` が指すサービスアカウントキーのファイル
2. `gcloud auth application-default login` の資格情報 (開発時)
3. メタデータサーバ (GCE / GKE / Cloud Run) —
   **GKE の Workload Identity がこれ**

#### 必要な権限

シークレットに対する `secretmanager.versions.access`。
**Secret Manager のシークレット アクセサー** ロールが該当する。

```bash
gcloud secrets add-iam-policy-binding dpf-api-token \
  --member='serviceAccount:<sa>@<project>.iam.gserviceaccount.com' \
  --role='roles/secretmanager.secretAccessor'
```

---

### 供給元の比較

| | `--dpf-token-secret-id` | 鍵の位置 | `--dpf-token-secret-endpoint` | 認証 |
|---|---|---|---|---|
| ファイル | — (パスは `--dpf-token-file`) | ファイルの内容全体 | — | 不要 |
| Vault | マウント `secret` 配下のパス | **`token` フィールド** | 接続先 URL (任意) | `VAULT_TOKEN` |
| AWS | シークレット名 または ARN | 値全体 | — (未使用) | SDK の既定解決 |
| Azure | シークレット名 | 値全体 | **Key Vault の URL (必須)** | `DefaultAzureCredential` |
| GCP | シークレット ID または リソース名 | 値全体 | **プロジェクト ID** | ADC |

**Vault だけが値の中の特定のフィールド (`token`) を読む。** 他の 3 つは値全体を
トークンとして扱う。JSON を格納しないこと。

---

## メトリクス

Prometheus 形式 (`/metrics`) と OTLP の双方で同じ計測値を提供する。
計測器は 1 組だけ定義し、両形式はそこから読み出される。

| 名前 | 種別 | 単位 | ラベル | 内容 | 値が変化する契機 |
|---|---|---|---|---|---|
| `dns_record_changes_total` | Counter | 件 | `operation`, `success` | DPF へ適用した DNS レコード変更の件数 | `POST /records` の適用が完了したとき。操作種別ごとに件数分だけ増える |
| `dns_apply_failures_total` | Counter | 回 | (なし) | 変更セットの適用に失敗した回数 | `POST /records` の適用が失敗したとき、1 回につき 1 増える |
| `dpf_api_calls_total` | Counter | 回 | `operation`, `success` | DPF API の呼び出し回数 | DPF API の呼び出しが終わるたびに 1 増える |
| `dpf_api_call_duration_seconds` | Histogram | 秒 | `operation`, `success` | DPF API の呼び出しに要した時間 | 同上。呼び出しごとに所要時間が記録される |

**ゾーンロックの取得・解放は計測されていない。** `dpf_api_calls_total` には
現れない。ロックの競合はログから読む ([既知の制限](#既知の制限))。

### 出力に現れる系列

上の表は計測器の一覧である。**Histogram は出力で 3 つの系列に展開される。**
Counter は名前に `_total` を含めて定義しているため、出力名と一致する。

<!-- reference:metrics -->

| 系列 | 由来 |
|---|---|
| `dns_record_changes_total` | 同名の Counter |
| `dns_apply_failures_total` | 同名の Counter |
| `dpf_api_calls_total` | 同名の Counter |
| `dpf_api_call_duration_seconds_bucket` | `dpf_api_call_duration_seconds` (Histogram) の区間ごとの累積 |
| `dpf_api_call_duration_seconds_sum` | 同 Histogram の合計 |
| `dpf_api_call_duration_seconds_count` | 同 Histogram の件数 |

OTLP では計測器の名前がそのまま使われる (`dns_record_changes` など)。
**両形式は同一の計測値を表す。** 計測器を 1 組だけ定義し、そこから読み出して
いるためである。

**まだ一度も記録が発生していない系列は出力に現れない。** 起動直後に
`/metrics` を取得しても、変更を 1 件も適用していなければ
`dns_record_changes_total` は現れない。記載漏れと区別すること。

### ラベルの値

| ラベル | 取りうる値 |
|---|---|
| `operation` (変更) | `create` \| `update` \| `delete` |
| `success` | `true` \| `false` |

`dpf_api_calls_total` と `dpf_api_call_duration_seconds` の `operation` は、
DPF API の呼び出しの種類を表す。

<!-- reference:dpf-operations -->

| `operation` | 対応する呼び出し |
|---|---|
| `list_zones` | ゾーンの一覧取得 |
| `list_records` | 反映済みレコードの一覧取得 (`GET /records` の実体) |
| `current_records` | 適用時に読み直す反映済みレコードの全件取得 |
| `atomic_changes` | ゾーンの一括更新と反映 |

### ラベルに含まれないもの

**ゾーン名・レコード名・レコード値をラベルに含めない。** 理由は 2 つある。

- 基数が非有界であり、時系列が際限なく増える
- `/metrics` は probe と同一ポートで**無認証**に公開される。これらを載せると
  DNS 構成が誰にでも読み取れる

障害の対象を特定する情報はログとトレースにある。そちらを使う。

---

## ログ

標準出力へ構造化ログを書く。

> **`--otlp-endpoint` を指定してもログは OTLP へ送られない。**
> 憲章はログの OTLP 送出を MUST としているが、**未実装**である
> ([既知の制限](#既知の制限))。`--otlp-endpoint` が効くのはメトリクスと
> トレースのみ。

| レベル | 出るもの |
|---|---|
| `error` | 適用の失敗、DPF API の失敗 |
| `warn` | 設定上の注意 (TLS 検証の無効化など) |
| `info` | 変更操作の内容 (ゾーン、レコード名、操作種別)、起動と停止 |
| `debug` | 内部の詳細 |

### 変更操作の記録

適用のたびに、ゾーン名と操作対象のレコード名を記録する。

| 項目 | 内容 |
|---|---|
| `zone` | ゾーン名 |
| `create` / `update` / `delete` | 操作対象のレコード (名前と種別) |

### 外部 API のエラー

**DPF がエラーを返した場合、応答を全文記録する。長さで切り詰めない。**

DPF のエラー応答は `request_id` を含み、これがサポートへの問い合わせのキーに
なる。切り詰めると、原因を DPF 側に照会する手段が失われる。

```
dpf api: status 400: 400 Bad Request: {"request_id":"...","error_details":[...],"error_type":"ParameterError",...}
```

送った要求の内容も、原因の特定に足る範囲で記録する (件数、先頭のレコード)。

**これらはログとエラーにのみ現れる。HTTP 応答の本文には載せない。**

### 認証情報

**ログ・メトリクス・トレース・エラーメッセージのいずれにもトークンは現れない。**
トークンの取得に失敗した場合のメッセージは定型文に置き換えられる。

---

## トレース

`--otlp-endpoint` を指定した場合のみ送出する。**未指定なら送出しない。**

`--otlp-endpoint` はメトリクスとトレースに効く。ログには効かない (上記)。

| スパン | 親 | 内容 |
|---|---|---|
| `provider.Records` | (要求) | レコード一覧の取得 |
| `provider.ApplyChanges` | (要求) | 変更セットの適用 |
| `dpf.list_zones` など | 上記 | DPF API の各呼び出し |

要求の受信から DPF の呼び出しまでが 1 つの流れとして繋がる。

| フラグ | 内容 |
|---|---|
| `--otlp-endpoint` | 送出先。**未指定なら送出しない** |
| `--otlp-protocol` | `grpc` (既定) または `http` |
| `--otlp-insecure` | TLS 検証を無効にする。**有効にすると警告をログに出す** |

**送出先が到達不能でも DNS の更新は継続する。** テレメトリの障害が本来の機能を
止めない。

---

## エラーの分類と状態コード

エラーは 2 つに分類され、それが HTTP の状態コードを決める。

| 分類 | 状態コード | ExternalDNS の振る舞い |
|---|---|---|
| 一時的 | `5xx` | 再試行する |
| 恒久的 | `4xx` | 再試行しない |

### 分類の規則

| 原因 | 分類 |
|---|---|
| DPF が `429` を返した (レート制限) | 一時的 |
| DPF が `5xx` を返した | 一時的 |
| DPF が `408` を返した | 一時的 |
| DPF が上記以外の `4xx` を返した | **恒久的** |
| ゾーンが他の操作でロックされている | 一時的 |
| 接続できない、名前を解決できない | 一時的 |
| 時間切れ、取り消し | 一時的 |
| **トークンを取得できない** | **恒久的** |
| ゾーンが見つからない | **恒久的** |
| 対応しないレコード種別 | **恒久的** |
| 形式違反 (TTL の範囲、CNAME の排他、apex NS の変更など) | **恒久的** |
| 分類のつかないもの | 一時的 |

**判断がつかないものは一時的に倒す。** 恒久的に倒すと ExternalDNS が再試行を
諦め、実際には復旧しうる障害で DNS の更新が止まる。

### 応答本文

**エラーの詳細を HTTP 応答の本文に載せない。** 状態コードに対応する定型文のみを
返す。詳細はログにある。応答に載せると、DNS 構成や内部状態が呼び出し側へ漏れる。

---

## レコード種別と制約

ExternalDNS が表現できる種別と DPF が提供する種別の交差である 9 種別を扱う。

<!-- reference:record-types -->

| 種別 | 内容 |
|---|---|
| `A` | IPv4 アドレス |
| `AAAA` | IPv6 アドレス |
| `CNAME` | 別名 |
| `TXT` | 文字列。ExternalDNS の所有権レジストリにも使われる |
| `SRV` | サービスの位置 |
| `NS` | 委任。**ゾーン apex の `NS` は対象外** |
| `PTR` | 逆引き |
| `MX` | メール交換 |
| `NAPTR` | 名前解決の書き換え規則 |

| 制約 | 内容 |
|---|---|
| TTL | `1`〜`2147483647`。`0` は「未指定」を意味し、ゾーンの既定 TTL が使われる |
| `A` / `AAAA` の名前 | アンダースコアを含められない |
| `CNAME` | 同一の名前に複数の値を持てない。他の種別と共存できない |
| `TXT` | character-string 1 つは 255 オクテットまで。**超える場合は自動分割される**。合計に上限はない |
| `MX` | `<preference> <exchange>` の 2 項目。preference は `0`〜`65535` |
| `SRV` | `<priority> <weight> <port> <target>` の 4 項目。数値は `0`〜`65535` |
| **ゾーン apex の `NS`** | **作成・更新・削除のいずれも受け付けない** |
| `SOA` | 扱わない。DPF 上に存在しても変更しない |

対象外の種別 (`CAA` `DS` `HTTPS` `SVCB` `TLSA` `ANAME` `DNAME`) は一覧に返さず、
変更も削除もしない。

### 名前の扱い

内部では正規化名 (小文字・末尾ドット付きの FQDN) のみを扱う。
`Example.JP`、`example.jp`、`example.jp.` はいずれも同一のレコードとして扱われる。
`GET /records` が返す名前は常に正規化名である。

---

## この文書と実装の一致

本文書の一部は、**実装との一致が機械的に検査されている**。実装を変えて
この文書を更新しないと、品質ゲート (`make all`) が失敗する。

### 検査されている記述

次の表には印 (`<!-- reference:... -->`) が付いており、実装から取り出した事実と
突き合わせられる。

| 記述 | 突き合わせる相手 |
|---|---|
| [設定](#設定)の一覧 (名前と既定値) | `--help` が出力する項目 |
| [対応するシークレット管理サービス](#対応するシークレット管理サービス)の値 | 実装が受理する値 |
| [出力に現れる系列](#出力に現れる系列) | 宣言された計測器から導いた系列名 |
| [`operation` の値](#ラベルの値) | DPF API 呼び出しの計測に渡される識別子 |
| [レコード種別](#レコード種別と制約)の一覧 | 実装が対応と宣言する種別 |
| [エンドポイント](#エンドポイント)の経路 | 実装が登録する経路 |

エンドポイントについては**経路のみ**が対象で、メソッドは検査されない。

### 検査されていない記述

**上記以外はすべて人が保っている。** 実装からは機械的に導けないためである。

| 記述 | 検査できない理由 |
|---|---|
| **トークンが読まれる位置** (値全体か、特定のフィールドか) | 依存ライブラリの既定値であり、本サービスのコードには「オプションを渡していない」ことしか現れない |
| 認証の解決順、参照される環境変数 | 各 SDK の内部仕様 |
| 必要な権限 (IAM ポリシー、RBAC ロール等) | 外部サービスの仕様。こちらから確かめられない |
| シークレットへのトークンの格納手順 | 同上 |
| 設定項目・計測値・ラベル・経路の**意味の説明** | 散文 |
| エラーの分類と外部 API の状態コードの対応 | 網羅的に列挙できない |
| レコード種別ごとの制約の内容 | 実装側の検証テストが守っている。文書との文字列一致には意味がない |
| 既知の制限の説明 | 散文 |

> [!IMPORTANT]
> **トークンが読まれる位置が検査対象外である点に注意してください。**
>
> Vault がシークレット内の `token` フィールドを読むこと、AWS / Azure / GCP が
> 値全体をトークンとして扱うことは、いずれも依存ライブラリの既定の振る舞いです。
> ライブラリが既定を変えれば、この文書は黙って誤りになります。
>
> 緩和として、依存ライブラリの版は固定されています。版が上がるときは
> Pull Request として現れるため、その時点で確かめられます。

「検査されているから正しい」と読まないでください。検査は上の表の範囲に限られます。

---

## 既知の制限

| 制限 | 内容 |
|---|---|
| **ログの OTLP 送出が未実装** | 憲章 (原則 V) が MUST としているが、実装されていない。`telemetry.New` はメトリクスとトレースのみを初期化する。`WithAdditionalSink` は用意されているが配線されていない。OTLP ログの exporter への依存もない |
| ゾーンロックの取得・解放が計測されていない | `dpf_api_calls_total` に現れない。ロックの競合はログから読む |
| トークンのキャッシュ期間を指定できない | シークレット管理サービスを使う場合、DPF API の呼び出しごとにサービスへの問い合わせが発生する。ローテーションの即時反映を優先した結果である |
| Vault のマウント・鍵名・KV バージョンを変更できない | `secret` / `token` / KV v2 に固定。`dpf-go` 側にはオプションがある |
| Vault の認証はトークンのみ | AppRole や Kubernetes 認証のログイン処理を行わない。`VAULT_TOKEN` が必要 |
| シークレットの値を JSON として解釈しない | AWS / Azure / GCP では値全体をトークンとして扱う。JSON から特定のキーを取り出す指定はできない |
| AWS / Azure / GCP でシークレットのバージョンを固定できない | 常に最新を取得する。ただし GCP のみ、完全修飾リソース名でバージョンを指定できる |
| 上流チャートで ServiceAccount トークンを無効化できない | Pod 全体に効くため、同居する ExternalDNS 本体が動かなくなる。[README](../README.md) 参照 |
| NetworkPolicy は上流チャートに含まれない | 別途マニフェストとして適用する。[README](../README.md) 参照 |
| ExternalDNS の待ち受け時間は既定では足りない | 適用が DPF の反映完了まで待つため。[README](../README.md) 参照 |
