# Phase 0 Research: ExternalDNS webhook provider 本体

**Feature**: [spec.md](./spec.md) | **Date**: 2026-09-04

本書は plan の前提となる技術判断を記録する。実測した項目は「検証」に方法と結果を記す。

---

## R1. ASLR (PIE) と scratch の両立

**Decision**: alpine ビルダー上で cgo を有効にし、外部リンカで static-pie を生成する。

```
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -buildmode=pie \
  -tags 'netgo osusergo' \
  -ldflags '-s -w -linkmode external -extldflags "-static-pie"' \
  -o /out/app .
```

**Rationale**: constitution v1.6.0 が ASLR 有効を MUST とし、v1.5.0 がベースイメージを
`scratch` に固定している。この 2 つは素直に組み合わせると両立しない。

**検証**: 実際にビルドして ELF を確認した。

| ビルド方法 | ELF Type | PT_INTERP | scratch |
|---|---|---|---|
| `CGO_ENABLED=0` (通常) | EXEC | なし | 動くが **ASLR 無効** |
| `CGO_ENABLED=0 -buildmode=pie` | DYN | **あり** (`/lib64/ld-linux-x86-64.so.2`) | **動かない** |
| 上記レシピ (cgo + static-pie) | DYN | なし | **動く** |

`CGO_ENABLED=0` との組み合わせでは内部リンカが動的 PIE を生成し、`scratch` には
動的リンカが存在しないため起動できない。外部リンカと `-static-pie` の指定が必須である。

**Alternatives considered**:

- `CGO_ENABLED=0 -buildmode=pie`: 上表のとおり scratch で起動できない
- distroless への変更: constitution v1.5.0 の改訂が必要。ASLR のためだけに
  ベースイメージの決定を覆す理由はない
- ASLR を諦める: constitution v1.6.0 の MUST に反する

**注意**: cgo が有効になるため、名前解決とユーザ参照を純 Go 実装に固定する
`netgo` / `osusergo` タグを必須とする。これがないと静的リンク環境で NSS の
動的読み込みに失敗し、名前解決が壊れる。

---

## R2. CA 証明書の scratch への同梱

**Decision**: ビルダーの `ca-certificates` パッケージから
`/etc/ssl/certs/ca-certificates.crt` を最終イメージへコピーする。

**Rationale**: constitution が「TLS 証明書検証が成立する状態でイメージを配布すること」を
MUST としている。`scratch` に証明書ストアはないため、イメージ自身が持つ必要がある。
Go の TLS スタックは `/etc/ssl/certs/ca-certificates.crt` を既定の探索先の 1 つとするため、
このパスに置けば追加の設定なしに検証が働く。

**検証**: R1 のバイナリを scratch イメージに入れ、`USER 65532:65532` で実行して
外部 HTTPS エンドポイントへの接続を確認した。

- DNS 解決: 成功
- TLS 接続: 成功 (HTTP 200)

これにより「静的 PIE」「scratch」「非 root 数値 UID」「名前解決」「TLS 検証」が
同時に成立することを実機で確認済み。

**Alternatives considered**:

- `golang.org/x/crypto` などで証明書をバイナリに埋め込む: 更新のたびに再ビルドが必要で、
  ビルダーのパッケージ更新に追随する方が運用が単純
- 証明書を実行時に Secret でマウント: 利用者に必須の手順を増やす

---

## R3. DPF の変更適用モデル

**Decision**: 公開中のレコード全件を読み、変更セットをメモリ上でマージし、
`PatchZoneAtomicChanges` (レコードの一括更新とゾーン反映) で 1 回の原子的操作として
適用する。戻り値 `*AsyncResponse` を `JobsAPI.SyncWait` で待ち合わせる。

```
GetRecordCurrents (公開中の全件)
  → 変更セットをマージ
  → 投入前ガード
  → PatchZoneAtomicChanges (overwrite_soa=false, overwrite_zone_apex_ns=false)
  → SyncWait
```

**Rationale**: DPF は編集を即時公開せず保留し、反映操作で確定する。
`/zones/{ZoneId}/atomic_changes` は「レコードの一括更新とゾーンの反映をアトミックに
処理します」と定義されており、更新と反映が 1 回で完結する。

この方式を採る決め手は 3 つある。

1. **他者の保留変更を巻き込まない。** 代替案 (下記) の `PatchZoneChanges` は
   「編集中レコードのゾーン反映」であり、ゾーン内の**全ての保留変更**を公開する。
   運用者が DPF コンソールで作業途中の未反映変更を持っていた場合、本 provider の適用が
   それを勝手に公開してしまう。`atomic_changes` の対象は渡した集合に限られる
2. **FR-012 (部分成功を成功としない) が原子性で構造的に保証される。** 保留状態が
   発生しないため、中断時に巻き戻す処理そのものが不要になる
3. **FR-029 (apex NS を変更しない) と SOA の保護を API 側が担保する。**
   `overwrite_soa` と `overwrite_zone_apex_ns` を**常に false** で送る。
   送った値が取り込まれないため、自前ロジックの正しさに依存しない

**`overwrite_*` フラグの意味 (実機検証で判明)**: これらは「`records` に載せた
SOA / apex NS の値を**取り込むか**」を決めるフラグであり、「`records` から
**省いてよいか**」ではない。`records` は**ゾーンの全レコード**でなければならず、
SOA と apex NS を含める (MUST)。

当初、既定 false を「投入対象から外す」と読み違えて実装した。検証ゾーンに対する
E2E で判明した。

```json
{"request_id":"80f978d9e2f347a68c3678647b979b1c",
 "error_details":[{"code":"soa_not_found","attribute":"records","target":"$.records"},
                  {"code":"apex_ns_not_found","attribute":"records","target":"$.records"}],
 "error_type":"ParameterError","error_message":"There are invalid parameters."}
```

含めるが、フラグが false であるため投入した値は反映されない。反映済みの値を
逐語コピーして送るので、いずれにしても変化しない。

この結果、apex NS の**作成・更新も受け付けない**。受け付けて適用されないのは、
FR-012 が禁じる「実行しなかった変更を成功として返す」ことにあたる。

FR-011 (反映完了前に成功を返さない) は `SyncWait` による待ち合わせで満たす。

**管理対象外レコードの保全**: 一括更新は渡さなかったレコードを削除するため、
管理対象外のレコード (`CAA` `TLSA` `DS` `SVCB` `HTTPS` `ANAME` および対象外ゾーンの
種別)、および SOA と apex NS も投入する集合に含めなければならない。読み取り型と書き込み型の設定可能項目は
一致しており、無損失で往復できる。

| `Record` (読み) | `OverwriteRecordsInner` (書き) |
|---|---|
| `Name` `Ttl` `Rrtype` `Rdata` `Description` `Labels` | すべて存在 |
| `Id` `State` `Operator` | なし (いずれもサーバ管理項目で設定不可) |

**管理対象外のレコードは本プロジェクトのドメインモデルを通さず、読み取った値を
逐語的にコピーして書き戻す (MUST)。** 変換を挟まないことで、変換バグが管理対象外の
レコードを壊す経路そのものを作らない。

**Alternatives considered**:

- **個別編集 + `PatchZoneChanges` による反映**: レコードを 1 件ずつ編集して保留状態にし、
  ゾーン単位で反映する方式。触れるのが変更対象のレコードだけで済み、送信量も小さい。
  しかし反映が**ゾーン内の全保留変更**を対象とするため、他者が残した未反映の変更を
  意図せず公開する。反映前に `GetRecordDiffs` を確認しても、確認と反映の間の競合は
  消えない。加えて、途中で失敗した場合に保留変更の破棄 (`DeleteZoneChanges`) が必要になり、
  FR-012 の成立が巻き戻し処理の正しさに依存する。採用しない
- 反映を待たずに成功を返す: FR-011 に反する

**この方式の弱点と対策**: 失敗時の影響範囲がゾーン全体に及ぶ。マージの誤りが
管理対象外のレコードを消しうる。次の 2 点で抑える。

1. 管理対象外レコードの逐語コピー (上記)
2. **投入前ガード**: マージ結果が、変更セットに含まれないレコードを削除する内容に
   なっていた場合、適用を中止して一時的な失敗とする。壊れ方を「消える」ではなく
   「適用されない」に閉じ込める (原則 IV の fail closed)

**確認済み (DPF の挙動)**:

- **1,000 件程度のレコードは 1 回の `atomic_changes` で投入できる。** SC-008 の規模で
  方式が成立する
- **`atomic_changes` は全て置き換えるため、ゾーンに存在した保留変更は消える。**
  公開はされず、破棄される

### 保留変更が破棄されることの帰結

これは代替案 (個別編集 + `PatchZoneChanges`) との比較を変える。

| | 他者の保留変更に対する影響 |
|---|---|
| 代替案 (個別編集 + 反映) | **公開される。** 作業途中の未レビューの変更が権威 DNS に出る |
| 本方式 (`atomic_changes`) | **破棄される。** 公開はされないが、他者の編集作業が失われる |

**本方式の方が望ましい。** DNS の正しさに直接影響するのは公開済みの内容であり、
未完成の変更が意図せず公開される方が、下書きが失われるより危険である。
ただし他者の作業が失われること自体は実害であり、利用前提条件で扱う
(spec の PC-001〜PC-003)。

### 保留変更を検出したときの動作

**Decision**: 保留変更の有無を調べない。適用は常に続行する。

**Rationale**: 検出には追加の API 呼び出しが要る。マージの土台に使う
`GetRecordCurrents` は**反映済みのレコードしか返さない**ため、他者の保留変更は
そもそも見えない。見るには `GetRecordList` (編集中を含む一覧) を別途呼ぶ必要がある。

そこまでして得られるのは「破棄が起きた」というログだけであり、その記録は
**DPF サービス側のログに残る**。本 provider が同じ事実を二重に記録する価値はなく、
適用のたびに 1 回多く API を呼ぶ分、レート制限を余計に消費する。

中止する設計を採らない理由は別にある。誰かが下書きを残したまま放置すると
**本 provider が恒久的に停止**し、DNS があるべき状態からずれ続ける。
無人で動作する前提の仕組みとして受け入れられない。

破棄されうることは利用前提条件 (PC-004) として利用者に伝え、記録の参照先は
DPF サービス側のログとする。

**Alternatives considered**:

- 保留変更があれば適用を中止する: 下書きの放置で provider が停止する。採用しない
- `GetRecordList` で保留変更を検出してログに残す: DPF 側のログと重複し、
  適用のたびに API 呼び出しが 1 回増える。採用しない
- 保留変更を読み取って投入内容に含める: 未レビューの変更を公開することになり、
  代替案の欠点をそのまま持ち込む。採用しない

### マージの土台に用いる一覧

**Decision**: `GetRecordCurrents` (DNS 反映済レコードの一覧) を用いる。
`GetRecordList` (編集中を含む一覧) は用いない。

**Rationale**: 一括更新の結果はそのまま公開される。したがって土台とすべきは
公開中の状態である。編集中の一覧を土台にすると、他者の未レビューの編集を
取り込んで公開してしまう。

---

## R4. 同時実行の制御

**Decision**: ゾーン単位の変更適用を `utils.Mutex` (`NewMutex` / `LockWait` / `Unlock`) で
直列化する。**ロックの範囲は変更適用ハンドラの内側に閉じる。**

```
POST /records ハンドラ
  ├─ ロック取得
  ├─ GetRecordCurrents   ← マージの土台をここで読み直す
  ├─ マージ・ガード
  ├─ PatchZoneAtomicChanges + SyncWait
  └─ ロック解放
```

**Rationale**: ロックが守るのは「マージの土台となる読み取り」と「書き込み」の間だけである。
この 2 つの間に他者がゾーンを変更すると、その変更が古い読み取り結果で上書きされて失われる
(lost update)。区間が 1 つのハンドラ内に収まるため、ロックの保持時間は短い。

**webhook の `GET /records` はロックの対象外とする。** ExternalDNS の
`GET /records` と `POST /records` は独立した HTTP 要求であり、`GET` の後に `POST` が
来る保証はない。両者をまたいでロックを保持すると、`POST` が来ないままロックが残り、
ゾーンが操作不能になる。したがって `GET /records` は素の読み取りとし、
適用時に改めて `GetRecordCurrents` で読み直す。

**この設計が守らないもの**:

1. ExternalDNS が `GET /records` で得た snapshot は、`POST /records` が届く時点では
   古くなっている可能性がある。変更セットはその古い snapshot から算出されている。
   これはロックでは防げず、防ぐ必要もない。ExternalDNS はあるべき状態を宣言する
   仕組みであり、ずれは次の周回で再計算される
2. **ロックを取らない変更者の変更は守れない。** ロックは同じ排他機構に参加する者の間で
   しか効かない。適用の読み取りと書き戻しの区間に、ロックを取らない他者の変更が入ると、
   その変更は書き戻しによって取り消される。本 provider はこれを検出しない

2 に対しては、技術的な対策ではなく**利用前提条件**で対処する
(spec の PC-001〜PC-003)。同一ゾーンを機械的に変更する他の仕組みは、同じロックを
取得するか、あるべき状態へ継続的に収束する動作 (reconcile) を持つことを求める。
本 provider 自身は後者を満たす。ExternalDNS が周期的に再計算するため、
取り消されても再適用される。

`dpf-go` はゾーンの SOA レコードのラベルを用いた排他制御を提供しており、DPF 側の状態のみで
排他が成立する。ExternalDNS が複数レプリカで動く場合や、運用者が同時に DPF コンソールを
操作する場合に効く。`WithTTL` によりロックの有効期限を設定し、異常終了時にロックが
残り続けないようにする。

**Alternatives considered**:

- **`GET /records` から `POST /records` までロックを保持**: 両者は別々の HTTP 要求であり、
  `POST` が来る保証がない。ロックが残留してゾーンが操作不能になる。採用しない
- プロセス内ロックのみ: 同一 Pod 内でしか効かず、複数レプリカで破綻する
- ロックなし: 適用中の読み取りと書き込みの間で lost update が起き、他者の変更を
  黙って取り消す
- 書き込み直前のロック取得: 適用内の読み取りとの間の競合を防げず、ロックの意味がない

**古い変更セットの扱い**: 変更セットに含まれる「更新前の値」が、適用時点の現在値と
一致しない場合がある (`GET` と `POST` の間に他者が変更した場合)。管理対象のレコードに
ついては、変更セットが指定する「更新後の値」を適用する。ExternalDNS が管理する
レコードの権威は ExternalDNS 側にあり、ずれは次の周回で収束するため。
一致しないことを理由に適用を拒否すると、差分が解消しないまま振動する (SC-007)。

---

## R5. アクセストークンの供給

**Decision**: `utils.WithTokenFile(path)` (ファイル) と `utils.WithTokenProvider(p)`
(シークレット管理サービス) の 2 経路のみを設定として公開する。
`misc/vault`, `misc/aws`, `misc/azure`, `misc/gcp` の `NewTokenProvider` が返す関数を
`WithTokenProvider` に渡す。

**Rationale**: constitution v1.8.0 がこの 2 経路への限定を MUST としている。
`dpf-go` の `TokenProvider` は**リクエストごとに評価される**ため、FR-037
(再起動なしのローテーション追随) はライブラリの仕組みでそのまま満たせる。
`TokenFromFile` は毎回ファイルを読むため、Secret のマウント内容が更新されれば
次のリクエストから新しいトークンが使われる。

呼び出し頻度を抑えたい場合は `WithTokenTTL` でキャッシュ期間を設定できる。
シークレット管理サービス利用時のみ設定可能とし、ファイル読み取りには既定で用いない
(ファイル読み取りは十分に安価であり、ローテーション反映を遅らせる理由がない)。

**採用しない経路**: `utils.WithToken(string)` (リテラル) と環境変数 `DPF_API_TOKEN`
(`utils.NewClient()` の既定動作) は、constitution v1.8.0 が MUST NOT としているため
設定として公開しない。

**エラーの扱い**: `TokenError` はライブラリ側でリトライされない設計であり、
FR-038 (取得失敗は恒久的失敗) と一致する。`TokenFromFile` は読み取り失敗時に
ファイル内容をエラーへ含めないため、FR-039 も満たす。

---

## R6. 対応レコード種別の対応関係

**Decision**: spec の 8 種別を `dpf.RecordsRrtype` へ静的に対応付ける許可リストを持つ。
**交差にある `NS` は、交差にありながら意図的に除外する。**

`dpf-go` が定義する種別は 16 個 (A, AAAA, ANAME, CAA, CNAME, DS, HTTPS, MX, NAPTR,
NS, PTR, SOA, SRV, SVCB, TLSA)。ExternalDNS の `KnownRecordTypes` は 10 個
(A, AAAA, CNAME, TXT, SRV, NS, PTR, MX, NAPTR, DNAME)。

- 交差 = 9 種別。うち `A` `AAAA` `CNAME` `TXT` `SRV` `PTR` `MX` `NAPTR` の 8 種別
  → 対応する
- 交差にあるが除外 = `NS` → 読み飛ばす。変更しない (FR-029、R12)
- DPF のみ = `ANAME` `CAA` `DS` `HTTPS` `SOA` `SVCB` `TLSA` → 読み飛ばす。変更しない
- ExternalDNS のみ = `DNAME` → DPF に対応する種別がなく、要求されても適用しない

**Rationale**: spec FR-026・FR-027 の裏付け。ライブラリの列挙値を実際に確認した結果、
spec が manual から導いた範囲と一致した。`NS` の除外理由は R12 に記す。

**初版からの変更 (2026-09-15)**: 初版は交差の 9 種別すべてを対応とし、`NS` については
「ゾーン apex のものだけを変更禁止」とする方針だった。この方針は親子ゾーンが併存する
環境で成立しないことが判明したため、`NS` を種別ごと除外する方針へ改めた (R12)。

---

## R7. ドメイン名の表現

**Decision**: 内部表現は `dns.CanonicalName` の出力 (小文字・末尾ドット) に固定し、
専用の型で保持する。境界でのみ変換する。

| 境界 | 方向 | 変換 |
|---|---|---|
| ExternalDNS からの受信 | 入 | `dns.CanonicalName` を適用 |
| ExternalDNS への応答 | 出 | 正規化名をそのまま返す |
| DPF からの受信 | 入 | `dns.CanonicalName` を適用 |
| DPF への送信 | 出 | 正規化名をそのまま渡す (DPF は正規化状態で扱う) |

**全ての境界で正規化する。** 受け取った名前は表現を問わず `dns.CanonicalName` に通し、
返す名前は正規化名のまま出す。境界ごとに異なる表現規則を持たせない。

**Rationale**: constitution v1.4.0 が「正規化済みか否かが呼び出し側に依存する関数を
作らない (MUST NOT)」「正規化状態は型または境界で保証する (MUST)」と定めている。
型で保証する方が、境界の実装漏れを型検査で検出できる。

**確認済み**: レコード一覧の応答は正規化名のまま返してよい。適用要求で受け取る名前の
表現は上流の実装依存で確定していないが、**いずれにせよ受信時に正規化する**ため、
本 provider の動作は表現に依存しない。

この結果、境界ごとの変換規則は不要になった。「受信したら正規化する」「保持するのは
正規化名だけ」「出すときも正規化名」の 3 つで閉じる。constitution v1.4.0 が求める
「正規化状態は型または境界で保証する」を、最も単純な形で満たせる。

契約テストでは、末尾ドットの有無や大文字小文字が異なる入力を与えても、同一の
レコードとして扱われることを固定する。

---

## R8. テレメトリの構成

**Decision**: OpenTelemetry の計測器を単一の組として定義し、**メトリクスは 2 つの
リーダーを接続する**。Prometheus 形式は pull 用のリーダー、OTLP は push 用のリーダーとする。

**Rationale**: 原則 V が「両形式が同一の計測値を表すこと (MUST)。形式ごとに異なる意味の
値を持たせないこと (MUST NOT)」を求めている。計測器を 1 組にしてリーダーを 2 つ付ければ、
この性質は構成上自動的に満たされる。Prometheus 用と OTLP 用に別々の計測コードを書くと、
両者が乖離しうる。

ログとトレースも OpenTelemetry の SDK に載せる。標準出力への構造化ログは
OTLP 送出の設定と独立に常時有効とする (原則 V)。

`dpf-go` は OpenTelemetry に対応しているため、DPF 呼び出しは同一トレースに接続できる。
これにより FR-022 (受信から DPF 呼び出しまでを 1 つの流れとして追跡) を満たす。

**既定値**: OTLP の送出先が未設定なら送出しない (原則 VI)。

---

## R9. テストの構成

**Decision**: 3 層に分ける。

| 層 | 対象 | 外部依存 |
|---|---|---|
| 契約テスト | webhook API の要求・応答の形 | なし |
| 統合テスト | provider ロジック + DPF クライアント層 | DPF API をモック |
| 単体テスト | 名前の正規化・範囲判定・種別変換・調整 | なし |

**Rationale**: `dpf-go` は API を `dpf.RecordsApi` / `dpf.ZonesApi` などの
インタフェースとして公開しており (`interfaces.go`)、`utils` の関数もこれらを
引数に取る。実 API に到達せずに上位層を検証できるため、原則 II が求める
「上位層のテストが実際の DPF API に到達しないこと (MUST)」を満たせる。

原則 III により、いずれの層もテストを先に書く。

---

## R10. 非公開モジュールへの依存

**解消済み (2026-09-14)**: `github.com/iij/dpf-go` は公開された。`GOPRIVATE` と
CI の認証設定はリポジトリから削除済みである。以下は公開前の判断の記録。

**Decision**: 開発期間中は `GOPRIVATE=github.com/iij/dpf-go` を設定し、CI では
認証付きでモジュールを取得する。`dpf-go` 公開後にこの設定を外す。

**Rationale**: `github.com/iij/dpf-go` は本機能の完成後に公開される予定であり、
現時点では非公開である。`go.mod` に固定バージョンで記載する点は公開前後で変わらない。

**注意**: 本リポジトリを `dpf-go` より先に公開すると、外部からビルドできない状態になる。
公開順序は `dpf-go` を先とする。

---

## 解決済みの NEEDS CLARIFICATION

Phase 0 開始時点で spec に未解決の項目はなかった。plan 作成中に生じた技術的な
不確定要素は R1〜R10 ですべて解決した。**実装前に確認を要する残件はない。**

当初 R3 と R7 に残していた 2 点は、DPF の挙動が確認できたため解消した。

| 項目 | 結果 |
|---|---|
| 1,000 件規模の一括投入 | 可能。SC-008 の規模で方式が成立する |
| 保留変更が存在する状態での一括更新 | 保留変更は破棄される。公開はされない |
| ExternalDNS への応答時の名前表現 | 正規化名のまま返してよい。境界ごとの変換は不要 |

---

## R11. 上流 external-dns Helm チャートとの対応 (T098)

**Decision**: デプロイは上流の external-dns Helm チャートを経路とする
(constitution v2.0.0)。独自のチャートは維持しない。

**調査結果**: チャートの `provider.webhook` および Pod 全体の設定が、
本サービスの設計とそのまま噛み合う。

| 事項 | チャートの扱い | 本サービス |
|---|---|---|
| webhook の provider API | external-dns 本体が `http://localhost:8888` を既定で参照 | provider リスナー既定 `127.0.0.1:8888` |
| webhook の公開ポート | `containerPort: 8080` (`http-webhook`、固定) | exposed リスナー既定 `:8080` |
| probe | `/healthz` を `http-webhook` に対して実行 | `/healthz` を exposed で提供 |
| メトリクスの収集 | `provider.webhook.serviceMonitor` | `/metrics` を exposed で提供 |

**チャートの appVersion は本サービスの前提より古い。** チャート 1.21.1 の
appVersion は 0.21.0 であり、`image.tag` を省くとそれが使われる。本サービスは
webhook provider API v0.22.0 を前提とするため、`image.tag` を明示する必要がある。

**ポートが一致するのは偶然ではない。** 両者とも上流のチュートリアルが示す
既定値 (provider `8888` / exposed `8080`) に従っているためである
(constitution v1.1.0)。

### 既定拒否の要件を values で満たせるか

| 要件 | 指定手段 | 可否 |
|---|---|---|
| 非 root、`seccompProfile` | Pod 全体の `podSecurityContext` (既定で `runAsNonRoot: true`) | 可 |
| `readOnlyRootFilesystem`、`capabilities.drop`、`allowPrivilegeEscalation: false` | `provider.webhook.securityContext` | 可 |
| `automountServiceAccountToken: false` | Pod 全体の同名の値 (**既定は `true`**) | **不可**。下記参照 |
| resources の requests / limits | `provider.webhook.resources` | 可 |
| トークンの Secret マウント | `extraVolumes` + `provider.webhook.extraVolumeMounts` | 可 |
| **NetworkPolicy** | **チャートにテンプレートがない** | **不可** |

**ServiceAccount トークンの無効化はチャートで賄えない。** `deployment.yaml` は
`automountServiceAccountToken` を Pod spec に置く。同じ Pod には ExternalDNS 本体が
同居し、そちらは Ingress と Service の監視のために API サーバへの認証を必要とする。
`false` にすると ExternalDNS が何も検出しなくなる。`serviceAccount` 配下の同名の値も
Pod 全体に効くため、逃げ道はない。

サイドカーだけトークンを渡さない指定はテンプレートに存在しない。本サービスの
コンテナにもトークンがマウントされる。緩和は ExternalDNS の RBAC を読み取りに
絞ること (チャートの既定) と、NetworkPolicy で egress を DPF API に限ることの 2 つ。

当初これを「可」と記録し、README の推奨 values に `false` を載せていた。Pod 全体に
効くことを見落としていた。上流のテンプレートを読んで訂正した。

**NetworkPolicy はチャートで賄えない。** テンプレート一覧に存在しないため、
利用者が別途マニフェストとして適用する必要がある。constitution が求める
既定拒否の egress 制限は、チャートの values では表現できない。

この事実を README に記載し、必要なマニフェストの例を示す。書けない設定を
推奨として載せることはできないが、要件そのものは消えない。

**注意**: webhook サイドカーのポートは `containerPort: 8080` として
テンプレートに直書きされており、values で変更できない。本サービスの
`--exposed-addr` を既定から変えると、チャートの probe と serviceMonitor が
届かなくなる。

---

## R12. ゾーンの帰属と `NS` の除外

**Decision**: 名前がどのゾーンに属するかを決める規則を **1 つだけ**定義し、読み取りと
書き込みの双方がそれを使う。あわせて `NS` を対応レコード種別から除外する。

### 問題

DPF は `example.jp` と `sub.example.jp` を、それぞれ独立したゾーンとして保持できる。
親子・孫の関係にあるゾーンが併存する環境で、次の 2 つが問題になる。

**(1) ゾーンカットの両側にある `NS`。** 委任の `NS` は親ゾーンに、同じ名前の apex `NS` は
子ゾーンに置かれる。名前も種別も同じで、意味だけが違う。ExternalDNS が webhook API で
渡すのは名前と種別だけであり、どちら側かを指す手段がない。

**(2) 読み取りと書き込みで帰属がずれる。** 読み取りはゾーンごとにレコードを集めるが、
書き込みは名前から最長一致でゾーンを選ぶ。親ゾーン側に子ゾーン配下の名前のレコードが
残っていると、読み取りではそれが返り、その削除要求は書き込みで子ゾーンへ振られる。
**親側の残骸を見た削除が、子側の権威レコードを消す。**

### 決定 1: `NS` を対応レコード種別から外す

(1) は webhook API の表現力の問題であり、provider 側の実装では解けない。したがって
`NS` を許可リストから外し、読み取りでも変更でも対象としない。

- ExternalDNS が Ingress や Service から算出するレコードに `NS` は現れないため、
  失うものがない。ゾーンの委任は運用者が DPF 上で直接管理する
- 除外により、apex `NS` を名指しで止める検証 (`validateApexNSModification`) が
  不要になる。許可リストの判定だけで同じ結果になる
- DPF 層への影響はない。一括更新の土台に使う一覧は種別で絞らない生のレコードであり
  (R3)、SOA と apex `NS` は従来どおり逐語コピーで投入集合に含まれる

**Alternatives considered**:

- *レコードにゾーンを持たせて区別する*: 却下。ExternalDNS 側にゾーンを表す場がなく、
  応答に載せれば原則 I (独自フィールドを追加しない) に反する
- *親側の委任 `NS` だけを除外し、子側の apex `NS` は扱う*: 却下。apex `NS` は
  `overwrite_zone_apex_ns` が常に false のため投入しても取り込まれず、受け付けて
  適用しないことは FR-012 に反する。結局どちらも扱えない
- *apex 判定を維持したまま親子併存を許す*: 却下。これが初版の方針であり、子ゾーンが
  DPF 上に存在するかどうかで同じ要求の成否が変わる。利用者から見て挙動が予測できない

### 決定 2: 帰属の規則を 1 箇所に置く

(2) は provider 側で解ける。名前からゾーンを選ぶ規則を `internal/provider` に 1 つだけ
置き、読み取りと書き込みの双方がそれを呼ぶ。

| 用途 | 使い方 |
|---|---|
| 書き込み | 名前の帰属先ゾーンを適用先とする (FR-040) |
| 読み取り | 読み取り元ゾーンが帰属先と一致するレコードだけを返す (FR-044) |

規則が 1 つしかないため、両者がずれることが構造上起こらない。2 箇所に書けば、片方だけ
直した変更でずれが復活する。

**索引の材料は `ListZones` の全件**とする。管理対象範囲 (domain filter) で絞ったものを
使わない。遮蔽するかどうかは「DPF がそこにゾーンを持っているか」で決まり、管理対象で
あるかとは別の問いである。

**Rationale**: 原則 IV は範囲判定を書き込み直前に行うことを求めるが、読み取り側の帰属が
それとずれていると、ExternalDNS が誤った差分を算出する経路が残る。規則の単一化は、
この対称性をコードの注意ではなく構造で保証する。

---

## R13. DPF は TXT の RDATA に引用符を要求する

**Decision**: DPF へ送る `TXT` の値は、常に引用符付きの表現形式にする。

**どう分かったか**: 実環境での検証 (constitution v2.1.0) を初めて実行したとき、
`TestTXTRoundTrip/e2e-txt-spf` が `POST /records = 400` で落ちた。値は
`v=spf1 -all` (引用符なし、空白入り)。引用符付きの 2 ケース (255 超の自動分割、
複数 character-string) は通っていた。

**モックでは分からなかった。** 本サービス側の検証も正規化も正しく、
`NormalizeTXT("v=spf1 -all")` は値を書き換えず、`SplitTXT` は 1 個として扱う。
FR-032b の意味では正しい。拒否したのは DPF である。DPF の API スキーマは
`value` に形式の制約を宣言しておらず、実際に投げるまで現れない種類の制約だった。

**Rationale**: 引用符は表現形式の話であり、character-string の境界を変えない。
`v=spf1 -all` は `"v=spf1 -all"` となり、1 個のまま保たれる。FR-032b が禁じる
「空白での分割」には当たらない。

`Adjust` (FR-014) が同じ `NormalizeTXT` を通るため、ExternalDNS には保存される
のと同じ引用符付きの値が返る。両者が食い違うと同じ差分が検出され続けるため、
この経路が繋がっていることが重要である (SC-007)。

**Alternatives considered**:

- *引用符のない値を恒久的な失敗として拒否する*: 却下。`v=spf1 -all` は
  ExternalDNS が普通に渡してくる形であり、拒否すると SPF レコードを管理できない
- *dpf 層だけで引用符を付ける*: 却下。`Adjust` が返す値と保存される値が
  食い違い、差分が振動する (SC-007)。境界の片側だけを直しても閉じない

**残った課題 (未修正)**: この失敗は `POST /records = 400: Bad Request` としか
CI ログに出なかった。**DPF の応答全文は `f.logs` に取り込まれているのに、
失敗時に出力されない。** constitution v2.2.0 が全文記録を MUST とした理由
(`request_id` を失わない) が、テスト側の取りこぼしで無に帰している。
原因の特定は、送信形と通った 2 ケースとの差分からの推論に頼った。
`test/e2e` の失敗時にログを吐かせる改善が要る。
