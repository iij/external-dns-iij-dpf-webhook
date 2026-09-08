// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"errors"
	"testing"

	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// cur は DPF から読み取った反映済みレコードを組み立てる。
func cur(name string, rrtype dpfapi.RecordsRrtype, ttl int32, values ...string) dpfapi.Record {
	rdata := make([]dpfapi.RecordsRdataInner, 0, len(values))
	for _, v := range values {
		rdata = append(rdata, dpfapi.RecordsRdataInner{Value: &v})
	}
	return dpfapi.Record{
		Name:        name,
		Ttl:         *dpfapi.NewNullableInt32(&ttl),
		Rrtype:      rrtype,
		Rdata:       rdata,
		Description: "既存のコメント",
		Labels:      map[string]string{"env": "prod"},
	}
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

	current := []dpfapi.Record{cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1")}
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

	current := []dpfapi.Record{
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

	current := []dpfapi.Record{
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

// SOA と apex NS は投入しない。
//
// 一括更新 API の overwrite_soa / overwrite_zone_apex_ns は既定 false であり、
// 投入対象から外れる。これにより FR-029 が自前ロジックではなく API 側で担保される。
func TestMerge_ExcludesSOAAndApexNS(t *testing.T) {
	t.Parallel()

	current := []dpfapi.Record{
		cur("example.jp.", dpfapi.RECORDSRRTYPE_SOA, 3600, "ns1.example.jp. root.example.jp. 1 2 3 4 5"),
		cur("example.jp.", dpfapi.RECORDSRRTYPE_NS, 3600, "ns1.example.jp."),
		cur("sub.example.jp.", dpfapi.RECORDSRRTYPE_NS, 3600, "ns1.other.jp."),
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
	}

	set, err := merge(current, provider.ChangeSet{})
	if err != nil {
		t.Fatalf("merge = error %v", err)
	}

	if find(t, set, "example.jp.", dpfapi.RECORDSRRTYPE_SOA) != nil {
		t.Error("SOA が投入集合に含まれている")
	}
	if find(t, set, "example.jp.", dpfapi.RECORDSRRTYPE_NS) != nil {
		t.Error("apex NS が投入集合に含まれている")
	}
	if find(t, set, "sub.example.jp.", dpfapi.RECORDSRRTYPE_NS) == nil {
		t.Error("apex 以外の NS が落ちた。委任は保持しなければならない")
	}
}

// 作成は投入集合に加わる。
func TestMerge_AddsCreatedRecords(t *testing.T) {
	t.Parallel()

	current := []dpfapi.Record{cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1")}
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

	current := []dpfapi.Record{
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

	current := []dpfapi.Record{cur("WWW.Example.JP.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1")}
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

	current := []dpfapi.Record{cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1")}
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

	current := []dpfapi.Record{
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
		cur("mail.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2"),
	}
	// mail が投入集合から抜け落ちた状態を模す。変更セットは削除を含まない。
	set := []dpfapi.OverwriteRecordsInner{
		toOverwrite(&current[0]),
	}

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

	current := []dpfapi.Record{
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
		cur("old.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.9"),
	}
	set := []dpfapi.OverwriteRecordsInner{toOverwrite(&current[0])}
	cs := provider.ChangeSet{
		Delete: []provider.Record{pr("old.example.jp", provider.TypeA, 300, "192.0.2.9")},
	}

	if err := guard(current, set, cs); err != nil {
		t.Errorf("要求どおりの削除でガードが止めた: %v", err)
	}
}

// SOA と apex NS は投入対象外だが、失われるわけではない。ガードの対象から除く。
func TestGuard_IgnoresSOAAndApexNS(t *testing.T) {
	t.Parallel()

	current := []dpfapi.Record{
		cur("example.jp.", dpfapi.RECORDSRRTYPE_SOA, 3600, "ns1.example.jp. root.example.jp. 1 2 3 4 5"),
		cur("example.jp.", dpfapi.RECORDSRRTYPE_NS, 3600, "ns1.example.jp."),
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
	}
	set := []dpfapi.OverwriteRecordsInner{toOverwrite(&current[2])}

	if err := guard(current, set, provider.ChangeSet{}); err != nil {
		t.Errorf("SOA / apex NS がガードに引っかかった: %v", err)
	}
}
