// SPDX-License-Identifier: Apache-2.0

// Package integration は provider のドメインロジックと DPF クライアント層を
// 組み合わせて検証する。DPF API はモックに差し替える (原則 II)。
package integration

import (
	"log/slog"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider/providertest"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func rec(name string, t provider.RecordType, ttl int, values ...string) provider.Record {
	return provider.Record{
		Name:   dnsname.MustParse(name),
		Type:   t,
		TTL:    ttl,
		Values: values,
	}
}

func names(records []provider.Record) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.Name.String()+" "+r.Type.String())
	}
	return out
}

// FR-006: 管理対象外のレコードを一覧に含めない。
//
// 含めてしまうと ExternalDNS がそれらを管理下と見なし、次の適用で
// 削除対象に含める。原則 IV が禁じる範囲外への書き込みが、読み取りの
// 誤りから発生する経路である。
func TestList_ExcludesOutOfScopeZones(t *testing.T) {
	t.Parallel()

	managed := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	other := provider.Zone{Name: dnsname.MustParse("other.jp"), ID: "z2"}

	backend := providertest.New().
		WithZone(managed, rec("www.example.jp", provider.TypeA, 300, "192.0.2.1")).
		WithZone(other, rec("www.other.jp", provider.TypeA, 300, "198.51.100.1"))

	p := provider.New(dnsname.NewScope(dnsname.MustParse("example.jp")), backend, discardLogger())

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1: %v", len(got), names(got))
	}
	if got[0].Name.String() != "www.example.jp." {
		t.Errorf("返ったレコード = %q, want %q", got[0].Name, "www.example.jp.")
	}
}

// 接尾辞一致では一致してしまう名前を、管理対象に含めない。
//
// evil-example.jp は example.jp の配下ではない。文字列の接尾辞一致で
// 判定していると、他人のゾーンを管理下に取り込む。
func TestList_ExcludesSuffixLookalikeZones(t *testing.T) {
	t.Parallel()

	managed := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	lookalike := provider.Zone{Name: dnsname.MustParse("evil-example.jp"), ID: "z2"}

	backend := providertest.New().
		WithZone(managed, rec("www.example.jp", provider.TypeA, 300, "192.0.2.1")).
		WithZone(lookalike, rec("www.evil-example.jp", provider.TypeA, 300, "198.51.100.1"))

	p := provider.New(dnsname.NewScope(dnsname.MustParse("example.jp")), backend, discardLogger())

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}

	for _, r := range got {
		if r.Name.String() == "www.evil-example.jp." {
			t.Fatalf("evil-example.jp のレコードが含まれている: %v", names(got))
		}
	}
	if len(got) != 1 {
		t.Errorf("件数 = %d, want 1: %v", len(got), names(got))
	}
}

// FR-004: 大文字小文字と末尾ドットの違いによらず同一の名前として扱う。
func TestList_CaseInsensitiveNames(t *testing.T) {
	t.Parallel()

	zone := provider.Zone{Name: dnsname.MustParse("EXAMPLE.JP"), ID: "z1"}
	backend := providertest.New().
		WithZone(zone, rec("WWW.Example.JP.", provider.TypeA, 300, "192.0.2.1"))

	p := provider.New(dnsname.NewScope(dnsname.MustParse("example.jp")), backend, discardLogger())

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1: %v", len(got), names(got))
	}
	if got[0].Name.String() != "www.example.jp." {
		t.Errorf("名前 = %q, want %q", got[0].Name, "www.example.jp.")
	}
}

// ゾーン内でも、管理対象範囲の外にある名前は含めない。
//
// example.jp を管理し、sub.example.jp のゾーンが別にある構成で、
// 範囲がゾーンより狭い場合を想定する。
func TestList_ExcludesNamesOutsideScopeWithinZone(t *testing.T) {
	t.Parallel()

	zone := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	backend := providertest.New().WithZone(zone,
		rec("a.sub.example.jp", provider.TypeA, 300, "192.0.2.1"),
		rec("b.example.jp", provider.TypeA, 300, "192.0.2.2"),
	)

	// 範囲を sub.example.jp に絞る。
	p := provider.New(dnsname.NewScope(dnsname.MustParse("sub.example.jp")), backend, discardLogger())

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1: %v", len(got), names(got))
	}
	if got[0].Name.String() != "a.sub.example.jp." {
		t.Errorf("名前 = %q, want %q", got[0].Name, "a.sub.example.jp.")
	}
}

// 複数のゾーンを同時に扱える (FR-007)。
func TestList_MultipleZones(t *testing.T) {
	t.Parallel()

	z1 := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	z2 := provider.Zone{Name: dnsname.MustParse("example.com"), ID: "z2"}

	backend := providertest.New().
		WithZone(z1, rec("www.example.jp", provider.TypeA, 300, "192.0.2.1")).
		WithZone(z2, rec("www.example.com", provider.TypeA, 300, "192.0.2.2"))

	scope := dnsname.NewScope(
		dnsname.MustParse("example.jp"),
		dnsname.MustParse("example.com"),
	)
	p := provider.New(scope, backend, discardLogger())

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}
	if len(got) != 2 {
		t.Errorf("件数 = %d, want 2: %v", len(got), names(got))
	}
}

// FR-002: 管理対象が空なら 0 件を返し、バックエンドにも問い合わせない。
func TestList_EmptyScopeQueriesNothing(t *testing.T) {
	t.Parallel()

	zone := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	backend := providertest.New().
		WithZone(zone, rec("www.example.jp", provider.TypeA, 300, "192.0.2.1"))

	p := provider.New(dnsname.NewScope(), backend, discardLogger())

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}
	if len(got) != 0 {
		t.Errorf("件数 = %d, want 0: %v", len(got), names(got))
	}

	zones, records, _ := backend.Counts()
	if zones != 0 || records != 0 {
		t.Errorf("バックエンドへの問い合わせが発生した (zones=%d, records=%d)", zones, records)
	}
}

// FR-001: 管理対象ドメインを返せる。
func TestFilters(t *testing.T) {
	t.Parallel()

	scope := dnsname.NewScope(
		dnsname.MustParse("example.jp"),
		dnsname.MustParse("example.com"),
	)
	p := provider.New(scope, providertest.New(), discardLogger())

	got := p.Filters()
	if len(got) != 2 {
		t.Fatalf("filters = %v, want 2 件", got)
	}
}

// 管理対象が空なら filters も空。nil ではなく空スライスを返す。
func TestFilters_EmptyScope(t *testing.T) {
	t.Parallel()

	p := provider.New(dnsname.NewScope(), providertest.New(), discardLogger())

	got := p.Filters()
	if got == nil {
		t.Error("filters が nil。空スライスを返すこと")
	}
	if len(got) != 0 {
		t.Errorf("filters = %v, want 空", got)
	}
}

// FR-044: 親ゾーン側に残るレコードは、レコード一覧に含めない。
//
// 権威を持つのは最長一致ゾーンの側である。親ゾーンに子ゾーン配下の名前の
// レコードが残っていても、それは名前解決に影響しない。
//
// 両方返すと、ExternalDNS は同じ名前・種別のレコードを 2 件見ることになる。
// さらに悪いことに、親側の残骸を見た削除要求は書き込み時に最長一致で子ゾーンへ
// 振られ、**子ゾーンの権威レコードを消す**。読み取りと書き込みの帰属を同じ
// 規則に揃えることで、この経路を断つ (research R12)。
func TestList_ExcludesRecordsOwnedByChildZone(t *testing.T) {
	t.Parallel()

	parent := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	child := provider.Zone{Name: dnsname.MustParse("sub.example.jp"), ID: "z2"}

	backend := providertest.New().
		// 親ゾーンに、子ゾーン配下の名前のレコードが残っている。
		WithZone(parent,
			rec("www.example.jp", provider.TypeA, 300, "192.0.2.1"),
			rec("www.sub.example.jp", provider.TypeA, 300, "192.0.2.99")).
		// 子ゾーンが権威を持つ側。
		WithZone(child, rec("www.sub.example.jp", provider.TypeA, 300, "192.0.2.2"))

	p := provider.New(scopeJP(), backend, discardLogger())

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("件数 = %d, want 2: %v", len(got), names(got))
	}

	// www.sub.example.jp は 1 件だけ。値は子ゾーン側のもの。
	var found int
	for _, r := range got {
		if r.Name.String() != "www.sub.example.jp." {
			continue
		}
		found++
		if len(r.Values) != 1 || r.Values[0] != "192.0.2.2" {
			t.Errorf("値 = %v, want [192.0.2.2] (子ゾーン側)", r.Values)
		}
	}
	if found != 1 {
		t.Errorf("www.sub.example.jp の件数 = %d, want 1: %v", found, names(got))
	}
}

// 孫ゾーンまで併存しても、帰属は最も深いゾーンに決まる。
func TestList_AttributesToDeepestZone(t *testing.T) {
	t.Parallel()

	zones := []struct {
		zone  provider.Zone
		value string
	}{
		{provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}, "192.0.2.1"},
		{provider.Zone{Name: dnsname.MustParse("sub.example.jp"), ID: "z2"}, "192.0.2.2"},
		{provider.Zone{Name: dnsname.MustParse("a.sub.example.jp"), ID: "z3"}, "192.0.2.3"},
	}

	backend := providertest.New()
	// 3 ゾーンすべてが www.a.sub.example.jp を持っている状態にする。
	for _, e := range zones {
		backend = backend.WithZone(e.zone,
			rec("www.a.sub.example.jp", provider.TypeA, 300, e.value))
	}

	p := provider.New(scopeJP(), backend, discardLogger())

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1: %v", len(got), names(got))
	}
	if len(got[0].Values) != 1 || got[0].Values[0] != "192.0.2.3" {
		t.Errorf("値 = %v, want [192.0.2.3] (孫ゾーン a.sub.example.jp)", got[0].Values)
	}
}
