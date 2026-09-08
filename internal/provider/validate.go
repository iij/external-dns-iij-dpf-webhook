// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/miekg/dns"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// numericFieldMax は MX の preference、SRV の priority / weight / port の上限。
const numericFieldMax = 65535

// ttlMax は TTL の上限。
//
// RFC 2181 は TTL を符号なし 31 ビットと定める。範囲外の値をそのまま
// 境界へ渡すと桁があふれ、意図しない TTL になる。
const ttlMax = 2147483647

// ValidateFormat は、ゾーンを知らなくても判断できる制約を検証する。
//
// 種別、TTL、名前の形、値の形、および CNAME の排他性が対象。これらは
// バックエンドの状態と無関係に決まるため、**いかなる API 呼び出しより前に**
// 実行する。
//
// 順序が重要である。バックエンド呼び出しの後に置くと、DPF が落ちている間は
// 形式違反が一時的な障害として返る。ExternalDNS はそれを再試行し続けるが、
// 形式違反は何度送っても通らない。
//
// 違反はすべて恒久的な失敗として返す。形式が誤っている要求は、再試行しても
// 同じように失敗する。
func ValidateFormat(cs ChangeSet) error {
	for _, r := range cs.All() {
		if err := validateRecord(r); err != nil {
			return err
		}
	}
	return validateCNAMEExclusivity(cs)
}

// ValidateForZone は、対象ゾーンが分かって初めて判断できる制約を検証する。
//
// 現在はゾーン apex の NS 削除のみが該当する。apex かどうかはゾーン名との
// 比較でしか決まらないため、ゾーンの解決後に実行する。
func ValidateForZone(cs ChangeSet, zone Zone) error {
	return validateApexNSDeletion(cs, zone)
}

// Validate は両方の検証をまとめて行う。ゾーンが確定している場面で使う。
func Validate(cs ChangeSet, zone Zone) error {
	if err := ValidateFormat(cs); err != nil {
		return err
	}
	return ValidateForZone(cs, zone)
}

// validateRecord は 1 件のレコードを検証する。
func validateRecord(r Record) error {
	if !IsSupportedRecordType(string(r.Type)) {
		return fmt.Errorf("%w: %s %s", ErrUnsupportedType, r.Name, r.Type)
	}

	if r.TTL < 0 || r.TTL > ttlMax {
		return fmt.Errorf("%w: %s %s: TTL %d は 0〜%d の範囲でなければなりません",
			ErrPermanent, r.Name, r.Type, r.TTL, ttlMax)
	}

	switch r.Type {
	case TypeA, TypeAAAA:
		return validateNoUnderscore(r)
	case TypeCNAME:
		return validateSingleValue(r)
	case TypeTXT:
		return validateTXT(r)
	case TypeMX:
		return validateNumericPrefix(r, 1, 2)
	case TypeSRV:
		return validateNumericPrefix(r, 3, 4)
	default:
		return nil
	}
}

// validateNoUnderscore は名前にアンダースコアが含まれないことを確かめる。
//
// FR-031: DPF は A / AAAA の名前にアンダースコアを許可しない。
// この制限は A / AAAA のみに適用される。_dmarc の TXT や _sip._tcp の SRV は
// 正当な名前であり、拒否してはならない。
func validateNoUnderscore(r Record) error {
	for _, label := range r.Name.Labels() {
		if strings.Contains(label, "_") {
			return fmt.Errorf(
				"%w: %s %s: 名前にアンダースコアを含む %s レコードは登録できません",
				ErrPermanent, r.Name, r.Type, r.Type)
		}
	}
	return nil
}

// validateSingleValue は値がちょうど 1 つであることを確かめる。
//
// FR-030: CNAME は同一の名前に複数の値を持てない。
func validateSingleValue(r Record) error {
	if len(r.Values) != 1 {
		return fmt.Errorf(
			"%w: %s %s: 値は 1 つでなければなりません (%d 個指定されています)",
			ErrPermanent, r.Name, r.Type, len(r.Values))
	}
	return nil
}

// validateApexNSDeletion はゾーン apex の NS を削除しようとしていないか確かめる。
//
// FR-029: DPF はゾーン名と同じ名前の NS レコードの削除を許さない。
//
// 黙って読み飛ばさず失敗として返すのは、要求された変更を実行しないまま
// 成功を返すことになるためである。それは FR-012 が禁じる「部分的に成功した
// 状態を成功として返す」ことにあたる。
func validateApexNSDeletion(cs ChangeSet, zone Zone) error {
	for _, r := range cs.Delete {
		if r.Type == TypeNS && r.Name == zone.Name {
			return fmt.Errorf(
				"%w: %s NS: ゾーン apex の NS レコードは削除できません",
				ErrPermanent, r.Name)
		}
	}
	return nil
}

// validateCNAMEExclusivity は同一の名前に CNAME と他種別が共存しないことを確かめる。
//
// FR-030: DPF は CNAME を他の種別と同じ名前に置くことを許さない。
// これは DNS の規則でもある。CNAME のある名前には他のデータを置けない。
func validateCNAMEExclusivity(cs ChangeSet) error {
	// 作成・更新の対象だけを見る。削除は共存を生まない。
	added := make(map[dnsname.Name]map[RecordType]bool)
	for _, r := range append(append([]Record{}, cs.Create...), cs.UpdateTo...) {
		if added[r.Name] == nil {
			added[r.Name] = make(map[RecordType]bool)
		}
		added[r.Name][r.Type] = true
	}

	for name, types := range added {
		if !types[TypeCNAME] {
			continue
		}
		if len(types) > 1 {
			return fmt.Errorf(
				"%w: %s: CNAME は他の種別と同じ名前に共存できません",
				ErrPermanent, name)
		}
	}
	return nil
}

// validateTXT は TXT の値が表現形式として解釈できることを確かめる。
//
// 長さによる拒否は行わない。255 オクテットを超える character-string は
// [NormalizeTXT] が分割して受け入れる (FR-032)。長すぎることを理由に
// 拒否するより、分割して受け入れる方が利用者にとって実害が小さい。
//
// 引用符が閉じていないなど、解釈できない値は恒久的な失敗とする。
// 再試行しても同じように失敗する。
func validateTXT(r Record) error {
	for _, v := range r.Values {
		if _, err := SplitTXT(v); err != nil {
			return fmt.Errorf("%w: %s TXT: 値を解釈できません: %w", ErrPermanent, r.Name, err)
		}
	}
	return nil
}

// validateNumericPrefix は値の先頭 n 個の項目が 0〜65535 の整数であることを確かめる。
//
// FR-033: MX の preference、SRV の priority / weight / port が対象。
// want は項目の総数であり、これに満たない値は形式が誤っている。
func validateNumericPrefix(r Record, n, want int) error {
	for _, v := range r.Values {
		fields := strings.Fields(v)
		if len(fields) != want {
			return fmt.Errorf(
				"%w: %s %s: 値の項目数が %d です (%d 個必要): %q",
				ErrPermanent, r.Name, r.Type, len(fields), want, v)
		}
		for i := range n {
			num, err := strconv.Atoi(fields[i])
			if err != nil || num < 0 || num > numericFieldMax {
				return fmt.Errorf(
					"%w: %s %s: 数値項目 %q は 0〜%d の整数でなければなりません",
					ErrPermanent, r.Name, r.Type, fields[i], numericFieldMax)
			}
		}
	}
	return nil
}

// SplitTXT は TXT の値を character-string の並びへ分解する。
//
// TXT の表現形式では、複数の character-string を引用符で区切って並べる。
//
//	"STR1" "STR2"
//
// 解釈は miekg/dns に委ねる。引用符の対応、エスケープ、10 進エスケープの扱いを
// 自前で実装すると、DNS の表現形式との差異がそのままバグになる。
//
// 255 オクテットを超える character-string は、miekg/dns が自動的に分割する。
// 長すぎることを理由に拒否するより、分割して受け入れる方が利用者にとって
// 実害が小さい。
//
// 引用符を含まない値は、全体を 1 つの character-string として扱う。
// そのまま miekg/dns に渡すと空白で分割されてしまうが、TXT の複数
// character-string は受信側で連結して解釈されるため、"v=spf1 -all" が
// "v=spf1" "-all" になると値の意味が変わる (連結すると空白が失われる)。
func SplitTXT(value string) ([]string, error) {
	txt, err := parseTXT(value)
	if err != nil {
		return nil, err
	}
	return txt.Txt, nil
}

// NormalizeTXT は DPF へ送る TXT の値を返す。
//
// character-string がすべて 255 オクテット以下であれば、受け取った値を
// そのまま返す。分割位置とエスケープの表現を変えないため (FR-032a)。
//
// 255 オクテットを超える character-string がある場合に限り、分割後の表現へ
// 書き換える。元の値のまま送っても DPF に拒否されるため、ここで整えないと
// 自動分割の意味がない。
func NormalizeTXT(value string) (string, error) {
	txt, err := parseTXT(value)
	if err != nil {
		return "", err
	}

	// miekg/dns は 255 オクテットを超える character-string を解釈の時点で
	// 分割してしまうため、分割後の長さを見ても元が長すぎたかは分からない。
	// 入力に含まれる引用符区間の数と、解釈結果の数を比べて判断する。
	if len(txt.Txt) <= countQuotedSegments(value) {
		return value, nil
	}

	// String() は "name TTL CLASS TXT <値>" を返す。値の部分だけを取り出す。
	full := txt.String()
	idx := strings.Index(full, "TXT\t")
	if idx < 0 {
		return "", fmt.Errorf("TXT の再直列化に失敗しました: %q", value)
	}
	return full[idx+len("TXT\t"):], nil
}

// countQuotedSegments は値に含まれる character-string の数を数える。
//
// 引用符を含まない値は全体で 1 つ。含む場合は、エスケープされていない
// 引用符の対の数を数える。
func countQuotedSegments(value string) int {
	if !strings.Contains(value, `"`) {
		return 1
	}

	quotes := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++ // 次の 1 文字はエスケープされている
		case '"':
			quotes++
		}
	}
	return quotes / 2
}

// parseTXT は値を TXT レコードとして解釈する。
//
// 名前と TTL は解釈のためだけに与える固定値であり、結果には影響しない。
func parseTXT(value string) (*dns.TXT, error) {
	v := value
	if !strings.Contains(v, `"`) {
		// 引用符で囲って 1 つの character-string にする。
		// バックスラッシュは引用の内側でエスケープ扱いになるため先に退避する。
		v = `"` + strings.ReplaceAll(v, `\\`, `\\\\`) + `"`
	}

	rr, err := dns.NewRR("txt-parse.invalid. 0 IN TXT " + v)
	if err != nil {
		return nil, err
	}
	txt, ok := rr.(*dns.TXT)
	if !ok {
		return nil, fmt.Errorf("TXT として解釈できません: %q", value)
	}
	return txt, nil
}
