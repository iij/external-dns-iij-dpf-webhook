// SPDX-License-Identifier: Apache-2.0

package dnsname_test

import (
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// 包含判定はラベル境界で行う。
//
// strings.HasSuffix(name, "example.jp") は evil-example.jp に一致してしまう。
// この一致が、原則 IV が禁じる「管理対象外への書き込み」を直接引き起こす。
func TestContains_LabelBoundary(t *testing.T) {
	t.Parallel()

	parent := dnsname.MustParse("example.jp")

	cases := []struct {
		child string
		want  bool
	}{
		{"example.jp", true},           // 自分自身は含む
		{"www.example.jp", true},       // 直下
		{"a.b.example.jp", true},       // 深い階層
		{"EXAMPLE.JP", true},           // 大文字小文字を問わない
		{"www.example.jp.", true},      // 末尾ドットの有無を問わない
		{"evil-example.jp", false},     // 接尾辞一致では一致してしまう名前
		{"myexample.jp", false},        // 同上
		{"example.jp.evil.com", false}, // 前方に現れるだけの名前
		{"example.com", false},
		{"jp", false}, // 親は含まれない
	}

	for _, c := range cases {
		child := dnsname.MustParse(c.child)
		if got := parent.Contains(child); got != c.want {
			t.Errorf("%q.Contains(%q) = %v, want %v", parent, child, got, c.want)
		}
	}
}

// ルートはすべての名前を含む。
func TestContains_Root(t *testing.T) {
	t.Parallel()

	root := dnsname.MustParse(".")
	for _, s := range []string{"example.jp", "a.b.c.example.com", "."} {
		if !root.Contains(dnsname.MustParse(s)) {
			t.Errorf("ルートが %q を含まないと判定された", s)
		}
	}
}

// ゼロ値は何も含まず、何にも含まれない。既定を拒否側に倒すため。
func TestContains_ZeroValue(t *testing.T) {
	t.Parallel()

	var zero dnsname.Name
	name := dnsname.MustParse("example.jp")

	if zero.Contains(name) {
		t.Error("ゼロ値が名前を含むと判定された")
	}
	if name.Contains(zero) {
		t.Error("名前がゼロ値を含むと判定された")
	}
}

// Scope は管理対象範囲を表す。
// FR-002: 未設定 (空) の範囲は、いかなる名前も含まない。「空集合 = 全許可」ではない。
func TestScope_EmptyContainsNothing(t *testing.T) {
	t.Parallel()

	empty := dnsname.NewScope()

	for _, s := range []string{"example.jp", "a.example.jp", "."} {
		if empty.Contains(dnsname.MustParse(s)) {
			t.Errorf("空の Scope が %q を含むと判定された。default-deny が破れている", s)
		}
	}
	if !empty.IsEmpty() {
		t.Error("空の Scope の IsEmpty() = false, want true")
	}
}

// 複数のドメインを範囲に持てる。いずれかに含まれれば管理対象。
func TestScope_MultipleDomains(t *testing.T) {
	t.Parallel()

	scope := dnsname.NewScope(
		dnsname.MustParse("example.jp"),
		dnsname.MustParse("example.com"),
	)

	cases := []struct {
		name string
		want bool
	}{
		{"www.example.jp", true},
		{"www.example.com", true},
		{"example.jp", true},
		{"evil-example.jp", false},
		{"example.net", false},
	}

	for _, c := range cases {
		if got := scope.Contains(dnsname.MustParse(c.name)); got != c.want {
			t.Errorf("Scope.Contains(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// 書き込み先のゾーンは最長一致で選ぶ。
// example.jp と sub.example.jp の双方を管理しているとき、
// a.sub.example.jp は sub.example.jp に属する。
func TestScope_LongestMatch(t *testing.T) {
	t.Parallel()

	scope := dnsname.NewScope(
		dnsname.MustParse("example.jp"),
		dnsname.MustParse("sub.example.jp"),
	)

	cases := []struct {
		name string
		want string
		ok   bool
	}{
		{"a.sub.example.jp", "sub.example.jp.", true},
		{"sub.example.jp", "sub.example.jp.", true},
		{"other.example.jp", "example.jp.", true},
		{"example.jp", "example.jp.", true},
		{"example.com", "", false},
		{"evil-example.jp", "", false},
	}

	for _, c := range cases {
		got, ok := scope.LongestMatch(dnsname.MustParse(c.name))
		if ok != c.ok {
			t.Errorf("LongestMatch(%q) ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && got.String() != c.want {
			t.Errorf("LongestMatch(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// 空の範囲では最長一致も成立しない。
func TestScope_LongestMatchOnEmpty(t *testing.T) {
	t.Parallel()

	if _, ok := dnsname.NewScope().LongestMatch(dnsname.MustParse("example.jp")); ok {
		t.Error("空の Scope で LongestMatch が成立した")
	}
}
