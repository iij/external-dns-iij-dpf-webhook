// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"

	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// ListRecords は zone の反映済みレコードを全件返す。
//
// 「反映済み」を使うのは、公開中の状態こそが上位層の求めるものだからである。
// 編集中を含む一覧を返すと、他者の未レビューの編集を管理対象として扱い、
// 次の適用でそれを公開してしまう (contracts/dpf-client.md)。
//
// 許可リスト外の種別は除外する。DPF 上に CAA や ANAME が存在すること自体は
// 正常であり、それらを上位層へ見せないことで、変更・削除の対象から外す (FR-027)。
func (c *Client) ListRecords(ctx context.Context, zone provider.Zone) ([]provider.Record, error) {
	var result []provider.Record

	err := c.observe(ctx, "list_records", func(ctx context.Context) error {
		return c.api.Operation(ctx, func() error {
			api := c.api.GetAPIClient()

			//nolint:bodyclose // dpf-go が Body を閉じたうえで返すため
			records, resp, err := api.RecordsAPI.GetRecordCurrents(ctx, zone.ID).ExecuteAll()
			if err != nil {
				return wrapAPIError(resp, err)
			}

			out := make([]provider.Record, 0, len(records.GetResults()))
			for _, r := range records.GetResults() {
				rec, ok := toProviderRecord(&r)
				if !ok {
					continue
				}
				out = append(out, rec)
			}

			result = out
			return nil
		})
	})
	if err != nil {
		return nil, Classify(fmt.Errorf("ゾーン %s のレコード取得に失敗: %w", zone.Name, err))
	}

	return result, nil
}

// recordLike は変換に必要な範囲だけを取り出したレコードの読み取り面。
//
// dpf.Record と dpf.OverwriteRecordsInner の双方を同じ処理で扱えるようにし、
// また変換のテストに生成型を要らなくする。
type recordLike interface {
	GetName() string
	GetTtl() int32
	GetRrtype() dpfapi.RecordsRrtype
	GetRdata() []dpfapi.RecordsRdataInner
}

// toProviderRecord は DPF のレコードをドメインの型へ変換する。
//
// ok が false の場合、そのレコードは管理対象外である。理由は種別が
// 許可リストにないか、名前を正規化名として解釈できないかのいずれか。
// どちらもエラーではなく除外として扱う。
func toProviderRecord(r recordLike) (provider.Record, bool) {
	rrtype, ok := fromDPF(r.GetRrtype())
	if !ok {
		return provider.Record{}, false
	}

	name, err := dnsname.Parse(r.GetName())
	if err != nil {
		return provider.Record{}, false
	}

	rdata := r.GetRdata()
	values := make([]string, 0, len(rdata))
	for _, d := range rdata {
		values = append(values, d.GetValue())
	}

	return provider.Record{
		Name:   name,
		Type:   rrtype,
		TTL:    int(r.GetTtl()),
		Values: values,
	}, true
}
