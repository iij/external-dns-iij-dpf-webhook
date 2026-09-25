// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"fmt"

	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// レコード種別の対応付け。
//
// DPF が定義する種別は 16 個、ExternalDNS が表現できる種別は 10 個である。
// 交差は 9 個だが、本サービスが扱うのは NS を除いた 8 個である (FR-026)。
//
//	対応 (8)          : A AAAA CNAME TXT SRV PTR MX NAPTR
//	交差だが除外 (1)  : NS                                → 管理対象外 (FR-029、research R12)
//	DPF のみ (7)      : SOA CAA DS HTTPS SVCB TLSA ANAME  → 管理対象外 (FR-027)
//	ExternalDNS のみ  : DNAME                             → 適用しない (FR-028)
//
// 対応表は許可リストであり、ここに載っていない種別は暗黙に通過しない (FR-025)。
var typeToDPF = map[provider.RecordType]dpfapi.RecordsRrtype{
	provider.TypeA:     dpfapi.RECORDSRRTYPE_A,
	provider.TypeAAAA:  dpfapi.RECORDSRRTYPE_AAAA,
	provider.TypeCNAME: dpfapi.RECORDSRRTYPE_CNAME,
	provider.TypeTXT:   dpfapi.RECORDSRRTYPE_TXT,
	provider.TypeSRV:   dpfapi.RECORDSRRTYPE_SRV,
	provider.TypePTR:   dpfapi.RECORDSRRTYPE_PTR,
	provider.TypeMX:    dpfapi.RECORDSRRTYPE_MX,
	provider.TypeNAPTR: dpfapi.RECORDSRRTYPE_NAPTR,
}

// typeFromDPF は typeToDPF の逆写像。init で組み立てる。
var typeFromDPF = func() map[dpfapi.RecordsRrtype]provider.RecordType {
	m := make(map[dpfapi.RecordsRrtype]provider.RecordType, len(typeToDPF))
	for k, v := range typeToDPF {
		m[v] = k
	}
	return m
}()

// toDPF は本サービスの種別を DPF の種別へ変換する。
//
// 許可リストにない種別は [provider.ErrUnsupportedType] を返す。DPF へ送る前に
// ここで止めることで、無駄な API 呼び出しとレート制限の消費を避ける。
func toDPF(t provider.RecordType) (dpfapi.RecordsRrtype, error) {
	d, ok := typeToDPF[t]
	if !ok {
		return "", fmt.Errorf("%w: %w: %q", provider.ErrPermanent, provider.ErrUnsupportedType, t)
	}
	return d, nil
}

// fromDPF は DPF の種別を本サービスの種別へ変換する。
//
// ok が false のとき、その種別は管理対象外である。レコード一覧から除外し、
// 変更・削除の対象にもしない (FR-027)。エラーではなく ok で返すのは、
// 管理対象外の種別が DPF 上に存在すること自体は正常だからである。
func fromDPF(d dpfapi.RecordsRrtype) (provider.RecordType, bool) {
	t, ok := typeFromDPF[d]
	return t, ok
}
