// Package dnsname は、ドメイン名を文字列ではなく DNS の名前として扱うための型を提供する。
//
// 本パッケージが存在する理由は、正規化の状態を型で保証することにある。
// constitution v1.4.0 は「正規化済みか否かが呼び出し側に依存する関数を作らない」
// 「正規化状態は型または境界で保証する」を求めている。[Name] を実体化できるのは
// [Parse] だけであり、[Parse] を通った値は必ず正規化名である。この不変条件があるため、
// 比較は値の比較で足り、呼び出しごとに大文字小文字を無視する比較関数を使う必要がない。
//
// 名前の操作は github.com/miekg/dns に委ねる。strings パッケージによる接尾辞一致や
// 区切り文字での分割は、DNS の規則と一致せず、範囲判定をすり抜ける。
package dnsname

import (
	"errors"
	"fmt"
	"strings"

	"github.com/miekg/dns"
)

// ErrInvalidName は、与えられた文字列がドメイン名として妥当でないことを表す。
var ErrInvalidName = errors.New("dnsname: invalid domain name")

// Name は正規化されたドメイン名である。
//
// 正規化名とは小文字かつ末尾ドットで終わる FQDN であり、dns.CanonicalName の出力を指す。
// 実体化できるのは [Parse] 経由のみで、生の文字列から直接構築することはできない。
// 比較可能であり、map のキーとして使える。
type Name struct {
	// canonical は dns.CanonicalName を通した値。ゼロ値は「名前なし」を表す。
	canonical string
}

// Parse は s を正規化名として解釈する。
//
// 大文字小文字と末尾ドットの有無は正規化により吸収されるため、同じ名前を指す
// 異なる表現は同一の [Name] になる。ドメイン名として妥当でない s は
// [ErrInvalidName] を返し、値は実体化されない。
//
// 境界で一度だけ呼ぶこと。内部処理の途中で呼び直す必要はない。
func Parse(s string) (Name, error) {
	// 前後の空白の除去は、ドメイン名としての判定ではなく入力の整形である。
	// 設定ファイルやマウントされたファイルから読んだ値には改行や空白が混ざる。
	// DNS のラベルは空白を含みうるため、これを残したまま解釈すると "   " が
	// 空白 3 文字の単一ラベルとして「妥当」に通ってしまう。
	// constitution v1.4.0 が禁じるのは正規化・比較・包含判定・ラベル分割を
	// strings で行うことであり、解釈前の入力整形はそれに当たらない。
	s = strings.TrimSpace(s)
	if s == "" {
		return Name{}, fmt.Errorf("%w: empty", ErrInvalidName)
	}

	c := dns.CanonicalName(s)
	if _, ok := dns.IsDomainName(c); !ok {
		return Name{}, fmt.Errorf("%w: %q", ErrInvalidName, s)
	}

	return Name{canonical: c}, nil
}

// MustParse は [Parse] と同じだが、失敗した場合に panic する。
// テストと、コンパイル時に妥当性が分かっている定数にのみ用いる。
func MustParse(s string) Name {
	n, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return n
}

// String は正規化名を返す。ゼロ値では空文字列を返す。
//
// 戻り値を判定や比較に使わないこと。比較は [Name] どうしで行う。
func (n Name) String() string { return n.canonical }

// IsZero は名前が実体化されていないことを報告する。
func (n Name) IsZero() bool { return n.canonical == "" }

// Labels は名前をラベルに分割して返す。ルートでは空スライスを返す。
//
// 区切り文字での分割ではなく dns.SplitDomainName を用いる。前者はエスケープされた
// ドットを含むラベルを誤って分割する。
func (n Name) Labels() []string {
	if n.IsZero() {
		return nil
	}
	return dns.SplitDomainName(n.canonical)
}

// CountLabel はラベル数を返す。ルートでは 0 を返す。
//
// ゾーンの最長一致を選ぶ際の比較に用いる。
func (n Name) CountLabel() int {
	if n.IsZero() {
		return 0
	}
	return dns.CountLabel(n.canonical)
}
