// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"fmt"

	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// recordKey は投入集合の中でレコードを一意に定める組。
//
// 名前は正規化して比較する。DPF から読み取った名前の表現が変わっても、
// 同じレコードが重複して投入されないようにするため。
type recordKey struct {
	name   string
	rrtype dpfapi.RecordsRrtype
}

// merge は反映済みレコードに変更セットを適用し、投入する集合を組み立てる。
//
// 一括更新は渡さなかったレコードを削除する。したがって投入する集合は
// ゾーンのあるべき全体でなければならない。変更対象でないレコードも、
// 管理対象外のレコードも、すべて含める。
//
// SOA とゾーン apex の NS は投入しない。一括更新 API の overwrite_soa /
// overwrite_zone_apex_ns が既定 false であり、これらは上書き対象から外れる。
// 投入対象から外すことで FR-029 が自前ロジックではなく API 側で担保される。
func merge(current []dpfapi.Record, cs provider.ChangeSet) ([]dpfapi.OverwriteRecordsInner, error) {
	set := make(map[recordKey]dpfapi.OverwriteRecordsInner, len(current))
	order := make([]recordKey, 0, len(current))

	// 1. 反映済みの内容を土台にする。
	for i := range current {
		r := &current[i]
		if excludedFromOverwrite(r.GetName(), r.GetRrtype(), current) {
			continue
		}

		key, err := keyOf(r.GetName(), r.GetRrtype())
		if err != nil {
			// 名前を解釈できないレコードは、こちらから触れない。
			// 投入集合から落とすと消えてしまうため、逐語のまま残す。
			key = recordKey{name: r.GetName(), rrtype: r.GetRrtype()}
		}
		if _, seen := set[key]; !seen {
			order = append(order, key)
		}
		set[key] = toOverwrite(r)
	}

	// 2. 削除対象を落とす。
	for _, d := range cs.Delete {
		key, err := providerKey(d)
		if err != nil {
			return nil, err
		}
		delete(set, key)
	}

	// 3. 作成と更新を反映する。両者は投入集合の上では同じ操作になる。
	for _, r := range append(append([]provider.Record{}, cs.Create...), cs.UpdateTo...) {
		key, err := providerKey(r)
		if err != nil {
			return nil, err
		}

		base, existed := set[key]
		if !existed {
			order = append(order, key)
			base = dpfapi.OverwriteRecordsInner{
				Name:   key.name,
				Rrtype: key.rrtype,
				Labels: map[string]string{},
			}
		}

		values, err := normalizeValues(r)
		if err != nil {
			return nil, err
		}

		base.Ttl = toTTL(r.TTL)
		base.Rdata = toRdata(values)
		set[key] = base
	}

	// 4. 反映済みの並び順を保って返す。順序が呼び出しごとに変わると、
	//    要求の差分が読みにくくなる。
	out := make([]dpfapi.OverwriteRecordsInner, 0, len(set))
	for _, k := range order {
		if v, ok := set[k]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}

// guard は投入集合を送る前に、失われるレコードが要求どおりかを確かめる。
//
// 変更セットの削除対象に含まれないレコードが投入集合から欠けている場合、
// それはマージの誤りである。適用を中止し、一時的な失敗として返す。
//
// この検査があるため、マージの誤りは「レコードが消える」ではなく
// 「適用されない」として現れる (原則 IV の fail closed)。管理対象外レコードの
// 逐語コピーと併せて、失敗時の影響範囲を抑える防壁になっている。
func guard(current []dpfapi.Record, set []dpfapi.OverwriteRecordsInner, cs provider.ChangeSet) error {
	submitted := make(map[recordKey]bool, len(set))
	for _, o := range set {
		submitted[recordKey{name: o.Name, rrtype: o.Rrtype}] = true
	}

	requested := make(map[recordKey]bool, len(cs.Delete))
	for _, d := range cs.Delete {
		key, err := providerKey(d)
		if err != nil {
			return err
		}
		requested[key] = true
	}

	for i := range current {
		r := &current[i]
		if excludedFromOverwrite(r.GetName(), r.GetRrtype(), current) {
			continue
		}

		key, err := keyOf(r.GetName(), r.GetRrtype())
		if err != nil {
			key = recordKey{name: r.GetName(), rrtype: r.GetRrtype()}
		}
		if submitted[key] || requested[key] {
			continue
		}

		return fmt.Errorf(
			"%w: 投入前の検査で中止しました: %s %s が削除要求なしに失われます",
			provider.ErrTemporary, key.name, key.rrtype)
	}

	return nil
}

// excludedFromOverwrite は、そのレコードが一括更新の対象外かを報告する。
//
// SOA と、ゾーン apex の NS が該当する。ゾーン apex は SOA レコードの名前から
// 判断する。ゾーンには必ず SOA が 1 つあり、その名前がゾーン名である。
func excludedFromOverwrite(name string, rrtype dpfapi.RecordsRrtype, current []dpfapi.Record) bool {
	if rrtype == dpfapi.RECORDSRRTYPE_SOA {
		return true
	}
	if rrtype != dpfapi.RECORDSRRTYPE_NS {
		return false
	}
	return name == zoneApex(current)
}

// zoneApex は SOA レコードの名前からゾーン apex を求める。
// SOA が見つからない場合は空文字列を返し、どの NS も apex とは見なさない。
func zoneApex(current []dpfapi.Record) string {
	for i := range current {
		if current[i].GetRrtype() == dpfapi.RECORDSRRTYPE_SOA {
			return current[i].GetName()
		}
	}
	return ""
}

// keyOf は DPF 側の名前と種別から突き合わせ用の鍵を作る。
func keyOf(name string, rrtype dpfapi.RecordsRrtype) (recordKey, error) {
	n, err := dnsname.Parse(name)
	if err != nil {
		return recordKey{}, err
	}
	return recordKey{name: n.String(), rrtype: rrtype}, nil
}

// providerKey はドメインのレコードから突き合わせ用の鍵を作る。
func providerKey(r provider.Record) (recordKey, error) {
	rrtype, err := toDPF(r.Type)
	if err != nil {
		return recordKey{}, err
	}
	return recordKey{name: r.Name.String(), rrtype: rrtype}, nil
}

// toOverwrite は反映済みレコードを投入形式へ逐語的に写す。
//
// ドメインモデルを通さない (MUST NOT)。種別の対応付けや正規化を経由させず、
// 読み取った表現のまま書き戻す。変換を挟まなければ、変換の誤りが
// 管理対象外のレコードを壊すことはない (research R3)。
//
// Id / State / Operator はサーバ管理項目であり投入形式に存在しない。
// 設定可能な項目はすべて写るため、往復は無損失である。
func toOverwrite(r *dpfapi.Record) dpfapi.OverwriteRecordsInner {
	labels := r.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	rdata := make([]dpfapi.RecordsRdataInner, len(r.GetRdata()))
	copy(rdata, r.GetRdata())

	ttl := r.GetTtl()
	return dpfapi.OverwriteRecordsInner{
		Name:        r.GetName(),
		Ttl:         *dpfapi.NewNullableInt32(&ttl),
		Rrtype:      r.GetRrtype(),
		Rdata:       rdata,
		Description: r.GetDescription(),
		Labels:      labels,
	}
}

// normalizeValues は DPF へ送る値を整える。
//
// 現在の対象は TXT のみ。255 オクテットを超える character-string を含む値は、
// 分割後の表現へ書き換える。元の値のまま送ると DPF に拒否されるため、
// 自動分割を実際に効かせるにはここで整える必要がある。
//
// 分割が不要な値は書き換えない。分割位置とエスケープの表現を保つため (FR-032a)。
func normalizeValues(r provider.Record) ([]string, error) {
	if r.Type != provider.TypeTXT {
		return r.Values, nil
	}

	out := make([]string, 0, len(r.Values))
	for _, v := range r.Values {
		n, err := provider.NormalizeTXT(v)
		if err != nil {
			return nil, fmt.Errorf("%w: %s TXT: %w", provider.ErrPermanent, r.Name, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// toTTL は TTL を DPF の表現へ変換する。
//
// DPF の TTL は nullable であり、許容範囲は 1〜2147483647 である。0 は範囲外で
// あり、本サービスでは「未指定」を表すため null として送る。ゾーンの既定 TTL が
// 使われる。0 をそのまま送ると DPF に拒否される。
func toTTL(ttl int) dpfapi.NullableInt32 {
	if ttl == 0 {
		return *dpfapi.NewNullableInt32(nil)
	}
	// 範囲は provider.ValidateFormat が検証済み。境界へ届く前に弾かれるため、
	// ここで桁があふれることはない。
	//nolint:gosec // provider.ValidateFormat で範囲を検証済み
	v := int32(ttl)
	return *dpfapi.NewNullableInt32(&v)
}

// toRdata は値の並びを DPF の rdata へ変換する。
func toRdata(values []string) []dpfapi.RecordsRdataInner {
	out := make([]dpfapi.RecordsRdataInner, 0, len(values))
	for _, v := range values {
		out = append(out, dpfapi.RecordsRdataInner{Value: &v})
	}
	return out
}
