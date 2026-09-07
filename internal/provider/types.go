// SPDX-License-Identifier: Apache-2.0

// Package provider は本サービスのドメインロジックを担う。
//
// 管理対象範囲の判定、変更セットの検証、DPF の制約に合わせた調整がここに属する。
// DPF のトランスポート詳細 (URL、ヘッダ、生成された API 型、ライブラリ固有のエラー) を
// 本パッケージが参照することはない。境界は [ports.go] のインタフェースで表す (原則 II)。
package provider

import (
	"fmt"
	"slices"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// RecordType は本サービスが扱うレコード種別。
//
// 許可リスト方式であり、[ParseRecordType] を通らない種別は実体化できない。
// 未知の種別を暗黙に通過させないため (FR-025)。
type RecordType string

// 対応するレコード種別。
//
// ExternalDNS が表現できる種別 (KnownRecordTypes) と DPF が提供する種別の交差である
// 9 種別 (FR-026)。DPF のみが持つ SOA/CAA/DS/HTTPS/SVCB/TLSA/ANAME は
// ExternalDNS 側に対応表現がなく、ExternalDNS のみが持つ DNAME は DPF 側にない。
const (
	TypeA     RecordType = "A"
	TypeAAAA  RecordType = "AAAA"
	TypeCNAME RecordType = "CNAME"
	TypeTXT   RecordType = "TXT"
	TypeSRV   RecordType = "SRV"
	TypeNS    RecordType = "NS"
	TypePTR   RecordType = "PTR"
	TypeMX    RecordType = "MX"
	TypeNAPTR RecordType = "NAPTR"
)

// supportedRecordTypes は対応する種別の許可リスト。
var supportedRecordTypes = []RecordType{
	TypeA, TypeAAAA, TypeCNAME, TypeTXT, TypeSRV, TypeNS, TypePTR, TypeMX, TypeNAPTR,
}

// SupportedRecordTypes は対応する種別を返す。
func SupportedRecordTypes() []RecordType {
	return slices.Clone(supportedRecordTypes)
}

// ParseRecordType は s を対応レコード種別として解釈する。
// 許可リストにない種別は [ErrUnsupportedType] を返す。
func ParseRecordType(s string) (RecordType, error) {
	t := RecordType(s)
	if !slices.Contains(supportedRecordTypes, t) {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedType, s)
	}
	return t, nil
}

// IsSupportedRecordType は s が対応する種別かを報告する。
func IsSupportedRecordType(s string) bool {
	return slices.Contains(supportedRecordTypes, RecordType(s))
}

func (t RecordType) String() string { return string(t) }

// Zone は本 provider が操作するゾーン。
//
// ゾーンは DPF 上に事前に作成されていることを前提とする。本 provider は
// ゾーンを作成しない (FR-013)。
type Zone struct {
	// Name はゾーン名 (正規化名)。
	Name dnsname.Name

	// ID は DPF がゾーンを識別する値。境界の内側でのみ意味を持つ。
	ID string
}

// Record は 1 つの DNS レコード。
//
// Name は正規化名であり、Type は許可リストの種別に限られる。この 2 つが型で
// 保証されているため、上位層は表現の揺れを気にせず比較できる。
type Record struct {
	Name dnsname.Name
	Type RecordType

	// TTL は秒。0 はゾーンの既定 TTL に委ねることを表す。
	TTL int

	// Values はレコードの値。TXT の 1 要素は、複数の character-string を
	// 引用符で区切った表現形式をそのまま保持する。分割位置を変えないこと (FR-032a)。
	Values []string
}

// Key はレコードを一意に定める組を返す。名前と種別の対で同一性を判断する。
func (r Record) Key() RecordKey {
	return RecordKey{Name: r.Name, Type: r.Type}
}

// RecordKey は名前と種別の対。マージと差分の突き合わせに用いる。
type RecordKey struct {
	Name dnsname.Name
	Type RecordType
}

func (k RecordKey) String() string {
	return fmt.Sprintf("%s %s", k.Name, k.Type)
}

// ChangeSet は ExternalDNS が算出した変更のまとまり。
//
// 適用は変更セット単位で成否が決まる。部分的に成功した状態を成功として
// 扱ってはならない (FR-012)。
type ChangeSet struct {
	// Create は新たに登録するレコード。
	Create []Record

	// UpdateTo は置き換え後のレコード。
	//
	// 置き換え前の値は保持しない。適用時点の現在値が置き換え前と食い違っていても、
	// 置き換え後を適用する。一致を要求すると差分が解消せず振動するため (SC-007)。
	UpdateTo []Record

	// Delete は取り除くレコード。
	Delete []Record
}

// IsEmpty は変更が 1 件もないことを報告する。
// 空の変更セットは、何も変更せずに成功として扱う。
func (c ChangeSet) IsEmpty() bool {
	return len(c.Create) == 0 && len(c.UpdateTo) == 0 && len(c.Delete) == 0
}

// All は変更セットに含まれる全レコードを返す。範囲判定と検証の対象を列挙するために用いる。
func (c ChangeSet) All() []Record {
	out := make([]Record, 0, len(c.Create)+len(c.UpdateTo)+len(c.Delete))
	out = append(out, c.Create...)
	out = append(out, c.UpdateTo...)
	out = append(out, c.Delete...)
	return out
}
