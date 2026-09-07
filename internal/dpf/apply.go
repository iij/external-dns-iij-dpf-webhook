// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// Apply は変更セットを zone へ適用する。
//
// 【未実装】本体は US2 (tasks.md T056) で実装する。US1 の時点では
// POST /records の経路自体が登録されていないため到達しない。
//
// 到達した場合は恒久的な失敗を返す。実装がない状態で成功を返すと、
// ExternalDNS は変更が反映されたと判断し、次の周回で差分を検出しなくなる。
// 適用されていないのに適用済みと見なされる方が、失敗として扱われるより危険である
// (原則 IV の fail closed)。
func (c *Client) Apply(_ context.Context, zone provider.Zone, _ provider.ChangeSet) error {
	return fmt.Errorf("%w: 変更の適用は未実装です (zone=%s)", provider.ErrPermanent, zone.Name)
}
