// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"
	"time"

	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// ロックの待ち方。
//
// lockTTL を過ぎたロックは他者が奪える。適用が異常終了してロックが残っても、
// この時間で自動的に解放される。短すぎると正常な適用の途中で奪われ、
// 長すぎると障害後の復旧が遅れる。
const (
	lockTTL          = 5 * time.Minute
	lockPollInterval = 500 * time.Millisecond
	lockWaitTimeout  = 30 * time.Second
)

// withZoneLock は zone のロックを取得し、fn を実行してから解放する。
//
// ロックが守るのは「マージの土台となる読み取り」と「書き込み」の間だけである。
// この 2 つの間に他者がゾーンを変更すると、その変更が古い読み取り結果で
// 上書きされて失われる (lost update)。
//
// ロックの範囲はこの関数の内側に閉じる。webhook のレコード取得と適用は
// 独立した HTTP 要求であり、取得の後に適用が来る保証がない。両者をまたいで
// ロックを保持すると、適用が来ないままロックが残留してゾーンが操作不能になる
// (research R4)。
//
// ロックは fn の成否によらず解放する。
func (c *Client) withZoneLock(ctx context.Context, zone provider.Zone, fn func() error) error {
	api := c.api.GetAPIClient()
	mu := utils.NewMutex(api.RecordsAPI, zone.ID, utils.WithTTL(lockTTL))

	lockCtx, cancel := context.WithTimeout(ctx, lockWaitTimeout)
	defer cancel()

	if err := mu.LockWait(lockCtx, lockPollInterval); err != nil {
		return Classify(fmt.Errorf("ゾーン %s のロック取得に失敗: %w", zone.Name, err))
	}

	// 解放は必ず行う。失敗しても TTL で回収されるため、本来の結果を
	// 覆い隠さないようにログ相当の情報だけを添える。
	defer func() {
		// 解放用の文脈は打ち切られていないものを使う。ctx がすでに完了している
		// 場合、そのまま渡すと解放要求自体が送れない。
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), lockWaitTimeout)
		defer releaseCancel()
		//nolint:errcheck,gosec // 解放失敗は TTL で回収される。本来の結果を優先する
		mu.Unlock(releaseCtx)
	}()

	return fn()
}
