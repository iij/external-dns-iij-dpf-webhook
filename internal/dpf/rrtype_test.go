// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"errors"
	"testing"

	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// FR-026: 対応するのは 9 種別。
// ExternalDNS が表現できる種別と DPF が提供する種別の交差である。
func TestToDPF_SupportedTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   provider.RecordType
		want dpfapi.RecordsRrtype
	}{
		{provider.TypeA, dpfapi.RECORDSRRTYPE_A},
		{provider.TypeAAAA, dpfapi.RECORDSRRTYPE_AAAA},
		{provider.TypeCNAME, dpfapi.RECORDSRRTYPE_CNAME},
		{provider.TypeTXT, dpfapi.RECORDSRRTYPE_TXT},
		{provider.TypeSRV, dpfapi.RECORDSRRTYPE_SRV},
		{provider.TypeNS, dpfapi.RECORDSRRTYPE_NS},
		{provider.TypePTR, dpfapi.RECORDSRRTYPE_PTR},
		{provider.TypeMX, dpfapi.RECORDSRRTYPE_MX},
		{provider.TypeNAPTR, dpfapi.RECORDSRRTYPE_NAPTR},
	}

	if len(cases) != len(provider.SupportedRecordTypes()) {
		t.Fatalf("対応種別の数が一致しない: 表 %d, 許可リスト %d",
			len(cases), len(provider.SupportedRecordTypes()))
	}

	for _, c := range cases {
		got, err := toDPF(c.in)
		if err != nil {
			t.Errorf("toDPF(%s) = error %v, want success", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("toDPF(%s) = %v, want %v", c.in, got, c.want)
		}
	}
}

// 対応する 9 種別は往復しても変わらない。
func TestRecordType_RoundTrip(t *testing.T) {
	t.Parallel()

	for _, pt := range provider.SupportedRecordTypes() {
		d, err := toDPF(pt)
		if err != nil {
			t.Fatalf("toDPF(%s) = error %v", pt, err)
		}
		back, ok := fromDPF(d)
		if !ok {
			t.Errorf("fromDPF(%v) が対応外と判定された", d)
			continue
		}
		if back != pt {
			t.Errorf("往復で種別が変化した: %s → %v → %s", pt, d, back)
		}
	}
}

// FR-027: DPF が提供するが ExternalDNS が表現できない種別は管理対象外。
// これらが DPF 上に存在しても、変更・削除してはならない。
func TestFromDPF_UnmanagedTypes(t *testing.T) {
	t.Parallel()

	unmanaged := []dpfapi.RecordsRrtype{
		dpfapi.RECORDSRRTYPE_SOA,
		dpfapi.RECORDSRRTYPE_CAA,
		dpfapi.RECORDSRRTYPE_DS,
		dpfapi.RECORDSRRTYPE_HTTPS,
		dpfapi.RECORDSRRTYPE_SVCB,
		dpfapi.RECORDSRRTYPE_TLSA,
		dpfapi.RECORDSRRTYPE_ANAME,
	}

	if len(unmanaged) != 7 {
		t.Fatalf("管理対象外種別の数が想定と異なる: %d", len(unmanaged))
	}

	for _, d := range unmanaged {
		if _, ok := fromDPF(d); ok {
			t.Errorf("fromDPF(%v) が管理対象と判定された。FR-027 に反する", d)
		}
	}
}

// FR-028: ExternalDNS が表現できるが DPF にない DNAME は適用しない。
func TestToDPF_RejectsDNAME(t *testing.T) {
	t.Parallel()

	// DNAME は provider.RecordType の許可リストにないため、そもそも実体化できない。
	if _, err := provider.ParseRecordType("DNAME"); err == nil {
		t.Fatal("DNAME が対応種別として実体化できてしまった")
	}

	// 万一 RecordType として渡された場合も、境界で拒否する。
	if _, err := toDPF(provider.RecordType("DNAME")); err == nil {
		t.Error("toDPF(DNAME) が成功した。DPF に対応する種別はない")
	}
}

// 未知の種別は恒久的な失敗として扱う。暗黙に通過させない (FR-025)。
func TestToDPF_RejectsUnknown(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"", "UNKNOWN", "a", "SOA", "CAA"} {
		_, err := toDPF(provider.RecordType(s))
		if err == nil {
			t.Errorf("toDPF(%q) が成功した。許可リスト外は拒否しなければならない", s)
			continue
		}
		if !errors.Is(err, provider.ErrUnsupportedType) {
			t.Errorf("toDPF(%q) = %v, want ErrUnsupportedType", s, err)
		}
	}
}

// DPF 側の全種別のうち、管理対象は 9 個、管理対象外は 7 個で、合計 16 個。
// dpf-go の列挙が増減したら気付けるようにしておく。
func TestDPFTypeCoverage(t *testing.T) {
	t.Parallel()

	all := dpfapi.AllowedRecordsRrtypeEnumValues

	var managed, unmanaged int
	for _, d := range all {
		if _, ok := fromDPF(d); ok {
			managed++
		} else {
			unmanaged++
		}
	}

	if managed != 9 {
		t.Errorf("管理対象種別 = %d, want 9", managed)
	}
	if unmanaged != 7 {
		t.Errorf("管理対象外種別 = %d, want 7 (dpf-go の列挙が変わった可能性がある)", unmanaged)
	}
}
