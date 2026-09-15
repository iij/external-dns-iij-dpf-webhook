// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"
	"time"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// ZoneHistory はゾーン反映の履歴 1 件。
//
// DPF の生成型を境界の外へ出さないため、必要な範囲だけを写した型である
// (原則 II)。編集者 (operator) は DPF が資格情報から決める値であり、
// 本サービスは設定しない。
type ZoneHistory struct {
	// ID は DPF が履歴を識別する値。
	ID int64

	// CommittedAt は反映が行われた時刻。ログとの突き合わせに使える。
	CommittedAt time.Time

	// Description は反映に添えられた説明。本サービスの反映には
	// [applyAttribution] が入る。
	Description string

	// Operator は DPF が記録した編集者。値がない場合は空文字列。
	Operator string
}

// ZoneHistories はゾーン反映の履歴を新しい順に返す。
//
// **本サービスの通常の動作経路では使わない。** 記録は運用者が読むためのもので
// あり、本サービスがこれを読み返して動作を変えることはない。読み返す設計にすると、
// DPF 側の保持期間や取得の失敗が DNS の更新を止める経路になる
// (004 contracts、plan.md 設計上の注意点)。
//
// 用途は受け入れ確認である。記録が実際に履歴へ現れることを、人の目視ではなく
// 機械的な表明として書けるようにする (004 research R2)。人手の確認に頼る
// 受け入れ条件は CI で守れず、守れない条件はいずれ守られなくなる。
//
// 取得件数は historyLimit 件までとする。検証が見るのは直近の数件であり、
// 全件をたどる理由がない。
func (c *Client) ZoneHistories(ctx context.Context, zone provider.Zone) ([]ZoneHistory, error) {
	var result []ZoneHistory

	err := c.observe(ctx, "list_zone_histories", func(ctx context.Context) error {
		return c.api.Operation(ctx, func() error {
			api := c.api.GetAPIClient()

			//nolint:bodyclose // dpf-go が Body を閉じたうえで返すため
			histories, resp, err := api.ZoneHistoriesAPI.
				GetZoneHistoryList(ctx, zone.ID).
				Limit(historyLimit).
				Execute()
			if err != nil {
				return wrapAPIError(resp, err)
			}

			out := make([]ZoneHistory, 0, len(histories.GetResults()))
			for _, h := range histories.GetResults() {
				out = append(out, ZoneHistory{
					ID:          h.GetId(),
					CommittedAt: h.GetCommittedAt(),
					Description: h.GetDescription(),
					Operator:    h.GetOperator(),
				})
			}

			result = out
			return nil
		})
	})
	if err != nil {
		return nil, Classify(fmt.Errorf("ゾーン %s の反映履歴の取得に失敗: %w", zone.Name, err))
	}

	return result, nil
}

// historyLimit は 1 回の取得で得る履歴の件数。
//
// 検証が見るのは直近の数件である。DPF の既定は新しい順であり、先頭から数件で足りる。
const historyLimit = 20
