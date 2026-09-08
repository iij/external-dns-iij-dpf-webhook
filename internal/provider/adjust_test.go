// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"strings"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// FR-014: DPF が受け付けない TTL を、受け付ける範囲に補正する。
//
// DPF の TTL は 1〜2147483647。0 は「ゾーンの既定 TTL に委ねる」を表し、
// DPF へは null として送るため補正しない。
func TestAdjust_ClampsTTL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   int
		want int
	}{
		{0, 0},                   // 未指定。ゾーン既定に委ねる
		{1, 1},                   // 下限
		{300, 300},               // 通常
		{2147483647, 2147483647}, // 上限
		{-1, 1},                  // 下限へ補正
		{2147483648, 2147483647}, // 上限へ補正
		{1 << 40, 2147483647},    // 同上
	}

	for _, c := range cases {
		in := []Record{{Name: dnsname.MustParse("www.example.jp"), Type: TypeA, TTL: c.in, Values: []string{"192.0.2.1"}}}
		got := Adjust(in)

		if len(got) != 1 {
			t.Fatalf("TTL %d: 件数 = %d, want 1", c.in, len(got))
		}
		if got[0].TTL != c.want {
			t.Errorf("TTL %d → %d, want %d", c.in, got[0].TTL, c.want)
		}
	}
}

// FR-015: 調整は冪等。調整済みの内容を再度調整しても変わらない。
//
// 冪等でないと、ExternalDNS が毎回「まだ差分がある」と判断し、同じ変更を
// 適用し続ける (SC-007)。
func TestAdjust_Idempotent(t *testing.T) {
	t.Parallel()

	in := []Record{
		{Name: dnsname.MustParse("www.example.jp"), Type: TypeA, TTL: 1 << 40, Values: []string{"192.0.2.1"}},
		{Name: dnsname.MustParse("t.example.jp"), Type: TypeTXT, TTL: 300, Values: []string{`"` + strings.Repeat("a", 600) + `"`}},
		{Name: dnsname.MustParse("m.example.jp"), Type: TypeMX, TTL: 0, Values: []string{"10 mail.example.jp."}},
	}

	once := Adjust(in)
	twice := Adjust(once)

	if len(once) != len(twice) {
		t.Fatalf("件数が変化した: %d → %d", len(once), len(twice))
	}
	for i := range once {
		if once[i].TTL != twice[i].TTL {
			t.Errorf("%s: TTL が再調整で変化した: %d → %d", once[i].Name, once[i].TTL, twice[i].TTL)
		}
		if strings.Join(once[i].Values, "\x00") != strings.Join(twice[i].Values, "\x00") {
			t.Errorf("%s: 値が再調整で変化した:\n  1 回目: %.80q\n  2 回目: %.80q",
				once[i].Name, once[i].Values, twice[i].Values)
		}
	}
}

// 調整の必要がない入力は、そのまま返る。
func TestAdjust_LeavesValidRecordsUnchanged(t *testing.T) {
	t.Parallel()

	in := []Record{
		{Name: dnsname.MustParse("www.example.jp"), Type: TypeA, TTL: 300, Values: []string{"192.0.2.1"}},
		{Name: dnsname.MustParse("t.example.jp"), Type: TypeTXT, TTL: 60, Values: []string{`"part-one" "part-two"`}},
		{Name: dnsname.MustParse("s.example.jp"), Type: TypeTXT, TTL: 60, Values: []string{"v=spf1 -all"}},
	}

	got := Adjust(in)

	if len(got) != len(in) {
		t.Fatalf("件数 = %d, want %d", len(got), len(in))
	}
	for i := range in {
		if got[i].TTL != in[i].TTL {
			t.Errorf("%s: TTL が変化した: %d → %d", in[i].Name, in[i].TTL, got[i].TTL)
		}
		if strings.Join(got[i].Values, "\x00") != strings.Join(in[i].Values, "\x00") {
			t.Errorf("%s: 値が変化した: %q → %q", in[i].Name, in[i].Values, got[i].Values)
		}
	}
}

// 255 オクテットを超える TXT は、分割後の表現に補正して返す。
//
// 補正せずに返すと、ExternalDNS の期待する値と DPF に保存される値が食い違い、
// 毎回差分として検出され続ける (SC-007)。
func TestAdjust_NormalizesOverlongTXT(t *testing.T) {
	t.Parallel()

	long := `"` + strings.Repeat("a", 600) + `"`
	in := []Record{{Name: dnsname.MustParse("t.example.jp"), Type: TypeTXT, TTL: 300, Values: []string{long}}}

	got := Adjust(in)

	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1", len(got))
	}
	if got[0].Values[0] == long {
		t.Fatal("600 オクテットの値が補正されていない")
	}

	parts, err := SplitTXT(got[0].Values[0])
	if err != nil {
		t.Fatalf("補正後の値を解釈できない: %v", err)
	}
	if len(parts) != 3 {
		t.Errorf("分割数 = %d, want 3 (255+255+90)", len(parts))
	}
	for i, p := range parts {
		if len(p) > 255 {
			t.Errorf("分割後の %d 番目が %d オクテット。255 を超えてはならない", i, len(p))
		}
	}
}

// 解釈できない値を含むレコードは、調整せずそのまま返す。
//
// 調整は失敗しない。判断できないものに手を加えるより、そのまま返して
// 適用時の検証に委ねる方が安全である。
func TestAdjust_PassesThroughUnparsableValues(t *testing.T) {
	t.Parallel()

	broken := `"unterminated`
	in := []Record{{Name: dnsname.MustParse("t.example.jp"), Type: TypeTXT, TTL: 300, Values: []string{broken}}}

	got := Adjust(in)

	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1", len(got))
	}
	if got[0].Values[0] != broken {
		t.Errorf("解釈できない値が書き換えられた: %q", got[0].Values[0])
	}
}

// 空の入力は空で返る。nil ではなく空スライス。
func TestAdjust_Empty(t *testing.T) {
	t.Parallel()

	got := Adjust(nil)
	if got == nil {
		t.Error("nil が返った。空スライスを返すこと")
	}
	if len(got) != 0 {
		t.Errorf("件数 = %d, want 0", len(got))
	}
}

// 入力を書き換えない。呼び出し側が渡した値は変化しない。
func TestAdjust_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	in := []Record{{Name: dnsname.MustParse("www.example.jp"), Type: TypeA, TTL: 1 << 40, Values: []string{"192.0.2.1"}}}
	original := in[0].TTL

	Adjust(in)

	if in[0].TTL != original {
		t.Errorf("入力の TTL が書き換えられた: %d → %d", original, in[0].TTL)
	}
}
