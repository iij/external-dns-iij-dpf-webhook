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

// FR-029: `NS` はゾーンによらず管理対象外である。
//
// ゾーンカットでは、委任の NS (親ゾーン側) と apex の NS (子ゾーン側) が
// 名前も種別も同じまま両側に存在する。ExternalDNS が渡すのは名前と種別だけで
// あり、どちら側かを指す手段がない。区別できないものを推測で更新すると、
// 子ゾーンの権威 NS が親側の値で書き換わる。
//
// 加えて apex の NS は overwrite_zone_apex_ns が常に false のため投入しても
// 取り込まれず、受け付けて適用しないことは FR-012 に反する。
//
// したがって apex かどうかで分岐せず、種別ごと拒否する。
func TestValidateFormat_RejectsNS(t *testing.T) {
	t.Parallel()

	// ゾーン apex、委任、いずれの名前でも同じ扱いになること。
	for _, name := range []string{"example.jp", "sub.example.jp"} {
		for _, cs := range []ChangeSet{
			{Create: []Record{r(name, "NS", 3600, "ns1.example.jp.")}},
			{UpdateTo: []Record{r(name, "NS", 3600, "ns1.example.jp.")}},
			{Delete: []Record{r(name, "NS", 3600, "ns1.example.jp.")}},
		} {
			err := ValidateFormat(cs)
			if err == nil {
				t.Errorf("%s の NS 変更が受け入れられた: %+v", name, cs)
				continue
			}
			if !errors.Is(err, ErrPermanent) {
				t.Errorf("%s: err = %v, want ErrPermanent", name, err)
			}
			if !errors.Is(err, ErrUnsupportedType) {
				t.Errorf("%s: err = %v, want ErrUnsupportedType", name, err)
			}
		}
	}
}

// NS は許可リストに存在しない (FR-026)。
func TestSupportedRecordTypes_ExcludesNS(t *testing.T) {
	t.Parallel()

	for _, rt := range SupportedRecordTypes() {
		if rt == "NS" {
			t.Fatal("NS が許可リストに残っている")
		}
	}
	if IsSupportedRecordType("NS") {
		t.Error("IsSupportedRecordType(\"NS\") = true, want false")
	}
	if _, err := ParseRecordType("NS"); !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("ParseRecordType(\"NS\") = %v, want ErrUnsupportedType", err)
	}
}

// FR-030: CNAME は同一の名前に他の種別と共存できない。
func TestValidate_RejectsCNAMECoexistence(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{
		r("www.example.jp", TypeCNAME, 300, "a.example.jp."),
		r("www.example.jp", TypeA, 300, "192.0.2.1"),
	}}

	err := ValidateFormat(cs)
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

	if err := ValidateFormat(cs); err != nil {
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
		err := ValidateFormat(cs)
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

	if err := ValidateFormat(cs); err != nil {
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
		if err := ValidateFormat(cs); err != nil {
			t.Errorf("MX %q が拒否された: %v", v, err)
		}
	}
	for _, v := range ng {
		cs := ChangeSet{Create: []Record{r("example.jp", TypeMX, 300, v)}}
		if err := ValidateFormat(cs); err == nil {
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
		if err := ValidateFormat(cs); err != nil {
			t.Errorf("SRV %q が拒否された: %v", v, err)
		}
	}
	for _, v := range ng {
		cs := ChangeSet{Create: []Record{r("_sip._tcp.example.jp", TypeSRV, 300, v)}}
		if err := ValidateFormat(cs); err == nil {
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
		if err := ValidateFormat(cs); err != nil {
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
		`"v=spf1 -all"`,
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
	if err := ValidateFormat(cs); err != nil {
		t.Errorf("合計 400 オクテットの TXT が拒否された。合計長は制限しない: %v", err)
	}
}

// 引用符が閉じていない値は恒久的な失敗。解釈できないものは再試行しても通らない。
func TestValidate_TXTRejectsUnparsable(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{r("t.example.jp", TypeTXT, 300, `"unterminated`)}}
	err := ValidateFormat(cs)
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
// DPF は TXT の RDATA に引用符を要求する。
//
// 引用符のない値をそのまま送ると DPF に拒否される (実環境の e2e で 400 を確認)。
// 引用符で囲うのは表現の話であり、character-string の境界は変わらない。
// 空白で分割しないこと (FR-032b) と両立する。
//
// Adjust が同じ関数を通すため、ExternalDNS には引用符付きの値が返る。
// 保存される値と一致するので、差分が振動しない (SC-007)。
func TestNormalizeTXT_QuotesUnquotedValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"v=spf1 -all", `"v=spf1 -all"`},
		{"single", `"single"`},
		{"a b c", `"a b c"`},
	}

	for _, c := range cases {
		got, err := NormalizeTXT(c.in)
		if err != nil {
			t.Errorf("NormalizeTXT(%q) = error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeTXT(%q) = %q, want %q", c.in, got, c.want)
		}

		// 境界は 1 個のまま。空白で分割していないこと (FR-032b)。
		parts, err := SplitTXT(got)
		if err != nil {
			t.Errorf("SplitTXT(%q) = error %v", got, err)
			continue
		}
		if len(parts) != 1 || parts[0] != c.in {
			t.Errorf("SplitTXT(%q) = %q, want [%q]", got, parts, c.in)
		}
	}
}

// 冪等であること (FR-015)。引用符を付けた値を再度通しても変わらない。
func TestNormalizeTXT_QuotingIsIdempotent(t *testing.T) {
	t.Parallel()

	for _, v := range []string{"v=spf1 -all", "single", `"already quoted"`} {
		once, err := NormalizeTXT(v)
		if err != nil {
			t.Errorf("NormalizeTXT(%q) = error %v", v, err)
			continue
		}
		twice, err := NormalizeTXT(once)
		if err != nil {
			t.Errorf("NormalizeTXT(%q) = error %v", once, err)
			continue
		}
		if twice != once {
			t.Errorf("再適用で変化した: %q -> %q -> %q", v, once, twice)
		}
	}
}

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
		if err := ValidateFormat(cs); err != nil {
			t.Errorf("TTL %d が拒否された: %v", ttl, err)
		}
	}
	for _, ttl := range ng {
		cs := ChangeSet{Create: []Record{r("www.example.jp", TypeA, ttl, "192.0.2.1")}}
		err := ValidateFormat(cs)
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

	if err := ValidateFormat(ChangeSet{}); err != nil {
		t.Errorf("空の変更セットが拒否された: %v", err)
	}
}

// 対応リスト外の種別は恒久的な失敗 (FR-028)。
func TestValidate_RejectsUnsupportedType(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{Create: []Record{r("d.example.jp", RecordType("DNAME"), 300, "t.example.jp.")}}

	err := ValidateFormat(cs)
	if err == nil {
		t.Fatal("DNAME が受け入れられた")
	}
	if !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("err = %v, want ErrUnsupportedType", err)
	}
}
