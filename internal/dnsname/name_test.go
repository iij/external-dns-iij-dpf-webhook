// SPDX-License-Identifier: Apache-2.0

package dnsname_test

import (
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// 境界で受け取った表現がどうであれ、実体化された名前は正規化名になる。
// constitution v1.4.0: 内部で保持する名前は小文字かつ末尾ドットで終わる FQDN とする。
func TestParse_Canonicalizes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"example.jp", "example.jp."},
		{"example.jp.", "example.jp."},
		{"EXAMPLE.JP", "example.jp."},
		{"WWW.Example.JP.", "www.example.jp."},
		{"a.b.example.jp", "a.b.example.jp."},
		{"*.example.jp", "*.example.jp."},
	}

	for _, c := range cases {
		got, err := dnsname.Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q) = error %v, want success", c.in, err)
		}
		if got.String() != c.want {
			t.Errorf("Parse(%q).String() = %q, want %q", c.in, got.String(), c.want)
		}
	}
}

// 末尾ドットの有無と大文字小文字の違いは、同一の値に収束する。
// これが崩れると、同じ名前が別物として扱われ、レコードの重複作成や削除漏れになる。
func TestParse_EquivalentInputsProduceEqualValues(t *testing.T) {
	t.Parallel()

	groups := [][]string{
		{"example.jp", "example.jp.", "EXAMPLE.jp", "Example.JP."},
		{"www.example.jp", "WWW.EXAMPLE.JP."},
	}

	for _, g := range groups {
		first, err := dnsname.Parse(g[0])
		if err != nil {
			t.Fatalf("Parse(%q) = error %v", g[0], err)
		}
		for _, in := range g[1:] {
			got, err := dnsname.Parse(in)
			if err != nil {
				t.Fatalf("Parse(%q) = error %v", in, err)
			}
			if got != first {
				t.Errorf("Parse(%q) = %q, want equal to Parse(%q) = %q", in, got, g[0], first)
			}
		}
	}
}

// 妥当でない名前は実体化できない。
// FR: 外部から受け取った名前は使用前に妥当性を検証し、失敗した名前を管理対象としない。
func TestParse_RejectsInvalid(t *testing.T) {
	t.Parallel()

	// ラベルが 63 オクテットを超える名前。
	tooLongLabel := ""
	for range 64 {
		tooLongLabel += "a"
	}

	cases := []struct {
		name string
		in   string
	}{
		{"空文字列", ""},
		{"空白のみ", "   "},
		{"連続するドット", "a..example.jp"},
		{"63 オクテットを超えるラベル", tooLongLabel + ".example.jp"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := dnsname.Parse(c.in); err == nil {
				t.Errorf("Parse(%q) = success, want error", c.in)
			}
		})
	}
}

// ルートは正規化名として成立する。
func TestParse_Root(t *testing.T) {
	t.Parallel()

	got, err := dnsname.Parse(".")
	if err != nil {
		t.Fatalf("Parse(\".\") = error %v, want success", err)
	}
	if got.String() != "." {
		t.Errorf("Parse(\".\").String() = %q, want %q", got.String(), ".")
	}
}

// ゼロ値は名前として扱えない。生の文字列から直接構築させないための性質。
func TestZeroValue_IsNotUsable(t *testing.T) {
	t.Parallel()

	var zero dnsname.Name
	if !zero.IsZero() {
		t.Error("ゼロ値の IsZero() = false, want true")
	}
	if zero.String() != "" {
		t.Errorf("ゼロ値の String() = %q, want %q", zero.String(), "")
	}
}

// 実体化された名前は必ず正規化済みである。この不変条件があるため、比較は値の比較で足りる。
func TestParse_ResultIsIdempotent(t *testing.T) {
	t.Parallel()

	first, err := dnsname.Parse("Example.JP")
	if err != nil {
		t.Fatalf("Parse = error %v", err)
	}
	second, err := dnsname.Parse(first.String())
	if err != nil {
		t.Fatalf("再パース = error %v", err)
	}
	if first != second {
		t.Errorf("再パースで値が変化した: %q → %q", first, second)
	}
}

// ラベル分割は miekg/dns に委ねる。区切り文字での分割はエスケープされたラベルを壊す。
func TestName_Labels(t *testing.T) {
	t.Parallel()

	n, err := dnsname.Parse("a.b.example.jp")
	if err != nil {
		t.Fatalf("Parse = error %v", err)
	}

	want := []string{"a", "b", "example", "jp"}
	got := n.Labels()
	if len(got) != len(want) {
		t.Fatalf("Labels() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Labels()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if n.CountLabel() != 4 {
		t.Errorf("CountLabel() = %d, want 4", n.CountLabel())
	}
}

// ルートのラベル数は 0。
func TestName_LabelsOfRoot(t *testing.T) {
	t.Parallel()

	root, err := dnsname.Parse(".")
	if err != nil {
		t.Fatalf("Parse = error %v", err)
	}
	if got := root.CountLabel(); got != 0 {
		t.Errorf("ルートの CountLabel() = %d, want 0", got)
	}
}

// Unqualified は末尾ドットだけを落とす。
//
// ExternalDNS の TXT レジストリが素の文字列一致で照合する箇所があるため、
// 応答の表記は ExternalDNS 側の表記に揃える必要がある (004 の実環境で確認)。
func TestName_Unqualified(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"www.example.jp", "www.example.jp"},
		{"www.example.jp.", "www.example.jp"},
		// 小文字化は保つ。落とすのは末尾ドットだけである。
		{"WWW.Example.JP.", "www.example.jp"},
		{"example.jp", "example.jp"},
		{"a.b.c.example.jp.", "a.b.c.example.jp"},
	}

	for _, c := range cases {
		got := dnsname.MustParse(c.in).Unqualified()
		if got != c.want {
			t.Errorf("dnsname.MustParse(%q).Unqualified() = %q, want %q", c.in, got, c.want)
		}
	}
}

// ゼロ値は空文字列を返す。
func TestName_UnqualifiedZero(t *testing.T) {
	t.Parallel()

	var zero dnsname.Name
	if got := zero.Unqualified(); got != "" {
		t.Errorf("ゼロ値の Unqualified = %q, want 空文字列", got)
	}
}

// 正規化名そのものは末尾ドットを保つ。DPF へはこちらを渡す。
func TestName_StringKeepsTrailingDot(t *testing.T) {
	t.Parallel()

	n := dnsname.MustParse("www.example.jp")
	if n.String() != "www.example.jp." {
		t.Errorf("String() = %q, want %q", n.String(), "www.example.jp.")
	}
	if n.Unqualified() == n.String() {
		t.Error("String() と Unqualified() が同じ表記になっている")
	}
}
