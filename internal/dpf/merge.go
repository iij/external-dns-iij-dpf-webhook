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
// 入力は utils.ZoneApplier が渡す「公開されているレコードを投入形式へ写した
// もの」である。**入力と出力が同じ型であることに意味がある。** 触れない要素は
// そのまま返せばよく、TTL・コメント・ラベルを写し替える必要がない。写し忘れて
// 失う経路がそもそも存在しない。null の TTL が 0 に潰れる心配もない
// (DPF の TTL は 1 以上であり、0 を送ると out_of_range で拒否される)。
//
// 一括更新は渡さなかったレコードを削除する。したがって投入する集合は
// ゾーンのあるべき全体でなければならない。変更対象でないレコードも、
// 管理対象外のレコードも、すべて含める。
//
// **SOA とゾーン apex の NS も含める。** 一括更新 API は records にこの 2 つが
// 存在することを要求し、欠けると 400 (soa_not_found / apex_ns_not_found) で
// 拒否する。overwrite_soa / overwrite_zone_apex_ns は「送った値を取り込むか」を
// 決めるフラグであり、「省いてよいか」ではない (research R3)。
//
// この 2 つは常に false で送るため、投入した値は反映されない。読み取った値を
// 逐語のまま残すので、いずれにしても変化しない。
func merge(current []dpfapi.OverwriteRecordsInner, cs provider.ChangeSet) ([]dpfapi.OverwriteRecordsInner, error) {
	set := make(map[recordKey]dpfapi.OverwriteRecordsInner, len(current))
	order := make([]recordKey, 0, len(current))

	// 1. 反映済みの内容を土台にする。
	//
	// **要素は逐語のまま写す。** ドメインモデルを通さない (MUST NOT)。種別の
	// 対応付けや正規化を経由させず、読み取った表現のまま書き戻す。変換を挟ま
	// なければ、変換の誤りが管理対象外のレコードを壊すことはない (research R3)。
	for i := range current {
		r := current[i]

		key, err := keyOf(r.Name, r.Rrtype)
		if err != nil {
			// 名前を解釈できないレコードは、こちらから触れない。
			// 投入集合から落とすと消えてしまうため、逐語のまま残す。
			key = recordKey{name: r.Name, rrtype: r.Rrtype}
		}
		if _, seen := set[key]; !seen {
			order = append(order, key)
		}
		if r.Labels == nil {
			// ラベルは必須項目である。null を送らないよう空のマップにする。
			// 写しの側だけを変えるため、渡された一覧には触れない。
			r.Labels = map[string]string{}
		}
		set[key] = r
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
//
// 検査対象に例外を設けない。SOA と apex NS も投入集合に含まれるため、
// 欠けていれば同じように止まる。DPF が 400 を返す前に、こちら側で気付ける。
func guard(current []dpfapi.OverwriteRecordsInner, set []dpfapi.OverwriteRecordsInner, cs provider.ChangeSet) error {
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

		key, err := keyOf(r.Name, r.Rrtype)
		if err != nil {
			key = recordKey{name: r.Name, rrtype: r.Rrtype}
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
