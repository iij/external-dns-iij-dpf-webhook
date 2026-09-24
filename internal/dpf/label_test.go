// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"maps"
	"testing"

	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// labelsOf は投入集合から名前と種別で 1 件のラベルを取り出す。
func labelsOf(t *testing.T, set []dpfapi.OverwriteRecordsInner, name string, rrtype dpfapi.RecordsRrtype) map[string]string {
	t.Helper()
	got := find(t, set, name, rrtype)
	if got == nil {
		t.Fatalf("%s %v が投入集合にない", name, rrtype)
	}
	return got.Labels
}

// wantOnlyMark はラベルが印 1 つだけであることを確かめる。
func wantOnlyMark(t *testing.T, labels map[string]string, where string) {
	t.Helper()
	if len(labels) != 1 {
		t.Errorf("%s: ラベル数 = %d, want 1: %v", where, len(labels), labels)
		return
	}
	if got := labels[managedByLabelKey]; got != applyAttribution {
		t.Errorf("%s: labels[%q] = %q, want %q", where, managedByLabelKey, got, applyAttribution)
	}
}

// FR-001: 作成・更新するレコードのラベルは印のみになる。
func TestApplyManagedBy_SetsMarkOnChangedRecords(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{
		cur("keep.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
		cur("upd.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2"),
	}
	cs := provider.ChangeSet{
		Create:   []provider.Record{pr("new.example.jp", provider.TypeA, 60, "192.0.2.50")},
		UpdateTo: []provider.Record{pr("upd.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	applyManagedBy(set, cs)

	wantOnlyMark(t, labelsOf(t, set, "new.example.jp.", dpfapi.RECORDSRRTYPE_A), "作成")
	wantOnlyMark(t, labelsOf(t, set, "upd.example.jp.", dpfapi.RECORDSRRTYPE_A), "更新")
}

// FR-002: ExternalDNS の所有権 TXT も対象である。
//
// これらは ExternalDNS が生成するが、DPF へ書くのは本 provider である。
// 特別な扱いを要しないことを固定する。
func TestApplyManagedBy_SetsMarkOnOwnershipTXT(t *testing.T) {
	t.Parallel()

	cs := provider.ChangeSet{
		Create: []provider.Record{
			pr("a-www.example.jp", provider.TypeTXT, 0,
				`"heritage=external-dns,external-dns/owner=o1,external-dns/resource=ingress/app/www"`),
		},
	}

	set, err := merge(nil, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	applyManagedBy(set, cs)

	wantOnlyMark(t, labelsOf(t, set, "a-www.example.jp.", dpfapi.RECORDSRRTYPE_TXT), "所有権 TXT")
}

// FR-003: 変更セットに含まれないレコードのラベルは変えない。
//
// 001 の逐語コピーをそのまま保つ。人手のレコードを巻き込まないことが、
// 印の価値の前提である。
func TestApplyManagedBy_LeavesUntouchedRecordsAlone(t *testing.T) {
	t.Parallel()

	manual := cur("manual.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.7")
	manual.SetLabels(map[string]string{"env": "prod", "team": "web"})

	current := []dpfapi.OverwriteRecordsInner{manual}
	cs := provider.ChangeSet{
		Create: []provider.Record{pr("new.example.jp", provider.TypeA, 60, "192.0.2.50")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	applyManagedBy(set, cs)

	got := labelsOf(t, set, "manual.example.jp.", dpfapi.RECORDSRRTYPE_A)
	if len(got) != 2 || got["env"] != "prod" || got["team"] != "web" {
		t.Errorf("触らないレコードのラベルが変わった: %v", got)
	}
	if _, ok := got[managedByLabelKey]; ok {
		t.Errorf("触らないレコードに印が付いた: %v", got)
	}
}

// PC-001: 運用者が付けたラベルは、本 provider が更新した時点で失われる。
//
// **これは仕様であり、不具合ではない。** 表明しておかないと、後から
// 「保持すべきだ」と直されて上限の扱いが必要になる (research R2)。
func TestApplyManagedBy_DiscardsOperatorLabelsOnChangedRecords(t *testing.T) {
	t.Parallel()

	tagged := cur("upd.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2")
	tagged.SetLabels(map[string]string{"env": "prod", "team": "web"})

	cs := provider.ChangeSet{
		UpdateTo: []provider.Record{pr("upd.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge([]dpfapi.OverwriteRecordsInner{tagged}, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	applyManagedBy(set, cs)

	wantOnlyMark(t, labelsOf(t, set, "upd.example.jp.", dpfapi.RECORDSRRTYPE_A), "運用者のラベルを持つレコードの更新")
}

// managed-by に別の値があれば、自身の名前になる。
func TestApplyManagedBy_ReplacesForeignValue(t *testing.T) {
	t.Parallel()

	foreign := cur("upd.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2")
	foreign.SetLabels(map[string]string{managedByLabelKey: "someone-else"})

	cs := provider.ChangeSet{
		UpdateTo: []provider.Record{pr("upd.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge([]dpfapi.OverwriteRecordsInner{foreign}, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	applyManagedBy(set, cs)

	wantOnlyMark(t, labelsOf(t, set, "upd.example.jp.", dpfapi.RECORDSRRTYPE_A), "別の値を持つレコード")
}

// FR-005: 冪等である。2 回適用しても変わらない。
func TestApplyManagedBy_IsIdempotent(t *testing.T) {
	t.Parallel()

	cs := provider.ChangeSet{
		Create: []provider.Record{pr("new.example.jp", provider.TypeA, 60, "192.0.2.50")},
	}

	set, err := merge(nil, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	applyManagedBy(set, cs)
	once := maps.Clone(labelsOf(t, set, "new.example.jp.", dpfapi.RECORDSRRTYPE_A))

	applyManagedBy(set, cs)
	twice := labelsOf(t, set, "new.example.jp.", dpfapi.RECORDSRRTYPE_A)

	if !maps.Equal(once, twice) {
		t.Errorf("再適用で変化した: %v → %v", once, twice)
	}
	wantOnlyMark(t, twice, "再適用後")
}

// 反映済みレコードのラベルのマップを書き換えない。
//
// merge は元のマップへの**参照**を投入集合へ入れる。上書き方式は新しい
// マップを代入することでこれを避けている (research R3)。
//
// **加算方式へ戻されたときに、この表明が最初に落ちる。**
func TestApplyManagedBy_DoesNotMutateCurrentLabels(t *testing.T) {
	t.Parallel()

	original := map[string]string{"env": "prod"}
	rec := cur("upd.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2")
	rec.SetLabels(original)

	cs := provider.ChangeSet{
		UpdateTo: []provider.Record{pr("upd.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge([]dpfapi.OverwriteRecordsInner{rec}, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	applyManagedBy(set, cs)

	if len(original) != 1 || original["env"] != "prod" {
		t.Errorf("反映済みレコードのラベルのマップが書き換わった: %v", original)
	}
}

// ラベルは常に 1 件であり、DPF の上限 (10) に触れる経路が存在しない。
//
// 上限に達したレコードを更新しても、投入するラベルは 1 件である。
// **上限の扱いを定める必要がないことを固定する** (research R2)。
func TestApplyManagedBy_NeverExceedsDPFLimit(t *testing.T) {
	t.Parallel()

	const dpfLabelMax = 10

	full := map[string]string{}
	for i := range dpfLabelMax {
		full[string(rune('a'+i))] = "v"
	}
	rec := cur("upd.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2")
	rec.SetLabels(full)

	cs := provider.ChangeSet{
		UpdateTo: []provider.Record{pr("upd.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge([]dpfapi.OverwriteRecordsInner{rec}, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	applyManagedBy(set, cs)

	got := labelsOf(t, set, "upd.example.jp.", dpfapi.RECORDSRRTYPE_A)
	if len(got) > dpfLabelMax {
		t.Errorf("ラベル数 = %d, want <= %d", len(got), dpfLabelMax)
	}
	wantOnlyMark(t, got, "上限まで使われていたレコード")
}
