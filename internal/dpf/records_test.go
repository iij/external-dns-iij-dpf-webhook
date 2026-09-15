// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"testing"

	dpfapi "github.com/iij/dpf-go"
)

// FR-029: DPF 上に NS が存在しても、レコード一覧には現れない。
//
// 除外はこの変換で起きる。ゾーンカットでは委任の NS (親ゾーン側) と apex の
// NS (子ゾーン側) が名前も種別も同じまま両側に存在し、上位層は名前と種別しか
// 持たないため区別できない。見せないことで、区別できないものを更新する経路を
// 断つ (research R12)。
//
// なお、ここで除外しても DPF 上から消えることはない。一括更新の土台に使う
// 一覧は種別で絞らない生のレコードであり、NS は逐語コピーで投入集合に残る
// (merge の TestMerge_KeepsSOAAndApexNS が固定している)。
func TestToProviderRecord_ExcludesNS(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"example.jp.", "sub.example.jp."} {
		r := dpfapi.Record{}
		r.SetName(name)
		r.SetRrtype(dpfapi.RECORDSRRTYPE_NS)
		r.SetTtl(3600)

		if got, ok := toProviderRecord(&r); ok {
			t.Errorf("%s の NS が変換された: %+v", name, got)
		}
	}
}

// 対応する種別は従来どおり変換される。除外が NS に限ることを固定する。
func TestToProviderRecord_KeepsSupportedTypes(t *testing.T) {
	t.Parallel()

	r := dpfapi.Record{}
	r.SetName("www.example.jp.")
	r.SetRrtype(dpfapi.RECORDSRRTYPE_A)
	r.SetTtl(300)

	got, ok := toProviderRecord(&r)
	if !ok {
		t.Fatal("A が除外された")
	}
	if got.Name.String() != "www.example.jp." {
		t.Errorf("名前 = %q", got.Name.String())
	}
}
