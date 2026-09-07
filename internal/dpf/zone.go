package dpf

import (
	"context"
	"fmt"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// ListZones は操作できるゾーンを全件返す。
//
// ページングは ExecuteAll に委ね、この境界の内側で完結させる。ゾーン名は
// 取得時点で正規化し、以降は正規化名として扱う (research R7)。
//
// 名前を解釈できないゾーンは黙って落とす。DPF 上に本サービスが扱えない
// 名前のゾーンが存在すること自体は正常であり、それを理由に一覧全体を
// 失敗させると、無関係なゾーンのせいで DNS が更新されなくなる。
func (c *Client) ListZones(ctx context.Context) ([]provider.Zone, error) {
	api := c.api.GetAPIClient()

	var result []provider.Zone
	err := c.api.Operation(ctx, func() error {
		// dpf-go は応答ボディを内部で閉じたうえで *http.Response を返す。
		// ここで閉じ直す先はすでにない。
		//nolint:bodyclose // dpf-go が Body を閉じたうえで返すため
		zones, resp, err := api.ZonesAPI.GetZoneList(ctx).ExecuteAll()
		if err != nil {
			return wrapAPIError(resp, err)
		}

		out := make([]provider.Zone, 0, len(zones.GetResults()))
		for _, z := range zones.GetResults() {
			name, parseErr := dnsname.Parse(z.GetName())
			if parseErr != nil {
				continue
			}
			out = append(out, provider.Zone{Name: name, ID: z.GetId()})
		}

		result = out
		return nil
	})
	if err != nil {
		return nil, Classify(fmt.Errorf("ゾーン一覧の取得に失敗: %w", err))
	}

	return result, nil
}
