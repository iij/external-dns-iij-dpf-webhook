// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

func z(name, id string) Zone {
	return Zone{Name: dnsname.MustParse(name), ID: id}
}

// FR-040: 帰属先は最長一致で決まる。親・子・孫が併存しても最も深いものを選ぶ。
func TestZoneIndex_LongestMatchWins(t *testing.T) {
	t.Parallel()

	idx := newZoneIndex([]Zone{
		z("example.jp", "z1"),
		z("sub.example.jp", "z2"),
		z("a.sub.example.jp", "z3"),
	})

	cases := []struct {
		name string
		want string
	}{
		{"www.example.jp", "z1"},
		{"www.sub.example.jp", "z2"},
		{"www.a.sub.example.jp", "z3"},
		// ゾーン名そのもの (apex) は、そのゾーンに属する。
		{"example.jp", "z1"},
		{"sub.example.jp", "z2"},
		{"a.sub.example.jp", "z3"},
	}

	for _, c := range cases {
		got, ok := idx.Owner(dnsname.MustParse(c.name))
		if !ok {
			t.Errorf("Owner(%s) = not found", c.name)
			continue
		}
		if got.ID != c.want {
			t.Errorf("Owner(%s) = %s, want %s", c.name, got.ID, c.want)
		}
	}
}

// 孫ゾーンが DPF 上に存在しなければ、その名前は親ゾーンに属する。
//
// 「どのゾーンに属するか」は DPF がそこにゾーンを持っているかで決まる。
func TestZoneIndex_FallsBackWhenChildAbsent(t *testing.T) {
	t.Parallel()

	idx := newZoneIndex([]Zone{z("example.jp", "z1"), z("sub.example.jp", "z2")})

	got, ok := idx.Owner(dnsname.MustParse("www.a.sub.example.jp"))
	if !ok {
		t.Fatal("Owner = not found")
	}
	if got.ID != "z2" {
		t.Errorf("Owner = %s, want z2 (最も深く一致する sub.example.jp)", got.ID)
	}
}

// FR-041: 含むゾーンがなければ報告する。より浅いゾーンへ倒さない。
func TestZoneIndex_ReportsMissingOwner(t *testing.T) {
	t.Parallel()

	idx := newZoneIndex([]Zone{z("example.jp", "z1")})

	if got, ok := idx.Owner(dnsname.MustParse("www.other.jp")); ok {
		t.Errorf("Owner(www.other.jp) = %s, true; want ok=false", got.ID)
	}
}

// 空のゾーン一覧は何も含まない。「該当なし」を「全部」に読み替えない (原則 VI)。
func TestZoneIndex_EmptyOwnsNothing(t *testing.T) {
	t.Parallel()

	idx := newZoneIndex(nil)

	if _, ok := idx.Owner(dnsname.MustParse("www.example.jp")); ok {
		t.Error("空の索引が帰属先を返した")
	}
}

// ゼロ値のゾーンは索引の材料にしない。判定できない状態を「含む」に倒さない。
func TestZoneIndex_IgnoresZeroZones(t *testing.T) {
	t.Parallel()

	idx := newZoneIndex([]Zone{{}, z("example.jp", "z1"), {}})

	got, ok := idx.Owner(dnsname.MustParse("www.example.jp"))
	if !ok {
		t.Fatal("Owner = not found")
	}
	if got.ID != "z1" {
		t.Errorf("Owner = %s, want z1", got.ID)
	}
}

// OwnedBy は、あるゾーンがその名前の帰属先かを報告する。
//
// 読み取り側はこれで、親ゾーン側に残る値を落とす (FR-044)。
func TestZoneIndex_OwnedBy(t *testing.T) {
	t.Parallel()

	parent := z("example.jp", "z1")
	child := z("sub.example.jp", "z2")
	idx := newZoneIndex([]Zone{parent, child})

	name := dnsname.MustParse("www.sub.example.jp")

	if idx.OwnedBy(name, parent) {
		t.Error("親ゾーンが www.sub.example.jp の帰属先と判定された")
	}
	if !idx.OwnedBy(name, child) {
		t.Error("子ゾーンが www.sub.example.jp の帰属先と判定されなかった")
	}
}
