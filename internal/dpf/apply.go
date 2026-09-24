// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"
	"time"

	dpfapi "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// applyTimeout は 1 回の適用に許す時間。
//
// ロックの取得、反映済みレコードの読み直し、一括更新、反映の待ち合わせを
// すべて含む。ゾーン全件の送信と非同期ジョブの完了待ちがあるため、
// 単発の API 呼び出しより長く取る。
const applyTimeout = 10 * time.Minute

// Apply は変更セットを zone へ適用し、反映が完了するまで待つ。
//
// 手順は contracts/dpf-client.md に従う。
//
//  1. ゾーンロックの取得
//  2. 反映済みレコードの全件取得 (マージの土台をここで読み直す)
//  3. マージ (管理対象外は逐語コピー)
//  4. 投入前ガード
//  5. 一括更新とゾーン反映 (原子的に実行)
//  6. 反映完了の待ち合わせ
//
// **1・2・5・6 は [utils.ZoneApplier] が担う。** 本サービスが書くのは 3 と 4、
// すなわち「どのレコードを投入するか」だけである。ロックの取得と解放、保持中の
// 延長、SOA と apex NS を取り込まない指定、一括更新がロックを解くことへの対処は、
// いずれも間違えてもその場では成功して見える。dpf-go 側にまとめて任せる。
//
// 呼び出し側は変更セットを渡すだけでよい。ゾーンの現在の内容を読み直して
// マージする処理は、この境界の内側で完結する。
//
// 反映完了前に成功を返さない (FR-011)。途中で中止した場合も成功を返さない
// (FR-012)。更新と反映は原子的に行われるため、中止時に巻き戻す保留状態は
// そもそも発生しない。
//
// **他の操作と違い、[utils.Client.Operation] で包まない。** あれは単発の
// 呼び出しを対象とした再試行であり、ロックの取得から反映までを 1 つにまとめた
// この経路には合わない。途中の通信障害で再試行すると、ロックの取り直しと
// 読み直しからやり直すことになる。ここでの失敗は一時的な失敗として返し、
// ExternalDNS の次の周期に任せる。
func (c *Client) Apply(ctx context.Context, zone provider.Zone, cs provider.ChangeSet) error {
	if cs.IsEmpty() {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, applyTimeout)
	defer cancel()

	ctx, wait := newLockWait(ctx, lockWaitTimeout)
	defer wait.stop()

	api := c.api.GetAPIClient()
	applier := utils.NewZoneApplier(api.RecordsAPI, api.ZonesAPI, api.JobsAPI, zone.ID, lockOptions()...)

	// DPF が要求を拒んだとき、何を送ったのかが分からないと原因を追えない。
	// 編集の中でしか投入集合を見られないため、要約をここへ残す。
	// 投入集合が決まる前に失敗した場合は、そのことが分かる値のままになる。
	summary := "records=未確定"

	err := c.observe(ctx, "apply", func(ctx context.Context) error {
		edit := func(ctx context.Context, current []dpfapi.OverwriteRecordsInner) ([]dpfapi.OverwriteRecordsInner, error) {
			// ここへ来た時点でロックは取れている。待ち時間の上限を解除する。
			wait.enter()

			set, err := merge(current, cs)
			if err != nil {
				return nil, Classify(err)
			}

			// **ガードより前に置く。** 投入前ガードは投入する集合を検査する。
			// 印を付けた後の集合を検査しなければ、検査した集合と送る集合が違う。
			applyManagedBy(set, cs)

			if err := guard(current, set, cs); err != nil {
				return nil, err
			}

			summary = setSummary(set)
			return set, nil
		}

		return applier.Apply(ctx, edit, utils.WithApplyDescription(applyAttribution))
	})
	if err != nil {
		// ロック待ちの上限に達していた場合、Apply は context の打ち切りとして
		// 失敗する。原因が読める形に置き換える。
		if waitErr := wait.err(); waitErr != nil {
			err = waitErr
		}
		return Classify(fmt.Errorf("ゾーン %s の適用に失敗 (%s): %w", zone.Name, summary, err))
	}

	return nil
}

// applyAttribution は、ゾーン反映の実行者として記録する名前。
//
// DPF はゾーン反映の履歴に、要求へ添えた説明を保持する。運用者が履歴を開いた
// とき、本サービスによる反映と人手による反映を見分けられるようにする (004)。
//
// **固定値である。** 変更セットの内容、ゾーン、時刻のいずれにも依存させない。
// DPF の共通スキーマ Description は maxLength: 80 を宣言しており、固定値なら
// 収まることが定数として言える。可変長の内容を足すと、その保証が消える
// (004 research R4/R5)。
//
// 履歴には編集者 (operator) も残るが、これは DPF が資格情報から決める値であり、
// 区別できるのは「どの DPF アカウントか」までである。人が使うアカウントと
// 本サービスのアカウントが同じ場合に区別がつかない。どのソフトウェアが実行したかは、
// そのソフトウェア自身が名乗るしかない (004 research R3)。
const applyAttribution = "external-dns-iij-dpf-webhook"

// setSummary は投入集合を 1 行で要約する。
//
// 件数と先頭の 1 件だけを添える。全件を載せるとログが埋まる。
func setSummary(set []dpfapi.OverwriteRecordsInner) string {
	summary := fmt.Sprintf("records=%d", len(set))
	if len(set) == 0 {
		return summary
	}

	first := set[0]
	ttl := "null"
	if v := first.Ttl.Get(); v != nil {
		ttl = fmt.Sprintf("%d", *v)
	}
	return fmt.Sprintf("%s first=%q %v ttl=%s rdata=%d",
		summary, first.Name, first.Rrtype, ttl, len(first.Rdata))
}
