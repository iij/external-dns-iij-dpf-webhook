// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"strings"
	"testing"

	dpfapi "github.com/iij/dpf-go"
)

// FR-001 / FR-002: 一括更新の要求に、実行者が本サービスであることを示す説明を添える。
//
// 反映は一括更新の一部として行われるため (research R1)、説明はこの要求に載る。
// 追加の API 呼び出しは増えない。
func TestAtomicChangesBody_CarriesAttribution(t *testing.T) {
	t.Parallel()

	set := []dpfapi.OverwriteRecordsInner{
		{Name: "www.example.jp.", Rrtype: dpfapi.RECORDSRRTYPE_A},
	}

	body := atomicChangesBody(set)

	if body.Description == nil {
		t.Fatal("説明が設定されていない")
	}
	if got := *body.Description; !strings.Contains(got, "external-dns-iij-dpf-webhook") {
		t.Errorf("説明 = %q, want external-dns-iij-dpf-webhook を含む", got)
	}
}

// data-model 1 の不変条件: 説明は呼び出しごとに変わらない。
//
// 変更セットの内容、ゾーン、時刻のいずれにも依存させない。長さが入力で変動すると、
// 上限 (80 オクテット) に対する安全性が定数として言えなくなる (research R5)。
func TestAtomicChangesBody_AttributionIsConstant(t *testing.T) {
	t.Parallel()

	sets := [][]dpfapi.OverwriteRecordsInner{
		nil,
		{},
		{{Name: "a.example.jp.", Rrtype: dpfapi.RECORDSRRTYPE_A}},
		{
			{Name: "b.example.jp.", Rrtype: dpfapi.RECORDSRRTYPE_TXT},
			{Name: "c.sub.example.jp.", Rrtype: dpfapi.RECORDSRRTYPE_CNAME},
		},
	}

	var first string
	for i, set := range sets {
		body := atomicChangesBody(set)
		if body.Description == nil {
			t.Fatalf("%d 件目: 説明が設定されていない", i)
		}
		if i == 0 {
			first = *body.Description
			continue
		}
		if got := *body.Description; got != first {
			t.Errorf("%d 件目: 説明 = %q, want %q (入力によって変わってはならない)", i, got, first)
		}
	}
}

// FR-006: 説明は 80 オクテット以内に収める。
//
// DPF の共通スキーマ Description が maxLength: 80 を宣言している (research R5)。
// 固定文字列であるため、この検査は定数に対する検査になる。可変長の内容を足した
// 場合に、この表明が最初に落ちる。
func TestAtomicChangesBody_AttributionWithinLimit(t *testing.T) {
	t.Parallel()

	const dpfDescriptionMaxOctets = 80

	body := atomicChangesBody(nil)
	if body.Description == nil {
		t.Fatal("説明が設定されていない")
	}
	if n := len(*body.Description); n > dpfDescriptionMaxOctets {
		t.Errorf("説明の長さ = %d オクテット, want <= %d: %q",
			n, dpfDescriptionMaxOctets, *body.Description)
	}
}

// 001 の回帰防止: overwrite フラグは常に false のままであること。
//
// 要求の組み立てを切り出したことで、これらが落ちる余地が生まれた。
// SOA と apex NS の値が意図せず取り込まれると、ゾーンの権威データが壊れる。
func TestAtomicChangesBody_OverwriteFlagsRemainFalse(t *testing.T) {
	t.Parallel()

	body := atomicChangesBody(nil)

	if body.OverwriteSoa == nil || *body.OverwriteSoa {
		t.Errorf("OverwriteSoa = %v, want false を明示", body.OverwriteSoa)
	}
	if body.OverwriteZoneApexNs == nil || *body.OverwriteZoneApexNs {
		t.Errorf("OverwriteZoneApexNs = %v, want false を明示", body.OverwriteZoneApexNs)
	}
}

// 投入するレコードがそのまま載ること。説明の追加が records を壊していないこと。
func TestAtomicChangesBody_KeepsRecords(t *testing.T) {
	t.Parallel()

	set := []dpfapi.OverwriteRecordsInner{
		{Name: "a.example.jp.", Rrtype: dpfapi.RECORDSRRTYPE_A},
		{Name: "b.example.jp.", Rrtype: dpfapi.RECORDSRRTYPE_TXT},
	}

	body := atomicChangesBody(set)

	if len(body.Records) != len(set) {
		t.Fatalf("records = %d 件, want %d 件", len(body.Records), len(set))
	}
	for i := range set {
		if body.Records[i].Name != set[i].Name {
			t.Errorf("records[%d].Name = %q, want %q", i, body.Records[i].Name, set[i].Name)
		}
	}
}
