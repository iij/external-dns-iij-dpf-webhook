// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"errors"
	"strings"
	"testing"

	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// cur は DPF から読み取った反映済みレコードを組み立てる。
//
// 型は投入形式である。utils.ZoneApplier は公開されているレコードをこの形へ
// 写して編集関数へ渡すため、merge の入力もこの形になる。
func cur(name string, rrtype dpfapi.RecordsRrtype, ttl int32, values ...string) dpfapi.OverwriteRecordsInner {
	rdata := make([]dpfapi.RecordsRdataInner, 0, len(values))
	for _, v := range values {
		rdata = append(rdata, dpfapi.RecordsRdataInner{Value: &v})
	}
	return dpfapi.OverwriteRecordsInner{
		Name:        name,
		Ttl:         *dpfapi.NewNullableInt32(&ttl),
		Rrtype:      rrtype,
		Rdata:       rdata,
		Description: "既存のコメント",
		Labels:      map[string]string{"env": "prod"},
	}
}

// curNullTTL は TTL が null の反映済みレコードを組み立てる。
//
// DPF は TTL 未指定のレコードを `"ttl": null` で返す。SOA とゾーン apex の NS が
// これに当たる。ゾーンの既定 TTL が使われることを意味する。
func curNullTTL(name string, rrtype dpfapi.RecordsRrtype, values ...string) dpfapi.OverwriteRecordsInner {
	r := cur(name, rrtype, 0, values...)
	r.Ttl = *dpfapi.NewNullableInt32(nil)
	return r
}

func pr(name string, t provider.RecordType, ttl int, values ...string) provider.Record {
	return provider.Record{Name: dnsname.MustParse(name), Type: t, TTL: ttl, Values: values}
}

// 投入する集合から名前と種別で 1 件を探す。
func find(t *testing.T, set []dpfapi.OverwriteRecordsInner, name string, rrtype dpfapi.RecordsRrtype) *dpfapi.OverwriteRecordsInner {
	t.Helper()
	for i := range set {
		if set[i].Name == name && set[i].Rrtype == rrtype {
			return &set[i]
		}
	}
	return nil
}

func values(o *dpfapi.OverwriteRecordsInner) []string {
	out := make([]string, 0, len(o.Rdata))
	for _, d := range o.Rdata {
		out = append(out, d.GetValue())
	}
	return out
}

// 変更セットに含まれる管理対象は、変更後の内容で投入される。
func TestMerge_AppliesChanges(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1")}
	cs := provider.ChangeSet{
		UpdateTo: []provider.Record{pr("www.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	got := find(t, set, "www.example.jp.", dpfapi.RECORDSRRTYPE_A)
	if got == nil {
		t.Fatalf("投入集合に対象がない: %+v", set)
	}
	if v := values(got); len(v) != 1 || v[0] != "192.0.2.99" {
		t.Errorf("値 = %v, want [192.0.2.99]", v)
	}
	if got.Ttl.Get() == nil || *got.Ttl.Get() != 60 {
		t.Errorf("TTL が変更後になっていない: %+v", got.Ttl)
	}
}

// 変更セットに含まれない管理対象は、現在の内容がそのまま投入される。
//
// 一括更新は渡さなかったレコードを削除するため、触れないものも
// 投入集合に含めなければならない。
func TestMerge_KeepsUnchangedRecords(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
		cur("mail.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2"),
	}
	cs := provider.ChangeSet{
		UpdateTo: []provider.Record{pr("www.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	kept := find(t, set, "mail.example.jp.", dpfapi.RECORDSRRTYPE_A)
	if kept == nil {
		t.Fatal("変更対象外のレコードが投入集合から消えた")
	}
	if v := values(kept); len(v) != 1 || v[0] != "192.0.2.2" {
		t.Errorf("値 = %v, want [192.0.2.2]", v)
	}
}

// 管理対象外のレコードは逐語的にコピーされる。
//
// ドメインモデルを通さないため、TTL・値・コメント・ラベルがすべて元のまま残る。
// 変換を挟まなければ、変換の誤りが管理対象外のレコードを壊すことはない。
func TestMerge_CopiesUnmanagedRecordsVerbatim(t *testing.T) {
	t.Parallel()

	caa := cur("example.jp.", dpfapi.RECORDSRRTYPE_CAA, 3600, `0 issue "letsencrypt.org"`)
	caa.Description = "CAA のコメント"
	caa.Labels = map[string]string{"managed-by": "human"}

	current := []dpfapi.OverwriteRecordsInner{
		caa,
		cur("apex.example.jp.", dpfapi.RECORDSRRTYPE_ANAME, 60, "target.example.jp."),
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
	}
	cs := provider.ChangeSet{
		UpdateTo: []provider.Record{pr("www.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	got := find(t, set, "example.jp.", dpfapi.RECORDSRRTYPE_CAA)
	if got == nil {
		t.Fatal("管理対象外の CAA が投入集合から消えた。FR-027 に反する")
	}
	if v := values(got); len(v) != 1 || v[0] != `0 issue "letsencrypt.org"` {
		t.Errorf("CAA の値が変化した: %v", v)
	}
	if got.Description != "CAA のコメント" {
		t.Errorf("コメントが失われた: %q", got.Description)
	}
	if got.Labels["managed-by"] != "human" {
		t.Errorf("ラベルが失われた: %v", got.Labels)
	}
	if got.Ttl.Get() == nil || *got.Ttl.Get() != 3600 {
		t.Errorf("TTL が変化した: %+v", got.Ttl)
	}

	if find(t, set, "apex.example.jp.", dpfapi.RECORDSRRTYPE_ANAME) == nil {
		t.Error("管理対象外の ANAME が投入集合から消えた")
	}
}

// SOA と apex NS を投入集合に含める。
//
// `atomic_changes` は records に SOA と apex NS が含まれることを要求する。
// 欠けると 400 (soa_not_found / apex_ns_not_found) になる。実 DPF に対する
// 検証で判明した (research R3)。
//
// 含めるが、`overwrite_soa` と `overwrite_zone_apex_ns` は常に false であり、
// 送った値は取り込まれない。現在の値を逐語コピーするため、いずれにしても
// 変化しない。
func TestMerge_IncludesSOAAndApexNS(t *testing.T) {
	t.Parallel()

	soa := cur("example.jp.", dpfapi.RECORDSRRTYPE_SOA, 3600, "ns1.example.jp. root.example.jp. 1 2 3 4 5")
	current := []dpfapi.OverwriteRecordsInner{
		soa,
		cur("example.jp.", dpfapi.RECORDSRRTYPE_NS, 3600, "ns1.example.jp."),
		cur("sub.example.jp.", dpfapi.RECORDSRRTYPE_NS, 3600, "ns1.other.jp."),
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
	}

	set, err := merge(current, provider.ChangeSet{})
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	gotSOA := find(t, set, "example.jp.", dpfapi.RECORDSRRTYPE_SOA)
	if gotSOA == nil {
		t.Fatal("SOA が投入集合にない。atomic_changes は soa_not_found で拒否する")
	}
	if v := values(gotSOA); len(v) != 1 || v[0] != "ns1.example.jp. root.example.jp. 1 2 3 4 5" {
		t.Errorf("SOA の値が変化した: %v", v)
	}

	gotApexNS := find(t, set, "example.jp.", dpfapi.RECORDSRRTYPE_NS)
	if gotApexNS == nil {
		t.Fatal("apex NS が投入集合にない。atomic_changes は apex_ns_not_found で拒否する")
	}
	if v := values(gotApexNS); len(v) != 1 || v[0] != "ns1.example.jp." {
		t.Errorf("apex NS の値が変化した: %v", v)
	}

	if find(t, set, "sub.example.jp.", dpfapi.RECORDSRRTYPE_NS) == nil {
		t.Error("apex 以外の NS が落ちた。委任は保持しなければならない")
	}
	if len(set) != len(current) {
		t.Errorf("投入件数 = %d, want %d (全件を含める)", len(set), len(current))
	}
}

// 作成は投入集合に加わる。
func TestMerge_AddsCreatedRecords(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1")}
	cs := provider.ChangeSet{
		Create: []provider.Record{pr("new.example.jp", provider.TypeA, 60, "192.0.2.50")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	if find(t, set, "new.example.jp.", dpfapi.RECORDSRRTYPE_A) == nil {
		t.Error("作成したレコードが投入集合にない")
	}
	if find(t, set, "www.example.jp.", dpfapi.RECORDSRRTYPE_A) == nil {
		t.Error("既存のレコードが消えた")
	}
}

// 削除は投入集合から外れる。
func TestMerge_RemovesDeletedRecords(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
		cur("old.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.9"),
	}
	cs := provider.ChangeSet{
		Delete: []provider.Record{pr("old.example.jp", provider.TypeA, 300, "192.0.2.9")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	if find(t, set, "old.example.jp.", dpfapi.RECORDSRRTYPE_A) != nil {
		t.Error("削除したレコードが投入集合に残っている")
	}
	if find(t, set, "www.example.jp.", dpfapi.RECORDSRRTYPE_A) == nil {
		t.Error("削除対象でないレコードが消えた")
	}
}

// 名前の表現が違っても同一のレコードとして突き合わせる。
func TestMerge_MatchesRecordsCaseInsensitively(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{cur("WWW.Example.JP.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1")}
	cs := provider.ChangeSet{
		UpdateTo: []provider.Record{pr("www.example.jp", provider.TypeA, 60, "192.0.2.99")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	if len(set) != 1 {
		t.Fatalf("投入集合の件数 = %d, want 1 (重複して増えてはならない): %+v", len(set), set)
	}
	if v := values(&set[0]); v[0] != "192.0.2.99" {
		t.Errorf("値 = %v, want [192.0.2.99]", v)
	}
}

// 存在しないレコードの削除要求は、最終状態を変えない (FR-010 の冪等性)。
func TestMerge_DeletingAbsentRecordIsNoop(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1")}
	cs := provider.ChangeSet{
		Delete: []provider.Record{pr("absent.example.jp", provider.TypeA, 300, "192.0.2.9")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	if len(set) != 1 || find(t, set, "www.example.jp.", dpfapi.RECORDSRRTYPE_A) == nil {
		t.Errorf("投入集合が想定と異なる: %+v", set)
	}
}

// 投入前ガード: 変更セットの削除対象と、実際に失われるレコードが一致すること。
//
// マージの誤りは「レコードが消える」ではなく「適用されない」として現れさせる
// (原則 IV の fail closed)。この 2 つが、失敗時の影響範囲を抑える唯一の防壁である。
func TestGuard_DetectsUnexpectedRemoval(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
		cur("mail.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2"),
	}
	// mail が投入集合から抜け落ちた状態を模す。変更セットは削除を含まない。
	set := []dpfapi.OverwriteRecordsInner{current[0]}

	err := guard(current, set, provider.ChangeSet{})
	if err == nil {
		t.Fatal("変更セット外のレコードが失われるのに適用が止まらなかった")
	}
	if !errors.Is(err, provider.ErrTemporary) {
		t.Errorf("err = %v, want ErrTemporary", err)
	}
}

// 削除対象と一致していればガードは通る。
func TestGuard_AllowsRequestedRemoval(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
		cur("old.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.9"),
	}
	set := []dpfapi.OverwriteRecordsInner{current[0]}
	cs := provider.ChangeSet{
		Delete: []provider.Record{pr("old.example.jp", provider.TypeA, 300, "192.0.2.9")},
	}

	if err := guard(current, set, cs); err != nil {
		t.Errorf("要求どおりの削除でガードが止めた: %v", err)
	}
}

// SOA と apex NS も投入集合に含まれるため、ガードは素通しする。
//
// 除外を持たないことで、ガードの対象と投入集合の対象が一致する。
// 両者にずれがあると、ガードが見ていない箇所で欠落が起きうる。
func TestGuard_CoversSOAAndApexNS(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{
		cur("example.jp.", dpfapi.RECORDSRRTYPE_SOA, 3600, "ns1.example.jp. root.example.jp. 1 2 3 4 5"),
		cur("example.jp.", dpfapi.RECORDSRRTYPE_NS, 3600, "ns1.example.jp."),
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
	}

	// 全件を投入すればガードは通る。
	set, err := merge(current, provider.ChangeSet{})
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}
	if guardErr := guard(current, set, provider.ChangeSet{}); guardErr != nil {
		t.Errorf("全件投入でガードが止めた: %v", guardErr)
	}

	// SOA が欠けたらガードが止める。DPF に拒否される前に気付ける。
	withoutSOA := make([]dpfapi.OverwriteRecordsInner, 0, len(set))
	for _, o := range set {
		if o.Rrtype != dpfapi.RECORDSRRTYPE_SOA {
			withoutSOA = append(withoutSOA, o)
		}
	}
	if guardErr := guard(current, withoutSOA, provider.ChangeSet{}); guardErr == nil {
		t.Error("SOA が欠けているのにガードが通した")
	}
}

// TTL が null のレコードは null のまま投入する。
//
// DPF の TTL は 1〜2147483647 であり 0 は範囲外。null を 0 に潰して送ると
// out_of_range で拒否され、ゾーン全体の適用が通らなくなる。SOA と apex NS は
// 未指定で運用されることが多く、この経路は必ず通る。
func TestMerge_PreservesNullTTL(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{
		curNullTTL("example.jp.", dpfapi.RECORDSRRTYPE_SOA, "ns1.example.jp. root.example.jp. 1 2 3 4 5"),
		curNullTTL("example.jp.", dpfapi.RECORDSRRTYPE_NS, "ns1.example.jp."),
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
	}

	set, err := merge(current, provider.ChangeSet{})
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	for _, rrtype := range []dpfapi.RecordsRrtype{
		dpfapi.RECORDSRRTYPE_SOA,
		dpfapi.RECORDSRRTYPE_NS,
	} {
		got := find(t, set, "example.jp.", rrtype)
		if got == nil {
			t.Fatalf("%v が投入集合にない", rrtype)
		}
		if v := got.Ttl.Get(); v != nil {
			t.Errorf("%v の TTL = %d, want null (0 は DPF の範囲外)", rrtype, *v)
		}
	}

	// 値のある TTL はそのまま保つ。
	a := find(t, set, "www.example.jp.", dpfapi.RECORDSRRTYPE_A)
	if a == nil {
		t.Fatal("A が投入集合にない")
	}
	if v := a.Ttl.Get(); v == nil || *v != 300 {
		t.Errorf("A の TTL = %v, want 300", v)
	}
}

// FR-007: 実行者の記録をレコードのコメントに書かない。
//
// レコード単位の Description は 001 の逐語コピーの対象である。本機能の記録はゾーン反映の説明として載るものであり、
// ここへ書き込むと逐語コピーの保証が崩れる。
//
// 既存のコメントが保たれることは TestMerge_* が既に固定している。こちらは
// **新たに書き込まれないこと**の側を押さえる。
func TestMerge_DoesNotWriteAttributionIntoRecordComments(t *testing.T) {
	t.Parallel()

	current := []dpfapi.OverwriteRecordsInner{
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
	}
	cs := provider.ChangeSet{
		Create: []provider.Record{pr("new.example.jp", provider.TypeA, 60, "192.0.2.50")},
	}

	set, err := merge(current, cs)
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	for _, o := range set {
		if strings.Contains(o.Description, "external-dns-iij-dpf-webhook") {
			t.Errorf("%s のコメントに記録が書かれた: %q", o.Name, o.Description)
		}
	}
}
