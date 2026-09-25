// SPDX-License-Identifier: Apache-2.0

package provider

import "github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"

// zoneIndex は、ある名前がどのゾーンに属するかを決める索引。
//
// **帰属の規則はこの型にしか存在しない。** 読み取り (FR-044) と書き込み
// (FR-040) の双方がここを通る。2 箇所に書くと、片方だけ直した変更で両者が
// ずれる。ずれた状態では、親ゾーン側に残ったレコードを見た削除要求が
// 子ゾーンの権威レコードに当たる (research R12)。
//
// 材料は ListZones が返す全ゾーンであり、管理対象範囲 (domain filter) で
// 絞ったものではない。遮蔽するかどうかは「DPF がそこにゾーンを持っているか」
// で決まり、管理対象であるかとは別の問いである。
type zoneIndex struct {
	scope  dnsname.Scope
	byName map[dnsname.Name]Zone
}

// newZoneIndex はゾーン一覧から索引を組み立てる。
//
// ゼロ値のゾーンは材料にしない。判定できない状態を「含む」に倒さないため
// (原則 VI)。空の一覧からは何も含まない索引ができる。
func newZoneIndex(zones []Zone) zoneIndex {
	byName := make(map[dnsname.Name]Zone, len(zones))
	names := make([]dnsname.Name, 0, len(zones))

	for _, z := range zones {
		if z.Name.IsZero() {
			continue
		}
		if _, seen := byName[z.Name]; seen {
			continue
		}
		byName[z.Name] = z
		names = append(names, z.Name)
	}

	return zoneIndex{scope: dnsname.NewScope(names...), byName: byName}
}

// Owner は name が属するゾーンを返す。
//
// 名前をラベル境界で包含し、かつラベル数が最大のゾーン (最長一致) を選ぶ。
// example.jp と sub.example.jp の双方が存在するとき、a.sub.example.jp が
// 属するのは sub.example.jp である。
//
// 含むゾーンがなければ ok に false を返す。より浅いゾーンへ倒さない
// (FR-041)。本サービスはゾーンを作らない (FR-013) ため、解決できない名前は
// 書き込み先を持たない。
func (x zoneIndex) Owner(name dnsname.Name) (Zone, bool) {
	owner, ok := x.scope.LongestMatch(name)
	if !ok {
		return Zone{}, false
	}
	z, ok := x.byName[owner]
	return z, ok
}

// OwnedBy は zone が name の帰属先かを報告する。
//
// 読み取り側はこれで、読み取り元ゾーンが帰属先でないレコードを落とす
// (FR-044)。権威を持つのは最長一致ゾーンの側であり、親ゾーン側に残る値
// (ゾーン作成前の残骸、委任のグルー) は名前解決に影響しない。
func (x zoneIndex) OwnedBy(name dnsname.Name, zone Zone) bool {
	owner, ok := x.Owner(name)
	return ok && owner.Name == zone.Name
}
