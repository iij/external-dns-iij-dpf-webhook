package dnsname

import "github.com/miekg/dns"

// Contains は child が n の配下 (n 自身を含む) にあるかをラベル境界で判定する。
//
// 判定には dns.IsSubDomain を用いる。接尾辞一致で代用してはならない。
// strings.HasSuffix("evil-example.jp", "example.jp") は真になり、
// 管理対象外のレコードを管理対象と誤認する。
//
// ゼロ値は何も含まず、ゼロ値は何にも含まれない。判定できない状態を
// 「含む」に倒さないため (原則 VI)。
func (n Name) Contains(child Name) bool {
	if n.IsZero() || child.IsZero() {
		return false
	}
	return dns.IsSubDomain(n.canonical, child.canonical)
}

// Scope は本 provider が操作を許される名前の範囲を表す。
//
// ゼロ値および空の Scope は「範囲なし」を意味し、いかなる名前も含まない。
// これを「全許可」と読み替えてはならない (FR-002)。設定漏れの結果は
// 「何も管理しない」であるべきで、「気付かないうちに全ゾーンを操作できる」で
// あってはならない。
type Scope struct {
	domains []Name
}

// NewScope は与えられたドメインからなる範囲を作る。
// 引数なしで呼ぶと空の範囲になり、何も含まない。
func NewScope(domains ...Name) Scope {
	if len(domains) == 0 {
		return Scope{}
	}

	// ゼロ値は範囲の要素として意味を持たないため落とす。
	kept := make([]Name, 0, len(domains))
	for _, d := range domains {
		if !d.IsZero() {
			kept = append(kept, d)
		}
	}
	if len(kept) == 0 {
		return Scope{}
	}
	return Scope{domains: kept}
}

// IsEmpty は範囲が空であることを報告する。
//
// 空であることは異常ではない。管理対象が設定されていない状態を表す。
// 呼び出し側はこれをログに出して利用者に知らせる。
func (s Scope) IsEmpty() bool { return len(s.domains) == 0 }

// Domains は範囲を構成するドメインを返す。
func (s Scope) Domains() []Name {
	if s.IsEmpty() {
		return nil
	}
	out := make([]Name, len(s.domains))
	copy(out, s.domains)
	return out
}

// Contains は name が範囲に含まれるかを報告する。
// 範囲が空なら常に false を返す。
func (s Scope) Contains(name Name) bool {
	_, ok := s.LongestMatch(name)
	return ok
}

// LongestMatch は name を含むドメインのうち、最もラベル数の多いものを返す。
//
// example.jp と sub.example.jp の双方を管理しているとき、a.sub.example.jp が
// 属するのは sub.example.jp である。書き込み先のゾーンを選ぶ際にこの判定を使う。
//
// 範囲が空の場合、および含むドメインがない場合は ok に false を返す。
func (s Scope) LongestMatch(name Name) (Name, bool) {
	if s.IsEmpty() || name.IsZero() {
		return Name{}, false
	}

	var best Name
	bestLabels := -1
	for _, d := range s.domains {
		if !d.Contains(name) {
			continue
		}
		if n := d.CountLabel(); n > bestLabels {
			best, bestLabels = d, n
		}
	}
	if bestLabels < 0 {
		return Name{}, false
	}
	return best, true
}
