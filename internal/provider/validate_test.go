// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"errors"
	"strings"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

func r(name string, t RecordType, ttl int, values ...string) Record {
	return Record{Name: dnsname.MustParse(name), Type: t, TTL: ttl, Values: values}
}

var zoneJP = Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}

// FR-029: ゾーン名と同じ名前の NS レコードを削除しない。
//
// DPF が削除を許さないため、要求を黙って読み飛ばすと「要求されたのに
// 実行しなかった変更」を成功として返すことになる。それは FR-012 が禁じる
// 「部分的に成功した状態を成功として返す」ことにあたるため、失敗として返す。
func TestValidate_RejectsApexNSDeletion(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Delete: []Record{r("example.jp", TypeNS, 3600, "ns1.example.jp.")}}

	err := Validate(cs, zoneJP)
	if err == nil {
		t.Fatal("apex NS の削除要求が受け入れられた")
	}
	if !errors.Is(err, ErrPermanent) {
		t.Errorf("err = %v, want ErrPermanent", err)
	}
}

// apex 以外の NS は削除できる。委任の取り消しは正当な操作である。
func TestValidate_AllowsNonApexNSDeletion(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Delete: []Record{r("sub.example.jp", TypeNS, 3600, "ns1.other.jp.")}}

	if err := Validate(cs, zoneJP); err != nil {
		t.Errorf("apex 以外の NS 削除が拒否された: %v", err)
	}
}

// apex NS の作成・更新も拒否する。
//
// DPF の `atomic_changes` は `overwrite_zone_apex_ns` を常に false で送るため、
// 投入した apex NS は取り込まれない。受け付けておいて適用しないと、
// 要求された変更を実行しないまま成功を返すことになり、FR-012 に反する。
func TestValidate_RejectsApexNSModification(t *testing.T) {
	t.Parallel()

	for _, cs := range []ChangeSet{
		{UpdateTo: []Record{r("example.jp", TypeNS, 3600, "ns1.example.jp.")}},
		{Create: []Record{r("example.jp", TypeNS, 3600, "ns1.example.jp.")}},
		{Delete: []Record{r("example.jp", TypeNS, 3600, "ns1.example.jp.")}},
	} {
		err := Validate(cs, zoneJP)
		if err == nil {
			t.Errorf("apex NS の変更が受け入れられた: %+v", cs)
			continue
		}
		if !errors.Is(err, ErrPermanent) {
			t.Errorf("err = %v, want ErrPermanent", err)
		}
	}
}

// FR-030: CNAME は同一の名前に複数の値を持てない。
func TestValidate_RejectsCNAMEWithMultipleValues(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{
		r("www.example.jp", TypeCNAME, 300, "a.example.jp.", "b.example.jp."),
	}}

	err := Validate(cs, zoneJP)
	if err == nil {
		t.Fatal("複数値の CNAME が受け入れられた")
	}
	if !errors.Is(err, ErrPermanent) {
		t.Errorf("err = %v, want ErrPermanent", err)
	}
}

// FR-030: CNAME は同一の名前に他の種別と共存できない。
func TestValidate_RejectsCNAMECoexistence(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{
		r("www.example.jp", TypeCNAME, 300, "a.example.jp."),
		r("www.example.jp", TypeA, 300, "192.0.2.1"),
	}}

	err := Validate(cs, zoneJP)
	if err == nil {
		t.Fatal("CNAME と A の共存が受け入れられた")
	}
	if !errors.Is(err, ErrPermanent) {
		t.Errorf("err = %v, want ErrPermanent", err)
	}
}

// 別々の名前なら CNAME と A は共存できる。
func TestValidate_AllowsCNAMEAndAOnDifferentNames(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{
		r("a.example.jp", TypeCNAME, 300, "t.example.jp."),
		r("b.example.jp", TypeA, 300, "192.0.2.1"),
	}}

	if err := Validate(cs, zoneJP); err != nil {
		t.Errorf("別名での共存が拒否された: %v", err)
	}
}

// FR-031: 名前にアンダースコアを含む A / AAAA は登録できない。
//
// DPF が許可しないため、送る前に止める。無駄な API 呼び出しで
// レート制限を消費しないためでもある。
func TestValidate_RejectsUnderscoreInAddressRecords(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		rtype  RecordType
		target string
	}{
		{"_dmarc.example.jp", TypeA, "192.0.2.1"},
		{"a._b.example.jp", TypeA, "192.0.2.1"},
		{"_svc.example.jp", TypeAAAA, "2001:db8::1"},
	}

	for _, c := range cases {
		cs := ChangeSet{Create: []Record{r(c.name, c.rtype, 300, c.target)}}
		err := Validate(cs, zoneJP)
		if err == nil {
			t.Errorf("%s %s が受け入れられた", c.name, c.rtype)
			continue
		}
		if !errors.Is(err, ErrPermanent) {
			t.Errorf("%s: err = %v, want ErrPermanent", c.name, err)
		}
	}
}

// アンダースコアの制限は A / AAAA のみ。TXT や SRV には適用しない。
func TestValidate_AllowsUnderscoreForOtherTypes(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{
		r("_dmarc.example.jp", TypeTXT, 300, `"v=DMARC1; p=none"`),
		r("_sip._tcp.example.jp", TypeSRV, 300, "10 60 5060 sip.example.jp."),
	}}

	if err := Validate(cs, zoneJP); err != nil {
		t.Errorf("A/AAAA 以外でアンダースコアが拒否された: %v", err)
	}
}

// FR-033: MX の preference は 0〜65535。
func TestValidate_MXPreferenceRange(t *testing.T) {
	t.Parallel()

	ok := []string{"0 mail.example.jp.", "65535 mail.example.jp.", "10 mail.example.jp."}
	ng := []string{"-1 mail.example.jp.", "65536 mail.example.jp.", "abc mail.example.jp.", "10"}

	for _, v := range ok {
		cs := ChangeSet{Create: []Record{r("example.jp", TypeMX, 300, v)}}
		if err := Validate(cs, zoneJP); err != nil {
			t.Errorf("MX %q が拒否された: %v", v, err)
		}
	}
	for _, v := range ng {
		cs := ChangeSet{Create: []Record{r("example.jp", TypeMX, 300, v)}}
		if err := Validate(cs, zoneJP); err == nil {
			t.Errorf("MX %q が受け入れられた", v)
		}
	}
}

// FR-033: SRV の priority / weight / port は 0〜65535。
func TestValidate_SRVNumericRange(t *testing.T) {
	t.Parallel()

	ok := []string{"0 0 0 sip.example.jp.", "65535 65535 65535 sip.example.jp."}
	ng := []string{
		"65536 0 0 sip.example.jp.",
		"0 65536 0 sip.example.jp.",
		"0 0 65536 sip.example.jp.",
		"-1 0 0 sip.example.jp.",
		"0 0 sip.example.jp.", // 項目数が足りない
	}

	for _, v := range ok {
		cs := ChangeSet{Create: []Record{r("_sip._tcp.example.jp", TypeSRV, 300, v)}}
		if err := Validate(cs, zoneJP); err != nil {
			t.Errorf("SRV %q が拒否された: %v", v, err)
		}
	}
	for _, v := range ng {
		cs := ChangeSet{Create: []Record{r("_sip._tcp.example.jp", TypeSRV, 300, v)}}
		if err := Validate(cs, zoneJP); err == nil {
			t.Errorf("SRV %q が受け入れられた", v)
		}
	}
}

// FR-032: 255 オクテットを超える character-string は拒否せず、自動的に分割する。
//
// 長すぎることを理由に止めるより、分割して受け入れる方が利用者にとって
// 実害が小さい。
func TestValidate_TXTOverlongIsAccepted(t *testing.T) {
	t.Parallel()

	for _, n := range []int{255, 256, 600} {
		value := `"` + strings.Repeat("a", n) + `"`
		cs := ChangeSet{Create: []Record{r("t.example.jp", TypeTXT, 300, value)}}
		if err := Validate(cs, zoneJP); err != nil {
			t.Errorf("%d オクテットの character-string が拒否された: %v", n, err)
		}
	}
}

// 255 を超える値は、DPF へ送る前に分割された表現へ書き換える。
//
// 元の値のまま送ると DPF に拒否されるため、ここで整えないと自動分割の
// 意味がない。
func TestNormalizeTXT_SplitsOverlong(t *testing.T) {
	t.Parallel()

	value := `"` + strings.Repeat("a", 256) + `"`

	got, err := NormalizeTXT(value)
	if err != nil {
		t.Fatalf("NormalizeTXT = error %v", err)
	}
	if got == value {
		t.Fatal("256 オクテットの値が書き換えられていない")
	}

	parts, err := SplitTXT(got)
	if err != nil {
		t.Fatalf("分割後の値を解釈できない: %v", err)
	}
	if len(parts) != 2 || len(parts[0]) != 255 || len(parts[1]) != 1 {
		t.Errorf("分割結果 = %d 個 (長さ %v), want [255 1]", len(parts), lengths(parts))
	}
}

// 255 以下の値はそのまま返す。分割位置とエスケープの表現を変えない (FR-032a)。
func TestNormalizeTXT_LeavesValidValuesUnchanged(t *testing.T) {
	t.Parallel()

	cases := []string{
		`"a" "b"`,
		`"only"`,
		`"has space" "second"`,
		`"esc\"aped"`,
		"v=spf1 -all",
		`"` + strings.Repeat("k", 200) + `" "` + strings.Repeat("k", 200) + `"`,
	}

	for _, v := range cases {
		got, err := NormalizeTXT(v)
		if err != nil {
			t.Errorf("NormalizeTXT(%.30q) = error %v", v, err)
			continue
		}
		if got != v {
			t.Errorf("NormalizeTXT(%.30q) が値を書き換えた: %.60q", v, got)
		}
	}
}

// FR-032: 複数の character-string の合計が 255 を超えるのは正常。
//
// DKIM 鍵のような長い値がこれに該当する。合計長を理由に失敗としてはならない。
func TestValidate_TXTMultipleStringsMayExceed255InTotal(t *testing.T) {
	t.Parallel()

	part := strings.Repeat("k", 200)
	value := `"` + part + `" "` + part + `"` // 合計 400 オクテット

	cs := ChangeSet{Create: []Record{r("dkim._domainkey.example.jp", TypeTXT, 300, value)}}
	if err := Validate(cs, zoneJP); err != nil {
		t.Errorf("合計 400 オクテットの TXT が拒否された。合計長は制限しない: %v", err)
	}
}

// 引用符が閉じていない値は恒久的な失敗。解釈できないものは再試行しても通らない。
func TestValidate_TXTRejectsUnparsable(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{r("t.example.jp", TypeTXT, 300, `"unterminated`)}}
	err := Validate(cs, zoneJP)
	if err == nil {
		t.Fatal("引用符が閉じていない値が受け入れられた")
	}
	if !errors.Is(err, ErrPermanent) {
		t.Errorf("err = %v, want ErrPermanent", err)
	}
}

func lengths(parts []string) []int {
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		out = append(out, len(p))
	}
	return out
}

// FR-032a: character-string の分割位置を保つ。
//
// 読み取った値を書き戻したとき、分割が変化しない。連結すると 255 制限に
// 抵触して登録できなくなり、分割位置を変えると受信側が解釈する値が変わる。
func TestSplitTXT_PreservesSplit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want []string
	}{
		{`"a" "b"`, []string{"a", "b"}},
		{`"only"`, []string{"only"}},
		{`"has space" "second"`, []string{"has space", "second"}},
		{`"esc\"aped"`, []string{`esc\"aped`}},
	}

	for _, c := range cases {
		got, err := SplitTXT(c.in)
		if err != nil {
			t.Errorf("SplitTXT(%q) = error %v", c.in, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("SplitTXT(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("SplitTXT(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// 引用符のない TXT 値も 1 つの character-string として扱う。
// 送信側が引用符を付けずに送ってくる場合がある。
func TestSplitTXT_UnquotedIsSingleString(t *testing.T) {
	t.Parallel()

	got, err := SplitTXT("v=spf1 -all")
	if err != nil {
		t.Fatalf("SplitTXT = error %v", err)
	}
	if len(got) != 1 || got[0] != "v=spf1 -all" {
		t.Errorf("SplitTXT = %q, want [\"v=spf1 -all\"]", got)
	}
}

// TTL は 0〜2147483647 の範囲に収まらなければならない。
//
// RFC 2181 は TTL を符号なし 31 ビットと定める。範囲外の値を受け取ったまま
// DPF へ送ると、境界で桁があふれて意図しない TTL になる。
func TestValidate_TTLRange(t *testing.T) {
	t.Parallel()

	ok := []int{0, 1, 300, 2147483647}
	ng := []int{-1, 2147483648, 1 << 40}

	for _, ttl := range ok {
		cs := ChangeSet{Create: []Record{r("www.example.jp", TypeA, ttl, "192.0.2.1")}}
		if err := Validate(cs, zoneJP); err != nil {
			t.Errorf("TTL %d が拒否された: %v", ttl, err)
		}
	}
	for _, ttl := range ng {
		cs := ChangeSet{Create: []Record{r("www.example.jp", TypeA, ttl, "192.0.2.1")}}
		err := Validate(cs, zoneJP)
		if err == nil {
			t.Errorf("TTL %d が受け入れられた", ttl)
			continue
		}
		if !errors.Is(err, ErrPermanent) {
			t.Errorf("TTL %d: err = %v, want ErrPermanent", ttl, err)
		}
	}
}

// 空の変更セットは検証を通る。
func TestValidate_EmptyChangeSet(t *testing.T) {
	t.Parallel()

	if err := Validate(ChangeSet{}, zoneJP); err != nil {
		t.Errorf("空の変更セットが拒否された: %v", err)
	}
}

// 対応リスト外の種別は恒久的な失敗 (FR-028)。
func TestValidate_RejectsUnsupportedType(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{r("d.example.jp", RecordType("DNAME"), 300, "t.example.jp.")}}

	err := Validate(cs, zoneJP)
	if err == nil {
		t.Fatal("DNAME が受け入れられた")
	}
	if !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("err = %v, want ErrUnsupportedType", err)
	}
}
