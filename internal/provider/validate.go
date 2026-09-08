// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// txtMaxOctets は TXT の character-string 1 個の上限。
//
// RFC 1035 が定める長さ制限であり、DPF のマニュアルにも同じ値が記載されている。
// 1 つの TXT レコードは複数の character-string を持てるため、合計がこの値を
// 超えることは正常である (FR-032)。
const txtMaxOctets = 255

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

// validateTXT は TXT の character-string の長さを検証する。
//
// FR-032: 制限は character-string 1 個あたり 255 オクテット。複数の
// character-string の合計がこれを超えるのは正常であり、合計長を理由に
// 失敗としてはならない (DKIM 鍵などが該当する)。
//
// 長さはオクテットで数える。文字数ではない。マルチバイト文字では両者が食い違う。
func validateTXT(r Record) error {
	for _, v := range r.Values {
		parts, err := SplitTXT(v)
		if err != nil {
			return fmt.Errorf("%w: %s TXT: 値を解釈できません: %w", ErrPermanent, r.Name, err)
		}
		for _, p := range parts {
			if len(p) > txtMaxOctets {
				return fmt.Errorf(
					"%w: %s TXT: character-string が %d オクテットあります (上限 %d)。"+
						"複数の character-string に分割してください",
					ErrPermanent, r.Name, len(p), txtMaxOctets)
			}
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
// 引用符を含まない値は、1 つの character-string として扱う。送信側が
// 引用符を付けずに送ってくる場合があるため。
//
// miekg/dns の RR 解釈を用いない。同ライブラリは 255 オクテットを超える
// character-string を自動的に分割するため、長さ違反が検出できなくなり、
// 分割位置を保つ要件 (FR-032a) にも反する。ここで必要なのは「与えられた
// 表現をそのまま分解すること」であり、正規化ではない。
//
// エスケープ (\\ と \" および \DDD) は復号する。長さはエスケープを解いた
// オクテット数で数えるため。
func SplitTXT(value string) ([]string, error) {
	if !strings.Contains(value, `"`) {
		return []string{value}, nil
	}

	var (
		out     []string
		cur     strings.Builder
		inQuote bool
	)

	for i := 0; i < len(value); i++ {
		c := value[i]

		switch {
		case c == '\\':
			if i+1 >= len(value) {
				return nil, fmt.Errorf("末尾のバックスラッシュが閉じていません")
			}
			next := value[i+1]
			// \DDD は 10 進 3 桁でオクテットを表す。
			if isDigit(next) && i+3 < len(value) && isDigit(value[i+2]) && isDigit(value[i+3]) {
				n, err := strconv.Atoi(value[i+1 : i+4])
				if err != nil || n < 0 || n > 255 {
					return nil, fmt.Errorf("10 進エスケープが不正です: %q", value[i:i+4])
				}
				cur.WriteByte(byte(n))
				i += 3
				continue
			}
			cur.WriteByte(next)
			i++

		case c == '"':
			if inQuote {
				out = append(out, cur.String())
				cur.Reset()
			}
			inQuote = !inQuote

		case inQuote:
			cur.WriteByte(c)

		default:
			// 引用符の外側の空白は区切り。それ以外の文字は表現形式として不正。
			if c != ' ' && c != '\t' {
				return nil, fmt.Errorf("引用符の外に文字があります: %q", value)
			}
		}
	}

	if inQuote {
		return nil, fmt.Errorf("引用符が閉じていません: %q", value)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("character-string がありません: %q", value)
	}
	return out, nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
