# Quickstart: アクセストークンの供給元をファイルに限る — 検証手順

**Feature**: [spec.md](./spec.md) | **Contract**: [contracts/configuration.md](./contracts/configuration.md)

実装が spec を満たすことを確かめる手順。すべてリポジトリルートで実行する。

## 前提

- Go 1.27、`golangci-lint` (CI と同じ版)
- 手順 5 のみ、実際の DPF の検証用ゾーンとそのトークンが必要 (CI の `e2e` で代替可)

## 1. 品質ゲートが通る

```bash
make all
```

**期待**: すべて成功する。とくに次が通ること。
- `lint`: `depguard` がシークレット管理サービスの SDK を import していないことを確かめる
- `license-deps`: 許容リスト外の依存がない
- `test`: `test/docs` の文書・実装の一致検査を含む

## 2. トークンはファイルからだけ取得される (US1)

```bash
go test ./internal/config/... ./internal/dpf/... -run 'Token|Load'
```

**期待**:
- `--dpf-token-file` だけで設定が成立する
- `--dpf-token-file` がなければ `ErrMissingRequired`
- ファイルの差し替えが次の取得に反映される
- 読めない・空のファイルは取得失敗になり、メッセージに内容が現れない

## 3. 廃止したフラグで起動しない (US2)

```bash
go build -o /tmp/webhook ./cmd/webhook

for f in --dpf-token-secret-manager=aws \
         --dpf-token-secret-id=x \
         --dpf-token-secret-endpoint=https://example.invalid/; do
  /tmp/webhook --dpf-token-file=/dev/null --domain-filter=example.jp "$f"
  echo "exit=$?"
done
```

**期待**: 3 回とも `exit` が非 0。出力にそれぞれのフラグ名が現れる。
`--dpf-token-file` を併せて指定していても失敗する。

```bash
/tmp/webhook --help 2>&1 | grep -c secret
```

**期待**: `0`。

## 4. シークレット管理サービスの依存がリンクされていない (FR-002 / FR-005 / FR-007)

```bash
go list -deps -f '{{with .Module}}{{.Path}}{{end}}' ./cmd/... | sort -u > /tmp/mods.txt
wc -l < /tmp/mods.txt
grep -E 'hashicorp|aws-sdk|azure-sdk|cloud.google.com/go/secretmanager|dpf-go/misc' /tmp/mods.txt
```

**期待**:
- モジュール数が基準値 **97** より少ない (SC-004)
- `grep` の出力が空

## 5. 実環境でのローテーション (SC-005)

CI の `e2e` ワークフローを実行する。トークンのローテーションの検証が成功すること。
本機能は e2e の入力を変えない (既にファイル経由である)。

## 6. 文書 (SC-006)

```bash
grep -n -i -E 'secret-manager|vault|secrets manager|key vault|secret manager' \
  README.md docs/reference.md
```

**期待**: 出現するのは次だけ。
- `docs/reference.md`「意図的に存在しないもの」の廃止フラグの行
- README の「外部のシークレット管理サービスを使う場合」の節 (Kubernetes 側でファイルに
  する案内)

README に `subPath` を使わない旨の注意があること。

README の推奨 values と NetworkPolicy の例が、DPF API・名前解決 (・OTLP の注記) 以外への
egress を含まないこと (FR-010)。
