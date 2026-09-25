// SPDX-License-Identifier: Apache-2.0

package docs

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/telemetry"
)

// referencePath は検査する文書。
const referencePath = "../../docs/reference.md"

// expectedMarkers は文書に存在しなければならない印の一覧。
//
// **この一覧は検査側が持つ。文書側に持たせない。** 文書を書き換えるだけで
// 検査を緩められる状態にしないため。検査する組を減らすには、この一覧を
// 変更する必要があり、差分としてレビューに現れる。
var expectedMarkers = []string{
	"flags",
	"secret-managers",
	"metrics",
	"dpf-operations",
	"record-types",
	"endpoints-provider",
	"endpoints-exposed",
}

func readReference(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("%s を読めません: %v", referencePath, err)
	}
	return string(b)
}

// column は行の集合から指定した列を取り出す。
func column(rows [][]string, i int) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if i < len(r) {
			out = append(out, r[i])
		}
	}
	return out
}

// normalize は重複を除いて並べ替える。突き合わせは集合として行う。
func normalize(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return slices.Compact(out)
}

// assertSameSet は文書側と実装側が集合として一致することを確かめる。
//
// **どちらにだけ存在する値かを報告する。** 「一致しません」だけでは、
// 文書と実装のどちらを直せばよいか判断できない。
func assertSameSet(t *testing.T, what string, doc, impl []string) {
	t.Helper()

	d, i := normalize(doc), normalize(impl)
	if slices.Equal(d, i) {
		return
	}

	var onlyDoc, onlyImpl []string
	for _, v := range d {
		if !slices.Contains(i, v) {
			onlyDoc = append(onlyDoc, v)
		}
	}
	for _, v := range i {
		if !slices.Contains(d, v) {
			onlyImpl = append(onlyImpl, v)
		}
	}

	if len(onlyImpl) > 0 {
		t.Errorf("%s: 実装にあって文書にない: %v\n  → docs/reference.md に追記してください", what, onlyImpl)
	}
	if len(onlyDoc) > 0 {
		t.Errorf("%s: 文書にあって実装にない: %v\n  → docs/reference.md から削除するか、実装を確認してください", what, onlyDoc)
	}
}

// docColumn は印の付いた表から指定した列を取り出す。
func docColumn(t *testing.T, doc, marker string, col int) []string {
	t.Helper()

	rows, err := table(doc, marker)
	if err != nil {
		t.Fatalf("%s", err)
	}
	return column(rows, col)
}

// 期待する印がすべて存在し、未知の印がないことを、突き合わせより先に確かめる。
//
// **印が欠けていたら失敗させる。** 「対象がないので何も検査しない」で通すと、
// 印を消すだけで検査を無効化でき、表ごと消しても気付けない (原則 VI)。
// 綴りの誤りで静かに対象から外れることも防ぐ。
func TestMarkers_AllPresentAndKnown(t *testing.T) {
	t.Parallel()

	doc := readReference(t)
	found := markers(doc)

	for _, want := range expectedMarkers {
		if !slices.Contains(found, want) {
			t.Errorf("印 reference:%s が %s にありません", want, referencePath)
		}
	}
	for _, got := range found {
		if !slices.Contains(expectedMarkers, got) {
			t.Errorf("未知の印 reference:%s があります。綴りの誤りか、検査側の一覧への追加漏れです", got)
		}
	}

	// 印があっても表が読めなければ意味がない。すべての印について
	// 行が取り出せることを確かめる。
	for _, name := range expectedMarkers {
		if _, err := table(doc, name); err != nil {
			t.Errorf("%s", err)
		}
	}
}

// 設定項目の名前と既定値が文書と一致すること。
func TestReference_Flags(t *testing.T) {
	t.Parallel()

	doc := readReference(t)
	rows, err := table(doc, "flags")
	if err != nil {
		t.Fatalf("%s", err)
	}

	docNames := make([]string, 0, len(rows))
	docDefaults := make(map[string]string, len(rows))
	for _, r := range rows {
		name := strings.TrimPrefix(r[0], "--")
		docNames = append(docNames, name)
		if len(r) > 1 {
			docDefaults[name] = r[1]
		}
	}

	impl := parseUsage(config.Usage())

	implNames := make([]string, 0, len(impl))
	for name := range impl {
		implNames = append(implNames, name)
	}
	assertSameSet(t, "設定項目", docNames, implNames)

	for name, want := range impl {
		got, ok := docDefaults[name]
		if !ok {
			continue // 名前の不一致は上で報告済み
		}
		if got != want {
			t.Errorf("--%s の既定値: 文書 %q, 実装 %q", name, got, want)
		}
	}
}

// flagLine は `  -name type` の形の行。type は bool では現れない。
var flagLine = regexp.MustCompile(`^\s+-([a-z0-9-]+)(?:\s+(\S+))?\s*$`)

// defaultLine は説明行の末尾に現れる `(default X)`。
var defaultLine = regexp.MustCompile(`\(default (.+)\)\s*$`)

// parseUsage は Usage の出力から設定項目の名前と既定値を取り出す。
//
// flag パッケージは既定値が型のゼロ値のとき `(default ...)` を出力しない。
// そのため型から補う。型の語がない項目は bool であり、ゼロ値は false である。
// 文字列とその他はゼロ値を「(なし)」と表記する。文書の書き方に合わせている。
func parseUsage(usage string) map[string]string {
	out := map[string]string{}
	current := ""

	for _, line := range strings.Split(usage, "\n") {
		if m := flagLine.FindStringSubmatch(line); m != nil {
			current = m[1]
			if m[2] == "" {
				out[current] = "false" // 型の語がない = bool
			} else {
				out[current] = "(なし)"
			}
			continue
		}
		if current == "" {
			continue
		}
		if m := defaultLine.FindStringSubmatch(line); m != nil {
			out[current] = strings.Trim(strings.TrimSpace(m[1]), `"`)
			current = ""
		}
	}
	return out
}

// シークレット管理サービスの名前が文書と一致すること。
func TestReference_SecretManagers(t *testing.T) {
	t.Parallel()

	doc := readReference(t)
	docNames := docColumn(t, doc, "secret-managers", 0)

	impl, err := sliceVarStrings("../../internal/config", "supportedSecretManagers")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(impl) == 0 {
		t.Fatal("supportedSecretManagers が見つかりません。変数名が変わった可能性があります")
	}
	assertSameSet(t, "シークレット管理サービス", docNames, impl)

	// 名前が一致するだけでなく、実際に受理されることを確かめる。
	// 一覧が使われていない状態を検出する。
	for _, name := range docNames {
		args := []string{
			"--domain-filter=example.jp",
			"--dpf-token-secret-manager=" + name,
			"--dpf-token-secret-id=dummy",
		}
		if name == "azure" {
			args = append(args, "--dpf-token-secret-endpoint=https://example.vault.azure.net/")
		}
		if _, err := config.Load(args); err != nil {
			t.Errorf("--dpf-token-secret-manager=%s が受理されません: %v", name, err)
		}
	}

	// 実在しない名前は拒否されること。
	if _, err := config.Load([]string{
		"--domain-filter=example.jp",
		"--dpf-token-secret-manager=nonexistent",
		"--dpf-token-secret-id=dummy",
	}); err == nil {
		t.Error("実在しないシークレット管理サービスが受理されました")
	}
}

// 出力に現れる計測値の系列名が文書と一致すること。
//
// 実装側の値は**宣言された計測器から導く。** 実際の出力だけを見ると、
// まだ記録が発生していない計測器を拾えない。計測器を足しただけで記録を
// 書いていない段階では出力に現れず、文書の更新漏れを見逃す (SC-008)。
//
// 導出した系列が実際の出力と食い違っていないことは、別途 [TestMetrics_DerivationMatchesOutput]
// が確かめる。導出の規則が正しいことをそちらで担保する。
func TestReference_Metrics(t *testing.T) {
	t.Parallel()

	doc := readReference(t)
	docNames := docColumn(t, doc, "metrics", 0)

	assertSameSet(t, "計測値の系列", docNames, derivedSeries(t))
}

// 導出した系列名が、実際の出力に現れる系列名を覆っていること。
//
// 計測器の種別から出力名を導く規則 (Counter は _total、Histogram は
// _bucket / _sum / _count) は Prometheus 形式の慣習であり、こちらの
// コードにはない。**毎回の実行で実際の出力と突き合わせて確かめる。**
// 規則が変われば、ここが落ちる。
func TestMetrics_DerivationMatchesOutput(t *testing.T) {
	t.Parallel()

	derived := derivedSeries(t)
	for _, s := range scrapeSeries(t) {
		if !slices.Contains(derived, s) {
			t.Errorf("出力に現れた %s が導出結果に含まれません。導出の規則を見直してください", s)
		}
	}
}

// instrumentKinds は計測器を作る関数の名前と、出力に現れる系列の接尾辞の対応。
//
// **未知の関数が現れたら失敗する。** 対応を書き足さないまま新しい種別の
// 計測器を足すと、その系列が検査から静かに漏れる。
var instrumentKinds = map[string][]string{
	"Int64Counter":   {"_total"},
	"Float64Counter": {"_total"},
	"Int64Histogram": {"_bucket", "_sum", "_count"},
	"Float64Histogram": {
		"_bucket", "_sum", "_count",
	},
}

// instrumentPattern は計測器を作る関数の名前の形。
var instrumentPattern = regexp.MustCompile(`^(Int64|Float64)(Counter|UpDownCounter|Histogram|Gauge)$`)

// derivedSeries は宣言された計測器から、出力に現れる系列名を導く。
func derivedSeries(t *testing.T) []string {
	t.Helper()

	const dir = "../../internal/telemetry"

	// 未知の種別の計測器が足されていないかを先に確かめる。
	used, err := calleeNames(dir, instrumentPattern)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(used) == 0 {
		t.Fatal("計測器の宣言が見つかりません。宣言の書き方が変わった可能性があります")
	}
	for _, name := range used {
		if _, ok := instrumentKinds[name]; !ok {
			t.Fatalf("未知の計測器の種別 %s があります。instrumentKinds に接尾辞の対応を足してください", name)
		}
	}

	var out []string
	for fn, suffixes := range instrumentKinds {
		names, err := callStringArgs(dir, fn, 0)
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, n := range names {
			for _, suffix := range suffixes {
				// 計測器の名前に既に接尾辞が含まれている場合、公開形式は
				// 重ねて付けない。本サービスの Counter は名前を
				// dns_record_changes_total のように定義している。
				out = append(out, strings.TrimSuffix(n, suffix)+suffix)
			}
		}
	}
	return out
}

// scrapeSeries は各計測器に 1 件ずつ記録してから収集し、現れた系列名を返す。
//
// 記録してから収集するのは、未記録の系列が出力に現れないためである。
// 記録しなければ、実装にある計測値が「文書にしかない」と誤って報告される。
func scrapeSeries(t *testing.T) []string {
	t.Helper()

	ctx := t.Context()
	tel, err := telemetry.New(ctx, config.Telemetry{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("テレメトリの初期化に失敗: %v", err)
	}
	t.Cleanup(func() {
		//nolint:errcheck,gosec // 後始末
		tel.Shutdown(context.Background())
	})

	m := tel.Metrics()
	m.RecordChanges(ctx, telemetry.OpCreate, true, 1)
	m.RecordDPFCall(ctx, "list_zones", true, 0.1)
	m.RecordApplyFailure(ctx)

	rec := httptest.NewRecorder()
	tel.MetricsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("計測値の読み取りに失敗: %v", err)
	}

	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, _ := strings.Cut(line, "{")
		name, _, _ = strings.Cut(name, " ")
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// operation ラベルの値が文書と一致すること。
func TestReference_DPFOperations(t *testing.T) {
	t.Parallel()

	doc := readReference(t)
	docNames := docColumn(t, doc, "dpf-operations", 0)

	impl, err := callStringArgs("../../internal/dpf", "observe", 1)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(impl) == 0 {
		t.Fatal("observe の呼び出しが見つかりません。関数名が変わった可能性があります")
	}
	assertSameSet(t, "operation ラベルの値", docNames, impl)
}

// 対応レコード種別が文書と一致すること。
func TestReference_RecordTypes(t *testing.T) {
	t.Parallel()

	doc := readReference(t)
	docNames := docColumn(t, doc, "record-types", 0)

	impl := make([]string, 0)
	for _, rt := range provider.SupportedRecordTypes() {
		impl = append(impl, rt.String())
	}
	assertSameSet(t, "レコード種別", docNames, impl)
}

// 公開する経路が文書と一致すること。
//
// これにより原則 I「独自のエンドポイントを追加しない」が機械的に守られる。
// **方法 (メソッド) は検査しない。** 登録の形と表の書き方が一致しないため、
// 経路の文字列のみを見る。
func TestReference_Endpoints(t *testing.T) {
	t.Parallel()

	doc := readReference(t)

	docPaths := slices.Concat(
		docColumn(t, doc, "endpoints-provider", 1),
		docColumn(t, doc, "endpoints-exposed", 1),
	)

	var impl []string
	for _, dir := range []string{"../../internal/webhook", "../../internal/server"} {
		for _, fn := range []string{"HandleFunc", "Handle"} {
			got, err := callStringArgs(dir, fn, 0)
			if err != nil {
				t.Fatalf("%v", err)
			}
			impl = append(impl, got...)
		}
	}
	if len(impl) == 0 {
		t.Fatal("経路の登録が見つかりません。登録の書き方が変わった可能性があります")
	}

	paths := make([]string, 0, len(impl))
	for _, p := range impl {
		paths = append(paths, routePath(p))
	}
	assertSameSet(t, "公開する経路", docPaths, paths)
}

// routePath は登録パターンから経路の部分を取り出す。
//
//	"GET /{$}"      → "/"
//	"POST /records" → "/records"
//	"/healthz"      → "/healthz"
func routePath(pattern string) string {
	p := pattern
	if _, rest, ok := strings.Cut(p, " "); ok {
		p = rest
	}
	p = strings.TrimSpace(p)
	p = strings.TrimSuffix(p, "{$}")
	if p == "" {
		return "/"
	}
	return p
}
