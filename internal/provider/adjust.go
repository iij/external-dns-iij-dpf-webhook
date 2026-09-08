// SPDX-License-Identifier: Apache-2.0

package provider

// DPF が受け付ける TTL の範囲。
//
// DPF の API スキーマは TTL を nullable な整数とし、1〜2147483647 を許す。
// 0 は範囲外であり、「ゾーンの既定 TTL に委ねる」意味を持たせて null として送る。
const (
	ttlMin = 1
	ttlMax = 2147483647

	// ttlUnset は TTL を指定しないことを表す。DPF へは null として送られ、
	// ゾーンの既定 TTL が使われる。ExternalDNS が recordTTL を省略した場合に
	// この値になる。
	ttlUnset = 0
)

// Adjust は ExternalDNS が算出したレコードを、DPF に保存される形へ整えて返す。
//
// この操作の目的は、ExternalDNS が期待する値と DPF に実際に保存される値を
// 一致させることにある。両者が食い違うと、ExternalDNS は毎回「まだ差分がある」と
// 判断し、同じ変更を適用し続ける (SC-007)。
//
// 冪等でなければならない (FR-015)。調整済みの内容を再度調整しても結果は変わらない。
//
// 失敗しない。解釈できない値には手を加えず、そのまま返す。判断できないものを
// 書き換えるより、適用時の検証に委ねる方が安全である。
//
// 入力は書き換えない。
func Adjust(records []Record) []Record {
	out := make([]Record, 0, len(records))

	for _, r := range records {
		adjusted := r
		adjusted.TTL = clampTTL(r.TTL)
		adjusted.Values = adjustValues(r)
		out = append(out, adjusted)
	}

	return out
}

// clampTTL は TTL を DPF が受け付ける範囲へ収める。
//
// 0 は「未指定」を表すためそのまま残す。負の値は下限へ、上限超過は上限へ寄せる。
//
// 補正であって拒否ではないのは、この操作が「DPF に保存される形を返す」ための
// ものだからである。範囲外のまま返すと、ExternalDNS はその値が保存されると
// 期待し、実際には保存されず差分が残り続ける。
func clampTTL(ttl int) int {
	switch {
	case ttl == ttlUnset:
		return ttlUnset
	case ttl < ttlMin:
		return ttlMin
	case ttl > ttlMax:
		return ttlMax
	default:
		return ttl
	}
}

// adjustValues は値を DPF に保存される形へ整える。
//
// 現在の対象は TXT のみ。255 オクテットを超える character-string は分割される
// ため、分割後の表現を返す。返さないと、ExternalDNS の期待する値と保存される値が
// 食い違い、毎回差分として検出され続ける。
func adjustValues(r Record) []string {
	if r.Type != TypeTXT {
		return r.Values
	}

	out := make([]string, 0, len(r.Values))
	changed := false
	for _, v := range r.Values {
		n, err := NormalizeTXT(v)
		if err != nil {
			// 解釈できない値には手を加えない。適用時の検証で恒久的な失敗になる。
			out = append(out, v)
			continue
		}
		if n != v {
			changed = true
		}
		out = append(out, n)
	}

	if !changed {
		return r.Values
	}
	return out
}
